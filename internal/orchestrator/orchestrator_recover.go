package orchestrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Recover implements the crash-recovery contract (docs/design.md §State,
// persistence, recovery); MUST be called once on startup before Run,
// PollOnce, or ScheduleOnce. (1) Non-terminal agents whose PID is dead
// AND state ∈ {spawning, running, pr_open, gates_running} → crashed →
// re-queued via a fresh pending row. (2) AwaitingMerge agents reconcile
// via merge.Coordinator (no live PID by design — pid-alive doesn't
// apply). (3) Locks older than 2× LockTTL are dropped.
func (o *Orchestrator) Recover(ctx context.Context) error {
	if _, err := o.db.ExpireStaleLocks(ctx, 2*o.cfg.LockTTL); err != nil {
		return fmt.Errorf("orchestrator: expire stale locks: %w", err)
	}
	// AwaitingMerge intentionally excluded: agent has a PR open with no live worktree PID — pidAlive would force-crash a healthy agent.
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
		o.log.Info("orchestrator.recovered_crashed",
			string(obs.KeyAgentID), a.ID,
			string(obs.KeyWorkItemID), a.WorkItemID,
		)
	}
	// AwaitingMerge reconciles via external GitHub state (substrate doesn't own the merge); Coordinator-less daemon still recovers everything else.
	if o.mergeCoord != nil {
		if err := o.mergeCoord.Reconcile(ctx); err != nil {
			return fmt.Errorf("orchestrator: merge reconcile: %w", err)
		}
	}
	return nil
}
