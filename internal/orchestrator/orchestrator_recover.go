package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// stubSessionPrefix is the session_id stamp the stub spawner writes
// ("stub-<agentID>-<workItemID>"). Recover keys backend-mismatch
// detection off this prefix because a stub row carries no live OS PID
// and must not be re-attached under the claude backend (MAY-79).
const stubSessionPrefix = "stub-"

// Recover implements the crash-recovery contract (docs/design.md §State,
// persistence, recovery); MUST be called once on startup before Run,
// PollOnce, or ScheduleOnce. (1) Non-terminal agents whose PID is dead
// AND state ∈ {spawning, running, pr_open, gates_running} → crashed →
// re-queued via a fresh pending row. (1b) Agents whose session_id was
// written by the stub spawner ("stub-" prefix) but the daemon now runs
// under the claude backend are reboot ghosts: the stub row never had a
// real process, and re-attaching it holds the lane against a phantom
// (MAY-79, observed in the 2026-06-08 stub→claude soak). These are
// drained to the withdrawn tombstone (lane freed, no requeue). (2)
// AwaitingMerge agents reconcile via merge.Coordinator (no live PID by
// design — pid-alive doesn't apply). (3) Locks older than 2× LockTTL are
// dropped.
func (o *Orchestrator) Recover(ctx context.Context) error {
	if _, err := o.db.ExpireStaleLocks(ctx, 2*o.cfg.LockTTL); err != nil {
		return fmt.Errorf("orchestrator: expire stale locks: %w", err)
	}
	// AwaitingMerge intentionally excluded: agent has a PR open with no live worktree PID — pidAlive would force-crash a healthy agent.
	// AgentCrashed IS included: a daemon crash between the Crashed
	// transition and the requeue leaves the row stuck Crashed with locks
	// held. Re-running the same recovery branch finishes the transition
	// idempotently (R-MEGA-2 C3).
	pidBound := []state.AgentState{
		state.AgentSpawning,
		state.AgentRunning,
		state.AgentPROpen,
		state.AgentGatesRunning,
		state.AgentCrashed,
	}
	agents, err := o.db.ListAgentsByState(ctx, pidBound...)
	if err != nil {
		return err
	}
	for _, a := range agents {
		// A stub-written row recovered under the claude backend is a
		// reboot ghost — reap it before the pid-liveness branch so the
		// lane is freed instead of re-attached (MAY-79).
		if o.isBackendMismatch(a) {
			if err := o.reapBackendMismatch(ctx, a); err != nil {
				return err
			}
			continue
		}
		if a.State != state.AgentCrashed {
			if pidAlive(a.PID) {
				continue
			}
			if _, err := o.db.TransitionAgent(ctx, a.ID, state.AgentCrashed, state.AgentMutation{}); err != nil {
				if errors.Is(err, state.ErrInvalidTransition) {
					continue
				}
				return fmt.Errorf("orchestrator: mark agent %d crashed: %w", a.ID, err)
			}
		}
		if _, err := o.db.ReleaseAgentLocks(ctx, a.ID); err != nil {
			return fmt.Errorf("orchestrator: release locks for crashed agent %d: %w", a.ID, err)
		}
		if _, err := o.db.TransitionAgent(ctx, a.ID, state.AgentPending, state.AgentMutation{}); err != nil {
			if errors.Is(err, state.ErrInvalidTransition) {
				return fmt.Errorf("orchestrator: requeue agent %d: %w", a.ID, err)
			}
		}
		_ = o.db.RecordEvent(ctx, a.ID, string(obs.EventRecoveredCrashed), "{}")
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

// isBackendMismatch reports whether agent a was written by the stub
// spawner ("stub-" session_id) while the daemon now runs under a
// different, non-stub backend — the reboot-ghost condition from MAY-79.
// Under the stub backend (empty or "stub" SpawnerBackend) it returns
// false so a genuine stub crash still requeues through the normal path.
func (o *Orchestrator) isBackendMismatch(a state.Agent) bool {
	active := o.cfg.SpawnerBackend
	if active == "" || active == "stub" {
		return false
	}
	return strings.HasPrefix(a.SessionID, stubSessionPrefix)
}

// reapBackendMismatch drains a stub-written reboot ghost to the
// withdrawn tombstone, freeing its lane without requeueing. The FSM has
// no direct path to withdrawn from the pid-bound states, so it routes
// crashed→withdrawn (both valid edges per transitions.AgentEdges). An
// ErrInvalidTransition mid-flight means a concurrent writer already
// moved the row; the reap is then a no-op.
func (o *Orchestrator) reapBackendMismatch(ctx context.Context, a state.Agent) error {
	if _, err := o.db.TransitionAgent(ctx, a.ID, state.AgentCrashed, state.AgentMutation{}); err != nil {
		if errors.Is(err, state.ErrInvalidTransition) {
			return nil
		}
		return fmt.Errorf("orchestrator: mark ghost agent %d crashed: %w", a.ID, err)
	}
	if _, err := o.db.ReleaseAgentLocks(ctx, a.ID); err != nil {
		return fmt.Errorf("orchestrator: release locks for ghost agent %d: %w", a.ID, err)
	}
	if _, err := o.db.TransitionAgent(ctx, a.ID, state.AgentWithdrawn, state.AgentMutation{}); err != nil {
		if errors.Is(err, state.ErrInvalidTransition) {
			return nil
		}
		return fmt.Errorf("orchestrator: reap ghost agent %d: %w", a.ID, err)
	}
	_ = o.db.RecordEvent(ctx, a.ID, string(obs.EventSpawnerBackendChanged), fmt.Sprintf(`{"session_id":%q,"active_backend":%q}`, a.SessionID, o.cfg.SpawnerBackend))
	o.log.Warn(string(obs.EventSpawnerBackendChanged),
		string(obs.KeyAgentID), a.ID,
		string(obs.KeyWorkItemID), a.WorkItemID,
		"session_id", a.SessionID,
		"active_backend", o.cfg.SpawnerBackend,
	)
	return nil
}
