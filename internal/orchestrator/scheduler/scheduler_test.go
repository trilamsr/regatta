package scheduler

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func newDB(t *testing.T) *state.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)",
		filepath.Join(t.TempDir(), "state.db"))
	db, err := state.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustUpsert(t *testing.T, db *state.DB, id, lane string) *state.Agent {
	t.Helper()
	a, err := db.UpsertPending(context.Background(), id, lane)
	if err != nil {
		t.Fatalf("upsert %s: %v", id, err)
	}
	return a
}

func TestTickReservesPending(t *testing.T) {
	db := newDB(t)
	mustUpsert(t, db, "WORK-1", "server")
	mustUpsert(t, db, "WORK-2", "server")

	sch := New(db, Config{LockTTL: time.Minute})
	ids, err := sch.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("reserved %d, want 2", len(ids))
	}
	got, _ := db.ListAgentsByState(context.Background(), state.AgentSpawning)
	if len(got) != 2 {
		t.Fatalf("spawning=%d, want 2", len(got))
	}
}

func TestTickHonorsLaneCap(t *testing.T) {
	db := newDB(t)
	mustUpsert(t, db, "WORK-1", "server")
	mustUpsert(t, db, "WORK-2", "server")
	mustUpsert(t, db, "WORK-3", "client")

	sch := New(db, Config{LockTTL: time.Minute, LaneCaps: map[string]int{"server": 1}})
	ids, err := sch.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("reserved %d, want 2 (one server + one client)", len(ids))
	}

	// Second tick must NOT promote the remaining server item while
	// the first one is still active.
	more, err := sch.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if len(more) != 0 {
		t.Fatalf("second tick reserved %d, want 0", len(more))
	}
}

func TestTickHotspotBlocks(t *testing.T) {
	db := newDB(t)
	mustUpsert(t, db, "WORK-1", "server")
	mustUpsert(t, db, "WORK-2", "server")

	// Both items touch the same hotspot, so only one is reservable.
	sch := New(db, Config{
		LockTTL: time.Minute,
		Hotspots: func(string) []string {
			return []string{"package.json"}
		},
	})
	ids, err := sch.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("reserved %d, want 1", len(ids))
	}
}

func TestTickHotspotsSortedAcquisition(t *testing.T) {
	db := newDB(t)
	mustUpsert(t, db, "WORK-1", "server")
	mustUpsert(t, db, "WORK-2", "server")

	// Each item touches a disjoint pair but in a different declared
	// order. Lex-sorted acquisition lets both succeed when the locks
	// are actually disjoint.
	resolver := func(id string) []string {
		if id == "WORK-1" {
			return []string{"zzz", "aaa"}
		}
		return []string{"qqq", "bbb"}
	}
	sch := New(db, Config{LockTTL: time.Minute, Hotspots: resolver})
	ids, err := sch.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("reserved %d, want 2", len(ids))
	}
	locks, _ := db.ListLocks(context.Background())
	if len(locks) != 4 {
		t.Fatalf("locks=%d, want 4", len(locks))
	}
}

// TestResolveLocksSorts is a mutation-verify test for the
// sort.Strings call in scheduler.resolveLocks. Removing that sort
// would let the resolver's emitted order leak into TryAcquireLocks,
// breaking the cross-agent deadlock-safety property from
// docs/design.md §Concurrency & soft-lock policy.
//
// Verified by calling the unexported resolveLocks directly: an
// integration test through Tick + ListLocks cannot distinguish a
// sorted insert from an unsorted insert because sqlite returns rows
// by name regardless of insertion order.
func TestResolveLocksSorts(t *testing.T) {
	db := newDB(t)
	sch := New(db, Config{
		LockTTL: time.Minute,
		Hotspots: func(string) []string {
			return []string{"zeta", "alpha", "mu"}
		},
	})
	got := sch.resolveLocks("WORK-1")
	want := []string{"alpha", "mu", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("position %d = %q, want %q (full=%v)", i, got[i], want[i], got)
		}
	}
}

func TestResolveLocksDoesNotMutateResolverSlice(t *testing.T) {
	db := newDB(t)
	source := []string{"zeta", "alpha", "mu"}
	sch := New(db, Config{
		LockTTL:  time.Minute,
		Hotspots: func(string) []string { return source },
	})
	_ = sch.resolveLocks("WORK-1")
	if source[0] != "zeta" || source[1] != "alpha" || source[2] != "mu" {
		t.Fatalf("scheduler mutated resolver-owned slice: %v", source)
	}
}

func TestTickHonorsEmptyLaneCap(t *testing.T) {
	db := newDB(t)
	mustUpsert(t, db, "WORK-1", "")
	mustUpsert(t, db, "WORK-2", "")

	sch := New(db, Config{LockTTL: time.Minute, LaneCaps: map[string]int{"": 1}})
	ids, err := sch.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("reserved %d, want 1 (default-lane cap)", len(ids))
	}
}

func TestTickLogsSkipsOnLockHeld(t *testing.T) {
	db := newDB(t)
	mustUpsert(t, db, "WORK-1", "server")
	mustUpsert(t, db, "WORK-2", "server")

	sch := New(db, Config{
		LockTTL:  time.Minute,
		Hotspots: func(string) []string { return []string{"shared"} },
	})
	var logged []string
	sch.SetLogger(func(f string, a ...any) {
		logged = append(logged, fmt.Sprintf(f, a...))
	})
	if _, err := sch.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	gotSkip := false
	for _, line := range logged {
		if strings.Contains(line, "skipped") && strings.Contains(line, "hotspot") {
			gotSkip = true
		}
	}
	if !gotSkip {
		t.Fatalf("expected skip log, got %v", logged)
	}
}

func TestTickStaleLockReclaimed(t *testing.T) {
	db := newDB(t)
	clock := time.Unix(1_700_000_000, 0).UTC()
	db.SetClock(func() time.Time { return clock })

	a := mustUpsert(t, db, "WORK-1", "server")
	if err := db.TryAcquireLock(context.Background(), "shared", a.ID, time.Minute); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// Mark the first agent as gates_failed so it is no longer
	// pending; otherwise the scheduler would re-acquire its own lock
	// and the second item would be blocked legitimately.
	if _, err := db.TransitionAgent(context.Background(), a.ID, state.AgentSpawning, state.AgentMutation{}); err != nil {
		t.Fatalf("spawn a: %v", err)
	}

	mustUpsert(t, db, "WORK-2", "server")

	clock = clock.Add(10 * time.Minute)
	sch := New(db, Config{
		LockTTL: time.Minute,
		Hotspots: func(string) []string {
			return []string{"shared"}
		},
	})
	ids, err := sch.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("reserved %d, want 1 (stale lock should be evicted)", len(ids))
	}
}
