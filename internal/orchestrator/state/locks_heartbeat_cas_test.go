// locks_heartbeat_cas_test pins the CAS-guard contract on HeartbeatLock
// added by PR #558 adversarial-review Bug-2. Two orchestrator instances
// may run HeartbeatLock concurrently against the same row; the raw
// UPDATE without a monotonic guard lost newer writes to LIFO commit
// order, which then false-positived the stale-lock sweep.
package state

import (
	"context"
	"testing"
	"time"
)

// TestHeartbeatLock_OlderTimestampNoOp asserts a stale heartbeat write never overwrites a newer one.
func TestHeartbeatLock_OlderTimestampNoOp(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0).UTC()
	db := newClockedTestDB(t, &clock)
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-1", "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.TryAcquireLock(ctx, "alpha", a.ID, 5*time.Minute); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Advance + heartbeat so the row carries the newer timestamp.
	clock = clock.Add(10 * time.Minute)
	newer := clock
	if _, err := db.HeartbeatLock(ctx, a.ID); err != nil {
		t.Fatalf("heartbeat newer: %v", err)
	}

	// Rewind clock to simulate the two-instances-with-clock-drift race
	// (concrete failure: instance B's now() < instance A's last write).
	clock = clock.Add(-5 * time.Minute)
	if _, err := db.HeartbeatLock(ctx, a.ID); err != nil {
		t.Fatalf("heartbeat older: %v", err)
	}

	locks, err := db.ListLocks(ctx)
	if err != nil {
		t.Fatalf("list locks: %v", err)
	}
	if len(locks) != 1 {
		t.Fatalf("locks=%d, want 1", len(locks))
	}
	if !locks[0].HeartbeatAt.Equal(newer) {
		t.Fatalf("heartbeat_at=%v, want %v (stale write overwrote newer)", locks[0].HeartbeatAt, newer)
	}
}

// TestHeartbeatLock_MonotonicallyAdvancesAcrossInterleavedWrites asserts the row's heartbeat_at strictly never regresses across N interleaved older/newer write attempts.
func TestHeartbeatLock_MonotonicallyAdvancesAcrossInterleavedWrites(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0).UTC()
	db := newClockedTestDB(t, &clock)
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-1", "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.TryAcquireLock(ctx, "alpha", a.ID, 5*time.Minute); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Interleave older/newer writes — heartbeat_at must hold the MAX
	// after every step, never a prior smaller value.
	offsets := []int{30, 5, 60, 45, 90, 10, 75}
	var maxSeen time.Time
	base := clock
	for _, off := range offsets {
		clock = base.Add(time.Duration(off) * time.Second)
		if _, err := db.HeartbeatLock(ctx, a.ID); err != nil {
			t.Fatalf("heartbeat off=%d: %v", off, err)
		}
		if clock.After(maxSeen) {
			maxSeen = clock
		}
		locks, _ := db.ListLocks(ctx)
		if !locks[0].HeartbeatAt.Equal(maxSeen) {
			t.Fatalf("after off=%d: heartbeat_at=%v, want %v (monotonicity violated)", off, locks[0].HeartbeatAt, maxSeen)
		}
	}
}
