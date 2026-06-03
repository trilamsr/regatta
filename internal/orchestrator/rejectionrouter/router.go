// Package rejectionrouter wakes agents on rejecting AI-gate verdicts
// and escalates to a human after K rejections.
//
// Wiring per docs/design.md §Orchestrator shape, item 5
// (RejectionRouter) and §Failure modes ("AI reviewer rejects K=3
// times → label `needs-human`"):
//
//  1. Tick scans the events table for `gate_rejected` rows newer than
//     the cursor.
//  2. For each event whose payload `pr_sha` matches the agent's
//     current PRSHA AND whose agent is in `gates_running`, the agent
//     is transitioned to `gates_failed` with rejection_count++ (one
//     atomic TransitionAgent call via AgentMutation.IncrementRejection).
//  3. When rejection_count reaches K (default 3) the agent is
//     transitioned `gates_failed → escalated`, its locks are released
//     so Orchestrator.Heartbeat stops touching it, an `escalated`
//     event is appended, and the configured PRLabeler is invoked to
//     label the PR `needs-human`.
//
// Stale rejections (pr_sha mismatch — the agent has pushed a new
// commit since the gate ran) are dropped: the design anchor is
// "rejecting GateResult newer than the agent's last commit", so older
// shas do not count.
//
// Out of scope: the wake-and-retry transition (gates_failed → running)
// is owned by the agent-respawn path, not this router. This router
// only translates rejection events into counter+state writes and
// triggers escalation when the counter crosses K.
//
// The cursor is in-memory. On restart it begins at 0 and the
// idempotency guards above ensure the only effect of replay is a
// possibly-redundant labeler call — the counter is gated by state
// (`gates_running`) so replay cannot double-increment a settled agent.
package rejectionrouter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// PRLabeler attaches a label to the PR that backs an agent. The
// concrete implementation (GHLabeler) resolves agentID → branch → PR
// number → `gh pr edit --add-label`; tests use a fake. The interface
// takes agentID rather than work_item_id because work_item_id is
// brief-derived (e.g. "F-1") and bears no relation to any GitHub PR
// number — see issue #476 for the failure mode the prior shape caused.
// nil-safe: New() substitutes a no-op so callers without a PR surface
// (in-process tests, simulators) still work.
type PRLabeler interface {
	AddLabel(ctx context.Context, agentID int64, label string) error
}

// Config holds the deps + tunables for a Router. DB is required;
// everything else has a sensible default.
type Config struct {
	// DB is the orchestrator state store. Required.
	DB *state.DB

	// K is the rejection cap that triggers escalation. <=0 defaults to
	// 3 per docs/design.md §Failure modes.
	K int

	// Labeler is invoked when an agent escalates. Nil falls back to a
	// no-op so callers that have not wired the PR surface still get
	// the state transition + audit event.
	Labeler PRLabeler

	// EscalationLabel is the label applied on escalation. Empty
	// defaults to "needs-human" per docs/design.md.
	EscalationLabel string

	// BatchLimit caps the number of events processed per Tick. <=0
	// defaults to 256 so a backlog cannot starve other tickers.
	BatchLimit int

	// Logger is the structured-event sink. Nil falls back to
	// slog.Default().
	Logger *slog.Logger
}

const (
	// EventKindGateRejected is the events.kind value written by gate
	// runners when a verdict comes back rejecting. The router watches
	// for this kind exclusively.
	EventKindGateRejected = "gate_rejected"

	// EventKindEscalated is appended when the router escalates an
	// agent past K rejections.
	EventKindEscalated = "escalated"

	// EventKindLabeled is appended once the PRLabeler.AddLabel call
	// for an escalation succeeds. Its existence is the idempotency
	// marker the labeler-retry sweep checks: an agent in `escalated`
	// state with no `labeled` event still needs its PR labeled.
	EventKindLabeled = "labeled"

	defaultK             = 3
	defaultBatchLimit    = 256
	defaultEscalationLbl = "needs-human"
)

// Router wakes + escalates agents on AI-gate rejections.
type Router struct {
	cfg    Config
	cursor int64
	log    *slog.Logger

	// txHook fires inside the escalation tx between the gates_failed
	// write and the escalated/release-locks/event-record writes. Tests
	// pin the atomic-rollback contract via export_test.SetTxHook (see
	// issue #477); production paths leave it nil.
	txHook func(*sql.Tx) error
}

// New constructs a Router. The cursor starts at 0; first Tick scans
// every gate_rejected row from the beginning of the events table —
// agent-state guards make replay idempotent.
func New(cfg Config) *Router {
	if cfg.K <= 0 {
		cfg.K = defaultK
	}
	if cfg.BatchLimit <= 0 {
		cfg.BatchLimit = defaultBatchLimit
	}
	if cfg.EscalationLabel == "" {
		cfg.EscalationLabel = defaultEscalationLbl
	}
	if cfg.Labeler == nil {
		cfg.Labeler = noopLabeler{}
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Router{cfg: cfg, log: log}
}

// gateRejectedPayload is the JSON shape this router reads from the
// events table. Only pr_sha is load-bearing — the rest is audit
// context the gate runner already wrote.
type gateRejectedPayload struct {
	PRSHA string `json:"pr_sha"`
}

// Tick processes the next batch of gate_rejected events newer than
// the cursor, then sweeps any agent left in `escalated` state without
// a corresponding `labeled` event so a prior labeler failure is
// retried. Per-event state errors propagate; the cursor advances only
// past events that were applied (or correctly skipped) so the next
// Tick retries the failing event.
//
// State transitions are durable before the labeler call so the
// counter cannot regress on retry. The labeler is idempotent in
// practice (gh's `--add-label` is a no-op when the label is already
// attached); the `labeled` audit row is the marker that lets the
// sweep stop after success.
func (r *Router) Tick(ctx context.Context) error {
	events, err := r.cfg.DB.ListEventsByKindSince(ctx, EventKindGateRejected, r.cursor, r.cfg.BatchLimit)
	if err != nil {
		return fmt.Errorf("rejectionrouter: list events: %w", err)
	}
	var firstErr error
	for _, ev := range events {
		if err := r.handle(ctx, ev); err != nil {
			// Cursor stays put — next Tick retries this event.
			if firstErr == nil {
				firstErr = err
			}
			break
		}
		r.cursor = ev.ID
	}
	if err := r.sweepUnlabeled(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// sweepUnlabeled retries the labeler for every escalated agent that
// has no `labeled` audit row yet. Bounded by the same BatchLimit so a
// pathological backlog cannot stretch a single Tick unboundedly.
func (r *Router) sweepUnlabeled(ctx context.Context) error {
	escalated, err := r.cfg.DB.ListAgentsByState(ctx, state.AgentEscalated)
	if err != nil {
		return fmt.Errorf("rejectionrouter: list escalated: %w", err)
	}
	for _, a := range escalated {
		labeled, err := r.cfg.DB.ListAgentEventsByKind(ctx, a.ID, EventKindLabeled)
		if err != nil {
			return fmt.Errorf("rejectionrouter: list labeled events: %w", err)
		}
		if len(labeled) > 0 {
			continue
		}
		if err := r.cfg.Labeler.AddLabel(ctx, a.ID, r.cfg.EscalationLabel); err != nil {
			return fmt.Errorf("rejectionrouter: retry label for agent %d (work_item %q): %w", a.ID, a.WorkItemID, err)
		}
		_ = r.cfg.DB.RecordEvent(ctx, a.ID, EventKindLabeled,
			fmt.Sprintf(`{"label":%q}`, r.cfg.EscalationLabel))
		r.log.Info("rejectionrouter.labeled",
			"agent_id", a.ID,
			"work_item_id", a.WorkItemID,
			"label", r.cfg.EscalationLabel,
		)
	}
	return nil
}

// handle applies the per-event side-effects: idempotency guard +
// increment+transition + optional escalation. Returns nil for events
// the router correctly drops (stale sha, agent not in gates_running,
// missing agent_id).
func (r *Router) handle(ctx context.Context, ev state.Event) error {
	if !ev.AgentID.Valid {
		// Adapter-level event, not tied to an agent — nothing to
		// route. Advance the cursor.
		return nil
	}
	agentID := ev.AgentID.Int64

	var payload gateRejectedPayload
	if ev.PayloadJSON != "" {
		if err := json.Unmarshal([]byte(ev.PayloadJSON), &payload); err != nil {
			// Malformed payload — log and skip rather than wedge the
			// daemon on a single bad row.
			r.log.Warn("rejectionrouter.payload_unmarshal",
				"event_id", ev.ID, "err", err.Error())
			return nil
		}
	}

	agent, err := r.cfg.DB.GetAgent(ctx, agentID)
	if err != nil {
		// Agent row vanished (compaction / FK cascade in some future
		// migration) — log + skip; the event is durable, the agent is
		// not.
		r.log.Warn("rejectionrouter.agent_missing",
			"event_id", ev.ID, "agent_id", agentID, "err", err.Error())
		return nil
	}

	if agent.State != state.AgentGatesRunning {
		// Either already routed (gates_failed/escalated), already
		// withdrawn, or rejection landed pre-gate. Drop.
		return nil
	}
	if payload.PRSHA == "" || agent.PRSHA == "" || payload.PRSHA != agent.PRSHA {
		// Stale rejection per design.md ("newer than the agent's last
		// commit"). The agent has pushed a new sha since the gate ran;
		// the next sha's gate run will produce a fresh event.
		return nil
	}

	// One tx covers the increment-and-transition AND, when the
	// counter crosses K, the escalated transition + lock release +
	// audit append. Issue #477: pre-fix the second transition ran in
	// its own tx, so a sqlite-busy on the second write left the agent
	// stranded at gates_failed with rejection_count=K — the cursor
	// then advanced past the event because the next Tick's state
	// check dropped the now-non-gates_running row. Atomic commit
	// means a mid-tx fault rolls back the counter too, and the next
	// Tick retries the same event from scratch.
	var (
		updated   *state.Agent
		escalated *state.Agent
	)
	txErr := r.cfg.DB.WithTx(ctx, func(tx *sql.Tx) error {
		a, err := r.cfg.DB.TransitionAgentTx(ctx, tx, agentID, state.AgentGatesFailed,
			state.AgentMutation{IncrementRejection: true})
		if err != nil {
			return err
		}
		updated = a
		if a.RejectionCount < r.cfg.K {
			return nil
		}
		// Test seam — surfaces sqlite-busy / mid-tx errors that would
		// otherwise be impossible to reproduce deterministically.
		if r.txHook != nil {
			if err := r.txHook(tx); err != nil {
				return err
			}
		}
		esc, err := r.cfg.DB.TransitionAgentTx(ctx, tx, agentID, state.AgentEscalated, state.AgentMutation{})
		if err != nil {
			return err
		}
		escalated = esc
		if _, err := r.cfg.DB.ReleaseAgentLocksTx(ctx, tx, agentID); err != nil {
			return err
		}
		// Audit row appended inside the same tx so it is observable
		// iff the escalation committed. The labeler call lives
		// outside the tx — sweepUnlabeled retries it on failure.
		return r.cfg.DB.RecordEventTx(ctx, tx, agentID, EventKindEscalated,
			fmt.Sprintf(`{"rejection_count":%d,"k":%d}`, esc.RejectionCount, r.cfg.K))
	})
	if txErr != nil {
		if errors.Is(txErr, state.ErrInvalidTransition) {
			// Race: agent moved out of gates_running between our read
			// and the transition. Drop the event — the new state owns
			// the verdict.
			return nil
		}
		return fmt.Errorf("rejectionrouter: escalation tx: %w", txErr)
	}

	r.log.Info("rejectionrouter.gates_failed",
		"agent_id", agentID,
		"work_item_id", updated.WorkItemID,
		"rejection_count", updated.RejectionCount,
		"k", r.cfg.K,
	)
	if escalated == nil {
		return nil
	}
	r.log.Info("rejectionrouter.escalated",
		"agent_id", agentID,
		"work_item_id", escalated.WorkItemID,
		"rejection_count", escalated.RejectionCount,
	)
	return nil
}

type noopLabeler struct{}

func (noopLabeler) AddLabel(context.Context, int64, string) error { return nil }
