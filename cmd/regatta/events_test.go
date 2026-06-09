package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func newEventsHarness(t *testing.T) (*state.DB, string, time.Time) {
	t.Helper()
	t0 := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return t0 }
	dbPath := filepath.Join(t.TempDir(), "events.db")
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, state.DSN(dbPath), t0
}

func runEventsTailCLI(t *testing.T, dsn string, clock func() time.Time, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runEventsTailWith(eventsTailDeps{
		Stdout: &stdout, Stderr: &stderr, Clock: clock, DSN: dsn,
	}, args)
	return code, stdout.String(), stderr.String()
}

// TestEventsTail_TableEmpty asserts an empty events table prints the no-rows sentinel and exits 0.
func TestEventsTail_TableEmpty(t *testing.T) {
	_, dsn, t0 := newEventsHarness(t)
	clock := func() time.Time { return t0 }
	code, stdout, stderr := runEventsTailCLI(t, dsn, clock)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(strings.ToLower(stdout), "no events") {
		t.Fatalf("expected 'no events' message, got stdout=%q", stdout)
	}
}

// TestEventsTail_JSONFormat asserts --format=json emits a parseable rows array preserving ID + kind + payload.
func TestEventsTail_JSONFormat(t *testing.T) {
	db, dsn, t0 := newEventsHarness(t)
	clock := func() time.Time { return t0 }
	ctx := context.Background()
	if err := db.RecordEvent(ctx, 0, "merge.completed", `{"pr":42}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	// Seed an agent so the agent_id FK on the second event resolves; without
	// this, RecordEvent fails with FOREIGN KEY constraint failed.
	a, err := db.UpsertPending(ctx, "BUG-EVTS", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"reason":"ok"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	code, stdout, stderr := runEventsTailCLI(t, dsn, clock, "--format", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("json.Unmarshal: %v stdout=%q", err, stdout)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d want 2: %v", len(rows), rows)
	}
	if rows[0]["kind"] != "merge.completed" {
		t.Fatalf("rows[0].kind=%v want merge.completed", rows[0]["kind"])
	}
	if rows[1]["agent_id"] == nil {
		t.Fatalf("rows[1].agent_id=nil want non-nil; got rows=%v", rows)
	}
}

// TestEventsTail_UnknownFormat asserts an unknown --format value is rejected with exit 2.
func TestEventsTail_UnknownFormat(t *testing.T) {
	_, dsn, t0 := newEventsHarness(t)
	clock := func() time.Time { return t0 }
	code, _, stderr := runEventsTailCLI(t, dsn, clock, "--format", "yaml")
	if code != 2 {
		t.Fatalf("exit=%d want 2 stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "table|json") {
		t.Fatalf("stderr=%q want table|json hint", stderr)
	}
}

// TestEventsTail_NoSubcommand asserts `regatta events` with no verb exits 2 with a hint.
func TestEventsTail_NoSubcommand(t *testing.T) {
	code := runEvents(nil)
	if code != 2 {
		t.Fatalf("exit=%d want 2", code)
	}
}

// TestEventsTail_KindFilter asserts --kind routes through ListEventsByKindSince and excludes other kinds.
func TestEventsTail_KindFilter(t *testing.T) {
	db, dsn, t0 := newEventsHarness(t)
	clock := func() time.Time { return t0 }
	ctx := context.Background()
	if err := db.RecordEvent(ctx, 0, "merge.completed", `{}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	a, err := db.UpsertPending(ctx, "BUG-EVTS-KIND", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	code, stdout, stderr := runEventsTailCLI(t, dsn, clock, "--kind", "agent.exited", "--format", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("Unmarshal: %v stdout=%q", err, stdout)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d want 1: %v", len(rows), rows)
	}
	if rows[0]["kind"] != "agent.exited" {
		t.Fatalf("rows[0].kind=%v want agent.exited", rows[0]["kind"])
	}
}
