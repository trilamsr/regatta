package orchestrator

import (
	"context"

	"github.com/trilamsr/regatta/internal/orchestrator/reaper"
)

// SetReaper installs the Reaper used by Run to sweep terminal
// agents. Optional; without a Reaper the daemon still functions but
// leaves worktrees on disk after terminal transitions.
func (o *Orchestrator) SetReaper(r *reaper.Reaper) {
	o.reaper = r
}

// ReapTerminal invokes the configured Reaper.ReapAll AND
// Reaper.SweepCrashedWithPID. Without a Reaper this is a no-op.
//
// The crashed-with-PID sweep is the R19-A follow-up cleanup: post-spawn
// TransitionAgent failures stamp PID+SessionID on a crashed row so the
// reaper can kill the orphan child, release locks, and requeue the work
// item to pending. ReapAll's terminal predicate excludes crashed
// (intentionally — crashed is a recovery state, not a tombstone), so
// the crashed sweep is a sibling call, not a predicate extension.
func (o *Orchestrator) ReapTerminal(ctx context.Context) error {
	if o.reaper == nil {
		return nil
	}
	if err := o.reaper.ReapAll(ctx); err != nil {
		return err
	}
	return o.reaper.SweepCrashedWithPID(ctx)
}
