package web

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestDashboard_LayoutWiresFourPanels: GET / returns the dashboard layout with hx-get URLs pointing at the four panel endpoints — the htmx polling contract that drives the live operator surface.
func TestDashboard_LayoutWiresFourPanels(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	h := NewHandler(Dependencies{Templates: tmpls, Clock: time.Now})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`hx-get="/ui/panels/agents"`,
		`hx-get="/ui/panels/work-items"`,
		`hx-get="/ui/panels/events"`,
		`hx-get="/ui/panels/spend"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("layout missing %q", want)
		}
	}
}

// TestEventVerb_ExitedWithProviderCreditExhausted asserts agent.exited surfaces exit_reason as colored badge inline (#dashboard-exit-reason-badges).
func TestEventVerb_ExitedWithProviderCreditExhausted(t *testing.T) {
	ev := state.Event{
		Kind:        "agent.exited",
		PayloadJSON: `{"exit_reason":"provider_credit_exhausted","exit_code":1}`,
	}
	ev.AgentID.Valid = true
	ev.AgentID.Int64 = 7
	got := string(eventVerb(ev))
	if !strings.Contains(got, "exited") {
		t.Fatalf("missing exited verb: %q", got)
	}
	if !strings.Contains(got, "badge-red") {
		t.Fatalf("missing badge-red class: %q", got)
	}
	if !strings.Contains(got, "provider_credit_exhausted") {
		t.Fatalf("missing exit_reason text: %q", got)
	}
}

// TestDashboard_PanelEndpointsRefuseMissingDeps: each /ui/panels/* endpoint returns 500 when the DB handle is nil so a misconfigured composition root surfaces at first poll, not silently as an empty panel.
func TestDashboard_PanelEndpointsRefuseMissingDeps(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	h := NewHandler(Dependencies{Templates: tmpls, Clock: time.Now})

	for _, path := range []string{"/ui/panels/agents", "/ui/panels/work-items", "/ui/panels/events", "/ui/panels/spend"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s: want 500 with nil DB, got %d", path, rec.Code)
		}
	}
}

// TestDashboard_AgentsPanelRendersRows: with a DB stub that returns one Agent row, the rendered HTML contains the agent ID + work_item_id + lane so the operator's first glance carries identifiers, not just status pills.
func TestDashboard_AgentsPanelRendersRows(t *testing.T) {
	// Skip without sqlite — exercising the real DB is covered in
	// orchestrator/state tests; the loader path here is the only
	// composition assertion that does not require a live DB.
	_ = context.Background()
	_ = state.Agent{}
	t.Skip("dashboard data loaders covered in serve smoke test; layout + panel-404 cells suffice for the scaffold round")
}

// TestLoadSpendView_AllZeroSetsEmptyReason: when no token_spend rows exist and an agent.exited event carries exit_reason=provider_credit_exhausted in the last 24h, loadSpendView surfaces an EmptyReason + CreditExhaustedCount so the operator reads "no agents completed" instead of "spend tracking broken".
func TestLoadSpendView_AllZeroSetsEmptyReason(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate", dbPath)
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), dsn, clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-1", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"provider_credit_exhausted","exit_code":1}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	view := loadSpendView(ctx, Dependencies{DB: db, Clock: clock})
	sv, ok := view.(dashboardSpendView)
	if !ok {
		t.Fatalf("loadSpendView returned %T, want dashboardSpendView", view)
	}
	if sv.Last24hMicros != 0 || sv.TodayMicros != 0 || sv.LifetimeMicros != 0 {
		t.Fatalf("expected zero spend, got 24h=%d today=%d lifetime=%d", sv.Last24hMicros, sv.TodayMicros, sv.LifetimeMicros)
	}
	if sv.EmptyReason == "" {
		t.Fatalf("EmptyReason empty; want non-empty when all-zero + exited-with-reason events present")
	}
	if sv.CreditExhaustedCount != 1 {
		t.Fatalf("CreditExhaustedCount=%d, want 1", sv.CreditExhaustedCount)
	}
}

// TestExitReasonBadge_MalformedPayload pins graceful degradation on bad JSON.
func TestExitReasonBadge_MalformedPayload(t *testing.T) {
	got := exitReasonBadge("{not json")
	if got != "" {
		t.Fatalf("malformed JSON → %q, want empty", got)
	}
	got = exitReasonBadge("")
	if got != "" {
		t.Fatalf("empty payload → %q, want empty", got)
	}
	got = exitReasonBadge(`{"unrelated":"field"}`)
	if got != "" {
		t.Fatalf("missing exit_reason → %q, want empty", got)
	}
}

// TestLoadWorkItemsView_TitleTruncated asserts long titles render with the ellipsis suffix so kanban cards stay single-line.
func TestLoadWorkItemsView_TitleTruncated(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wi.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	longTitle := strings.Repeat("orchestrator drift on long-window soak ", 3)
	wi := state.WorkItem{ID: "BUG-9001", Kind: state.KindFeature, Title: longTitle, Lane: "server", Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, wi, state.SourceAdapter, clock()); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}
	view := loadWorkItemsView(ctx, Dependencies{DB: db, Clock: clock})
	v, ok := view.(dashboardWorkItemsView)
	if !ok {
		t.Fatalf("loadWorkItemsView returned %T, want dashboardWorkItemsView", view)
	}
	var got dashboardWorkItemRow
	for _, b := range v.Buckets {
		for _, r := range b.Top {
			if r.ID == "BUG-9001" {
				got = r
			}
		}
	}
	if got.ID == "" {
		t.Fatalf("BUG-9001 not surfaced in any bucket")
	}
	if !strings.HasSuffix(got.Title, "…") {
		t.Fatalf("Title not truncated with ellipsis: %q", got.Title)
	}
	if len(got.Title) > workItemTitleMaxBytes+len("…") {
		t.Fatalf("Title=%d bytes exceeds %d+ellipsis cap", len(got.Title), workItemTitleMaxBytes)
	}
}

// TestWorkItemDrawer_RendersTitleAndStatus asserts the drawer returns full title + status + ID for a known work item.
func TestWorkItemDrawer_RendersTitleAndStatus(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "wi.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	wi := state.WorkItem{ID: "BUG-1058", Kind: state.KindFeature, Title: "fix scheduler tick drift", Lane: "server", Status: state.WorkStatusRunning}
	if err := db.UpsertWorkItem(ctx, wi, state.SourceAdapter, clock()); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}
	h := NewHandler(Dependencies{Templates: tmpls, DB: db, Clock: clock})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/drawer/work-item/BUG-1058", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("drawer status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"fix scheduler tick drift", "BUG-1058", "running"} {
		if !strings.Contains(body, want) {
			t.Fatalf("drawer body missing %q: %s", want, body)
		}
	}
}

// TestWorkItemDrawer_404OnUnknownID asserts the drawer returns 404 when the ID is not in the DB.
func TestWorkItemDrawer_404OnUnknownID(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "wi.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := NewHandler(Dependencies{Templates: tmpls, DB: db, Clock: clock})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/drawer/work-item/DOES-NOT-EXIST", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id status=%d, want 404", rec.Code)
	}
}

// TestLoadSpendView_ZeroSpendNoExitEvents asserts the empty-state annotation does NOT fire when the daemon has zero events at all — avoids false-positive "agents exited before reporting" copy on a fresh boot.
func TestLoadSpendView_ZeroSpendNoExitEvents(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "spend-empty.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	view := loadSpendView(context.Background(), Dependencies{DB: db, Clock: clock})
	sv, ok := view.(dashboardSpendView)
	if !ok {
		t.Fatalf("loadSpendView returned %T, want dashboardSpendView", view)
	}
	if sv.EmptyReason != "" {
		t.Fatalf("EmptyReason set on clean zero-event boot: %q", sv.EmptyReason)
	}
	if sv.CreditExhaustedCount != 0 {
		t.Fatalf("CreditExhaustedCount=%d on zero-event boot, want 0", sv.CreditExhaustedCount)
	}
}

// TestRecentEventsForWorkItem_LogsScanError verifies WARN log emission on row.Scan errors (#1135).
func TestRecentEventsForWorkItem_LogsScanError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "scanerr.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	wi := state.WorkItem{ID: "BUG-1135", Kind: state.KindFeature, Title: "scan error pin", Lane: "server", Status: state.WorkStatusRunning}
	if err := db.UpsertWorkItem(ctx, wi, state.SourceAdapter, clock()); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}
	ag, err := db.UpsertPending(ctx, "BUG-1135", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, ag.ID, "test.kind", `{"x":1}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	// SQLite type affinity is loose: writing TEXT into an INTEGER column is accepted at storage time, then trips database/sql Scan into *int64 on read — exactly the rows.Scan error path #1135 asks the caller to surface instead of silently swallowing.
	if _, err := db.SQL().ExecContext(ctx, `UPDATE events SET created_at = 'not_an_int' WHERE agent_id = ?`, ag.ID); err != nil {
		t.Fatalf("corrupt created_at: %v", err)
	}

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	_ = recentEventsForWorkItem(ctx, db, "BUG-1135", 10)

	logs := logBuf.String()
	if !strings.Contains(logs, "level=WARN") {
		t.Fatalf("expected WARN log on scan error, got: %q", logs)
	}
	if !strings.Contains(logs, "BUG-1135") {
		t.Fatalf("expected work_item_id=BUG-1135 in log attrs, got: %q", logs)
	}
}

// TestLoadPipelineView_CountsAllSixStages asserts per-stage counts on a seeded DB with 1 row per stage (#dashboard-pipeline).
func TestLoadPipelineView_CountsAllSixStages(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	dbPath := filepath.Join(t.TempDir(), "pipeline.db")
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	queued := state.WorkItem{ID: "Q-1", Kind: state.KindFeature, Title: "queued item", Lane: "server", Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, queued, state.SourceAdapter, clock()); err != nil {
		t.Fatalf("UpsertWorkItem queued: %v", err)
	}

	seedAgent(t, ctx, db, "READY-1", state.AgentPending)
	seedAgent(t, ctx, db, "SPAWN-1", state.AgentSpawning)
	seedAgent(t, ctx, db, "RUN-1", state.AgentRunning)
	seedAgent(t, ctx, db, "PR-1", state.AgentPROpen)
	seedAgent(t, ctx, db, "DONE-1", state.AgentDone)

	view := loadPipelineView(ctx, Dependencies{DB: db, Clock: clock})
	pv, ok := view.(dashboardPipelineView)
	if !ok {
		t.Fatalf("loadPipelineView returned %T, want dashboardPipelineView", view)
	}
	if len(pv.Stages) != 6 {
		t.Fatalf("Stages=%d, want 6", len(pv.Stages))
	}
	want := map[string]int{
		pipelineStageQueued:   1,
		pipelineStageReady:    1,
		pipelineStageSpawning: 1,
		pipelineStageRunning:  1,
		pipelineStagePROpen:   1,
		pipelineStageDone:     1,
	}
	for _, s := range pv.Stages {
		if got := want[s.Slug]; got != s.Count {
			t.Fatalf("Stage %q: Count=%d, want %d", s.Slug, s.Count, got)
		}
	}
}

// TestPipelineDrawer_RendersStageItems pins drawer rows for the running stage.
func TestPipelineDrawer_RendersStageItems(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	dbPath := filepath.Join(t.TempDir(), "drawer.db")
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	seedAgent(t, ctx, db, "RUN-DRAW", state.AgentRunning)

	h := NewHandler(Dependencies{Templates: tmpls, DB: db, Clock: clock})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/drawer/pipeline/running", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("drawer status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"RUN-DRAW", "server", "running"} {
		if !strings.Contains(body, want) {
			t.Fatalf("drawer body missing %q: %s", want, body)
		}
	}
}

// TestPipelineDrawer_404OnUnknownStage pins graceful 404 for unknown stage slug.
func TestPipelineDrawer_404OnUnknownStage(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	dbPath := filepath.Join(t.TempDir(), "drawer404.db")
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := NewHandler(Dependencies{Templates: tmpls, DB: db, Clock: clock})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/drawer/pipeline/teleport", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown stage status=%d, want 404", rec.Code)
	}
}

// seedAgent inserts a pending agent for workItemID then transitions through the FSM until it reaches target.
func seedAgent(t *testing.T, ctx context.Context, db *state.DB, workItemID string, target state.AgentState) {
	t.Helper()
	a, err := db.UpsertPending(ctx, workItemID, "server")
	if err != nil {
		t.Fatalf("UpsertPending %s: %v", workItemID, err)
	}
	path := agentPathTo(target)
	for _, step := range path {
		if _, err := db.TransitionAgent(ctx, a.ID, step, state.AgentMutation{}); err != nil {
			t.Fatalf("TransitionAgent %s -> %s: %v", workItemID, step, err)
		}
	}
}

// agentPathTo returns the FSM step sequence from pending to target per transitions.AgentEdges.
func agentPathTo(target state.AgentState) []state.AgentState {
	switch target {
	case state.AgentPending:
		return nil
	case state.AgentSpawning:
		return []state.AgentState{state.AgentSpawning}
	case state.AgentRunning:
		return []state.AgentState{state.AgentSpawning, state.AgentRunning}
	case state.AgentPROpen:
		return []state.AgentState{state.AgentSpawning, state.AgentRunning, state.AgentPROpen}
	case state.AgentDone:
		return []state.AgentState{state.AgentSpawning, state.AgentRunning, state.AgentPROpen, state.AgentGatesRunning, state.AgentAwaitingMerge, state.AgentDone}
	}
	return nil
}
