package state

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestGetWorkItemsBatch_ChunksAtSQLiteLimit asserts N>SQLITE_MAX_VARIABLE_NUMBER ids return without error.
func TestGetWorkItemsBatch_ChunksAtSQLiteLimit(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()

	const n = 40000
	items := make([]WorkItem, n)
	ids := make([]string, n)
	for i := range items {
		id := fmt.Sprintf("W-%05d", i)
		ids[i] = id
		items[i] = WorkItem{
			ID: id, Kind: KindFeature, Title: "t",
			Lane: "server", Status: WorkStatusPlanned,
		}
	}
	if err := db.BatchUpsertWorkItems(ctx, items, SourceBrief, t0); err != nil {
		t.Fatalf("BatchUpsertWorkItems seed: %v", err)
	}

	got, err := db.GetWorkItemsBatch(ctx, ids)
	if err != nil {
		t.Fatalf("GetWorkItemsBatch(%d ids): %v", n, err)
	}
	if len(got) != n {
		t.Fatalf("got %d items, want %d", len(got), n)
	}
	for _, id := range ids {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing id %s in result", id)
			return
		}
	}
}
