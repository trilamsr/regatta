package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestStatusJSON_DegradedNoDB_EmitsValidJSON asserts unreachable DB → traffic_light=red, valid JSON (MAY-47 AC1, AC4).
func TestStatusJSON_DegradedNoDB_EmitsValidJSON(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.db")
	var buf strings.Builder
	rc := runStatusJSON(context.Background(), &buf, statusJSONOpts{
		DBPath:       missing,
		SocketURL:    "",
		ConnectTimeo: 50 * time.Millisecond,
		Now:          func() time.Time { return time.Date(2026, 6, 21, 5, 0, 0, 0, time.UTC) },
	})
	if rc != 0 {
		t.Fatalf("runStatusJSON degraded rc=%d body=%q", rc, buf.String())
	}
	var out StatusJSON
	if err := json.Unmarshal([]byte(buf.String()), &out); err != nil {
		t.Fatalf("invalid json: %v\nraw=%q", err, buf.String())
	}
	if out.TrafficLight != "red" {
		t.Errorf("traffic_light = %q; want red (no db reachable)", out.TrafficLight)
	}
	if out.GeneratedAt == "" {
		t.Error("generated_at missing")
	}
	if _, err := time.Parse(time.RFC3339, out.GeneratedAt); err != nil {
		t.Errorf("generated_at not RFC3339: %v", err)
	}
}

// TestStatusJSON_LiveDB_EmitsAllRequiredFields asserts every AC2/AC5 field present + RFC3339 timestamps on seeded events.
func TestStatusJSON_LiveDB_EmitsAllRequiredFields(t *testing.T) {
	db, dbPath := seedStatusDB(t)
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 6, 21, 5, 0, 0, 0, time.UTC)
	var buf strings.Builder
	rc := runStatusJSON(context.Background(), &buf, statusJSONOpts{
		DBPath:       dbPath,
		ConnectTimeo: 50 * time.Millisecond,
		Now:          func() time.Time { return now },
	})
	if rc != 0 {
		t.Fatalf("runStatusJSON live rc=%d body=%q", rc, buf.String())
	}
	var out StatusJSON
	if err := json.Unmarshal([]byte(buf.String()), &out); err != nil {
		t.Fatalf("invalid json: %v\nraw=%q", err, buf.String())
	}

	if out.Orchestrator.PID == 0 {
		// pid=0 means "not yet probed"; -1 means "unknown/no socket". 0 is invalid.
		t.Errorf("orchestrator.pid = 0; want -1 (no socket) or real pid")
	}
	if out.Orchestrator.UptimeSeconds < 0 {
		t.Errorf("orchestrator.uptime_seconds = %d; want >=0", out.Orchestrator.UptimeSeconds)
	}

	if out.Agents.InFlight < 1 {
		t.Errorf("agents.in_flight = %d; want >=1 (seeded one running)", out.Agents.InFlight)
	}
	if got := out.Agents.ByState["running"]; got != 1 {
		t.Errorf("agents.by_state[running] = %d; want 1", got)
	}

	if out.PRs.ByMergeState == nil {
		t.Error("prs.by_merge_state map missing")
	}

	if out.Events.LastSpawnAt == "" {
		t.Error("events.last_spawn_at empty; seed inserted spawn.completed")
	} else if _, err := time.Parse(time.RFC3339, out.Events.LastSpawnAt); err != nil {
		t.Errorf("last_spawn_at not RFC3339: %v", err)
	}
	if out.Events.LastExitAt == "" {
		t.Error("events.last_exit_at empty; seed inserted agent.exited")
	}
	if out.Events.LastMergeAt == "" {
		t.Error("events.last_merge_at empty; seed inserted merge_completed")
	}

	switch out.TrafficLight {
	case "green", "yellow", "red":
	default:
		t.Errorf("traffic_light = %q; want green/yellow/red", out.TrafficLight)
	}
}

// TestStatusJSON_TrafficLight_StalenessToYellow asserts last-event age > 5min flips traffic_light off green.
func TestStatusJSON_TrafficLight_StalenessToYellow(t *testing.T) {
	db, dbPath := seedStatusDB(t)
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 6, 21, 5, 10, 0, 0, time.UTC)
	var buf strings.Builder
	rc := runStatusJSON(context.Background(), &buf, statusJSONOpts{
		DBPath:       dbPath,
		ConnectTimeo: 50 * time.Millisecond,
		Now:          func() time.Time { return now },
	})
	if rc != 0 {
		t.Fatalf("rc=%d body=%q", rc, buf.String())
	}
	var out StatusJSON
	if err := json.Unmarshal([]byte(buf.String()), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if out.TrafficLight == "green" {
		t.Errorf("traffic_light = green; want yellow/red with 10-min stale events")
	}
}

// TestStatusJSON_SocketProbe_FillsOrchestratorPID asserts /healthz response sets orchestrator.alive=true + version (AC3).
func TestStatusJSON_SocketProbe_FillsOrchestratorPID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"ok","version":"vTEST","checks":{}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	missingDB := filepath.Join(t.TempDir(), "no.db")
	var buf strings.Builder
	rc := runStatusJSON(context.Background(), &buf, statusJSONOpts{
		DBPath:       missingDB,
		SocketURL:    srv.URL,
		ConnectTimeo: 200 * time.Millisecond,
		Now:          func() time.Time { return time.Date(2026, 6, 21, 5, 0, 0, 0, time.UTC) },
	})
	if rc != 0 {
		t.Fatalf("rc=%d body=%q", rc, buf.String())
	}
	var out StatusJSON
	if err := json.Unmarshal([]byte(buf.String()), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if !out.Orchestrator.Alive {
		t.Errorf("orchestrator.alive = false; want true (healthz responded)")
	}
	if out.Orchestrator.Version != "vTEST" {
		t.Errorf("orchestrator.version = %q; want vTEST", out.Orchestrator.Version)
	}
}

// TestStatusJSON_RunStatus_JSONFlagRouting asserts `runStatus --json` writes parseable JSON to stdout (AC1 e2e).
func TestStatusJSON_RunStatus_JSONFlagRouting(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = prev }()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	missing := filepath.Join(t.TempDir(), "absent.db")
	rc := runStatus([]string{"--json", "--db=" + missing})
	_ = w.Close()
	body := <-done

	if rc != 0 {
		t.Fatalf("runStatus --json rc=%d body=%q", rc, body)
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(body), &probe); err != nil {
		t.Fatalf("--json output not parseable: %v\nraw=%q", err, body)
	}
	if _, ok := probe["traffic_light"]; !ok {
		t.Error("--json output missing traffic_light field")
	}
}

// seedStatusDB creates a temp sqlite DB with the minimal schema this test
// suite reads from and inserts one running agent + one spawn.completed +
// one agent.exited + one merge_completed event at `now`.
func seedStatusDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "status.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatalf("wal: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE agents (
		id INTEGER PRIMARY KEY,
		work_item_id TEXT,
		lane TEXT,
		state TEXT NOT NULL,
		pid INTEGER DEFAULT 0,
		session_id TEXT DEFAULT '',
		pr_sha TEXT DEFAULT '',
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`); err != nil {
		t.Fatalf("create agents: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE events (
		id INTEGER PRIMARY KEY,
		agent_id INTEGER,
		kind TEXT NOT NULL,
		payload_json TEXT DEFAULT '{}',
		created_at INTEGER NOT NULL
	)`); err != nil {
		t.Fatalf("create events: %v", err)
	}
	seedTS := time.Date(2026, 6, 21, 5, 0, 0, 0, time.UTC).Unix()
	if _, err := db.Exec(`INSERT INTO agents (id, work_item_id, lane, state, created_at, updated_at) VALUES (1,'MAY-47','default','running',?,?)`, seedTS, seedTS); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	for _, kind := range []string{"spawn.completed", "agent.exited", "merge_completed"} {
		if _, err := db.Exec(`INSERT INTO events (agent_id, kind, created_at) VALUES (1, ?, ?)`, kind, seedTS); err != nil {
			t.Fatalf("seed event %s: %v", kind, err)
		}
	}
	return db, dbPath
}
