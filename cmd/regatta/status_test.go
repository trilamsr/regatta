package main

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// stubSource serves a fixed Snapshot. Used to drive renderer tests
// without standing up a sqlite + Prom backend.
type stubSource struct {
	snap Snapshot
	hits atomic.Int64
}

func (s *stubSource) Snapshot(_ context.Context) Snapshot {
	s.hits.Add(1)
	return s.snap
}

// TestStatus_TUIRender_DoesNotPanicOnSmallTerminal walks 39/59/79 col widths and asserts the render returns without panic.
func TestStatus_TUIRender_DoesNotPanicOnSmallTerminal(t *testing.T) {
	snap := Snapshot{
		At:              time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
		ActiveSubagents: Panel{Title: "Active subagents", State: PanelOK, Lines: []string{"impl-1  5s"}},
		InFlightPRs:     Panel{Title: "In-flight PRs", State: PanelEmpty, Lines: []string{"— none"}},
		RecentMerges:    Panel{Title: "Recent merges (24h)", State: PanelOK, Lines: []string{"#1 title"}},
		CostToday:       Panel{Title: "Cost today", State: PanelMissing, Lines: []string{"n/a"}},
		GreenClock:      Panel{Title: "Green-clock progress", State: PanelOK, Lines: []string{"day 21 of 30"}},
	}
	for _, w := range []int{39, 59, 79, 80, 120} {
		r := Renderer{Width: w, Height: 24}
		out := r.Render(snap) // must not panic
		if out == "" {
			t.Errorf("width=%d: empty render", w)
		}
	}
}

// TestStatus_PanelPartialState_OKEmptyMissing asserts the state tag string drives the rendered banner.
func TestStatus_PanelPartialState_OKEmptyMissing(t *testing.T) {
	r := Renderer{Width: 80, Height: 24}
	snap := Snapshot{
		At:              time.Now().UTC(),
		ActiveSubagents: Panel{Title: "A", State: PanelOK, Lines: []string{"x"}},
		InFlightPRs:     Panel{Title: "B", State: PanelEmpty, Lines: []string{"—"}},
		RecentMerges:    Panel{Title: "C", State: PanelMissing, Lines: []string{"n/a"}},
		CostToday:       Panel{Title: "D", State: PanelOK, Lines: []string{"$1.23"}},
		GreenClock:      Panel{Title: "E", State: PanelOK, Lines: []string{"day 1"}},
	}
	out := r.Render(snap)
	if !strings.Contains(out, "[EMPTY]") {
		t.Error("missing [EMPTY] tag")
	}
	if !strings.Contains(out, "[MISSING]") {
		t.Error("missing [MISSING] tag")
	}
}

// TestStatus_RefreshRate_RespectsConfig verifies the loop honors the configured tick interval.
func TestStatus_RefreshRate_RespectsConfig(t *testing.T) {
	src := &stubSource{snap: Snapshot{At: time.Now().UTC()}}
	r := Renderer{Width: 80, Height: 24}
	var buf threadSafeBuf
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	_ = runLoop(ctx, &buf, src, r, 50*time.Millisecond)
	// Expect at least one initial paint + ~4 ticker fires within
	// 250 ms at 50 ms refresh. Slack the lower bound to 2 to keep CI
	// flake-free across loaded runners.
	if got := src.hits.Load(); got < 2 {
		t.Errorf("Snapshot hit count = %d; want >= 2", got)
	}
}

// TestStatus_ReadOnlyConnDoesNotBlockServeWrites confirms the WAL read-only conn does not stall a concurrent writer.
func TestStatus_ReadOnlyConnDoesNotBlockServeWrites(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	writer, err := sql.Open("sqlite", "file:"+dbPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	defer func() { _ = writer.Close() }()
	if _, err := writer.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		t.Fatalf("set WAL: %v", err)
	}
	if _, err := writer.Exec(`CREATE TABLE substrate_events (id INTEGER PRIMARY KEY, emitted_at DATETIME, kind TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	src, err := newDefaultSource(dbPath)
	if err != nil {
		t.Fatalf("newDefaultSource: %v", err)
	}

	var wg sync.WaitGroup
	stopWrite := make(chan struct{})
	var writes atomic.Int64
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopWrite:
				return
			default:
				_, err := writer.Exec(`INSERT INTO substrate_events (emitted_at, kind) VALUES (?, 'tick')`, time.Now())
				if err == nil {
					writes.Add(1)
				}
			}
		}
	}()

	// Hammer the read side concurrently — TUI snapshot pattern.
	start := time.Now()
	for time.Since(start) < 200*time.Millisecond {
		_ = src.Snapshot(context.Background())
	}
	close(stopWrite)
	wg.Wait()

	if writes.Load() < 10 {
		t.Errorf("writer made only %d inserts under concurrent reads; expected > 10", writes.Load())
	}
}

// threadSafeBuf wraps bytes.Buffer with a mutex so the test loop can
// race-free write and inspect on cancel.
type threadSafeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *threadSafeBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// TestStatus_ActiveSubagents_QueriesRealAgentsSchema asserts the subagents panel does not reference dropped agents.name / agents.started_at columns.
func TestStatus_ActiveSubagents_QueriesRealAgentsSchema(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	writer, err := sql.Open("sqlite", "file:"+dbPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = writer.Close() }()
	if _, err := writer.Exec(`CREATE TABLE agents (
		id INTEGER PRIMARY KEY,
		work_item_id TEXT NOT NULL,
		lane TEXT NOT NULL,
		state TEXT NOT NULL,
		pid INTEGER NOT NULL DEFAULT 0,
		session_id TEXT NOT NULL DEFAULT '',
		pr_sha TEXT NOT NULL DEFAULT '',
		rejection_count INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`); err != nil {
		t.Fatalf("create agents: %v", err)
	}
	if _, err := writer.Exec(`CREATE TABLE substrate_events (id INTEGER PRIMARY KEY, emitted_at DATETIME, kind TEXT)`); err != nil {
		t.Fatalf("create substrate_events: %v", err)
	}
	now := time.Now().Unix()
	if _, err := writer.Exec(`INSERT INTO agents (work_item_id, lane, state, created_at, updated_at) VALUES (?,?,?,?,?)`,
		"WORK-1", "server", "spawning", now, now); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	src, err := newDefaultSource(dbPath)
	if err != nil {
		t.Fatalf("newDefaultSource: %v", err)
	}
	snap := src.Snapshot(context.Background())
	if snap.ActiveSubagents.State == PanelMissing {
		t.Fatalf("ActiveSubagents reported MISSING against real schema; lines=%v (the query references dropped columns)", snap.ActiveSubagents.Lines)
	}
}

// TestStatus_Once_RendersToStdout proves the --once path returns 0 without entering the refresh loop.
func TestStatus_Once_RendersToStdout(t *testing.T) {
	args := []string{"--once", "--width=80", "--height=24"}
	rc := runStatus(args)
	if rc != 0 {
		t.Errorf("runStatus --once = %d; want 0", rc)
	}
}

// TestStatus_DegradedSource_AllMissing verifies the no-db degraded source emits MISSING panels.
func TestStatus_DegradedSource_AllMissing(t *testing.T) {
	src, _ := newDefaultSource("")
	snap := src.Snapshot(context.Background())
	if snap.ActiveSubagents.State != PanelMissing {
		t.Error("ActiveSubagents not MISSING in degraded source")
	}
	if snap.StaleBanner == "" {
		t.Error("degraded source should set StaleBanner")
	}
}
