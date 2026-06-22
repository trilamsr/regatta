package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestSqliteSource_MostRecentEventAgeReadsEventsTable pins R30: status reads `events.created_at` (where DB.RecordEvent writes) not `substrate_events.emitted_at` (legacy, only tests write to it).
func TestSqliteSource_MostRecentEventAgeReadsEventsTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "status.db")
	rw, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	defer func() { _ = rw.Close() }()
	if _, err := rw.Exec(`CREATE TABLE events (id INTEGER PRIMARY KEY, agent_id INTEGER, kind TEXT, payload_json TEXT, created_at INTEGER NOT NULL)`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	now := time.Unix(1_780_000_000, 0).UTC()
	if _, err := rw.Exec(`INSERT INTO events (kind, payload_json, created_at) VALUES (?,?,?)`,
		"tick.completed", "{}", now.Add(-30*time.Second).Unix()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ro, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("open ro: %v", err)
	}
	defer func() { _ = ro.Close() }()
	s := &sqliteSource{db: ro}

	got := s.mostRecentEventAge(context.Background(), now)
	if got > 31*time.Second || got < 29*time.Second {
		t.Fatalf("mostRecentEventAge=%s; want ~30s (events.created_at-derived)", got)
	}
}
