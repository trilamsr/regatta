package merge_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestExecutor_ConcurrentReleaseLocksIdempotent asserts a concurrent reaper-style ReleaseAgentLocks call against the same crashed agent does not produce a Go-level error or DB constraint violation (R-MEGA-3 LIVE-10).
func TestExecutor_ConcurrentReleaseLocksIdempotent(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-LIVE10")
	exec := &fakeExecutor{
		Result: merge.ExecutorResult{Outcome: merge.OutcomeConflict},
		Err:    errors.New("merge conflict"),
	}
	c := newCoordinatorWithExecutor(t, db, exec)

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)

	go func() {
		defer wg.Done()
		if err := c.ExecuteMerge(ctx, a.ID, 99, "sha-live10"); err != nil {
			errs <- err
		}
	}()
	go func() {
		defer wg.Done()
		// Mimic a parallel reaper firing a second ReleaseAgentLocks; the
		// underlying SQL is idempotent so this must not error.
		if _, err := db.ReleaseAgentLocks(ctx, a.ID); err != nil {
			errs <- err
		}
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent release: %v", err)
	}

	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentCrashed {
		t.Errorf("state=%s want Crashed", got.State)
	}
}
