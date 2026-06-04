// Package testutil hosts test-only helpers shared across packages.
package testutil

import (
	"context"
	"testing"
	"time"
)

// Eventually polls fn at interval until it returns true or ctx is done.
// Replaces the bare time.Sleep + state-check loop pattern that flakes when
// scheduler stalls expand the gap; deadline from ctx bounds the wait.
func Eventually(t testing.TB, ctx context.Context, interval time.Duration, fn func() bool, msg string) {
	t.Helper()
	if fn() {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Eventually: %s (ctx: %v)", msg, ctx.Err())
			return
		case <-ticker.C:
			if fn() {
				return
			}
		}
	}
}

// EventuallyT is the best-effort variant of Eventually: timeout logs via t.Logf
// instead of t.Fatalf. Use for cleanup polls where a miss is a warning, not a
// failure (e.g. a child process the test killed should die, but a stuck child
// must not turn the host test red).
func EventuallyT(t testing.TB, ctx context.Context, interval time.Duration, fn func() bool, msg string) {
	t.Helper()
	if fn() {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Logf("EventuallyT: %s (ctx: %v)", msg, ctx.Err())
			return
		case <-ticker.C:
			if fn() {
				return
			}
		}
	}
}

// AssertStable polls fn `count` times at `interval` and Fatals if the
// predicate ever returns false. Use for negative assertions where the test
// asserts a condition does NOT change during a settling window — generic
// Eventually cannot express "stayed true throughout N polls".
func AssertStable(t testing.TB, interval time.Duration, count int, fn func() bool, msg string) {
	t.Helper()
	for i := 0; i < count; i++ {
		if !fn() {
			t.Fatalf("AssertStable: %s (flipped false at poll %d/%d)", msg, i+1, count)
			return
		}
		if i < count-1 {
			time.Sleep(interval)
		}
	}
}
