package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func newSchedTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "s.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestTick_ReservesAllPlannedNoDeps(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	now := time.Now()
	for _, id := range []string{"F-1", "F-2", "F-3"} {
		w := state.WorkItem{ID: id, Kind: state.KindFeature, Title: id,
			Lane: "server", Status: state.WorkStatusPlanned}
		if err := db.UpsertWorkItemAt(ctx, w, state.SourceBrief, now); err != nil {
			t.Fatal(err)
		}
	}

	s := New(db, Config{})
	ids, err := s.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("reserved=%d want 3 (ids=%v)", len(ids), ids)
	}
}

func TestTick_DepBlocksUntilMerged(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	now := time.Now()
	c1 := state.WorkItem{ID: "F-1", Kind: state.KindFeature, Title: "c1",
		Lane: "server", Status: state.WorkStatusPlanned}
	c2 := state.WorkItem{ID: "F-2", Kind: state.KindFeature, Title: "c2",
		Lane: "server", Status: state.WorkStatusPlanned,
		DependsOnFeatures: []string{"F-1"}}
	for _, w := range []state.WorkItem{c1, c2} {
		if err := db.UpsertWorkItemAt(ctx, w, state.SourceBrief, now); err != nil {
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
}

func TestTick_IdempotentSecondCall(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	now := time.Now()
	w := state.WorkItem{ID: "F-1", Kind: state.KindFeature, Title: "x",
		Lane: "server", Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItemAt(ctx, w, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}

	s := New(db, Config{})
	first, _ := s.Tick(ctx)
	second, _ := s.Tick(ctx)
	if len(first) != 1 || len(second) != 0 {
		t.Fatalf("first=%d second=%d want 1, 0", len(first), len(second))
	}
}
