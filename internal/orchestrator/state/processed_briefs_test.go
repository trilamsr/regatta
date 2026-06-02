package state_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func newPBTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "pb.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// Issue #92: GetProcessedBrief returns (zero,false,nil) for unknown parents.
func TestGetProcessedBrief_NoRow(t *testing.T) {
	db := newPBTestDB(t)
	_, ok, err := db.GetProcessedBrief(context.Background(), "PROG-unknown")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("ok=true want false on empty table")
	}
}

// Issue #92: round-trip the watermark.
func TestRecordAndGetProcessedBrief_RoundTrip(t *testing.T) {
	db := newPBTestDB(t)
	at := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	produced := at.Add(-1 * time.Minute)
	if err := db.RecordProcessedBrief(context.Background(), "PROG-1", produced, "mac-abc", at); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, ok, err := db.GetProcessedBrief(context.Background(), "PROG-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("ok=false want true after record")
	}
	if !got.LastProducedAt.Equal(produced.Truncate(time.Second)) {
		t.Fatalf("LastProducedAt=%v want %v", got.LastProducedAt, produced)
	}
	if got.BriefHMAC != "mac-abc" {
		t.Fatalf("BriefHMAC=%q want mac-abc", got.BriefHMAC)
	}
}

// Issue #92: HasProcessedBriefHMAC catches exact-replay regardless of parent.
func TestHasProcessedBriefHMAC_DetectsReplay(t *testing.T) {
	db := newPBTestDB(t)
	at := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	if err := db.RecordProcessedBrief(context.Background(), "PROG-1", at, "mac-xyz", at); err != nil {
		t.Fatalf("Record: %v", err)
	}
	seen, err := db.HasProcessedBriefHMAC(context.Background(), "mac-xyz")
	if err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatalf("seen=false want true")
	}
	miss, err := db.HasProcessedBriefHMAC(context.Background(), "mac-other")
	if err != nil {
		t.Fatal(err)
	}
	if miss {
		t.Fatalf("seen=true want false for unknown hmac")
	}
}

// Issue #92: re-record under same parent_program_id is an upsert, not a duplicate row.
func TestRecordProcessedBrief_UpsertOnSameParent(t *testing.T) {
	db := newPBTestDB(t)
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	if err := db.RecordProcessedBrief(context.Background(), "PROG-1", t0, "mac-1", t0); err != nil {
		t.Fatal(err)
	}
	t1 := t0.Add(1 * time.Hour)
	if err := db.RecordProcessedBrief(context.Background(), "PROG-1", t1, "mac-2", t1); err != nil {
		t.Fatal(err)
	}
	got, _, _ := db.GetProcessedBrief(context.Background(), "PROG-1")
	if got.BriefHMAC != "mac-2" {
		t.Fatalf("BriefHMAC=%q want mac-2 (upsert)", got.BriefHMAC)
	}
	if !got.LastProducedAt.Equal(t1) {
		t.Fatalf("LastProducedAt=%v want %v", got.LastProducedAt, t1)
	}
}
