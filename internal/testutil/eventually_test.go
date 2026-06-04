package testutil_test

import (
	"context"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/testutil"
)

// TestEventually_SucceedsOnFirstPoll pins fast-path: predicate true on first call returns without sleep.
func TestEventually_SucceedsOnFirstPoll(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	start := time.Now()
	testutil.Eventually(t, ctx, 100*time.Millisecond, func() bool {
		calls++
		return true
	}, "should not fail")
	if calls != 1 {
		t.Fatalf("calls=%d want 1", calls)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("elapsed=%v want <50ms (no sleep on first-poll success)", elapsed)
	}
}

// TestEventually_SucceedsAfterNPolls pins poll-loop: predicate flips true after N intervals.
func TestEventually_SucceedsAfterNPolls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	testutil.Eventually(t, ctx, 5*time.Millisecond, func() bool {
		calls++
		return calls >= 3
	}, "should succeed by 3rd call")
	if calls != 3 {
		t.Fatalf("calls=%d want 3", calls)
	}
}

// TestEventually_FailsOnDeadline pins timeout path: predicate never true → t.Fatal with msg.
func TestEventually_FailsOnDeadline(t *testing.T) {
	fake := &fakeT{TB: t}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	testutil.Eventually(fake, ctx, 5*time.Millisecond, func() bool { return false }, "needle-not-found")
	if !fake.failed {
		t.Fatal("expected Fatal call on deadline expiry")
	}
	if fake.msg == "" {
		t.Fatal("expected fail message; got empty")
	}
}

// fakeT captures Fatal/Fatalf without aborting the host test.
type fakeT struct {
	testing.TB
	failed bool
	msg    string
}

func (f *fakeT) Helper() {}

func (f *fakeT) Fatalf(format string, args ...any) {
	f.failed = true
	f.msg = format
}

func (f *fakeT) Fatal(args ...any) {
	f.failed = true
	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			f.msg = s
		} else {
			f.msg = "fatal"
		}
	}
}
