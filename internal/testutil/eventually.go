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
