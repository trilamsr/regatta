package merge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Coordinator owns the recovery sweep for agents stranded in
// AwaitingMerge. It is intentionally narrow — one method, Reconcile —
// because the policy engine that DECIDES when to merge (W2 c2) lives
// elsewhere; this package only guards the external side-effect.
//
// Concurrency model: Reconcile is called from orchestrator.Recover
// (startup, single-shot) and may also be wired into the periodic
// scheduler tick for long-running re-probe of agents whose first
// probe returned PRStatusUnknown (gh rate limit). The DB layer
// serializes writers via SetMaxOpenConns(1); no internal locking
// needed.
type Coordinator struct {
	db       *state.DB
	prober   PRProber
	log      *slog.Logger
	executor Executor
}

// Config wires the Coordinator's deps. DB + Prober are required; Log
// falls back to slog.Default() for embedded callers.
type Config struct {
	DB     *state.DB
	Prober PRProber
	Logger *slog.Logger
}

// New constructs a Coordinator. Required deps return an error rather
// than panicking so wiring mistakes surface at boot, not at the first
// crash recovery.
func New(cfg Config) (*Coordinator, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("merge: New: DB required")
	}
	if cfg.Prober == nil {
		return nil, fmt.Errorf("merge: New: Prober required")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Coordinator{db: cfg.DB, prober: cfg.Prober, log: log}, nil
}

// PrepareMerge atomically writes the merge_intent audit row + transitions
// the agent from GatesRunning → AwaitingMerge in a single transaction.
// The c2 auto-merge policy engine calls this immediately before the
// external `gh pr merge` invocation so a crash between the policy
// decision and the external call leaves a recoverable intent row and a
// state Recover() + Coordinator.Reconcile can re-probe.
//
// Single tx guarantees: either both writes commit (intent on file,
// state == awaiting_merge) or neither does (no orphan intent, no half-
// transitioned agent). Validation runs PRE-tx so a buggy caller cannot
// burn DB cycles on doomed inputs.
//
// The external `gh pr merge` call is NOT made here — c2 wires that
// after PrepareMerge succeeds. The split exists so the substrate side
// (this method) stays gh-CLI-free and the external side can fail
// independently without corrupting the audit row.
func (c *Coordinator) PrepareMerge(ctx context.Context, agentID int64, prNumber int, headSHA string) error {
	if prNumber <= 0 {
		return fmt.Errorf("merge: PrepareMerge: pr_number must be positive, got %d", prNumber)
	}
	if headSHA == "" {
		return fmt.Errorf("merge: PrepareMerge: head_sha required")
	}
	return c.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := WriteIntent(ctx, tx, c.db, agentID, prNumber, headSHA); err != nil {
			return err
		}
		if _, err := c.db.TransitionAgentTx(ctx, tx, agentID, state.AgentAwaitingMerge, state.AgentMutation{}); err != nil {
			return err
		}
		return nil
	})
}

// Reconcile walks every agent in AwaitingMerge, looks up the latest
// merge_intent, and probes GitHub for the real PR state. Outcomes:
//
//   - PRStatusMerged → write merge_completed (source=recovery),
//     transition AwaitingMerge → Done.
//   - PRStatusOpenSHAMatches → leave the agent in AwaitingMerge so the
//     next normal-path tick can re-issue the merge. Reconcile does NOT
//     retry the merge itself — the policy engine owns that call.
//   - PRStatusOpenSHADiverged → write merge_failed
//     (reason=sha_diverged), transition AwaitingMerge → Crashed so the
//     crashed→pending requeue path re-spawns the work_item against
//     fresh code.
//   - PRStatusClosedUnmerged → write merge_failed
//     (reason=closed_unmerged), transition AwaitingMerge → Crashed.
//   - PRStatusUnknown → leave the agent in AwaitingMerge; next sweep
//     retries. Surfaced for transient gh-API failures.
//
// Errors from the prober are non-fatal — Reconcile logs and continues
// to the next agent so one flaky PR does not halt recovery.
func (c *Coordinator) Reconcile(ctx context.Context) error {
	agents, err := c.db.ListAgentsByState(ctx, state.AgentAwaitingMerge)
	if err != nil {
		return fmt.Errorf("merge: reconcile: list awaiting_merge: %w", err)
	}
	for _, a := range agents {
		if err := c.reconcileOne(ctx, a); err != nil {
			// Per-agent failures stay non-fatal so a single bad PR
			// (deleted upstream, prober bug) does not strand the
			// rest of the awaiting_merge set.
			c.log.Warn("merge.reconcile_agent_failed",
				"agent_id", a.ID,
				"work_item_id", a.WorkItemID,
				"err", err.Error(),
			)
		}
	}
	return nil
}

// reconcileOne handles a single agent. Split out so Reconcile can keep
// a tight error-handling loop without nesting.
func (c *Coordinator) reconcileOne(ctx context.Context, a state.Agent) error {
	intent, err := LatestIntent(ctx, c.db, a.ID)
	if err != nil {
		// No intent on file = substrate never authorized the merge
		// (logic bug or pre-c0 leftover). Transition to crashed so
		// the requeue path applies — leaving the agent in
		// awaiting_merge forever would silently leak the lane slot.
		if errors.Is(err, ErrNoIntent) {
			return c.markFailed(ctx, a, 0, "", "no_intent_on_file")
		}
		return fmt.Errorf("load intent: %w", err)
	}
	result, err := c.prober.Probe(ctx, intent.PRNumber, intent.HeadSHA)
	if err != nil {
		// Probe errors are transient by default — leave in
		// awaiting_merge for the next sweep. The W4 detector watches
		// for the same agent_id appearing in three consecutive
		// sweeps as a self-improvement trigger.
		return fmt.Errorf("probe pr=%d sha=%s: %w", intent.PRNumber, intent.HeadSHA, err)
	}
	switch result.Status {
	case PRStatusMerged:
		return c.markCompleted(ctx, a, intent.PRNumber, intent.HeadSHA, result.MergeSHA)
	case PRStatusOpenSHAMatches:
		c.log.Info("merge.reconcile_open_sha_matches",
			"agent_id", a.ID,
			"pr_number", intent.PRNumber,
			"head_sha", intent.HeadSHA,
		)
		return nil
	case PRStatusOpenSHADiverged:
		return c.markFailed(ctx, a, intent.PRNumber, intent.HeadSHA, "sha_diverged")
	case PRStatusClosedUnmerged:
		return c.markFailed(ctx, a, intent.PRNumber, intent.HeadSHA, "closed_unmerged")
	case PRStatusUnknown:
		c.log.Info("merge.reconcile_unknown_retry_next_sweep",
			"agent_id", a.ID,
			"pr_number", intent.PRNumber,
		)
		return nil
	default:
		return fmt.Errorf("unknown PRStatus %d", result.Status)
	}
}

// markCompleted writes the completion event + transitions to Done.
// Event-before-state ordering matches the rest of regatta's audit
// discipline: a crash between the two leaves the agent recoverable
// (next sweep sees Done + completed-event = no-op).
func (c *Coordinator) markCompleted(ctx context.Context, a state.Agent, prNumber int, headSHA, mergeSHA string) error {
	payload, err := json.Marshal(CompletedPayload{
		PRNumber: prNumber,
		HeadSHA:  headSHA,
		MergeSHA: mergeSHA,
		Source:   "recovery",
	})
	if err != nil {
		return fmt.Errorf("marshal completed: %w", err)
	}
	if err := c.recordMergeEvent(ctx, a, EventKindMergeCompleted, string(payload)); err != nil {
		return fmt.Errorf("record completed: %w", err)
	}
	if err := c.recordMergeEvent(ctx, a, EventKindMergeRecovered, `{"outcome":"completed"}`); err != nil {
		return fmt.Errorf("record recovered: %w", err)
	}
	if _, err := c.db.TransitionAgent(ctx, a.ID, state.AgentDone, state.AgentMutation{}); err != nil {
		// Already-Done from a concurrent winning instance is fine —
		// the unique-event-index already guarded against double-write;
		// the transition is the FSM-layer mirror. ErrInvalidTransition
		// here is the "agent is already terminal" race.
		if !errors.Is(err, state.ErrInvalidTransition) {
			return fmt.Errorf("transition done: %w", err)
		}
	}
	c.log.Info("merge.recovered_completed",
		"agent_id", a.ID,
		"work_item_id", a.WorkItemID,
		"pr_number", prNumber,
		"head_sha", headSHA,
		"merge_sha", mergeSHA,
	)
	return nil
}

// markFailed writes the failure event, releases agent locks, and
// transitions Awaiting→Crashed so the existing crashed→pending
// requeue path takes over. The work_item re-enters the spawnable set
// on the next PollOnce.
func (c *Coordinator) markFailed(ctx context.Context, a state.Agent, prNumber int, headSHA, reason string) error {
	payload, err := json.Marshal(FailedPayload{
		PRNumber: prNumber,
		HeadSHA:  headSHA,
		Reason:   reason,
		Source:   "recovery",
	})
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}
	if err := c.recordMergeEvent(ctx, a, EventKindMergeFailed, string(payload)); err != nil {
		return fmt.Errorf("record failed: %w", err)
	}
	if err := c.recordMergeEvent(ctx, a, EventKindMergeRecovered, fmt.Sprintf(`{"outcome":"failed","reason":%q}`, reason)); err != nil {
		return fmt.Errorf("record recovered: %w", err)
	}
	if _, err := c.db.TransitionAgent(ctx, a.ID, state.AgentCrashed, state.AgentMutation{}); err != nil {
		// Already-Crashed/Done from a concurrent winning instance is
		// fine — the unique-event-index already guarded against
		// double-write; the FSM transition is the mirror.
		if !errors.Is(err, state.ErrInvalidTransition) {
			return fmt.Errorf("transition crashed: %w", err)
		}
	}
	if _, err := c.db.ReleaseAgentLocks(ctx, a.ID); err != nil {
		return fmt.Errorf("release locks: %w", err)
	}
	c.log.Warn("merge.recovered_failed",
		"agent_id", a.ID,
		"work_item_id", a.WorkItemID,
		"pr_number", prNumber,
		"head_sha", headSHA,
		"reason", reason,
	)
	return nil
}

// recordMergeEvent wraps state.RecordEvent with unique-violation
// suppression. Two coordinator instances may race the same agent into
// markCompleted/markFailed; the partial unique index on
// events(agent_id, kind) (migration 0013) makes the second writer's
// INSERT fail with a UNIQUE constraint violation. We swallow that as
// "the other instance already recorded this outcome" and continue —
// without the suppression the loser's per-agent reconcile errors,
// gets logged as a sweep failure, and a future W4 self-improvement
// detector mistakes routine multi-instance recovery for a real bug.
func (c *Coordinator) recordMergeEvent(ctx context.Context, a state.Agent, kind, payloadJSON string) error {
	err := c.db.RecordEvent(ctx, a.ID, kind, payloadJSON)
	if err == nil {
		return nil
	}
	if isUniqueViolation(err) {
		c.log.Info("merge.duplicate_event_suppressed",
			"agent_id", a.ID,
			"work_item_id", a.WorkItemID,
			"kind", kind,
		)
		return nil
	}
	return err
}

// isUniqueViolation matches modernc.org/sqlite's UNIQUE-constraint
// error text. Same substring-probe shape substrate uses for its replay
// guard — see internal/orchestrator/state/substrate/event.go's
// isUniqueNonceViolation. The driver does not surface extended error
// codes through database/sql so the substring is the stable seam.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
