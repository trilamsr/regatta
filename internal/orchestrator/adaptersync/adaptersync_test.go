package adaptersync

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

type stubAdapter struct {
	items []schemas.WorkItem
}

func (s *stubAdapter) List(context.Context) ([]schemas.WorkItem, error) { return s.items, nil }

func newSyncTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "s.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSync_UpsertsAdapterItems(t *testing.T) {
	db := newSyncTestDB(t)
	adapter := &stubAdapter{items: []schemas.WorkItem{
		{ID: "ITEM-1", Kind: schemas.KindFeature, Title: "a", Lane: "server", Status: schemas.StatusPlanned},
		{ID: "ITEM-2", Kind: schemas.KindFeature, Title: "b", Lane: "server", Status: schemas.StatusPlanned},
	}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	syncer := New(adapter, db, func() time.Time { return now })

	if err := syncer.Sync(context.Background(), now); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	got, err := db.GetWorkItem(context.Background(), "ITEM-1")
	if err != nil {
		t.Fatalf("GetWorkItem ITEM-1: %v", err)
	}
	if got.Source != state.SourceAdapter {
		t.Fatalf("ITEM-1.source=%s want adapter", got.Source)
	}
}

func TestSync_TombstonesMissingOnSecondTick(t *testing.T) {
	db := newSyncTestDB(t)
	adapter := &stubAdapter{items: []schemas.WorkItem{
		{ID: "ITEM-1", Kind: schemas.KindFeature, Title: "a", Lane: "server", Status: schemas.StatusPlanned},
		{ID: "ITEM-2", Kind: schemas.KindFeature, Title: "b", Lane: "server", Status: schemas.StatusPlanned},
	}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	syncer := New(adapter, db, func() time.Time { return now })

	if err := syncer.Sync(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	// Second tick: ITEM-2 removed.
	now = now.Add(1 * time.Second)
	adapter.items = adapter.items[:1]
	if err := syncer.Sync(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetWorkItem(context.Background(), "ITEM-2")
	if err != nil {
		t.Fatalf("GetWorkItem ITEM-2: %v", err)
	}
	if got.Status != state.WorkStatusArchived {
		t.Fatalf("ITEM-2.status=%s want archived", got.Status)
	}
}
