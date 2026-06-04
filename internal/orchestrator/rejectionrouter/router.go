// Package rejectionrouter wakes agents on rejecting AI-gate verdicts
// and escalates to a human after K rejections.
//
// Per docs/design.md §Orchestrator item 5 + §Failure modes: each Tick
// scans `gate_rejected` rows newer than the cursor, transitions the
// matching `gates_running` agent to `gates_failed` with one atomic
// IncrementRejection, and at rejection_count==K transitions to
// `escalated`, releases locks, appends an `escalated` event, and
// invokes PRLabeler with `needs-human`. Stale rejections (pr_sha
// mismatch) are dropped — only rejections newer than the agent's last
// commit count. The respawn path (gates_failed → running) lives
// elsewhere.
//
// The cursor is in-memory: restart begins at 0. Replay is safe because
// the gates_running state guard prevents double-increment; the only
// possible duplicate effect is a redundant labeler call, which is
// idempotent at the GitHub layer.
package rejectionrouter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// PRLabeler attaches a label to the PR backing an agent. Takes agentID
// (not work_item_id) because work_item_id is brief-derived (e.g. "F-1")
// and bears no relation to any GitHub PR number — see #476. New()
// substitutes a no-op when nil so in-process callers still work.
type PRLabeler interface {
	AddLabel(ctx context.Context, agentID int64, label string) error
}

// Config holds Router deps + tunables; DB is required, everything else
// has a default applied in New.
type Config struct {
	DB *state.DB

	// K defaults to 3 (docs/design.md §Failure modes) when <=0.
	K int

	// Labeler nil ⇒ no-op fallback so callers without a PR surface
	// still get the state transition + audit event.
	Labeler PRLabeler

	// EscalationLabel empty ⇒ "needs-human" per docs/design.md.
	EscalationLabel string

	// BatchLimit caps events per Tick so a backlog cannot starve other
	// tickers. <=0 ⇒ 256.
	BatchLimit int

	Logger *slog.Logger

	// Meter nil ⇒ otel.Meter(scopeName) at Router.New so the global
	// MeterProvider Setup wins by default. Mirrors the W6 Config.Tracer
	// + Config.Meter DI seam used across cost/spend + gates/l4.
	//
	// Router.New wraps the configured Labeler with a metric-emitting
	// decorator so every PRLabeler impl surfaces LabelFailureMetricName
	// without threading a meter into its own constructor (#499).
	Meter metric.Meter
}

// Event kinds the router reads (gate_rejected) and appends
// (escalated, labeled).
const (
	EventKindGateRejected = "gate_rejected"
	EventKindEscalated    = "escalated"

	// EventKindLabeled marks idempotency for the labeler-retry sweep:
	// an `escalated` agent with no `labeled` event still needs labeling.
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

	// txHook fires inside the escalation tx between gates_failed write
	// and the escalated/release-locks/event-record writes. Tests pin
	// the atomic-rollback contract via export_test.SetTxHook (#477);
	// production paths leave it nil.
	txHook func(*sql.Tx) error
}

// New constructs a Router with the cursor at 0 — first Tick scans
// every gate_rejected row; agent-state guards make replay idempotent.
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
	// Wrap at the Router boundary (not per-impl) so Config.Meter is the
	// single authoritative DI knob for telemetry routing — fakes,
	// GHLabeler, and noopLabeler share the same counter shape.
	cfg.Labeler = &metricLabeler{
		inner:       cfg.Labeler,
		instruments: newInstruments(cfg.resolveMeter()),
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Router{cfg: cfg, log: log}
}

// gateRejectedPayload is the JSON shape read from the events table.
// Only pr_sha is load-bearing — the rest is audit context.
type gateRejectedPayload struct {
	PRSHA string `json:"pr_sha"`
}

// Tick processes the next batch of gate_rejected events past the
// cursor, then sweeps escalated agents missing a `labeled` event so a
// prior labeler failure is retried. State transitions are durable
// before the labeler call so the counter cannot regress on retry; the
// `labeled` audit row is the marker that lets the sweep terminate.
// Per-event errors stall the cursor so the next Tick retries.
func (r *Router) Tick(ctx context.Context) error {
	events, err := r.cfg.DB.ListEventsByKindSince(ctx, EventKindGateRejected, r.cursor, r.cfg.BatchLimit)
	if err != nil {
		return fmt.Errorf("rejectionrouter: list events: %w", err)
	}
	var firstErr error
	for _, ev := range events {
		if err := r.handle(ctx, ev); err != nil {
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

// sweepUnlabeled retries the labeler for every escalated agent without
// a `labeled` audit row. ListEscalatedUnlabeled's SQL-level NOT EXISTS
// collapses the prior N+1 round-trip into one index-driven scan and
// caps labeler calls per Tick at BatchLimit regardless of escalated
// backlog (#478).
func (r *Router) sweepUnlabeled(ctx context.Context) error {
	escalated, err := r.cfg.DB.ListEscalatedUnlabeled(ctx, r.cfg.BatchLimit)
	if err != nil {
		return fmt.Errorf("rejectionrouter: list escalated unlabeled: %w", err)
	}
	for _, a := range escalated {
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

// handle applies per-event side-effects. Returns nil for events the
// router correctly drops (stale sha, agent not in gates_running,
// missing agent_id) so the cursor can advance.
func (r *Router) handle(ctx context.Context, ev state.Event) error {
	if !ev.AgentID.Valid {
		return nil
	}
	agentID := ev.AgentID.Int64

	var payload gateRejectedPayload
	if ev.PayloadJSON != "" {
		if err := json.Unmarshal([]byte(ev.PayloadJSON), &payload); err != nil {
			// Skip rather than wedge the daemon on a single bad row.
			r.log.Warn("rejectionrouter.payload_unmarshal",
				"event_id", ev.ID, "err", err.Error())
			return nil
		}
	}

	agent, err := r.cfg.DB.GetAgent(ctx, agentID)
	if err != nil {
		// Agent row vanished (FK cascade in some future migration) —
		// the event is durable, the agent is not.
		r.log.Warn("rejectionrouter.agent_missing",
			"event_id", ev.ID, "agent_id", agentID, "err", err.Error())
		return nil
	}

	if agent.State != state.AgentGatesRunning {
		return nil
	}
	if payload.PRSHA == "" || agent.PRSHA == "" || payload.PRSHA != agent.PRSHA {
		// Stale rejection per design.md ("newer than the agent's last
		// commit"); the next sha's gate run produces a fresh event.
		return nil
	}

	// One tx covers increment-and-transition AND, when the counter
	// crosses K, the escalated transition + lock release + audit
	// append. #477: pre-fix the second transition ran in its own tx,
	// so a sqlite-busy on the second write stranded the agent at
	// gates_failed with rejection_count=K while the cursor advanced
	// past the event. Atomic commit rolls back the counter on fault.
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
		// Test seam — surfaces sqlite-busy / mid-tx errors otherwise
		// impossible to reproduce deterministically.
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
		// Audit row inside the tx so it is observable iff escalation
		// committed; the labeler call lives outside — sweepUnlabeled
		// retries on failure.
		return r.cfg.DB.RecordEventTx(ctx, tx, agentID, EventKindEscalated,
			fmt.Sprintf(`{"rejection_count":%d,"k":%d}`, esc.RejectionCount, r.cfg.K))
	})
	if txErr != nil {
		if errors.Is(txErr, state.ErrInvalidTransition) {
			// Race: agent moved out of gates_running between our read
			// and the transition — new state owns the verdict.
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
