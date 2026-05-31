package scheduler

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func newSchedTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "s.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedPlanned upserts a planned feature work_item with the given id +
// lane via the production UpsertWorkItemAt path so ListSpawnable picks
// it up. Tests must exercise the work_items → materializePending →
// reserve path rather than calling UpsertPending directly.
func seedPlanned(t *testing.T, db *state.DB, id, lane string) {
	t.Helper()
	w := state.WorkItem{
		ID:     id,
		Kind:   state.KindFeature,
		Title:  id,
		Lane:   lane,
		Status: state.WorkStatusPlanned,
	}
	if err := db.UpsertWorkItemAt(context.Background(), w, state.SourceBrief, time.Now()); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// agentForWorkItem returns the single agent row whose work_item_id
// equals id, or fails the test. Used in place of an in-package
// GetAgentByWorkItem helper.
func agentForWorkItem(t *testing.T, db *state.DB, id string) state.Agent {
	t.Helper()
	all := []state.AgentState{
		state.AgentPending, state.AgentSpawning, state.AgentRunning,
		state.AgentPROpen, state.AgentGatesRunning, state.AgentGatesFailed,
		state.AgentAwaitingMerge, state.AgentDone, state.AgentWithdrawn,
		state.AgentCrashed, state.AgentEscalated,
	}
	agents, err := db.ListAgentsByState(context.Background(), all...)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	for _, a := range agents {
		if a.WorkItemID == id {
			return a
		}
	}
	t.Fatalf("no agent for work_item %s", id)
	return state.Agent{}
}

func TestTick_ReservesAllPlannedNoDeps(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	for _, id := range []string{"F-1", "F-2", "F-3"} {
		seedPlanned(t, db, id, "server")
	}

	s := New(db, Config{})
	ids, err := s.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("reserved=%d want 3 (ids=%v)", len(ids), ids)
	}

	spawning, err := db.ListAgentsByState(ctx, state.AgentSpawning)
	if err != nil {
		t.Fatalf("list spawning: %v", err)
	}
	gotIDs := map[string]bool{}
	for _, a := range spawning {
		gotIDs[a.WorkItemID] = true
	}
	for _, want := range []string{"F-1", "F-2", "F-3"} {
		if !gotIDs[want] {
			t.Fatalf("work_item %s missing from spawning set %v", want, gotIDs)
		}
	}
}

func TestTick_DepBlocksUntilMerged(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	now := time.Now()
	c1 := state.WorkItem{ID: "F-1", Kind: state.KindFeature, Title: "c1",
		Lane: "server", Status: state.WorkStatusPlanned}
	c2 := state.WorkItem{ID: "F-2", Kind: state.KindFeature, Title: "c2",
		Lane: "server", Status: state.WorkStatusPlanned,
		DependsOnFeatures: []string{"F-1"}}
	for _, w := range []state.WorkItem{c1, c2} {
		if err := db.UpsertWorkItemAt(ctx, w, state.SourceBrief, now); err != nil {
			t.Fatal(err)
		}
	}

	s := New(db, Config{})
	ids, err := s.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("first tick reserved=%d want 1", len(ids))
	}

	// The reserved id must be F-1's agent, not F-2's: F-2 has an
	// unmerged dep so ListSpawnable should never have surfaced it.
	a1 := agentForWorkItem(t, db, "F-1")
	if ids[0] != a1.ID {
		t.Fatalf("reserved id=%d want F-1 agent id=%d", ids[0], a1.ID)
	}
	if a1.State != state.AgentSpawning {
		t.Fatalf("F-1 agent state=%s want spawning", a1.State)
	}
}

func TestTick_IdempotentSecondCall(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "F-1", "server")

	s := New(db, Config{})
	first, _ := s.Tick(ctx)
	second, _ := s.Tick(ctx)
	if len(first) != 1 || len(second) != 0 {
		t.Fatalf("first=%d second=%d want 1, 0", len(first), len(second))
	}

	spawning, err := db.ListAgentsByState(ctx, state.AgentSpawning)
	if err != nil {
		t.Fatalf("list spawning: %v", err)
	}
	pending, err := db.ListAgentsByState(ctx, state.AgentPending)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(spawning) != 1 {
		t.Fatalf("spawning=%d want 1", len(spawning))
	}
	if len(pending) != 0 {
		t.Fatalf("pending=%d want 0", len(pending))
	}
}

func TestTickReservesPending(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WORK-1", "server")
	seedPlanned(t, db, "WORK-2", "server")

	sch := New(db, Config{LockTTL: time.Minute})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("reserved %d, want 2", len(ids))
	}
	got, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(got) != 2 {
		t.Fatalf("spawning=%d, want 2", len(got))
	}
}

func TestTickHonorsLaneCap(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WORK-1", "server")
	seedPlanned(t, db, "WORK-2", "server")
	seedPlanned(t, db, "WORK-3", "client")

	sch := New(db, Config{LockTTL: time.Minute, LaneCaps: map[string]int{"server": 1}})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("reserved %d, want 2 (one server + one client)", len(ids))
	}

	// Second tick must NOT promote the remaining server item while the
	// first one is still active.
	more, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if len(more) != 0 {
		t.Fatalf("second tick reserved %d, want 0", len(more))
	}
}

func TestTickHotspotBlocks(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WORK-1", "server")
	seedPlanned(t, db, "WORK-2", "server")

	sch := New(db, Config{
		LockTTL: time.Minute,
		Hotspots: func(string) []string {
			return []string{"package.json"}
		},
	})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("reserved %d, want 1", len(ids))
	}
}

func TestTickHotspotsSortedAcquisition(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WORK-1", "server")
	seedPlanned(t, db, "WORK-2", "server")

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
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("reserved %d, want 2", len(ids))
	}
	locks, _ := db.ListLocks(ctx)
	if len(locks) != 4 {
		t.Fatalf("locks=%d, want 4", len(locks))
	}
}

// TestResolveLocksSorts mutation-verifies the sort.Strings call in
// scheduler.resolveLocks. Removing the sort would let the resolver's
// emitted order leak into TryAcquireLocks, breaking the cross-agent
// deadlock-safety property from docs/design.md §Concurrency &
// soft-lock policy. An integration test through Tick + ListLocks
// cannot distinguish a sorted insert from an unsorted insert because
// sqlite returns rows by name regardless of insertion order, so this
// test calls the unexported resolveLocks directly.
func TestResolveLocksSorts(t *testing.T) {
	db := newSchedTestDB(t)
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
	db := newSchedTestDB(t)
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
	db := newSchedTestDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WORK-1", "")
	seedPlanned(t, db, "WORK-2", "")

	sch := New(db, Config{LockTTL: time.Minute, LaneCaps: map[string]int{"": 1}})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("reserved %d, want 1 (default-lane cap)", len(ids))
	}
}

func TestTickLogsSkipsOnLockHeld(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WORK-1", "server")
	seedPlanned(t, db, "WORK-2", "server")

	sch := New(db, Config{
		LockTTL:  time.Minute,
		Hotspots: func(string) []string { return []string{"shared"} },
	})
	var logged []string
	sch.SetLogger(func(f string, a ...any) {
		logged = append(logged, fmt.Sprintf(f, a...))
	})
	if _, err := sch.Tick(ctx); err != nil {
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
	db := newSchedTestDB(t)
	ctx := context.Background()
	clock := time.Unix(1_700_000_000, 0).UTC()
	db.SetClock(func() time.Time { return clock })

	// Seed first item, run Tick to drive it through
	// materializePending → reserve → acquire("shared") → spawning.
	seedPlanned(t, db, "WORK-1", "server")
	sch := New(db, Config{
		LockTTL: time.Minute,
		Hotspots: func(string) []string {
			return []string{"shared"}
		},
	})
	ids1, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	if len(ids1) != 1 {
		t.Fatalf("tick 1 reserved=%d want 1", len(ids1))
	}

	// Now seed second item competing for the same hotspot.
	seedPlanned(t, db, "WORK-2", "server")

	// Advance the clock past LockTTL so ExpireStaleLocks evicts the
	// heartbeat held by WORK-1's agent.
	clock = clock.Add(10 * time.Minute)

	ids2, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	if len(ids2) != 1 {
		t.Fatalf("tick 2 reserved %d, want 1 (stale lock should be evicted)", len(ids2))
	}
	a2 := agentForWorkItem(t, db, "WORK-2")
	if ids2[0] != a2.ID {
		t.Fatalf("tick 2 reserved id=%d want WORK-2 agent id=%d", ids2[0], a2.ID)
	}
}
