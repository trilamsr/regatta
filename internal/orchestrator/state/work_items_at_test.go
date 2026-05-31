package state

import (
	"context"
	"testing"
	"time"
)

// The *At variants exist so production writers (AdapterSync,
// BriefLoader) align rows with a poll-start tick without mutating
// *DB's clock via SetClock — see state.go's SetClock warning.

func TestUpsertWorkItemAt_UsesProvidedTimestamp(t *testing.T) {
	db := newWorkItemsTestDB(t)
	wallClock := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	db.SetClock(func() time.Time { return wallClock })
	ctx := context.Background()

	at := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	item := WorkItem{ID: "F-AT", Kind: KindFeature, Title: "at", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItemAt(ctx, item, SourceAdapter, at); err != nil {
		t.Fatalf("UpsertWorkItemAt: %v", err)
	}

	got, err := db.GetWorkItem(ctx, "F-AT")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if got.LastSeenAt.Unix() != at.Unix() {
		t.Fatalf("LastSeenAt=%d want %d (must use caller-supplied at, not d.now)", got.LastSeenAt.Unix(), at.Unix())
	}
	if got.UpdatedAt.Unix() != at.Unix() {
		t.Fatalf("UpdatedAt=%d want %d", got.UpdatedAt.Unix(), at.Unix())
	}
	if got.CreatedAt.Unix() != at.Unix() {
		t.Fatalf("CreatedAt=%d want %d (insert path stamps created_at from at)", got.CreatedAt.Unix(), at.Unix())
	}
}

func TestTombstoneBySourceAt_UsesProvidedCutoff(t *testing.T) {
	db := newWorkItemsTestDB(t)
	wallClock := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	db.SetClock(func() time.Time { return wallClock })
	ctx := context.Background()

	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	item := WorkItem{ID: "F-TOMB", Kind: KindFeature, Title: "tomb", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItemAt(ctx, item, SourceBrief, t0); err != nil {
		t.Fatalf("UpsertWorkItemAt: %v", err)
	}

	archived, err := db.TombstoneBySourceAt(ctx, SourceBrief, t0.Add(time.Second))
	if err != nil {
		t.Fatalf("TombstoneBySourceAt: %v", err)
	}
	if len(archived) != 1 || archived[0] != "F-TOMB" {
		t.Fatalf("archived=%v want [F-TOMB]", archived)
	}

	got, _ := db.GetWorkItem(ctx, "F-TOMB")
	if got.Status != WorkStatusArchived {
		t.Fatalf("F-TOMB.status=%s want archived", got.Status)
	}
}

func TestUpsertWorkItem_DelegatesToAt(t *testing.T) {
	db := newWorkItemsTestDB(t)
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	db.SetClock(func() time.Time { return t0 })
	ctx := context.Background()

	item := WorkItem{ID: "F-DEL", Kind: KindFeature, Title: "del", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, item, SourceAdapter); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}

	got, _ := db.GetWorkItem(ctx, "F-DEL")
	if got.LastSeenAt.Unix() != t0.Unix() {
		t.Fatalf("LastSeenAt=%d want %d (delegation must thread d.now through *At)", got.LastSeenAt.Unix(), t0.Unix())
	}
	if got.UpdatedAt.Unix() != t0.Unix() {
		t.Fatalf("UpdatedAt=%d want %d", got.UpdatedAt.Unix(), t0.Unix())
	}
}

func TestTombstoneBySource_DelegatesToAt(t *testing.T) {
	db := newWorkItemsTestDB(t)
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	tickClock := t0
	db.SetClock(func() time.Time { return tickClock })
	ctx := context.Background()

	item := WorkItem{ID: "F-TDEL", Kind: KindFeature, Title: "tdel", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, item, SourceBrief); err != nil {
		t.Fatal(err)
	}

	tickClock = t0.Add(time.Minute)
	archived, err := db.TombstoneBySource(ctx, SourceBrief, tickClock)
	if err != nil {
		t.Fatalf("TombstoneBySource: %v", err)
	}
	if len(archived) != 1 || archived[0] != "F-TDEL" {
		t.Fatalf("archived=%v want [F-TDEL]", archived)
	}
}
