package orchestrator

import (
	"context"
	"fmt"

	"github.com/trilamsr/regatta/internal/orchestrator/lockfile"
)

// PollOnce acquires the process flock, mirrors the adapter into
// work_items, then loads + verifies briefs into work_items. Per spec
// §2.9 the tick sequence is flock -> AdapterSync -> BriefLoader.
// Fail-fast: any error returns immediately so the next tick retries
// from a clean slate.
//
// Reservation lives in ScheduleOnce.Tick — splitting "update the queue"
// from "drain the queue" keeps the flock-window short and lets the Run
// loop independently throttle producer vs consumer.
func (o *Orchestrator) PollOnce(ctx context.Context) error {
	lock, err := lockfile.Acquire(o.dbPath + ".lock")
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	pollStartedAt := o.cfg.Clock().UTC()

	if err := o.adapterSync.Sync(ctx, pollStartedAt); err != nil {
		return fmt.Errorf("orchestrator: adapter sync: %w", err)
	}
	if err := o.briefLoader.Sync(ctx, pollStartedAt); err != nil {
		return fmt.Errorf("orchestrator: brief sync: %w", err)
	}
	return nil
}
