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

// TestEventuallyT_SucceedsOnFirstPoll pins fast-path: predicate true on first call returns without logging.
func TestEventuallyT_SucceedsOnFirstPoll(t *testing.T) {
	fake := &fakeT{TB: t}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	testutil.EventuallyT(fake, ctx, 100*time.Millisecond, func() bool { return true }, "ignored")
	if fake.failed {
		t.Fatal("EventuallyT must not Fatal on success")
	}
	if fake.logged {
		t.Fatal("EventuallyT must not Logf on success")
	}
}

// TestEventuallyT_LogsOnDeadline pins best-effort path: timeout calls t.Logf, never t.Fatal.
func TestEventuallyT_LogsOnDeadline(t *testing.T) {
	fake := &fakeT{TB: t}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	testutil.EventuallyT(fake, ctx, 5*time.Millisecond, func() bool { return false }, "needle-not-found")
	if fake.failed {
		t.Fatal("EventuallyT must not Fatal on timeout; expected Logf only")
	}
	if !fake.logged {
		t.Fatal("expected Logf call on timeout")
	}
	if fake.msg == "" {
		t.Fatal("expected log message; got empty")
	}
}

// TestAssertStable_StaysTrue pins success path: predicate holds across all N polls returns without fail.
func TestAssertStable_StaysTrue(t *testing.T) {
	fake := &fakeT{TB: t}
	calls := 0
	testutil.AssertStable(fake, 5*time.Millisecond, 5, func() bool {
		calls++
		return true
	}, "should be stable")
	if fake.failed {
		t.Fatalf("AssertStable Fataled on stable predicate; msg=%q", fake.msg)
	}
	if calls != 5 {
		t.Fatalf("calls=%d want 5", calls)
	}
}

// TestAssertStable_FlipsFalse pins fail-fast path: predicate flipping false triggers Fatal with msg.
func TestAssertStable_FlipsFalse(t *testing.T) {
	fake := &fakeT{TB: t}
	calls := 0
	testutil.AssertStable(fake, 1*time.Millisecond, 5, func() bool {
		calls++
		return calls < 3
	}, "must-stay-true")
	if !fake.failed {
		t.Fatal("AssertStable must Fatal when predicate flips false")
	}
	if fake.msg == "" {
		t.Fatal("expected fail message; got empty")
	}
}

// fakeT captures Fatal/Fatalf/Logf without aborting the host test.
type fakeT struct {
	testing.TB
	failed bool
	logged bool
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

func (f *fakeT) Logf(format string, args ...any) {
	f.logged = true
	f.msg = format
}
