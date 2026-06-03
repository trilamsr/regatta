package orchestrator

import (
	"context"

	"github.com/trilamsr/regatta/internal/orchestrator/prwatch"
)

// SetPRWatcher installs the prwatch.Watcher used by Run to drive the
// running→pr_open agent transition by polling GitHub. Optional; without
// a Watcher the daemon still functions but agents stay in running
// forever (no PR-open signal). Wired by serve.go in production; nil
// in --no-pr-watch fixture paths.
func (o *Orchestrator) SetPRWatcher(w *prwatch.Watcher) {
	o.prWatcher = w
}

// WatchPRs invokes the configured Watcher.Sweep. Safe to call with no
// Watcher set; returns nil in that case. Same nil-safety shape as
// ReapTerminal so a Watcher-less daemon keeps booting.
func (o *Orchestrator) WatchPRs(ctx context.Context) error {
	if o.prWatcher == nil {
		return nil
	}
	return o.prWatcher.Sweep(ctx)
}
