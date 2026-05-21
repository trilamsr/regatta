// Package reaper tears down worktrees and child processes for
// agents that have reached a terminal state.
//
// Per docs/design.md §Reaper, terminal transitions trigger:
//
//   1. Send SIGTERM to the agent's child process (if known)
//   2. Remove the worktree via WorktreeManager
//   3. Release any locks the agent still holds
//   4. Kick the scheduler so its next tick promotes a pending agent
//
// The skeleton implements 1-3. Step 4 lands once the orchestrator
// exposes a tick-now signal; the existing periodic ticker absorbs
// the latency until then.
package reaper

import (
	"context"
	"errors"
	"fmt"

	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// ChildKiller is implemented by spawners that own live processes.
// The Reaper consults this to send SIGTERM before removing the
// worktree (a forced rm of a worktree currently being written to
// races against the child's filesystem activity).
type ChildKiller interface {
	// KillAgent best-effort signals the agent's child process and
	// returns whether a process was actually signaled. The
	// implementation MUST be safe to call when the agent is unknown
	// (returns false, nil).
	KillAgent(agentID int64) (signaled bool, err error)
}

// Reaper owns the post-terminal cleanup path.
type Reaper struct {
	db      *state.DB
	wm      *spawner.WorktreeManager
	killer  ChildKiller
	logf    func(format string, args ...any)
	terminal []state.AgentState
}

// New constructs a Reaper. killer may be nil if the spawner has no
// live processes to signal (e.g. the stub).
func New(db *state.DB, wm *spawner.WorktreeManager, killer ChildKiller) *Reaper {
	return &Reaper{
		db:     db,
		wm:     wm,
		killer: killer,
		logf:   func(string, ...any) {},
		terminal: []state.AgentState{
			state.AgentDone,
			state.AgentWithdrawn,
			state.AgentEscalated,
		},
	}
}

// SetLogger installs a printf-style logger. Silent by default.
func (r *Reaper) SetLogger(f func(format string, args ...any)) {
	if f != nil {
		r.logf = f
	}
}

// Reap performs idempotent cleanup for a single agent: signal the
// child, remove the worktree, release any leftover locks. The agent
// row itself is left in place so the audit trail survives.
//
// Returns nil if the agent has no leftover state (the common case
// for already-reaped agents).
func (r *Reaper) Reap(ctx context.Context, agentID int64) error {
	if r.killer != nil {
		signaled, err := r.killer.KillAgent(agentID)
		if err != nil {
			return fmt.Errorf("reaper: kill agent %d: %w", agentID, err)
		}
		if signaled {
			r.logf("reaper: signaled agent %d", agentID)
		}
	}
	if r.wm != nil {
		if err := r.wm.Remove(ctx, agentID); err != nil {
			return fmt.Errorf("reaper: remove worktree for agent %d: %w", agentID, err)
		}
	}
	if _, err := r.db.ReleaseAgentLocks(ctx, agentID); err != nil {
		return fmt.Errorf("reaper: release locks for agent %d: %w", agentID, err)
	}
	_ = r.db.RecordEvent(ctx, agentID, "reaped", "{}")
	return nil
}

// ReapAll sweeps every agent currently in a terminal state and
// invokes Reap on each. The orchestrator's Run loop calls ReapAll on
// a timer so a missed terminal-edge hook (e.g. external state mutation
// via sql) is eventually cleaned up.
func (r *Reaper) ReapAll(ctx context.Context) error {
	agents, err := r.db.ListAgentsByState(ctx, r.terminal...)
	if err != nil {
		return fmt.Errorf("reaper: list terminal agents: %w", err)
	}
	var firstErr error
	for _, a := range agents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.Reap(ctx, a.ID); err != nil {
			r.logf("reaper: agent %d: %v", a.ID, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// ErrNoKiller is returned by stub ChildKillers that cannot signal a
// process. Callers may ignore it; Reaper treats it as a no-op.
var ErrNoKiller = errors.New("reaper: no child-killer configured")
