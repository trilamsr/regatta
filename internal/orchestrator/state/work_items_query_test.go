// External _test package: keeps the state package import surface
// honest by exercising state from the outside, matching how every
// production caller will see it.
package state_test

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func newQueryTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "q.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// queryTestDBAt opens a query-test DB with its clock pinned to t0 so
// callers don't need to thread a time argument through helper paths
// that touch agents/locks/events (those still consult d.now). The
// returned DB still requires explicit at on UpsertWorkItem.
func queryTestDBAt(t *testing.T, t0 time.Time) *state.DB {
	t.Helper()
	db, err := state.OpenWithClock(context.Background(), state.DSN(filepath.Join(t.TempDir(), "q.db")), func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func idsOf(items []state.WorkItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	sort.Strings(out)
	return out
}

func TestListSpawnable_NoDeps_ReturnsAllPlanned(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := queryTestDBAt(t, now)
	ctx := context.Background()
	for _, id := range []string{"F-1", "F-2", "F-3"} {
		w := state.WorkItem{ID: id, Kind: state.KindFeature, Title: id, Lane: "server", Status: state.WorkStatusPlanned}
		if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, now); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListSpawnable(ctx)
	if err != nil {
		t.Fatalf("ListSpawnable: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3 (got IDs %v)", len(got), idsOf(got))
	}
}

func TestListSpawnable_DepBlockedUntilMerged(t *testing.T) {
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	c1 := state.WorkItem{ID: "F-1", Kind: state.KindFeature, Title: "c1", Lane: "server", Status: state.WorkStatusPlanned}
	c2 := state.WorkItem{ID: "F-2", Kind: state.KindFeature, Title: "c2", Lane: "server", Status: state.WorkStatusPlanned,
		DependsOnFeatures: []string{"F-1"}}
	if err := db.UpsertWorkItem(ctx, c1, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkItem(ctx, c2, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}

	got, _ := db.ListSpawnable(ctx)
	if len(got) != 1 || got[0].ID != "F-1" {
		t.Fatalf("first round: got %v want [F-1]", idsOf(got))
	}

	c1.Status = state.WorkStatusMerged
	if err := db.UpsertWorkItem(ctx, c1, state.SourceBrief, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	got, _ = db.ListSpawnable(ctx)
	if len(got) != 1 || got[0].ID != "F-2" {
		t.Fatalf("after merge: got %v want [F-2]", idsOf(got))
	}
}

func TestListSpawnable_ExcludesAlreadyReserved(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := queryTestDBAt(t, now)
	ctx := context.Background()
	w := state.WorkItem{ID: "F-1", Kind: state.KindFeature, Title: "x", Lane: "server", Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}
	// Simulate a reservation already in the agents table via the
	// existing UpsertPending helper.
	if _, err := db.UpsertPending(ctx, "F-1", "server"); err != nil {
		t.Fatal(err)
	}
	got, _ := db.ListSpawnable(ctx)
	if len(got) != 0 {
		t.Fatalf("got %v want [] (agent exists -> not spawnable)", idsOf(got))
	}
}

func TestListSpawnable_ExcludesArchived(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := queryTestDBAt(t, now)
	ctx := context.Background()
	w := state.WorkItem{ID: "F-arch", Kind: state.KindFeature, Title: "x", Lane: "server", Status: state.WorkStatusArchived}
	if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}
	got, _ := db.ListSpawnable(ctx)
	if len(got) != 0 {
		t.Fatalf("got %v want [] (archived must not appear)", idsOf(got))
	}
}

func TestListSpawnable_ExcludesBlocked(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := queryTestDBAt(t, now)
	ctx := context.Background()
	w := state.WorkItem{ID: "F-block", Kind: state.KindFeature, Title: "x", Lane: "server", Status: state.WorkStatusBlocked}
	if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}
	got, _ := db.ListSpawnable(ctx)
	if len(got) != 0 {
		t.Fatalf("got %d want 0 (blocked rows must not appear)", len(got))
	}
}

func TestCycleCheck_RejectsCycle(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := queryTestDBAt(t, now)
	ctx := context.Background()

	a := state.WorkItem{ID: "F-A", Kind: state.KindFeature, Title: "a", Lane: "server",
		Status: state.WorkStatusPlanned, DependsOnFeatures: []string{"F-B"}}
	if err := db.UpsertWorkItem(ctx, a, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}

	b := state.WorkItem{ID: "F-B", Kind: state.KindFeature, Title: "b", Lane: "server",
		Status: state.WorkStatusPlanned, DependsOnFeatures: []string{"F-A"}}
	err := db.CycleCheck(ctx, b)
	if !errors.Is(err, state.ErrCycleDetected) {
		t.Fatalf("err=%v want ErrCycleDetected", err)
	}
}

func TestCycleCheck_AllowsAcyclicAddition(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := queryTestDBAt(t, now)
	ctx := context.Background()

	a := state.WorkItem{ID: "F-A", Kind: state.KindFeature, Title: "a", Lane: "server",
		Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, a, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}

	b := state.WorkItem{ID: "F-B", Kind: state.KindFeature, Title: "b", Lane: "server",
		Status: state.WorkStatusPlanned, DependsOnFeatures: []string{"F-A"}}
	if err := db.CycleCheck(ctx, b); err != nil {
		t.Fatalf("CycleCheck rejected acyclic candidate: %v", err)
	}
}

func TestCycleCheck_RejectsSelfLoop(t *testing.T) {
	db := newQueryTestDB(t)
	ctx := context.Background()
	w := state.WorkItem{ID: "F-S", Kind: state.KindFeature, Title: "self", Lane: "server",
		Status: state.WorkStatusPlanned, DependsOnFeatures: []string{"F-S"}}
	err := db.CycleCheck(ctx, w)
	if !errors.Is(err, state.ErrCycleDetected) {
		t.Fatalf("err=%v want ErrCycleDetected", err)
	}
}

// TestListArchivedProgramsWithLiveChildren_ReturnsOrphanedParents asserts archived programs with non-cascaded live children surface for AdapterSync reconciliation.
func TestListArchivedProgramsWithLiveChildren_ReturnsOrphanedParents(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := queryTestDBAt(t, now)
	ctx := context.Background()

	// PROG-DEAD: archived parent, one live + one archived child. Must surface.
	dead := state.WorkItem{ID: "PROG-DEAD", Kind: state.KindProgram, Title: "d", Lane: "server", Status: state.WorkStatusArchived}
	if err := db.UpsertWorkItem(ctx, dead, state.SourceAdapter, now); err != nil {
		t.Fatal(err)
	}
	live := state.WorkItem{ID: "C-LIVE", Kind: state.KindFeature, Title: "l", Lane: "server",
		Status: state.WorkStatusRunning, ParentProgramID: "PROG-DEAD"}
	if err := db.UpsertWorkItem(ctx, live, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}
	gone := state.WorkItem{ID: "C-GONE", Kind: state.KindFeature, Title: "g", Lane: "server",
		Status: state.WorkStatusArchived, ParentProgramID: "PROG-DEAD"}
	if err := db.UpsertWorkItem(ctx, gone, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}

	// PROG-CLEAN: archived parent, all children archived. Must NOT surface.
	clean := state.WorkItem{ID: "PROG-CLEAN", Kind: state.KindProgram, Title: "c", Lane: "server", Status: state.WorkStatusArchived}
	if err := db.UpsertWorkItem(ctx, clean, state.SourceAdapter, now); err != nil {
		t.Fatal(err)
	}
	cchild := state.WorkItem{ID: "C-ARCH", Kind: state.KindFeature, Title: "ca", Lane: "server",
		Status: state.WorkStatusArchived, ParentProgramID: "PROG-CLEAN"}
	if err := db.UpsertWorkItem(ctx, cchild, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}

	// PROG-ALIVE: parent still planned, live children. Must NOT surface (parent not archived).
	alive := state.WorkItem{ID: "PROG-ALIVE", Kind: state.KindProgram, Title: "a", Lane: "server", Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, alive, state.SourceAdapter, now); err != nil {
		t.Fatal(err)
	}
	achild := state.WorkItem{ID: "C-ALIVE", Kind: state.KindFeature, Title: "aa", Lane: "server",
		Status: state.WorkStatusRunning, ParentProgramID: "PROG-ALIVE"}
	if err := db.UpsertWorkItem(ctx, achild, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}

	got, err := db.ListArchivedProgramsWithLiveChildren(ctx)
	if err != nil {
		t.Fatalf("ListArchivedProgramsWithLiveChildren: %v", err)
	}
	if len(got) != 1 || got[0] != "PROG-DEAD" {
		t.Fatalf("got %v want [PROG-DEAD] (only archived programs with live children must surface)", got)
	}
}

func TestListByParent_ReturnsChildrenInIDOrder(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := queryTestDBAt(t, now)
	ctx := context.Background()
	for _, id := range []string{"F-3", "F-1", "F-2"} {
		w := state.WorkItem{ID: id, Kind: state.KindFeature, Title: id, Lane: "server",
			Status: state.WorkStatusPlanned, ParentProgramID: "PROG-1"}
		if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, now); err != nil {
			t.Fatal(err)
		}
	}
	// Orphan with different parent — must NOT appear.
	other := state.WorkItem{ID: "F-OTHER", Kind: state.KindFeature, Title: "o", Lane: "server",
		Status: state.WorkStatusPlanned, ParentProgramID: "PROG-2"}
	if err := db.UpsertWorkItem(ctx, other, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}

	got, err := db.ListByParent(ctx, "PROG-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	for i, want := range []string{"F-1", "F-2", "F-3"} {
		if got[i].ID != want {
			t.Fatalf("got[%d].ID=%s want %s (must be id-sorted)", i, got[i].ID, want)
		}
	}
}

func TestMaxUpdatedAtForBriefChildren_Empty(t *testing.T) {
	db := newQueryTestDB(t)
	got, err := db.MaxUpdatedAtForBriefChildren(context.Background(), "PROG-NONE")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !got.IsZero() {
		t.Fatalf("got=%v want zero", got)
	}
}

// TestListSpawnable_HonoursFiredEdges asserts a planned row with an inbound edge is non-spawnable until MarkEdgeFired flips it (W4-A).
func TestListSpawnable_HonoursFiredEdges(t *testing.T) {
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	scan := state.WorkItem{ID: "F-SCAN", Kind: state.KindFeature, Title: "scan", Lane: "server", Status: state.WorkStatusMerged}
	if err := db.UpsertWorkItem(ctx, scan, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}
	deep := state.WorkItem{ID: "F-DEEP", Kind: state.KindFeature, Title: "deep", Lane: "server", Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, deep, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertEdges(ctx, "m-1", []state.EdgeRow{{
		ProgramID: "m-1", FromID: "F-SCAN", ToID: "F-DEEP",
		PredicateCEL: `out.severity == "high"`, OnSkip: "cascade",
	}}); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}
	rows, err := db.ListSpawnable(ctx)
	if err != nil {
		t.Fatalf("ListSpawnable: %v", err)
	}
	for _, r := range rows {
		if r.ID == "F-DEEP" {
			t.Fatalf("F-DEEP spawnable while edge pending")
		}
	}
	edges, err := db.ListEdgesFrom(ctx, "F-SCAN")
	if err != nil {
		t.Fatalf("ListEdgesFrom: %v", err)
	}
	if err := db.MarkEdgeFired(ctx, edges[0].ID, "true", "sha-x"); err != nil {
		t.Fatalf("MarkEdgeFired: %v", err)
	}
	rows, err = db.ListSpawnable(ctx)
	if err != nil {
		t.Fatalf("ListSpawnable #2: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.ID == "F-DEEP" {
			found = true
		}
	}
	if !found {
		t.Fatalf("F-DEEP must be spawnable once edge fired; got %v", idsOf(rows))
	}
}

// TestListSpawnable_DefaultNextOnAllFalse asserts a default-next target spawns when all inbound edges resolved and at least one carries on_skip='ignore'.
func TestListSpawnable_DefaultNextOnAllFalse(t *testing.T) {
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	scan := state.WorkItem{ID: "F-SCAN", Kind: state.KindFeature, Title: "scan", Lane: "server", Status: state.WorkStatusMerged}
	if err := db.UpsertWorkItem(ctx, scan, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}
	quick := state.WorkItem{ID: "F-QUICK", Kind: state.KindFeature, Title: "quick", Lane: "server", Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, quick, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertEdges(ctx, "m-1", []state.EdgeRow{{
		ProgramID: "m-1", FromID: "F-SCAN", ToID: "F-QUICK",
		PredicateCEL: `out.severity == "high"`, OnSkip: "cascade", IsDefault: true,
	}}); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}
	edges, err := db.ListEdgesFrom(ctx, "F-SCAN")
	if err != nil {
		t.Fatalf("ListEdgesFrom: %v", err)
	}
	if err := db.MarkEdgeFired(ctx, edges[0].ID, "true", "sha-y"); err != nil {
		t.Fatalf("MarkEdgeFired: %v", err)
	}
	rows, err := db.ListSpawnable(ctx)
	if err != nil {
		t.Fatalf("ListSpawnable: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "F-QUICK" {
		t.Fatalf("default-next path: got %v want [F-QUICK]", idsOf(rows))
	}
}

func TestMaxUpdatedAtForBriefChildren_ReturnsMax(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := newQueryTestDB(t)
	// Two brief children stamped at different times.
	w1 := state.WorkItem{ID: "F-1", Kind: state.KindFeature, Title: "a", Lane: "server",
		Status: state.WorkStatusPlanned, ParentProgramID: "PROG-1"}
	w2 := state.WorkItem{ID: "F-2", Kind: state.KindFeature, Title: "b", Lane: "server",
		Status: state.WorkStatusPlanned, ParentProgramID: "PROG-1"}
	if err := db.UpsertWorkItem(ctx, w1, state.SourceBrief, t0); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkItem(ctx, w2, state.SourceBrief, t0.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	// An adapter row at a later time — must be ignored (source filter).
	wAdapter := state.WorkItem{ID: "P-1", Kind: state.KindProgram, Title: "p", Lane: "server",
		Status: state.WorkStatusPlanned, ParentProgramID: "PROG-1"}
	if err := db.UpsertWorkItem(ctx, wAdapter, state.SourceAdapter, t0.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := db.MaxUpdatedAtForBriefChildren(ctx, "PROG-1")
	if err != nil {
		t.Fatal(err)
	}
	want := t0.Add(2 * time.Second).Truncate(time.Second)
	if !got.Equal(want) {
		t.Fatalf("got=%v want %v", got, want)
	}
}
