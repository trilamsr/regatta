package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
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

// TestEventsTail_TableEmpty asserts empty-table output (#1078).
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

// TestEventsTail_JSONFormat asserts --format=json round-trips (#1078).
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

// TestEventsTail_UnknownFormat asserts bad --format exits 2 (#1078).
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

// TestEventsTail_NegativeSinceRejected asserts --since 0 or negative exits 2 with hint (R14-Bug-1; previously silent no-op because cutoff = now.Add(-(-dur)) lands in the future).
func TestEventsTail_NegativeSinceRejected(t *testing.T) {
	_, dsn, t0 := newEventsHarness(t)
	clock := func() time.Time { return t0 }
	code, _, stderr := runEventsTailCLI(t, dsn, clock, "--since", "-1h")
	if code != 2 {
		t.Fatalf("exit=%d want 2 for --since=-1h; stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "--since must be > 0") {
		t.Errorf("stderr missing --since hint: %q", stderr)
	}
}

// TestEventsTail_NoSubcommand asserts no-verb exit 2 (#1078).
func TestEventsTail_NoSubcommand(t *testing.T) {
	code := runEvents(nil)
	if code != 2 {
		t.Fatalf("exit=%d want 2", code)
	}
}

// TestEventsTail_KindFilter asserts --kind filters by event kind (#1078).
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

// TestEventsTail_AgentFilter asserts --agent N excludes other agents (#1078).
func TestEventsTail_AgentFilter(t *testing.T) {
	db, dsn, t0 := newEventsHarness(t)
	clock := func() time.Time { return t0 }
	ctx := context.Background()
	a1, err := db.UpsertPending(ctx, "BUG-EVTS-A1", "server")
	if err != nil {
		t.Fatalf("UpsertPending a1: %v", err)
	}
	a2, err := db.UpsertPending(ctx, "BUG-EVTS-A2", "server")
	if err != nil {
		t.Fatalf("UpsertPending a2: %v", err)
	}
	if err := db.RecordEvent(ctx, a1.ID, "agent.exited", `{"who":1}`); err != nil {
		t.Fatalf("RecordEvent a1: %v", err)
	}
	if err := db.RecordEvent(ctx, a2.ID, "agent.exited", `{"who":2}`); err != nil {
		t.Fatalf("RecordEvent a2: %v", err)
	}

	code, stdout, stderr := runEventsTailCLI(t, dsn, clock, "--agent", fmtAgentID(a2.ID), "--format", "json")
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
	if got := rows[0]["agent_id"]; got == nil {
		t.Fatalf("rows[0].agent_id=nil want %d", a2.ID)
	}
}

// TestEventsTail_KindAndSinceCutoff asserts --kind K --since DUR returns fresh row past LIMIT cap of older same-kind rows (R5-Bug-1 second half).
func TestEventsTail_KindAndSinceCutoff(t *testing.T) {
	t0 := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	now := t0
	clock := func() time.Time { return now }
	dbPath := filepath.Join(t.TempDir(), "events.db")
	dsn := state.DSN(dbPath)
	db, err := state.OpenWithClock(context.Background(), dsn, clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := db.RecordEvent(ctx, 0, "secrets_rotated", `{"old":true}`); err != nil {
			t.Fatalf("record old %d: %v", i, err)
		}
	}
	now = t0.Add(48 * time.Hour)
	if err := db.RecordEvent(ctx, 0, "secrets_rotated", `{"fresh":true}`); err != nil {
		t.Fatalf("record fresh: %v", err)
	}
	now = now.Add(1 * time.Minute)

	code, stdout, stderr := runEventsTailCLI(t, dsn, clock, "--kind", "secrets_rotated", "--since", "5m", "--format", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("Unmarshal: %v stdout=%q", err, stdout)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d want 1 (only the fresh row): %v", len(rows), rows)
	}
	payload, ok := rows[0]["payload_json"].(string)
	if !ok || !strings.Contains(payload, "fresh") {
		t.Fatalf("rows[0].payload=%v want fresh", rows[0]["payload_json"])
	}
}

// TestDefaultDBPath_HonorsEnv asserts defaultDBPath falls through to REGATTA_STATE_DB when no --db is given (R5-Bug-5).
func TestDefaultDBPath_HonorsEnv(t *testing.T) {
	t.Setenv("REGATTA_STATE_DB", "/data/regatta.db")
	if got := defaultDBPath([]string{"--decision", "allow"}); got != "/data/regatta.db" {
		t.Fatalf("defaultDBPath=%q want /data/regatta.db", got)
	}
	if got := defaultDBPath([]string{"--db", "/custom/path.db"}); got != "/custom/path.db" {
		t.Fatalf("defaultDBPath --db override=%q want /custom/path.db", got)
	}
}

// TestEventsTail_SinceCutoff asserts --since excludes events older than the window (#1078).
func TestEventsTail_SinceCutoff(t *testing.T) {
	db, dsn, t0 := newEventsHarness(t)
	ctx := context.Background()
	if err := db.RecordEvent(ctx, 0, "merge.completed", `{"old":true}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	// Advance the clock past the --since window so the seeded row falls outside it.
	future := t0.Add(48 * time.Hour)
	clock := func() time.Time { return future }
	code, stdout, stderr := runEventsTailCLI(t, dsn, clock, "--since", "1h", "--format", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("Unmarshal: %v stdout=%q", err, stdout)
	}
	if len(rows) != 0 {
		t.Fatalf("rows=%d want 0 (cutoff should exclude old row): %v", len(rows), rows)
	}
}

func fmtAgentID(id int64) string {
	return strconv.FormatInt(id, 10)
}
