package state

import (
	"context"
	"testing"
	"time"
)

// UpsertWorkItem, TombstoneBySource, and CascadeArchiveChildren take
// an explicit at so production writers (AdapterSync, BriefLoader)
// align rows with a poll-start tick instead of racing on the DB's
// constructor-bound clock.

func TestUpsertWorkItem_UsesProvidedTimestamp(t *testing.T) {
	db := newWorkItemsTestDB(t)
	ctx := context.Background()

	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	item := WorkItem{ID: "F-AT", Kind: KindFeature, Title: "at", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, item, SourceAdapter, at); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}

	got, err := db.GetWorkItem(ctx, "F-AT")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if got.LastSeenAt.Unix() != at.Unix() {
		t.Fatalf("LastSeenAt=%d want %d (must use caller-supplied at)", got.LastSeenAt.Unix(), at.Unix())
	}
	if got.UpdatedAt.Unix() != at.Unix() {
		t.Fatalf("UpdatedAt=%d want %d", got.UpdatedAt.Unix(), at.Unix())
	}
	if got.CreatedAt.Unix() != at.Unix() {
		t.Fatalf("CreatedAt=%d want %d (insert path stamps created_at from at)", got.CreatedAt.Unix(), at.Unix())
	}
}

func TestTombstoneBySource_UsesProvidedCutoff(t *testing.T) {
	db := newWorkItemsTestDB(t)
	ctx := context.Background()

	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	item := WorkItem{ID: "F-TOMB", Kind: KindFeature, Title: "tomb", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, item, SourceBrief, t0); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}

	archived, err := db.TombstoneBySource(ctx, SourceBrief, t0.Add(time.Second))
	if err != nil {
		t.Fatalf("TombstoneBySource: %v", err)
	}
	if len(archived) != 1 || archived[0] != "F-TOMB" {
		t.Fatalf("archived=%v want [F-TOMB]", archived)
	}

	got, _ := db.GetWorkItem(ctx, "F-TOMB")
	if got.Status != WorkStatusArchived {
		t.Fatalf("F-TOMB.status=%s want archived", got.Status)
	}
}

func TestCascadeArchiveChildren_UsesProvidedTimestamp(t *testing.T) {
	db := newWorkItemsTestDB(t)
	ctx := context.Background()

	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	parent := WorkItem{ID: "PROG-AT", Kind: KindProgram, Title: "p", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, parent, SourceAdapter, at); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"F-AT-1", "F-AT-2"} {
		child := WorkItem{ID: id, Kind: KindFeature, Title: id, Lane: "server",
			Status: WorkStatusRunning, ParentProgramID: "PROG-AT"}
		if err := db.UpsertWorkItem(ctx, child, SourceBrief, at); err != nil {
			t.Fatal(err)
		}
	}

	cascadeAt := at.Add(time.Hour)
	archived, err := db.CascadeArchiveChildren(ctx, "PROG-AT", cascadeAt)
	if err != nil {
		t.Fatalf("CascadeArchiveChildren: %v", err)
	}
	if len(archived) != 2 {
		t.Fatalf("archived=%v want 2 ids", archived)
	}
	for _, id := range archived {
		got, err := db.GetWorkItem(ctx, id)
		if err != nil {
			t.Fatalf("GetWorkItem %s: %v", id, err)
		}
		if got.UpdatedAt.Unix() != cascadeAt.Unix() {
			t.Fatalf("%s.UpdatedAt=%d want %d (must use caller-supplied at)", id, got.UpdatedAt.Unix(), cascadeAt.Unix())
		}
		if got.Status != WorkStatusArchived {
			t.Fatalf("%s.status=%s want archived", id, got.Status)
		}
	}
}

func TestCascadeArchiveChildren_ReturnsNewlyArchivedIDs(t *testing.T) {
	db := newWorkItemsTestDB(t)
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	parent := WorkItem{ID: "PROG-N", Kind: KindProgram, Title: "p", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, parent, SourceAdapter, t0); err != nil {
		t.Fatal(err)
	}
	live := []string{"F-N-1", "F-N-2"}
	for _, id := range live {
		c := WorkItem{ID: id, Kind: KindFeature, Title: id, Lane: "server",
			Status: WorkStatusRunning, ParentProgramID: "PROG-N"}
		if err := db.UpsertWorkItem(ctx, c, SourceBrief, t0); err != nil {
			t.Fatal(err)
		}
	}
	already := WorkItem{ID: "F-N-DONE", Kind: KindFeature, Title: "done", Lane: "server",
		Status: WorkStatusArchived, ParentProgramID: "PROG-N"}
	if err := db.UpsertWorkItem(ctx, already, SourceBrief, t0); err != nil {
		t.Fatal(err)
	}

	archived, err := db.CascadeArchiveChildren(ctx, "PROG-N", t0.Add(time.Minute))
	if err != nil {
		t.Fatalf("CascadeArchiveChildren: %v", err)
	}
	if len(archived) != 2 {
		t.Fatalf("archived=%v want 2 (the live pair, not the already-archived row)", archived)
	}
	seen := map[string]bool{}
	for _, id := range archived {
		seen[id] = true
	}
	for _, want := range live {
		if !seen[want] {
			t.Fatalf("missing %s in archived=%v", want, archived)
		}
	}
	if seen["F-N-DONE"] {
		t.Fatalf("already-archived F-N-DONE leaked into return slice: %v", archived)
	}
}
