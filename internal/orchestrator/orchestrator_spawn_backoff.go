package orchestrator

import "sync"

const (
	// spawnBackoffBaseTicks is the suppression window applied on the first
	// spawn failure, in scheduler ticks (default tick = 5s, serve.go). It
	// is ≥2 so the window set on a failing tick survives that same tick's
	// end-of-tick decrement and still suppresses the immediately following
	// tick's re-dispatch.
	spawnBackoffBaseTicks = 2

	// spawnBackoffMaxTicks caps the exponential window so a chronically
	// failing item still re-attempts within a bounded interval rather than
	// drifting toward never (1<<6 = 64 ticks ≈ 5min at the default 5s tick).
	spawnBackoffMaxTicks = 1 << 6

	// spawnBackoffStaleTicks evicts an entry that has sat past its window
	// without a fresh failure, bounding map growth while preserving the
	// failure count long enough for a consecutive failure to grow the
	// window exponentially.
	spawnBackoffStaleTicks = spawnBackoffMaxTicks
)

// spawnBackoff is the per-work_item exponential suppression window that
// gates spawn re-dispatch after a failed Spawn — without it the 5s
// scheduler tick re-attempts the same failing item every tick forever,
// burning a real agent invocation each time (MAY-80). Modeled on the
// scheduler's recheckBackoff map+entry+Tick shape, but deliberately NOT
// that primitive: recheckBackoff is unexported in package scheduler,
// fires only on a Kth strike, and holds a fixed window — spawn failures
// need immediate suppression on the FIRST failure and an exponential
// window that widens with each consecutive failure.
type spawnBackoff struct {
	mu      sync.Mutex
	entries map[string]*spawnBackoffEntry
}

type spawnBackoffEntry struct {
	failures      int
	suppressTicks int
	idleTicks     int
}

func newSpawnBackoff() *spawnBackoff {
	return &spawnBackoff{entries: make(map[string]*spawnBackoffEntry)}
}

// Admit reports whether id may be spawned this tick: true when it has no
// active suppression window, false while it is cooling down.
func (b *spawnBackoff) Admit(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entries[id]
	return e == nil || e.suppressTicks == 0
}

// RecordFailure widens id's suppression window: failure N opens a window
// of min(base<<(N-1), max) ticks, so each consecutive failure (with no
// intervening success) suppresses re-dispatch strictly longer.
func (b *spawnBackoff) RecordFailure(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entries[id]
	if e == nil {
		e = &spawnBackoffEntry{}
		b.entries[id] = e
	}
	e.failures++
	e.idleTicks = 0
	window := spawnBackoffBaseTicks << (e.failures - 1)
	if window > spawnBackoffMaxTicks || window <= 0 {
		window = spawnBackoffMaxTicks
	}
	e.suppressTicks = window
}

// RecordSuccess clears id's failure history so its next cooldown starts
// from the base window — a recovered item is no longer penalized.
func (b *spawnBackoff) RecordSuccess(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.entries, id)
}

// Tick decrements every active suppression window by one and evicts
// entries whose window has elapsed, so the map cannot grow unbounded.
func (b *spawnBackoff) Tick() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, e := range b.entries {
		if e.suppressTicks > 0 {
			e.suppressTicks--
			continue
		}
		// Window elapsed: keep the failure count for a bounded idle span
		// so a consecutive failure widens the window exponentially, then
		// evict once the item has been quiet past the stale horizon.
		e.idleTicks++
		if e.idleTicks >= spawnBackoffStaleTicks {
			delete(b.entries, id)
		}
	}
}
