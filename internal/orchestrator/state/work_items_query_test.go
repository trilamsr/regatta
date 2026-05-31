// External _test package to avoid import cycle: this file needs
// orchestrator.ErrCycleDetected (re-exporter), and orchestrator
// imports state. By living in package state_test we sit outside the
// internal package graph.
package state_test

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator"
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

func idsOf(items []state.WorkItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	sort.Strings(out)
	return out
}

func TestListSpawnable_NoDeps_ReturnsAllPlanned(t *testing.T) {
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
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
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
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
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	w := state.WorkItem{ID: "F-arch", Kind: state.KindFeature, Title: "x", Lane: "server", Status: state.WorkStatusArchived}
	if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}
	got, _ := db.ListSpawnable(ctx)
	if len(got) != 0 {
		t.Fatalf("got %v want [] (archived must not appear)", idsOf(got))
	}
}

func TestCycleCheck_RejectsCycle(t *testing.T) {
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

	a := state.WorkItem{ID: "F-A", Kind: state.KindFeature, Title: "a", Lane: "server",
		Status: state.WorkStatusPlanned, DependsOnFeatures: []string{"F-B"}}
	if err := db.UpsertWorkItem(ctx, a, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}

	b := state.WorkItem{ID: "F-B", Kind: state.KindFeature, Title: "b", Lane: "server",
		Status: state.WorkStatusPlanned, DependsOnFeatures: []string{"F-A"}}
	err := db.CycleCheck(ctx, b)
	if !errors.Is(err, orchestrator.ErrCycleDetected) {
		t.Fatalf("err=%v want ErrCycleDetected", err)
	}
}

func TestCycleCheck_AllowsAcyclicAddition(t *testing.T) {
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

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
	if !errors.Is(err, orchestrator.ErrCycleDetected) {
		t.Fatalf("err=%v want ErrCycleDetected", err)
	}
}

func TestListByParent_ReturnsChildrenInIDOrder(t *testing.T) {
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
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
