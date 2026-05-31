package state

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// seedWorkItem inserts a minimal planned work_item so journal rows can
// honour the work_item_outputs.work_item_id FK without dragging the full
// PlannedFeature shape into journal-only tests. The seed timestamp is
// fixed and irrelevant to the journal-only tests below.
func seedWorkItem(t *testing.T, db *DB, id string) {
	t.Helper()
	w := WorkItem{ID: id, Kind: KindFeature, Title: id, Lane: "server", Status: WorkStatusPlanned}
	seedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := db.UpsertWorkItem(context.Background(), w, SourceBrief, seedAt); err != nil {
		t.Fatalf("seedWorkItem %s: %v", id, err)
	}
}

func TestAppendOutput_NewAttempt(t *testing.T) {
	at := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := fixedClockDB(t, at)
	seedWorkItem(t, db, "F-1")
	ctx := context.Background()

	entry, err := db.AppendOutput(ctx, "F-1", json.RawMessage(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatalf("AppendOutput: %v", err)
	}
	if entry.WorkItemID != "F-1" {
		t.Fatalf("WorkItemID=%q want F-1", entry.WorkItemID)
	}
	if entry.AttemptNo != 1 {
		t.Fatalf("first AttemptNo=%d want 1", entry.AttemptNo)
	}
	if entry.OutputJSON != `{"a":1,"b":2}` {
		t.Fatalf("OutputJSON=%q want canonical {\"a\":1,\"b\":2}", entry.OutputJSON)
	}
	if len(entry.ContentSHA) != 64 {
		t.Fatalf("ContentSHA len=%d want 64 (sha256 hex)", len(entry.ContentSHA))
	}
	if entry.ProducedAt.Unix() != at.Unix() {
		t.Fatalf("ProducedAt=%v want %v", entry.ProducedAt, at)
	}
	if entry.ID == 0 {
		t.Fatalf("ID=0 — INSERT did not stamp LastInsertId")
	}

	entry2, err := db.AppendOutput(ctx, "F-1", json.RawMessage(`{"a":1,"b":3}`))
	if err != nil {
		t.Fatalf("AppendOutput #2: %v", err)
	}
	if entry2.AttemptNo != 2 {
		t.Fatalf("second AttemptNo=%d want 2 (monotonic per work_item)", entry2.AttemptNo)
	}
}

// TestAppendOutput_RejectsInvalidJSON pins the integrity contract: the
// journal must refuse non-JSON payloads at write time rather than store
// uncanonical bytes that later break predicate eval (Risk 4, spec §7).
func TestAppendOutput_RejectsInvalidJSON(t *testing.T) {
	db := newWorkItemsTestDB(t)
	seedWorkItem(t, db, "F-BAD")
	_, err := db.AppendOutput(context.Background(), "F-BAD", json.RawMessage(`{not json`))
	if err == nil {
		t.Fatalf("AppendOutput accepted invalid JSON; want error")
	}
}

func TestGetLatestOutput_ReturnsHighestAttempt(t *testing.T) {
	db := newWorkItemsTestDB(t)
	seedWorkItem(t, db, "F-1")
	ctx := context.Background()
	if _, err := db.AppendOutput(ctx, "F-1", json.RawMessage(`{"v":1}`)); err != nil {
		t.Fatalf("append #1: %v", err)
	}
	if _, err := db.AppendOutput(ctx, "F-1", json.RawMessage(`{"v":2}`)); err != nil {
		t.Fatalf("append #2: %v", err)
	}
	got, err := db.GetLatestOutput(ctx, "F-1")
	if err != nil {
		t.Fatalf("GetLatestOutput: %v", err)
	}
	if got.AttemptNo != 2 {
		t.Fatalf("AttemptNo=%d want 2", got.AttemptNo)
	}
	if got.OutputJSON != `{"v":2}` {
		t.Fatalf("OutputJSON=%q want {\"v\":2} (newest attempt)", got.OutputJSON)
	}
}

func TestGetLatestOutput_ErrJournalNotFound(t *testing.T) {
	db := newWorkItemsTestDB(t)
	seedWorkItem(t, db, "F-1")
	_, err := db.GetLatestOutput(context.Background(), "F-1")
	if !errors.Is(err, ErrJournalNotFound) {
		t.Fatalf("err=%v want ErrJournalNotFound", err)
	}
}

// TestAppendOutput_TwoWorkItems_SamePayload_OK enforces spec §7 risk 11:
// the migration intentionally omits UNIQUE(content_sha) so two work_items
// that emit byte-identical payloads (common under the stub spawner) do
// not collide.
func TestAppendOutput_TwoWorkItems_SamePayload_OK(t *testing.T) {
	db := newWorkItemsTestDB(t)
	seedWorkItem(t, db, "F-A")
	seedWorkItem(t, db, "F-B")
	ctx := context.Background()
	payload := json.RawMessage(`{"completed":true}`)
	if _, err := db.AppendOutput(ctx, "F-A", payload); err != nil {
		t.Fatalf("F-A: %v", err)
	}
	if _, err := db.AppendOutput(ctx, "F-B", payload); err != nil {
		t.Fatalf("F-B: %v", err)
	}
}

func TestGetOutputByContentSHA_RoundTrip(t *testing.T) {
	db := newWorkItemsTestDB(t)
	seedWorkItem(t, db, "F-1")
	ctx := context.Background()
	want, err := db.AppendOutput(ctx, "F-1", json.RawMessage(`{"x":42}`))
	if err != nil {
		t.Fatalf("AppendOutput: %v", err)
	}
	got, err := db.GetOutputByContentSHA(ctx, want.ContentSHA)
	if err != nil {
		t.Fatalf("GetOutputByContentSHA: %v", err)
	}
	if got.ID != want.ID || got.AttemptNo != want.AttemptNo {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, want)
	}
}

func TestGetOutputByContentSHA_ErrJournalNotFound(t *testing.T) {
	db := newWorkItemsTestDB(t)
	_, err := db.GetOutputByContentSHA(context.Background(),
		"0000000000000000000000000000000000000000000000000000000000000000")
	if !errors.Is(err, ErrJournalNotFound) {
		t.Fatalf("err=%v want ErrJournalNotFound", err)
	}
}
