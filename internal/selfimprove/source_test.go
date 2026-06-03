package selfimprove

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTestDB opens an in-memory SQLite with a minimal substrate_events
// schema — column set is the projection Fetch() reads (id, kind,
// written_at, payload_json). Production schema has more columns but
// the reader only depends on these four.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE substrate_events (
	    id TEXT PRIMARY KEY,
	    kind TEXT NOT NULL,
	    written_at INTEGER NOT NULL,
	    payload_json TEXT
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertEvent(t *testing.T, db *sql.DB, id, kind string, writtenAt time.Time, payload string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO substrate_events (id, kind, written_at, payload_json) VALUES (?,?,?,?)`,
		id, kind, writtenAt.UnixMilli(), payload,
	); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

// TestDetector_RealSubstrateEventSource_ReadsAndFires asserts SQL reader fires rule against real substrate rows (#645).
func TestDetector_RealSubstrateEventSource_ReadsAndFires(t *testing.T) {
	db := newTestDB(t)
	now := fixedNow
	for i := 0; i < 3; i++ {
		insertEvent(t, db, "e"+string(rune('1'+i)), "gate_fail",
			now.Add(-time.Duration(i+1)*time.Hour),
			`{"gate_kind":"pr-lint","gate_reason":"banned-phrase"}`)
	}

	src := NewSQLEventSource(db)
	events, err := src.Fetch(context.Background(), now.Add(-7*24*time.Hour), []string{"gate_fail"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d", len(events))
	}

	got := DefaultRules()[0].Match(events, now)
	if f := findByRule(got, RuleSameGateFailRepeats); f == nil || f.Count != 3 {
		t.Fatalf("want R1 fire from real-substrate read, got %+v", got)
	}
}

// TestSQLEventSource_WindowExcludesOldRows asserts written_at < since rows are not returned.
func TestSQLEventSource_WindowExcludesOldRows(t *testing.T) {
	db := newTestDB(t)
	now := fixedNow
	insertEvent(t, db, "fresh", "gate_fail", now.Add(-1*time.Hour), `{"gate_kind":"x","gate_reason":"y"}`)
	insertEvent(t, db, "stale", "gate_fail", now.Add(-30*24*time.Hour), `{"gate_kind":"x","gate_reason":"y"}`)

	src := NewSQLEventSource(db)
	events, err := src.Fetch(context.Background(), now.Add(-7*24*time.Hour), []string{"gate_fail"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(events) != 1 || events[0].ID != "fresh" {
		t.Fatalf("want only fresh row, got %+v", events)
	}
}

// TestSQLEventSource_MalformedPayload_Skipped asserts a bad payload row never aborts the scan.
func TestSQLEventSource_MalformedPayload_Skipped(t *testing.T) {
	db := newTestDB(t)
	now := fixedNow
	insertEvent(t, db, "bad", "gate_fail", now.Add(-1*time.Hour), `{not json`)
	insertEvent(t, db, "good", "gate_fail", now.Add(-2*time.Hour), `{"gate_kind":"x","gate_reason":"y"}`)

	src := NewSQLEventSource(db)
	events, err := src.Fetch(context.Background(), now.Add(-7*24*time.Hour), []string{"gate_fail"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(events) != 1 || events[0].ID != "good" {
		t.Fatalf("want only good row, got %+v", events)
	}
}

// TestSQLEventSource_NilDB_ReturnsError asserts the constructor is no-fail but Fetch surfaces a nil DB.
func TestSQLEventSource_NilDB_ReturnsError(t *testing.T) {
	src := NewSQLEventSource(nil)
	if _, err := src.Fetch(context.Background(), time.Time{}, []string{"gate_fail"}); err == nil {
		t.Fatal("want error on nil DB, got nil")
	}
}
