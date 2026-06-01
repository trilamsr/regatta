package state

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openOrphanTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), DSN(filepath.Join(t.TempDir(), "o.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedOrphanWI(t *testing.T, db *DB, id string, status WorkItemStatus) {
	t.Helper()
	w := WorkItem{ID: id, Kind: KindFeature, Title: id, Lane: "server", Status: status}
	if err := db.UpsertWorkItem(context.Background(), w, SourceBrief, time.Now()); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// TestListWorkItemsWithJournalNotMerged_ReturnsOrphans pins the #99 detection contract.
func TestListWorkItemsWithJournalNotMerged_ReturnsOrphans(t *testing.T) {
	ctx := context.Background()
	db := openOrphanTestDB(t)

	// F-orphan: journal present, status=planned (crashed mid-Complete).
	seedOrphanWI(t, db, "F-orphan", WorkStatusPlanned)
	if _, err := db.AppendOutput(ctx, "F-orphan", json.RawMessage(`{"v":1}`)); err != nil {
		t.Fatalf("AppendOutput F-orphan: %v", err)
	}

	// F-merged: clean completion (journal + status=merged) — must NOT appear.
	seedOrphanWI(t, db, "F-merged", WorkStatusPlanned)
	if _, err := db.AppendOutput(ctx, "F-merged", json.RawMessage(`{"v":2}`)); err != nil {
		t.Fatalf("AppendOutput F-merged: %v", err)
	}
	wi, _ := db.GetWorkItem(ctx, "F-merged")
	wi.Status = WorkStatusMerged
	if err := db.UpsertWorkItem(ctx, wi, wi.Source, time.Now()); err != nil {
		t.Fatalf("merge F-merged: %v", err)
	}

	// F-planned: no journal at all — must NOT appear.
	seedOrphanWI(t, db, "F-planned", WorkStatusPlanned)

	// F-archived: journal present but row archived — must NOT appear
	// (re-flipping would resurrect a tombstoned item).
	seedOrphanWI(t, db, "F-archived", WorkStatusPlanned)
	if _, err := db.AppendOutput(ctx, "F-archived", json.RawMessage(`{"v":3}`)); err != nil {
		t.Fatalf("AppendOutput F-archived: %v", err)
	}
	wi, _ = db.GetWorkItem(ctx, "F-archived")
	wi.Status = WorkStatusArchived
	if err := db.UpsertWorkItem(ctx, wi, wi.Source, time.Now()); err != nil {
		t.Fatalf("archive F-archived: %v", err)
	}

	got, err := db.ListWorkItemsWithJournalNotMerged(ctx)
	if err != nil {
		t.Fatalf("ListWorkItemsWithJournalNotMerged: %v", err)
	}
	if len(got) != 1 || got[0] != "F-orphan" {
		t.Fatalf("orphans=%v, want [F-orphan]", got)
	}
}

// TestListWorkItemsWithJournalNotMerged_Empty pins the no-orphans path.
func TestListWorkItemsWithJournalNotMerged_Empty(t *testing.T) {
	ctx := context.Background()
	db := openOrphanTestDB(t)
	got, err := db.ListWorkItemsWithJournalNotMerged(ctx)
	if err != nil {
		t.Fatalf("ListWorkItemsWithJournalNotMerged: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("orphans=%v, want empty", got)
	}
}
