// Package clock provides a Clock interface so time-sensitive
// orchestrator paths (PollOnce timestamps, tombstone cutoffs,
// stale-PID reclaim) are deterministic under test.
//
// Production code uses System(). Tests use Fake(t0) + Advance(d).
package clock

import (
	"sync"
	"time"
)

// Clock is the abstraction over time.Now. Implementations must be
// safe for concurrent use.
type Clock interface {
	Now() time.Time
}

// System returns a Clock backed by time.Now.
func System() Clock { return systemClock{} }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// FakeClock is a Clock with a settable now value, safe for concurrent
// reads via internal mutex.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// Fake returns a FakeClock initialized at t.
func Fake(t time.Time) *FakeClock {
	return &FakeClock{now: t}
}

// Now reports the current fake time.
func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the fake time forward by d. d may be zero or
// negative; callers control monotonicity.
func (f *FakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
