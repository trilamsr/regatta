package state

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestBatchUpsertWorkItems_InsertsAndPreservesCreatedAt pins the per-row UPSERT behaviour: first call inserts; second call updates and leaves 
func TestBatchUpsertWorkItems_InsertsAndPreservesCreatedAt(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()

	items := []WorkItem{
		{ID: "A", Kind: KindFeature, Title: "alpha", Lane: "server", Status: WorkStatusPlanned},
		{ID: "B", Kind: KindFeature, Title: "beta", Lane: "server", Status: WorkStatusPlanned},
	}
	if err := db.BatchUpsertWorkItems(ctx, items, SourceBrief, t0); err != nil {
		t.Fatalf("BatchUpsertWorkItems insert: %v", err)
	}

	t1 := t0.Add(5 * time.Minute)
	items[0].Title = "alpha-updated"
	if err := db.BatchUpsertWorkItems(ctx, items, SourceBrief, t1); err != nil {
		t.Fatalf("BatchUpsertWorkItems update: %v", err)
	}

	got, err := db.GetWorkItem(ctx, "A")
	if err != nil {
		t.Fatalf("GetWorkItem A: %v", err)
	}
	if got.Title != "alpha-updated" {
		t.Fatalf("A.title=%q want alpha-updated", got.Title)
	}
	if got.CreatedAt.Unix() != t0.Unix() {
		t.Fatalf("A.created_at=%d want %d (must preserve)", got.CreatedAt.Unix(), t0.Unix())
	}
	if got.UpdatedAt.Unix() != t1.Unix() {
		t.Fatalf("A.updated_at=%d want %d", got.UpdatedAt.Unix(), t1.Unix())
	}
}

// TestBatchUpsertWorkItems_EmptySliceNoop confirms zero-len input is a no-op — no tx is opened, no error returned.
func TestBatchUpsertWorkItems_EmptySliceNoop(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	if err := db.BatchUpsertWorkItems(context.Background(), nil, SourceBrief, t0); err != nil {
		t.Fatalf("BatchUpsertWorkItems nil: %v", err)
	}
}

// TestBatchUpsertWorkItems_ChunkBoundary writes a slice >batchUpsertChunk to exercise the chunk-loop seam — a row in the second chunk must lan
func TestBatchUpsertWorkItems_ChunkBoundary(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	n := batchUpsertChunk + 5
	items := make([]WorkItem, n)
	for i := range items {
		items[i] = WorkItem{
			ID: fmt.Sprintf("F-%04d", i), Kind: KindFeature,
			Title: "t", Lane: "server", Status: WorkStatusPlanned,
		}
	}
	if err := db.BatchUpsertWorkItems(context.Background(), items, SourceBrief, t0); err != nil {
		t.Fatalf("BatchUpsertWorkItems: %v", err)
	}
	got, err := db.GetWorkItem(context.Background(), fmt.Sprintf("F-%04d", n-1))
	if err != nil {
		t.Fatalf("GetWorkItem last: %v", err)
	}
	if got.Title != "t" {
		t.Fatalf("last row not landed; title=%q", got.Title)
	}
}

// TestBatchUpsertWorkItems_AcceptanceJSONValidation rejects invalid JSON matching UpsertWorkItem's contract.
func TestBatchUpsertWorkItems_AcceptanceJSONValidation(t *testing.T) {
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	items := []WorkItem{
		{ID: "X", Kind: KindFeature, Title: "x", Lane: "server",
			Status: WorkStatusPlanned, AcceptanceJSON: "{not json"},
	}
	if err := db.BatchUpsertWorkItems(context.Background(), items, SourceBrief, t0); err == nil {
		t.Fatal("BatchUpsertWorkItems accepted invalid JSON; want error")
	}
}
