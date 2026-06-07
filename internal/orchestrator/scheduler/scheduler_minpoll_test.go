package scheduler

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestScheduler_HonoursMinPoll_Integration pins the cross-boundary contract: scheduler MUST call adapter.List ≤ ceil(tickCount / minPoll·tickInterval) times across N ticks (#840, blocks #847).
func TestScheduler_HonoursMinPoll_Integration(t *testing.T) {
	const (
		tickCount    = 3
		tickInterval = time.Second
		minPoll      = 5 * time.Second
	)

	clock := time.Unix(1_700_000_000, 0).UTC()
	clockFn := func() time.Time { return clock }
	db, err := state.OpenWithClock(context.Background(), state.DSN(filepath.Join(t.TempDir(), "minpoll.db")), clockFn)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ad := &countingMinPollAdapter{minPoll: minPoll}

	sch := New(db, Config{
		Clock:    clockFn,
		Adapters: []schemas.SpecAdapter{ad},
	})

	ctx := context.Background()
	for i := 0; i < tickCount; i++ {
		if _, err := sch.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", i+1, err)
		}
		clock = clock.Add(tickInterval)
	}

	got := ad.listCalls()
	want := int64(1)
	if got < want {
		t.Fatalf("adapter.List called %d times across %d ticks (%v window); want ≥%d — scheduler skipped MinPoll-due poll", got, tickCount, time.Duration(tickCount)*tickInterval, want)
	}
	maxAllowed := int64((time.Duration(tickCount)*tickInterval)/minPoll) + 1
	if got > maxAllowed {
		t.Fatalf("adapter.List called %d times across %d ticks (%v window, MinPoll=%v); want ≤%d — scheduler ignored MinPollInterval", got, tickCount, time.Duration(tickCount)*tickInterval, minPoll, maxAllowed)
	}
}

// TestScheduler_LastPollAdvancesOnListError pins #885 error-path: failing adapter is not re-polled inside MinPoll window (#890).
func TestScheduler_LastPollAdvancesOnListError(t *testing.T) {
	const minPoll = 5 * time.Second

	clock := time.Unix(1_700_000_000, 0).UTC()
	clockFn := func() time.Time { return clock }
	db, err := state.OpenWithClock(context.Background(), state.DSN(filepath.Join(t.TempDir(), "minpoll_err.db")), clockFn)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ad := &countingMinPollAdapter{minPoll: minPoll, listErr: schemas.ErrRateLimited}

	sch := New(db, Config{
		Clock:    clockFn,
		Adapters: []schemas.SpecAdapter{ad},
	})

	ctx := context.Background()
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	if got := ad.listCalls(); got != 1 {
		t.Fatalf("after tick 1: adapter.List called %d times; want 1 (zero-value lastPoll must fire first poll)", got)
	}

	clock = clock.Add(minPoll - time.Second)
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if got := ad.listCalls(); got != 1 {
		t.Fatalf("after tick 2 inside MinPoll window: adapter.List called %d times; want 1 — error on prior poll must still advance lastPoll", got)
	}
}

type countingMinPollAdapter struct {
	calls   atomic.Int64
	minPoll time.Duration
	listErr error
}

func (c *countingMinPollAdapter) List(_ context.Context) ([]schemas.WorkItem, error) {
	c.calls.Add(1)
	return nil, c.listErr
}

func (c *countingMinPollAdapter) Get(_ context.Context, _ schemas.WorkItemID) (schemas.WorkItem, error) {
	return schemas.WorkItem{}, nil
}

func (c *countingMinPollAdapter) UpdateStatus(_ context.Context, _ schemas.WorkItemID, _ schemas.Status, _ string) error {
	return nil
}

func (c *countingMinPollAdapter) Capabilities() schemas.Capabilities {
	return schemas.Capabilities{MinPollInterval: c.minPoll}
}

func (c *countingMinPollAdapter) listCalls() int64 { return c.calls.Load() }
