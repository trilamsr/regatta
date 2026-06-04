package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

func recordHasAttr(r slog.Record, key string) bool {
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = true
			return false
		}
		return true
	})
	return found
}

// fakeEvaluator is the in-test EdgeEvaluator. The production wiring
// passes program.NewEdgeEvaluator() (wired from BriefLoader in W5);
// tests stay package-local to avoid the scheduler -> program ->
// orchestrator -> scheduler import cycle.
type fakeEvaluator struct {
	// rule keys edge.ID -> (fired, err)
	rules map[int64]fakeRule
	// fallback returns (true,"unconditional",nil) when an edge has
	// empty predicate, matching program.EdgeEvaluator.
	defaultUnconditional bool
}

type fakeRule struct {
	fired  bool
	reason string
	err    error
}

func newFakeEvaluator() *fakeEvaluator {
	return &fakeEvaluator{rules: map[int64]fakeRule{}, defaultUnconditional: true}
}

func (f *fakeEvaluator) Eval(_ context.Context, edge state.EdgeRow, _ any, _ state.OutputJournalEntry) (bool, string, error) {
	if r, ok := f.rules[edge.ID]; ok {
		return r.fired, r.reason, r.err
	}
	if edge.PredicateCEL == "" && f.defaultUnconditional {
		return true, "unconditional", nil
	}
	return false, "no-rule", nil
}


// seedMerged inserts a merged work_item so ListPendingEdgesFromMerged
// sees the from_id as a valid edge source.
func seedMerged(t *testing.T, db *state.DB, id string) {
	t.Helper()
	w := state.WorkItem{
		ID: id, Kind: state.KindFeature, Title: id,
		Lane: "server", Status: state.WorkStatusMerged,
	}
	if err := db.UpsertWorkItem(context.Background(), w, state.SourceBrief, time.Now()); err != nil {
		t.Fatalf("seedMerged %s: %v", id, err)
	}
}

// seedPlanned upserts a planned feature work_item with the given id +
// lane via the production UpsertWorkItem path so ListSpawnable picks
// it up. Tests must exercise the work_items → ListSpawnable →
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
	if err := db.UpsertWorkItem(context.Background(), w, state.SourceBrief, time.Now()); err != nil {
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
	db := statetest.OpenDB(t)
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
	db := statetest.OpenDB(t)
	ctx := context.Background()
	now := time.Now()
	c1 := state.WorkItem{ID: "F-1", Kind: state.KindFeature, Title: "c1",
		Lane: "server", Status: state.WorkStatusPlanned}
	c2 := state.WorkItem{ID: "F-2", Kind: state.KindFeature, Title: "c2",
		Lane: "server", Status: state.WorkStatusPlanned,
		DependsOnFeatures: []string{"F-1"}}
	for _, w := range []state.WorkItem{c1, c2} {
		if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, now); err != nil {
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
	db := statetest.OpenDB(t)
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
	db := statetest.OpenDB(t)
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
	db := statetest.OpenDB(t)
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
	db := statetest.OpenDB(t)
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
	db := statetest.OpenDB(t)
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

// TestResolveLocksSorts mutation-verifies sort.Strings in scheduler.resolveLocks for cross-agent deadlock safety.
func TestResolveLocksSorts(t *testing.T) {
	db := statetest.OpenDB(t)
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
	db := statetest.OpenDB(t)
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
	db := statetest.OpenDB(t)
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
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WORK-1", "server")
	seedPlanned(t, db, "WORK-2", "server")

	h := obstest.New()
	sch := New(db, Config{
		LockTTL:  time.Minute,
		Hotspots: func(string) []string { return []string{"shared"} },
		Logger:   slog.New(h),
	})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	gotSkip := false
	records := h.Records()
	msgs := make([]string, 0, len(records))
	for _, r := range records {
		msgs = append(msgs, r.Message)
		if strings.Contains(r.Message, "skipped") && strings.Contains(r.Message, "hotspot") {
			gotSkip = true
		}
	}
	if !gotSkip {
		t.Fatalf("expected hotspot-skip log, got %v", msgs)
	}
}

func TestTickStaleLockReclaimed(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0).UTC()
	db, err := state.OpenWithClock(context.Background(), state.DSN(filepath.Join(t.TempDir(), "s.db")), func() time.Time { return clock })
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	// Seed first item, run Tick to drive it through
	// reserveFromSpawnable → acquire("shared") → spawning.
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

func TestTick_EvaluatesPendingEdgesBeforeReserve(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	seedMerged(t, db, "F-A")
	seedPlanned(t, db, "F-B", "server")

	if err := db.UpsertEdges(ctx, "m-1", []state.EdgeRow{{
		ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
	}}); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}
	if _, err := db.AppendOutput(ctx, "F-A", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("AppendOutput: %v", err)
	}

	sch := New(db, Config{Evaluator: newFakeEvaluator()})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	edges, err := db.ListEdgesFrom(ctx, "F-A")
	if err != nil {
		t.Fatalf("ListEdgesFrom: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("edges=%d want 1", len(edges))
	}
	if edges[0].Fired != "true" {
		t.Fatalf("unconditional edge not fired after tick: %+v", edges[0])
	}
	if edges[0].FiredAgainst == "" {
		t.Fatalf("FiredAgainst empty: %+v", edges[0])
	}
}

func TestTick_FiresPredicateAgainstJournal(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	seedMerged(t, db, "F-A")
	seedPlanned(t, db, "F-B", "server")

	if err := db.UpsertEdges(ctx, "m-1", []state.EdgeRow{{
		ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
		PredicateCEL: `out.severity == "high"`, OnSkip: "cascade",
	}}); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}
	if _, err := db.AppendOutput(ctx, "F-A", json.RawMessage(`{"severity":"high"}`)); err != nil {
		t.Fatalf("AppendOutput: %v", err)
	}

	ev := newFakeEvaluator()
	// Identify the edge by inspecting the inserted row's ID.
	rows, _ := db.ListEdgesFrom(ctx, "F-A")
	ev.rules[rows[0].ID] = fakeRule{fired: true, reason: "predicate=true"}

	sch := New(db, Config{Evaluator: ev})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	edges, _ := db.ListEdgesFrom(ctx, "F-A")
	if edges[0].Fired != "true" {
		t.Fatalf("predicate edge not fired: %+v", edges[0])
	}
	if edges[0].FiredAgainst == "" {
		t.Fatalf("FiredAgainst empty: %+v", edges[0])
	}
}

func TestTick_PredicateFalseSkipsEdge(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	seedMerged(t, db, "F-A")
	seedPlanned(t, db, "F-B", "server")

	if err := db.UpsertEdges(ctx, "m-1", []state.EdgeRow{{
		ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
		PredicateCEL: `out.severity == "high"`,
	}}); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}
	if _, err := db.AppendOutput(ctx, "F-A", json.RawMessage(`{"severity":"low"}`)); err != nil {
		t.Fatalf("AppendOutput: %v", err)
	}

	ev := newFakeEvaluator()
	rows, _ := db.ListEdgesFrom(ctx, "F-A")
	ev.rules[rows[0].ID] = fakeRule{fired: false, reason: "predicate=false"}

	sch := New(db, Config{Evaluator: ev})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	edges, _ := db.ListEdgesFrom(ctx, "F-A")
	if edges[0].Fired != "false" {
		t.Fatalf("predicate=false edge should fire=false: %+v", edges[0])
	}
}

func TestTick_EdgeEvalFailureLogsAndContinues(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	seedMerged(t, db, "F-A")
	seedMerged(t, db, "F-C")
	seedPlanned(t, db, "F-B", "server")
	seedPlanned(t, db, "F-D", "server")

	// F-A's edge throws an eval error; F-C's edge is unconditional.
	// The bad edge must not halt evaluation of the good one.
	if err := db.UpsertEdges(ctx, "m-1", []state.EdgeRow{
		{ProgramID: "m-1", FromID: "F-A", ToID: "F-B", PredicateCEL: `???invalid`},
		{ProgramID: "m-1", FromID: "F-C", ToID: "F-D"},
	}); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}
	if _, err := db.AppendOutput(ctx, "F-A", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("AppendOutput A: %v", err)
	}
	if _, err := db.AppendOutput(ctx, "F-C", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("AppendOutput C: %v", err)
	}

	ev := newFakeEvaluator()
	aRows, _ := db.ListEdgesFrom(ctx, "F-A")
	ev.rules[aRows[0].ID] = fakeRule{err: errors.New("boom")}

	sch := New(db, Config{Evaluator: ev})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Good edge fired despite bad sibling.
	cEdges, _ := db.ListEdgesFrom(ctx, "F-C")
	if cEdges[0].Fired != "true" {
		t.Fatalf("good edge not fired: %+v", cEdges[0])
	}
	// Bad edge must be marked false so the tick doesn't re-evaluate
	// it forever; spec §3.9 records eval errors as fired=false.
	aEdges, _ := db.ListEdgesFrom(ctx, "F-A")
	if aEdges[0].Fired != "false" {
		t.Fatalf("bad edge expected fired=false, got %+v", aEdges[0])
	}
}

// Regatta#98 regression: partial-tick crash must not refire the default on replay.
func TestTick_DefaultFallbackDoesNotRefireAfterPartialTick(t *testing.T) {
	ctx := context.Background()
	realDB := statetest.OpenDB(t)
	wdb := newWrappingDB(realDB)

	seedMerged(t, realDB, "F-1")
	seedPlanned(t, realDB, "T-A", "server")
	seedPlanned(t, realDB, "T-B", "server")
	seedPlanned(t, realDB, "T-D", "server")

	if err := realDB.UpsertEdges(ctx, "m-1", []state.EdgeRow{
		{ProgramID: "m-1", FromID: "F-1", ToID: "T-A", PredicateCEL: `x > 0`},
		{ProgramID: "m-1", FromID: "F-1", ToID: "T-B", PredicateCEL: `y > 0`},
		{ProgramID: "m-1", FromID: "F-1", ToID: "T-D", IsDefault: true},
	}); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}
	if _, err := realDB.AppendOutput(ctx, "F-1", json.RawMessage(`{"x":5,"y":0}`)); err != nil {
		t.Fatalf("AppendOutput: %v", err)
	}

	edges, err := realDB.ListEdgesFrom(ctx, "F-1")
	if err != nil {
		t.Fatalf("ListEdgesFrom seed: %v", err)
	}
	var idE1, idE2, idD int64
	for _, e := range edges {
		switch e.ToID {
		case "T-A":
			idE1 = e.ID
		case "T-B":
			idE2 = e.ID
		case "T-D":
			idD = e.ID
			wdb.defaultIDs[e.ID] = true
		}
	}

	ev := newFakeEvaluator()
	ev.rules[idE1] = fakeRule{fired: true, reason: "x>0"}
	ev.rules[idE2] = fakeRule{fired: false, reason: "y==0"}

	sch := newWithDB(wdb, Config{Evaluator: ev})

	wdb.markEdgeFiredHook = failAfterNMarkHook(1)
	_, _ = sch.Tick(ctx)

	edges, _ = realDB.ListEdgesFrom(ctx, "F-1")
	state1 := map[int64]string{}
	for _, e := range edges {
		state1[e.ID] = e.Fired
	}
	if state1[idE1] != "true" {
		t.Fatalf("tick1: E1 fired=%q want true", state1[idE1])
	}
	if state1[idE2] != "pending" {
		t.Fatalf("tick1: E2 fired=%q want pending (crash before write)", state1[idE2])
	}
	if state1[idD] != "pending" {
		t.Fatalf("tick1: D fired=%q want pending", state1[idD])
	}

	wdb.markEdgeFiredHook = passThroughHook()
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("tick2: %v", err)
	}

	edges, _ = realDB.ListEdgesFrom(ctx, "F-1")
	state2 := map[int64]string{}
	for _, e := range edges {
		state2[e.ID] = e.Fired
	}
	if state2[idE1] != "true" {
		t.Fatalf("tick2: E1 fired=%q want true (unchanged)", state2[idE1])
	}
	if state2[idE2] != "false" {
		t.Fatalf("tick2: E2 fired=%q want false", state2[idE2])
	}
	if state2[idD] != "pending" {
		t.Fatalf("tick2: D fired=%q want pending — default must not refire after partial-tick crash", state2[idD])
	}
	if c := wdb.defaultFireCount(); c != 0 {
		t.Fatalf("default fired %d times across recovery; want 0", c)
	}
}

// Randomised crash-injection invariant: default fires iff canonical trace would; spec §5.3 A-tier.
func TestTick_DefaultFallbackInvariantUnderRandomCrashInjection(t *testing.T) {
	const trials = 200
	rng := rand.New(rand.NewPCG(0xDEFEA7, 0xDEFEA7)) //nolint:gosec // G404: deterministic seed for reproducible crash-injection trials, not crypto

	for trial := range trials {
		t.Run(fmt.Sprintf("trial=%d", trial), func(t *testing.T) {
			ctx := context.Background()
			realDB := statetest.OpenDB(t)
			wdb := newWrappingDB(realDB)

			seedMerged(t, realDB, "F-1")
			seedPlanned(t, realDB, "T-A", "server")
			seedPlanned(t, realDB, "T-B", "server")
			seedPlanned(t, realDB, "T-D", "server")

			if err := realDB.UpsertEdges(ctx, "m-1", []state.EdgeRow{
				{ProgramID: "m-1", FromID: "F-1", ToID: "T-A", PredicateCEL: `a`},
				{ProgramID: "m-1", FromID: "F-1", ToID: "T-B", PredicateCEL: `b`},
				{ProgramID: "m-1", FromID: "F-1", ToID: "T-D", IsDefault: true},
			}); err != nil {
				t.Fatalf("UpsertEdges: %v", err)
			}
			if _, err := realDB.AppendOutput(ctx, "F-1", json.RawMessage(`{}`)); err != nil {
				t.Fatalf("AppendOutput: %v", err)
			}

			eA := rng.IntN(2) == 1
			eB := rng.IntN(2) == 1
			expectDefault := !eA && !eB

			edges, _ := realDB.ListEdgesFrom(ctx, "F-1")
			var idA, idB int64
			for _, e := range edges {
				switch e.ToID {
				case "T-A":
					idA = e.ID
				case "T-B":
					idB = e.ID
				case "T-D":
					wdb.defaultIDs[e.ID] = true
				}
			}

			ev := newFakeEvaluator()
			ev.rules[idA] = fakeRule{fired: eA}
			ev.rules[idB] = fakeRule{fired: eB}

			sch := newWithDB(wdb, Config{Evaluator: ev})

			// Crash at a random successful-write count in [0, 3).
			crashAt := rng.IntN(3)
			wdb.markEdgeFiredHook = failAfterNMarkHook(crashAt)
			_, _ = sch.Tick(ctx)

			// Replay-to-fixpoint with pass-through.
			wdb.markEdgeFiredHook = passThroughHook()
			for i := range 5 {
				if _, err := sch.Tick(ctx); err != nil {
					t.Fatalf("replay tick %d: %v", i, err)
				}
			}

			got := wdb.defaultFireCount()
			want := 0
			if expectDefault {
				want = 1
			}
			if got != want {
				t.Fatalf("trial eA=%v eB=%v crashAt=%d: defaultFireCount=%d want %d",
					eA, eB, crashAt, got, want)
			}

			edges, _ = realDB.ListEdgesFrom(ctx, "F-1")
			var dFired string
			for _, e := range edges {
				if e.IsDefault {
					dFired = e.Fired
				}
			}
			if expectDefault && dFired != "true" {
				t.Fatalf("expected default fired=true after replay, got %q", dFired)
			}
			if !expectDefault && dFired == "true" {
				t.Fatalf("default fired=true but at least one non-default was true (eA=%v eB=%v)", eA, eB)
			}
		})
	}
}

func TestTick_EdgeEvalSkippedNoJournal(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	seedMerged(t, db, "F-A")
	seedPlanned(t, db, "F-B", "server")

	if err := db.UpsertEdges(ctx, "m-1", []state.EdgeRow{{
		ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
	}}); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}
	// No AppendOutput — journal absent.

	sch := New(db, Config{Evaluator: newFakeEvaluator()})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	edges, _ := db.ListEdgesFrom(ctx, "F-A")
	if edges[0].Fired != "pending" {
		t.Fatalf("expected pending on missing journal, got %+v", edges[0])
	}
}

// Lone default (no predicated siblings) must stay pending — spec §3.3 rule 2c, line 83.
func TestTick_DefaultDoesNotFireWithoutNonDefaultSiblings(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	seedMerged(t, db, "F-1")
	seedPlanned(t, db, "T-D", "server")

	if err := db.UpsertEdges(ctx, "m-1", []state.EdgeRow{
		{ProgramID: "m-1", FromID: "F-1", ToID: "T-D", IsDefault: true},
	}); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}
	if _, err := db.AppendOutput(ctx, "F-1", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("AppendOutput: %v", err)
	}

	sch := New(db, Config{Evaluator: newFakeEvaluator()})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	edges, _ := db.ListEdgesFrom(ctx, "F-1")
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d", len(edges))
	}
	if edges[0].Fired != "pending" {
		t.Fatalf("lone default fired=%q want pending — spec disallows lone defaults", edges[0].Fired)
	}
}

// TestScheduler_Tick_EmitsEdgeFiredEvent pins spec §5.2: edge.fired routes through Config.Logger with from_id/to_id/edge_id.
func TestScheduler_Tick_EmitsEdgeFiredEvent(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	seedMerged(t, db, "F-A")
	seedPlanned(t, db, "F-B", "server")

	if err := db.UpsertEdges(ctx, "m-1", []state.EdgeRow{{
		ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
	}}); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}
	if _, err := db.AppendOutput(ctx, "F-A", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("AppendOutput: %v", err)
	}

	h := obstest.New()
	sch := New(db, Config{
		Evaluator: newFakeEvaluator(),
		Logger:    slog.New(h),
	})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	r, ok := h.FindEvent(obs.EventEdgeFired)
	if !ok {
		records := h.Records()
		msgs := make([]string, 0, len(records))
		for _, rec := range records {
			msgs = append(msgs, rec.Message)
		}
		t.Fatalf("captured logger missing %q event; got %v", obs.EventEdgeFired, msgs)
	}
	for _, key := range []string{string(obs.KeyFromID), string(obs.KeyToID), string(obs.KeyEdgeID)} {
		if !recordHasAttr(r, key) {
			t.Errorf("edge.fired record missing %q attr", key)
		}
	}
}

// TestScheduler_GateRecheckAtReservation_PendingAgentBlocked pins that an orphan pending agent re-checks the gate before reservation (#167).
func TestScheduler_GateRecheckAtReservation_PendingAgentBlocked(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "wi-1", "prod")

	t1 := New(db, Config{LaneCaps: map[string]int{"prod": 0}})
	if _, err := t1.Tick(ctx); err != nil {
		t.Fatalf("T1 Tick: %v", err)
	}
	pending, err := db.ListAgentsByState(ctx, state.AgentPending)
	if err != nil {
		t.Fatalf("ListAgentsByState pending: %v", err)
	}
	if len(pending) != 1 || pending[0].WorkItemID != "wi-1" {
		t.Fatalf("after T1 pending=%+v; want one pending agent for wi-1", pending)
	}

	gate := &fakeGate{verdicts: map[string]approval.Result{"wi-1": approval.ResultPause}}
	t2 := New(db, Config{
		Gate:         gate,
		GateResolver: gateResolverByID(map[string]approval.Config{"wi-1": gateCfgFor("prod-gate")}),
	})
	ids, err := t2.Tick(ctx)
	if err != nil {
		t.Fatalf("T2 Tick: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("T2 reserved=%v; want 0 (gate paused the orphan)", ids)
	}
	if len(gate.calls) == 0 {
		t.Fatalf("gate.calls=%+v; want >=1 — reservation loop must re-check the gate", gate.calls)
	}
	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 0 {
		t.Fatalf("spawning=%+v; want 0 (paused wi must not transition out of pending)", spawning)
	}
	stillPending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(stillPending) != 1 || stillPending[0].WorkItemID != "wi-1" {
		t.Fatalf("after T2 pending=%+v; want wi-1 still pending", stillPending)
	}
}

// TestScheduler_GateRecheckAtReservation_IntegrationFlow pins approve-then-tick: a paused orphan spawns once the next tick's gate flips to proceed (#167). Sequential by design; concurrent gate flips are covered at the state-DB layer.
func TestScheduler_GateRecheckAtReservation_IntegrationFlow(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "wi-race", "prod")

	t1 := New(db, Config{LaneCaps: map[string]int{"prod": 0}})
	if _, err := t1.Tick(ctx); err != nil {
		t.Fatalf("T1 Tick: %v", err)
	}

	gate := &fakeGate{verdicts: map[string]approval.Result{"wi-race": approval.ResultPause}}
	cfg := Config{
		Gate:         gate,
		GateResolver: gateResolverByID(map[string]approval.Config{"wi-race": gateCfgFor("prod-gate")}),
	}
	sch := New(db, cfg)

	if ids, err := sch.Tick(ctx); err != nil {
		t.Fatalf("T2 Tick: %v", err)
	} else if len(ids) != 0 {
		t.Fatalf("T2 reserved=%v; want 0 (still paused)", ids)
	}

	gate.verdicts["wi-race"] = approval.ResultProceed
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("T3 Tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("T3 reserved=%v; want 1 (approval granted)", ids)
	}
	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 1 || spawning[0].WorkItemID != "wi-race" {
		t.Fatalf("spawning=%+v; want one spawning agent for wi-race", spawning)
	}
}
