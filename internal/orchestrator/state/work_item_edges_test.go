package state

import (
	"context"
	"testing"
	"time"
)

func seedEdgeWorkItem(t *testing.T, db *DB, id string) {
	t.Helper()
	item := WorkItem{ID: id, Kind: KindFeature, Title: id, Lane: "server", Status: WorkStatusPlanned}
	seedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := db.UpsertWorkItem(context.Background(), item, SourceBrief, seedAt); err != nil {
		t.Fatalf("seedEdgeWorkItem(%s): %v", id, err)
	}
}

func TestUpsertEdges_InsertThenUpdatePreservesFired(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, t0)
	ctx := context.Background()
	seedEdgeWorkItem(t, db, "F-1")
	seedEdgeWorkItem(t, db, "F-2")

	rows := []EdgeRow{{
		ProgramID:    "m-aaaa",
		FromID:       "F-1",
		ToID:         "F-2",
		PredicateCEL: `out.severity == "high"`,
		OnSkip:       "cascade",
	}}
	if err := db.UpsertEdges(ctx, "m-aaaa", rows); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}

	got, err := db.ListEdgesFrom(ctx, "F-1")
	if err != nil {
		t.Fatalf("ListEdgesFrom: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1", len(got))
	}
	if got[0].PredicateCEL != `out.severity == "high"` {
		t.Fatalf("PredicateCEL=%q", got[0].PredicateCEL)
	}
	if got[0].Fired != "pending" {
		t.Fatalf("Fired=%q want pending", got[0].Fired)
	}
	if !got[0].CreatedAt.Equal(t0) {
		t.Fatalf("CreatedAt=%v want %v", got[0].CreatedAt, t0)
	}

	// Simulate evaluation between upserts: re-plan must preserve fired.
	if err := db.MarkEdgeFired(ctx, got[0].ID, "true", "sha-aa"); err != nil {
		t.Fatalf("MarkEdgeFired: %v", err)
	}

	rows[0].PredicateCEL = `out.severity == "critical"`
	if err := db.UpsertEdges(ctx, "m-aaaa", rows); err != nil {
		t.Fatalf("UpsertEdges #2: %v", err)
	}
	got, _ = db.ListEdgesFrom(ctx, "F-1")
	if len(got) != 1 {
		t.Fatalf("after re-upsert len=%d want 1 (must remain idempotent)", len(got))
	}
	if got[0].PredicateCEL != `out.severity == "critical"` {
		t.Fatalf("predicate not refreshed: %q", got[0].PredicateCEL)
	}
	if got[0].Fired != "true" || got[0].FiredAgainst != "sha-aa" {
		t.Fatalf("re-upsert silently flipped fired: %+v", got[0])
	}
}

func TestUpsertEdges_Empty(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertEdges(context.Background(), "m-x", nil); err != nil {
		t.Fatalf("UpsertEdges(nil): %v", err)
	}
}

func TestUpsertEdges_IsDefaultRoundTrip(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedEdgeWorkItem(t, db, "F-A")
	seedEdgeWorkItem(t, db, "F-B")
	if err := db.UpsertEdges(ctx, "m-1", []EdgeRow{{
		ProgramID: "m-1", FromID: "F-A", ToID: "F-B",
		IsDefault: true, OnSkip: "ignore",
	}}); err != nil {
		t.Fatalf("UpsertEdges: %v", err)
	}
	got, _ := db.ListEdgesFrom(ctx, "F-A")
	if len(got) != 1 || !got[0].IsDefault || got[0].OnSkip != "ignore" {
		t.Fatalf("got %+v", got)
	}
}

func TestMarkEdgeFired_PendingToTrue(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedEdgeWorkItem(t, db, "F-1")
	seedEdgeWorkItem(t, db, "F-2")
	if err := db.UpsertEdges(ctx, "m-aaaa", []EdgeRow{{
		ProgramID: "m-aaaa", FromID: "F-1", ToID: "F-2",
		PredicateCEL: "true", OnSkip: "cascade",
	}}); err != nil {
		t.Fatal(err)
	}
	edges, _ := db.ListEdgesFrom(ctx, "F-1")

	if err := db.MarkEdgeFired(ctx, edges[0].ID, "true", "sha-deadbeef"); err != nil {
		t.Fatalf("MarkEdgeFired: %v", err)
	}
	edges, _ = db.ListEdgesFrom(ctx, "F-1")
	if edges[0].Fired != "true" {
		t.Fatalf("Fired=%q want true", edges[0].Fired)
	}
	if edges[0].FiredAgainst != "sha-deadbeef" {
		t.Fatalf("FiredAgainst=%q want sha-deadbeef", edges[0].FiredAgainst)
	}
	if edges[0].EvaluatedAt.IsZero() {
		t.Fatal("EvaluatedAt zero after MarkEdgeFired")
	}
}

func TestMarkEdgeFired_NotFound(t *testing.T) {
	db := newTestDB(t)
	err := db.MarkEdgeFired(context.Background(), 9999, "true", "sha")
	if err == nil {
		t.Fatal("MarkEdgeFired(non-existent) returned nil; want error")
	}
}

func TestListEdgesFrom_OrdersByID(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedEdgeWorkItem(t, db, "F-A")
	seedEdgeWorkItem(t, db, "F-B")
	seedEdgeWorkItem(t, db, "F-C")
	if err := db.UpsertEdges(ctx, "m-1", []EdgeRow{
		{ProgramID: "m-1", FromID: "F-A", ToID: "F-C", OnSkip: "cascade"},
		{ProgramID: "m-1", FromID: "F-A", ToID: "F-B", OnSkip: "cascade"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := db.ListEdgesFrom(ctx, "F-A")
	if err != nil {
		t.Fatalf("ListEdgesFrom: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got[0].ID >= got[1].ID {
		t.Fatalf("not ordered by id ASC: %d, %d", got[0].ID, got[1].ID)
	}
	if got[0].ToID != "F-C" || got[1].ToID != "F-B" {
		t.Fatalf("insertion order broken: %q, %q", got[0].ToID, got[1].ToID)
	}
}

func TestListEdgesTo_FiltersToTarget(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedEdgeWorkItem(t, db, "F-A")
	seedEdgeWorkItem(t, db, "F-B")
	seedEdgeWorkItem(t, db, "F-C")
	if err := db.UpsertEdges(ctx, "m-1", []EdgeRow{
		{ProgramID: "m-1", FromID: "F-A", ToID: "F-C", OnSkip: "cascade"},
		{ProgramID: "m-1", FromID: "F-B", ToID: "F-C", OnSkip: "ignore"},
		{ProgramID: "m-1", FromID: "F-A", ToID: "F-B", OnSkip: "cascade"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := db.ListEdgesTo(ctx, "F-C")
	if err != nil {
		t.Fatalf("ListEdgesTo: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	for _, e := range got {
		if e.ToID != "F-C" {
			t.Fatalf("leaked edge ToID=%q", e.ToID)
		}
	}
}

func TestListPendingEdgesFromMerged_OnlyMergedSources(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	// F-MERGED has fired-pending edge; F-RUNNING also has one but
	// must be excluded by the merged-source filter.
	merged := WorkItem{ID: "F-MERGED", Kind: KindFeature, Title: "m", Lane: "server", Status: WorkStatusMerged}
	running := WorkItem{ID: "F-RUNNING", Kind: KindFeature, Title: "r", Lane: "server", Status: WorkStatusRunning}
	tgt := WorkItem{ID: "F-TGT", Kind: KindFeature, Title: "t", Lane: "server", Status: WorkStatusPlanned}
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, w := range []WorkItem{merged, running, tgt} {
		if err := db.UpsertWorkItem(ctx, w, SourceBrief, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpsertEdges(ctx, "m-1", []EdgeRow{
		{ProgramID: "m-1", FromID: "F-MERGED", ToID: "F-TGT", OnSkip: "cascade"},
		{ProgramID: "m-1", FromID: "F-RUNNING", ToID: "F-TGT", OnSkip: "cascade"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := db.ListPendingEdgesFromMerged(ctx)
	if err != nil {
		t.Fatalf("ListPendingEdgesFromMerged: %v", err)
	}
	if len(got) != 1 || got[0].FromID != "F-MERGED" {
		t.Fatalf("got %+v want exactly [F-MERGED]", got)
	}

	// After firing, it must no longer appear in the pending slice.
	if err := db.MarkEdgeFired(ctx, got[0].ID, "true", "sha"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.ListPendingEdgesFromMerged(ctx)
	if len(got) != 0 {
		t.Fatalf("after fire len=%d want 0", len(got))
	}
}

func TestUpsertEdges_UpdatedAtUsesClock(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(2 * time.Minute)
	now := t0
	db := newClockedTestDB(t, &now)
	ctx := context.Background()
	seedEdgeWorkItem(t, db, "F-1")
	seedEdgeWorkItem(t, db, "F-2")

	if err := db.UpsertEdges(ctx, "m-1", []EdgeRow{{
		ProgramID: "m-1", FromID: "F-1", ToID: "F-2", OnSkip: "cascade",
	}}); err != nil {
		t.Fatal(err)
	}
	got, _ := db.ListEdgesFrom(ctx, "F-1")
	if !got[0].CreatedAt.Equal(t0) || !got[0].UpdatedAt.Equal(t0) {
		t.Fatalf("created=%v updated=%v want both %v", got[0].CreatedAt, got[0].UpdatedAt, t0)
	}

	now = t1
	if err := db.UpsertEdges(ctx, "m-1", []EdgeRow{{
		ProgramID: "m-1", FromID: "F-1", ToID: "F-2", PredicateCEL: "x", OnSkip: "ignore",
	}}); err != nil {
		t.Fatal(err)
	}
	got, _ = db.ListEdgesFrom(ctx, "F-1")
	if !got[0].CreatedAt.Equal(t0) {
		t.Fatalf("CreatedAt mutated on update: %v", got[0].CreatedAt)
	}
	if !got[0].UpdatedAt.Equal(t1) {
		t.Fatalf("UpdatedAt=%v want %v", got[0].UpdatedAt, t1)
	}
}
