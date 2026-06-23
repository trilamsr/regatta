package events

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakeDeleter struct {
	calls   atomic.Int32
	cutoff  atomic.Int64
	deleted int64
	err     error
}

func (f *fakeDeleter) DeleteEventsOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	f.calls.Add(1)
	f.cutoff.Store(cutoff.Unix())
	if f.err != nil {
		return 0, f.err
	}
	return f.deleted, nil
}

// TestRetention_DeletesOldRows asserts the first sweep computes the
// cutoff as now - RetentionDays and forwards the delete call to the DB
// (R-MEGA-2 P3).
func TestRetention_DeletesOldRows(t *testing.T) {
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	d := &fakeDeleter{deleted: 5}
	r := New(Config{
		DB:            d,
		RetentionDays: 7,
		Interval:      10 * time.Millisecond,
		Clock:         func() time.Time { return now },
	})
	if r == nil {
		t.Fatalf("New returned nil for enabled config")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	deadline := time.After(time.Second)
	for {
		if d.calls.Load() >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("DeleteEventsOlderThan never called")
		default:
		}
	}
	cancel()
	<-done

	wantCutoff := now.Add(-7 * 24 * time.Hour).Unix()
	if got := d.cutoff.Load(); got != wantCutoff {
		t.Fatalf("cutoff=%d want %d", got, wantCutoff)
	}
}

// TestRetention_DisabledWhenDaysZero asserts a zero-day horizon yields
// nil so the daemon can skip the goroutine entirely (R-MEGA-2 P3).
func TestRetention_DisabledWhenDaysZero(t *testing.T) {
	r := New(Config{DB: &fakeDeleter{}, RetentionDays: 0})
	if r != nil {
		t.Fatalf("expected nil for disabled retention; got %#v", r)
	}
}

// TestRetention_DisabledWhenDBNil asserts a missing DB yields nil.
func TestRetention_DisabledWhenDBNil(t *testing.T) {
	r := New(Config{RetentionDays: 7})
	if r != nil {
		t.Fatalf("expected nil for nil DB; got %#v", r)
	}
}

// TestRetention_SweepErrorDoesNotHaltLoop asserts a failed sweep logs +
// continues; the next tick still runs (R-MEGA-2 P3).
func TestRetention_SweepErrorDoesNotHaltLoop(t *testing.T) {
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	d := &fakeDeleter{err: errors.New("synthetic")}
	r := New(Config{
		DB:            d,
		RetentionDays: 7,
		Interval:      5 * time.Millisecond,
		Clock:         func() time.Time { return now },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	r.Run(ctx)
	if d.calls.Load() < 2 {
		t.Fatalf("loop halted after error; calls=%d want >= 2", d.calls.Load())
	}
}
