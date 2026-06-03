package orchestrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Recover implements the crash-recovery contract in docs/design.md
// §State, persistence, recovery. It MUST be called once on startup
// before Run, PollOnce, or ScheduleOnce.
//
// The contract:
//
//  1. Every non-terminal agent whose PID is no longer alive AND whose
//     state belongs to the pid-bound recovery set
//     ({spawning, running, pr_open, gates_running}) is transitioned
//     to crashed, then re-queued by re-inserting a pending row for
//     the same work_item_id on the next PollOnce.
//  2. Every agent in AwaitingMerge is reconciled against real GitHub
//     PR state by the merge.Coordinator (PHASE AUTONOMY §11 W2 c0).
//     AwaitingMerge agents have no live worktree PID by design — the
//     pid-alive probe does not apply. Without a Coordinator wired
//     (SetMergeCoordinator never called) the agent stays enumerated
//     so its lock is heartbeatable but no re-probe runs; production
//     wiring installs the Coordinator once auto-merge ships.
//  3. Stale locks (heartbeat older than 2× LockTTL) are dropped.
func (o *Orchestrator) Recover(ctx context.Context) error {
	if _, err := o.db.ExpireStaleLocks(ctx, 2*o.cfg.LockTTL); err != nil {
		return fmt.Errorf("orchestrator: expire stale locks: %w", err)
	}
	// pidBound is the set of states whose recovery uses pidAlive() to
	// decide live-vs-crashed. AwaitingMerge is intentionally excluded:
	// the agent has already produced a PR and is waiting on an
	// external merge — there is no live worktree PID, so pidAlive
	// would always read "dead" and force-crash a healthy agent.
	pidBound := []state.AgentState{
		state.AgentSpawning,
		state.AgentRunning,
		state.AgentPROpen,
		state.AgentGatesRunning,
	}
	agents, err := o.db.ListAgentsByState(ctx, pidBound...)
	if err != nil {
		return err
	}
	for _, a := range agents {
		if pidAlive(a.PID) {
			continue
		}
		if _, err := o.db.TransitionAgent(ctx, a.ID, state.AgentCrashed, state.AgentMutation{}); err != nil {
			if errors.Is(err, state.ErrInvalidTransition) {
				continue
			}
			return fmt.Errorf("orchestrator: mark agent %d crashed: %w", a.ID, err)
		}
		if _, err := o.db.ReleaseAgentLocks(ctx, a.ID); err != nil {
			return fmt.Errorf("orchestrator: release locks for crashed agent %d: %w", a.ID, err)
		}
		if _, err := o.db.TransitionAgent(ctx, a.ID, state.AgentPending, state.AgentMutation{}); err != nil {
			return fmt.Errorf("orchestrator: requeue agent %d: %w", a.ID, err)
		}
		_ = o.db.RecordEvent(ctx, a.ID, "recovered_crashed", "{}")
		// recovered_crashed is a state-layer audit row; the
		// orchestrator-side slog line uses the lifecycle keys so
		// dashboards can correlate without touching the events table.
		o.log.Info("orchestrator.recovered_crashed",
			string(obs.KeyAgentID), a.ID,
			string(obs.KeyWorkItemID), a.WorkItemID,
		)
	}
	// AwaitingMerge agents reconcile against the external GitHub PR
	// state — never against pidAlive — because the merge sits on the
	// far side of an external side-effect the substrate does not own.
	// Without a Coordinator the sweep is skipped (operator pre-W2);
	// non-fatal so a Coordinator-less daemon still recovers everything
	// else.
	if o.mergeCoord != nil {
		if err := o.mergeCoord.Reconcile(ctx); err != nil {
			return fmt.Errorf("orchestrator: merge reconcile: %w", err)
		}
	}
	return nil
}
