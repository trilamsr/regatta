package prwatch

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// blockingLister blocks on ListOpenByHead until ctx cancels or release fires — reproduces the network-hang scenario from MAY-bug6.
type blockingLister struct {
	release chan struct{}
	calls   int32
}

func (b *blockingLister) ListOpenByHead(ctx context.Context, _, _ string) ([]PullRequest, error) {
	atomic.AddInt32(&b.calls, 1)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.release:
		return nil, nil
	}
}

// TestWatcher_ListTimeout_BoundsSweepPerCall asserts Sweep returns within listTimeout+slop when the lister hangs (MAY-bug6).
func TestWatcher_ListTimeout_BoundsSweepPerCall(t *testing.T) {
	lister := &blockingLister{release: make(chan struct{})}
	defer close(lister.release)

	w, db := newTestWatcher(t, lister)
	w.listTimeout = 100 * time.Millisecond
	driveToRunning(t, db, "WORK-1")

	start := time.Now()
	// W-BUG14: Sweep now returns the aggregated per-agent lister error;
	// this test only cares that the call returns bounded by listTimeout.
	_ = w.Sweep(context.Background())
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("sweep took %s, want ≤500ms under 100ms listTimeout", elapsed)
	}
	if atomic.LoadInt32(&lister.calls) == 0 {
		t.Fatal("lister never called")
	}
}

// TestWatcher_ListTimeout_EscalatesToErrorOnThirdConsecutiveFailure asserts the 3rd consecutive per-agent list failure logs at ERROR (MAY-bug6).
func TestWatcher_ListTimeout_EscalatesToErrorOnThirdConsecutiveFailure(t *testing.T) {
	lister := &blockingLister{release: make(chan struct{})}
	defer close(lister.release)

	w, db := newTestWatcher(t, lister)
	var buf bytes.Buffer
	w.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	w.listTimeout = 50 * time.Millisecond
	driveToRunning(t, db, "WORK-1")

	for i := 0; i < 3; i++ {
		// W-BUG14: Sweep now returns the aggregated per-agent error; this
		// test asserts log-level escalation, not the return value.
		_ = w.Sweep(context.Background())
	}

	out := buf.String()
	errorLines, warnLines := 0, 0
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "prwatch.list_failed") {
			continue
		}
		if strings.Contains(line, "level=ERROR") {
			errorLines++
		}
		if strings.Contains(line, "level=WARN") {
			warnLines++
		}
	}
	if errorLines < 1 {
		t.Fatalf("expected ≥1 ERROR-level prwatch.list_failed after 3 consecutive failures; got:\n%s", out)
	}
	if warnLines < 2 {
		t.Fatalf("expected 2 WARN-level prwatch.list_failed on failures 1+2; got:\n%s", out)
	}
}

// TestWatcher_ListTimeout_ResetsCounterOnSuccess asserts a successful sweep between failures clears the per-agent counter so ERROR does not fire prematurely (MAY-bug6).
func TestWatcher_ListTimeout_ResetsCounterOnSuccess(t *testing.T) {
	toggle := &toggleLister{failFirst: 2}
	w, db := newTestWatcher(t, toggle)
	var buf bytes.Buffer
	w.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	w.listTimeout = 50 * time.Millisecond
	driveToRunning(t, db, "WORK-1")

	// 2 failures + 1 success + 1 failure ⇒ counter reset, no ERROR emitted.
	// W-BUG14: Sweep returns the aggregated per-agent error on failures;
	// this test asserts counter-reset log behavior, not the return value.
	for i := 0; i < 4; i++ {
		_ = w.Sweep(context.Background())
	}
	if strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("counter was not reset on success; unexpected ERROR log:\n%s", buf.String())
	}
}

// toggleLister returns an error for the first failFirst calls, nil afterward.
type toggleLister struct {
	calls     int
	failFirst int
}

func (l *toggleLister) ListOpenByHead(_ context.Context, _, _ string) ([]PullRequest, error) {
	l.calls++
	if l.calls <= l.failFirst {
		return nil, errors.New("simulated failure")
	}
	if l.calls == l.failFirst+2 {
		return nil, errors.New("post-success failure")
	}
	return nil, nil
}
