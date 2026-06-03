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

// ReapTerminal invokes the configured Reaper.ReapAll. Safe to call
// with no Reaper set; returns nil in that case.
func (o *Orchestrator) ReapTerminal(ctx context.Context) error {
	if o.reaper == nil {
		return nil
	}
	return o.reaper.ReapAll(ctx)
}
