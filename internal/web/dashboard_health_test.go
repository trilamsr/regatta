package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/health"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// transitionToRunning walks an agent pending → spawning → running so a test
// can seed the live-agent shape without re-stating the transition arms.
func transitionToRunning(t *testing.T, db *state.DB, agentID int64) {
	t.Helper()
	ctx := context.Background()
	pid := 1234
	sess := "sess-test"
	if _, err := db.TransitionAgent(ctx, agentID, state.AgentSpawning, state.AgentMutation{PID: &pid, SessionID: &sess}); err != nil {
		t.Fatalf("TransitionAgent spawning: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, agentID, state.AgentRunning, state.AgentMutation{}); err != nil {
		t.Fatalf("TransitionAgent running: %v", err)
	}
}

// TestLoadHealthSnapshot_GroupsAgentStates asserts O on I (#MAY-48 AC1).
func TestLoadHealthSnapshot_GroupsAgentStates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snap-agents.db")
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	a1, err := db.UpsertPending(ctx, "W-1", "server")
	if err != nil {
		t.Fatalf("UpsertPending W-1: %v", err)
	}
	a2, err := db.UpsertPending(ctx, "W-2", "server")
	if err != nil {
		t.Fatalf("UpsertPending W-2: %v", err)
	}
	a3, err := db.UpsertPending(ctx, "W-3", "server")
	if err != nil {
		t.Fatalf("UpsertPending W-3: %v", err)
	}
	transitionToRunning(t, db, a1.ID)
	transitionToRunning(t, db, a2.ID)
	pid := 1
	sess := "s"
	if _, err := db.TransitionAgent(ctx, a3.ID, state.AgentSpawning, state.AgentMutation{PID: &pid, SessionID: &sess}); err != nil {
		t.Fatalf("TransitionAgent a3 spawning: %v", err)
	}
	deps := Dependencies{DB: db, Clock: clock, TickInterval: 5 * time.Second}
	view := loadHealthSnapshotView(ctx, deps)
	snap, ok := view.(dashboardHealthSnapshot)
	if !ok {
		t.Fatalf("loadHealthSnapshotView returned %T, want dashboardHealthSnapshot", view)
	}
	if got := snap.AgentStateCounts[state.AgentRunning]; got != 2 {
		t.Fatalf("AgentStateCounts[running]=%d, want 2", got)
	}
	if got := snap.AgentStateCounts[state.AgentSpawning]; got != 1 {
		t.Fatalf("AgentStateCounts[spawning]=%d, want 1", got)
	}
}

// TestLoadHealthSnapshot_TickStaleBannerFires asserts O on I (#MAY-48 AC4).
func TestLoadHealthSnapshot_TickStaleBannerFires(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snap-stale.db")
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), func() time.Time { return now })
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cur := now
	hb := health.NewHeartbeatCell(func() time.Time { return cur })
	hb.Touch()
	cur = now.Add(15 * time.Second)
	deps := Dependencies{
		DB:           db,
		Clock:        func() time.Time { return cur },
		TickInterval: 5 * time.Second,
		Heartbeat:    hb,
	}
	view := loadHealthSnapshotView(context.Background(), deps)
	snap, ok := view.(dashboardHealthSnapshot)
	if !ok {
		t.Fatalf("loadHealthSnapshotView returned %T, want dashboardHealthSnapshot", view)
	}
	if !snap.TickStaleBannerVisible {
		t.Fatalf("TickStaleBannerVisible=false at age=15s with TickInterval=5s; banner MUST fire")
	}
	if snap.TickLastSuccessAgeSec < 15 {
		t.Fatalf("TickLastSuccessAgeSec=%d, want >=15", snap.TickLastSuccessAgeSec)
	}
}

// TestLoadHealthSnapshot_TickStaleBannerQuietBelowThreshold asserts O on I (#MAY-48 AC4 happy-path).
func TestLoadHealthSnapshot_TickStaleBannerQuietBelowThreshold(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snap-quiet.db")
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), func() time.Time { return now })
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cur := now
	hb := health.NewHeartbeatCell(func() time.Time { return cur })
	hb.Touch()
	cur = now.Add(3 * time.Second)
	deps := Dependencies{
		DB:           db,
		Clock:        func() time.Time { return cur },
		TickInterval: 5 * time.Second,
		Heartbeat:    hb,
	}
	view := loadHealthSnapshotView(context.Background(), deps)
	snap, ok := view.(dashboardHealthSnapshot)
	if !ok {
		t.Fatalf("loadHealthSnapshotView returned %T, want dashboardHealthSnapshot", view)
	}
	if snap.TickStaleBannerVisible {
		t.Fatalf("TickStaleBannerVisible=true at age=3s with TickInterval=5s; banner MUST stay quiet")
	}
}

// TestLoadHealthSnapshot_ExitReasonHistogram asserts O on I (#MAY-48 AC3).
func TestLoadHealthSnapshot_ExitReasonHistogram(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snap-hist.db")
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "W-1", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"completed"}`); err != nil {
		t.Fatalf("RecordEvent completed: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"completed"}`); err != nil {
		t.Fatalf("RecordEvent completed 2: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"provider_credit_exhausted"}`); err != nil {
		t.Fatalf("RecordEvent credit: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{}`); err != nil {
		t.Fatalf("RecordEvent empty: %v", err)
	}
	deps := Dependencies{DB: db, Clock: clock, TickInterval: 5 * time.Second}
	view := loadHealthSnapshotView(ctx, deps)
	snap, ok := view.(dashboardHealthSnapshot)
	if !ok {
		t.Fatalf("loadHealthSnapshotView returned %T, want dashboardHealthSnapshot", view)
	}
	if len(snap.LastExitReasonHistogram) != 3 {
		t.Fatalf("histogram rows=%d, want 3 (completed, provider_credit_exhausted, unknown); got %+v",
			len(snap.LastExitReasonHistogram), snap.LastExitReasonHistogram)
	}
	gotCounts := map[string]int{}
	for _, row := range snap.LastExitReasonHistogram {
		gotCounts[row.Reason] = row.Count
	}
	if gotCounts["completed"] != 2 {
		t.Fatalf("completed count=%d, want 2", gotCounts["completed"])
	}
	if gotCounts["provider_credit_exhausted"] != 1 {
		t.Fatalf("provider_credit_exhausted count=%d, want 1", gotCounts["provider_credit_exhausted"])
	}
	if gotCounts[exitReasonUnknown] != 1 {
		t.Fatalf("unknown count=%d, want 1", gotCounts[exitReasonUnknown])
	}
}

// TestLoadHealthSnapshot_RunningConsistentWithPipeline asserts O on I (#1217).
func TestLoadHealthSnapshot_RunningConsistentWithPipeline(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "snap-consistent.db")
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	for _, id := range []string{"W-A", "W-B", "W-C"} {
		a, err := db.UpsertPending(ctx, id, "server")
		if err != nil {
			t.Fatalf("UpsertPending %s: %v", id, err)
		}
		transitionToRunning(t, db, a.ID)
	}
	deps := Dependencies{DB: db, Clock: clock, TickInterval: 5 * time.Second}
	snap, ok := loadHealthSnapshotView(ctx, deps).(dashboardHealthSnapshot)
	if !ok {
		t.Fatalf("loadHealthSnapshotView returned non-snapshot")
	}
	if snap.AgentStateCounts[state.AgentRunning] != 3 {
		t.Fatalf("snapshot Running=%d, want 3", snap.AgentStateCounts[state.AgentRunning])
	}
	pipeline, ok := loadPipelineView(ctx, deps).(dashboardPipelineView)
	if !ok {
		t.Fatalf("loadPipelineView returned non-pipeline")
	}
	var pipelineRunning int
	for _, s := range pipeline.Stages {
		if s.Slug == pipelineStageRunning {
			pipelineRunning = s.Count
		}
	}
	if pipelineRunning != snap.AgentStateCounts[state.AgentRunning] {
		t.Fatalf("pipeline Running=%d != snapshot Running=%d (cross-panel drift, #1217)",
			pipelineRunning, snap.AgentStateCounts[state.AgentRunning])
	}
	workItems, ok := loadWorkItemsView(ctx, deps).(dashboardWorkItemsView)
	if !ok {
		t.Fatalf("loadWorkItemsView returned non-work-items")
	}
	var wiRunning int
	for _, b := range workItems.Buckets {
		if b.Label == bucketLabelRunning {
			wiRunning = b.Count
		}
	}
	if wiRunning != snap.AgentStateCounts[state.AgentRunning] {
		t.Fatalf("work-items Running=%d != snapshot Running=%d (cross-panel drift, #1217)",
			wiRunning, snap.AgentStateCounts[state.AgentRunning])
	}
}

// TestHealthPanelRoute_RendersBannerWhenStale asserts O on I (#MAY-48 AC4 wire).
func TestHealthPanelRoute_RendersBannerWhenStale(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	dbPath := filepath.Join(t.TempDir(), "snap-route.db")
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), func() time.Time { return now })
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cur := now
	hb := health.NewHeartbeatCell(func() time.Time { return cur })
	hb.Touch()
	cur = now.Add(30 * time.Second)
	h := NewHandler(Dependencies{
		Templates:    tmpls,
		DB:           db,
		Clock:        func() time.Time { return cur },
		TickInterval: 5 * time.Second,
		Heartbeat:    hb,
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/panels/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/ui/panels/health: status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "tick-stale") {
		t.Fatalf("body missing tick-stale marker: %s", body)
	}
}
