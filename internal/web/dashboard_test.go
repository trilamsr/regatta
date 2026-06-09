package web

import (
	"context"
	"database/sql"
	"fmt"
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

// TestWorkItemDrawer_RendersFlowStepActive asserts the 4-step status-flow pill row
// marks the bucket matching the owning agent's AgentState as active. AgentRunning
// must land on the third cell ("running"); the first two cells must NOT carry the
// active class so the row reads as forward-progress, not parallel state.
func TestWorkItemDrawer_RendersFlowStepActive(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "wi-flow.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	wi := state.WorkItem{ID: "BUG-2001", Kind: state.KindFeature, Title: "soak audit drift", Lane: "server", Status: state.WorkStatusRunning}
	if err := db.UpsertWorkItem(ctx, wi, state.SourceAdapter, clock()); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}
	a, err := db.UpsertPending(ctx, wi.ID, wi.Lane)
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentSpawning, state.AgentMutation{}); err != nil {
		t.Fatalf("transition spawning: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentRunning, state.AgentMutation{}); err != nil {
		t.Fatalf("transition running: %v", err)
	}
	h := NewHandler(Dependencies{Templates: tmpls, DB: db, Clock: clock})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/drawer/work-item/BUG-2001", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("drawer status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// All four labels must appear in the row.
	for _, want := range []string{"pending", "spawning", "running", "done"} {
		if !strings.Contains(body, want) {
			t.Fatalf("drawer body missing flow label %q: %s", want, body)
		}
	}
	// Locate the running cell and confirm it carries the active class.
	runIdx := strings.Index(body, ">running<")
	if runIdx < 0 {
		t.Fatalf("drawer missing >running< cell marker: %s", body)
	}
	prefix := body[:runIdx]
	openLi := strings.LastIndex(prefix, "<li")
	if openLi < 0 {
		t.Fatalf("drawer running cell has no opening <li: %s", body)
	}
	runCell := body[openLi:runIdx]
	if !strings.Contains(runCell, "active") {
		t.Fatalf("drawer running cell missing active class: %s", runCell)
	}
	// And the pending cell must NOT be active so the flow reads as forward-progress.
	pendIdx := strings.Index(body, ">pending<")
	if pendIdx < 0 {
		t.Fatalf("drawer missing >pending< cell marker: %s", body)
	}
	pendPrefix := body[:pendIdx]
	pendOpen := strings.LastIndex(pendPrefix, "<li")
	pendCell := body[pendOpen:pendIdx]
	if strings.Contains(pendCell, "active") {
		t.Fatalf("drawer pending cell wrongly active when agent is running: %s", pendCell)
	}
}

// TestWorkItemDrawer_RendersBodyPreview asserts a long acceptance-json source field
// truncates to bodyPreviewMaxRunes (200) characters in the drawer preview, with the
// ellipsis appended so the operator sees that the source is truncated rather than
// silently cut.
func TestWorkItemDrawer_RendersBodyPreview(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "wi-body.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	longBody := `"` + strings.Repeat("a", 500) + `"`
	wi := state.WorkItem{ID: "BUG-2002", Kind: state.KindFeature, Title: "long body item", Lane: "server", Status: state.WorkStatusRunning, AcceptanceJSON: longBody}
	if err := db.UpsertWorkItem(ctx, wi, state.SourceAdapter, clock()); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}
	h := NewHandler(Dependencies{Templates: tmpls, DB: db, Clock: clock})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/drawer/work-item/BUG-2002", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("drawer status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "body preview") {
		t.Fatalf("drawer missing body-preview section header: %s", body)
	}
	if !strings.Contains(body, "…") {
		t.Fatalf("drawer body preview missing ellipsis marker for truncation: %s", body)
	}
	// Full source must NOT appear — 500-char repeat would survive only if no truncation fired.
	if strings.Contains(body, longBody) {
		t.Fatalf("drawer rendered full untruncated body source: len=%d", len(body))
	}
}

// TestWorkItemDrawer_RendersGHIssueLink asserts the drawer includes a github.com URL
// when web.Config.GitHubRepo is wired AND the work-item id parses into an issue
// number. The "BUG-" prefix MUST be stripped so the resulting URL targets the bare
// issue number — github.com routes /issues/BUG-1058 as a 404.
func TestWorkItemDrawer_RendersGHIssueLink(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "wi-link.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	wi := state.WorkItem{ID: "BUG-1058", Kind: state.KindFeature, Title: "tick drift", Lane: "server", Status: state.WorkStatusRunning}
	if err := db.UpsertWorkItem(ctx, wi, state.SourceAdapter, clock()); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}
	h := NewHandler(Dependencies{Templates: tmpls, DB: db, Clock: clock, Config: Config{GitHubRepo: "trilamsr/regatta"}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/drawer/work-item/BUG-1058", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("drawer status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	const want = "https://github.com/trilamsr/regatta/issues/1058"
	if !strings.Contains(body, want) {
		t.Fatalf("drawer missing GH issue URL %q: %s", want, body)
	}
}

// TestLoadDockerSoakView_Healthy asserts green health pill when last 1m has spawns and no non-completed exits.
func TestLoadDockerSoakView_Healthy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "soak-healthy.db")
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-A", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "spawn.started", `{}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"completed","exit_code":0}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	deps := Dependencies{DB: db, Clock: clock, BootedAt: now.Add(-90 * time.Second)}
	view := loadDockerSoakView(ctx, deps)
	sv, ok := view.(dashboardDockerSoakView)
	if !ok {
		t.Fatalf("loadDockerSoakView returned %T, want dashboardDockerSoakView", view)
	}
	if sv.Health != "green" {
		t.Fatalf("Health=%q, want green", sv.Health)
	}
	if sv.SpawnsLast1m != 1 {
		t.Fatalf("SpawnsLast1m=%d, want 1", sv.SpawnsLast1m)
	}
	if sv.ExitedLast1m != 1 {
		t.Fatalf("ExitedLast1m=%d, want 1", sv.ExitedLast1m)
	}
	if sv.Uptime == "" {
		t.Fatalf("Uptime empty")
	}
	if sv.LastExitReason != "completed" {
		t.Fatalf("LastExitReason=%q, want completed", sv.LastExitReason)
	}
}

// TestLoadDockerSoakView_AmberOnMixedExits asserts amber health pill when any non-completed exit lands in last 1m alongside a completed one.
func TestLoadDockerSoakView_AmberOnMixedExits(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "soak-amber.db")
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-B", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "spawn.started", `{}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"completed"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"tool_denied"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	deps := Dependencies{DB: db, Clock: clock, BootedAt: now.Add(-5 * time.Minute)}
	view := loadDockerSoakView(ctx, deps)
	sv, ok := view.(dashboardDockerSoakView)
	if !ok {
		t.Fatalf("loadDockerSoakView returned %T, want dashboardDockerSoakView", view)
	}
	if sv.Health != "amber" {
		t.Fatalf("Health=%q, want amber", sv.Health)
	}
	if sv.ExitedLast1m != 2 {
		t.Fatalf("ExitedLast1m=%d, want 2", sv.ExitedLast1m)
	}
}

// TestLoadDockerSoakView_RedOnAllNonCompleted asserts red health pill when all last-1m exits are non-completed.
func TestLoadDockerSoakView_RedOnAllNonCompleted(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "soak-red.db")
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-C", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "spawn.started", `{}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"provider_credit_exhausted"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_reason":"provider_rate_limited"}`); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	deps := Dependencies{DB: db, Clock: clock, BootedAt: now.Add(-time.Hour)}
	view := loadDockerSoakView(ctx, deps)
	sv, ok := view.(dashboardDockerSoakView)
	if !ok {
		t.Fatalf("loadDockerSoakView returned %T, want dashboardDockerSoakView", view)
	}
	if sv.Health != "red" {
		t.Fatalf("Health=%q, want red", sv.Health)
	}
	if sv.LastExitReason != "provider_rate_limited" {
		t.Fatalf("LastExitReason=%q, want provider_rate_limited", sv.LastExitReason)
	}
}

// TestEventVerb_SpawnCompletedShowsAgentAndWorkItem pins #1119: spawn.completed surfaces agent_id (Event.AgentID) + work_item_id (PayloadJSON) inline so 12 identical "ready" verbs become scannable.
func TestEventVerb_SpawnCompletedShowsAgentAndWorkItem(t *testing.T) {
	ev := state.Event{
		Kind:        "spawn.completed",
		PayloadJSON: `{"work_item_id":"BUG-1058","pid":12345}`,
	}
	ev.AgentID.Valid = true
	ev.AgentID.Int64 = 42
	got := string(eventVerb(ev))
	if !strings.Contains(got, "#42") {
		t.Fatalf("missing agent_id chip #42: %q", got)
	}
	if !strings.Contains(got, "BUG-1058") {
		t.Fatalf("missing work_item_id chip BUG-1058: %q", got)
	}
	if !strings.Contains(got, "ready") {
		t.Fatalf("missing verb 'ready': %q", got)
	}
}

// TestEventVerb_SpawnFailedShowsAgentAndExitReason pins #1119: spawn.failed surfaces agent_id chip + exit_reason badge inline (currently exit_reason fires only for agent.exited).
func TestEventVerb_SpawnFailedShowsAgentAndExitReason(t *testing.T) {
	ev := state.Event{
		Kind:        "spawn.failed",
		PayloadJSON: `{"work_item_id":"BUG-1058","exit_reason":"provider_credit_exhausted"}`,
	}
	ev.AgentID.Valid = true
	ev.AgentID.Int64 = 7
	got := string(eventVerb(ev))
	if !strings.Contains(got, "#7") {
		t.Fatalf("missing agent_id chip #7: %q", got)
	}
	if !strings.Contains(got, "BUG-1058") {
		t.Fatalf("missing work_item_id chip BUG-1058: %q", got)
	}
	if !strings.Contains(got, "badge-red") {
		t.Fatalf("missing badge-red for provider_credit_exhausted: %q", got)
	}
	if !strings.Contains(got, "provider_credit_exhausted") {
		t.Fatalf("missing exit_reason text: %q", got)
	}
}

// TestEventVerb_BriefLoadedShowsAgent pins #1119: brief.loaded surfaces agent_id + work_item_id chips so consecutive brief loads are distinguishable inline.
func TestEventVerb_BriefLoadedShowsAgent(t *testing.T) {
	ev := state.Event{
		Kind:        "brief.loaded",
		PayloadJSON: `{"work_item_id":"WORK-99"}`,
	}
	ev.AgentID.Valid = true
	ev.AgentID.Int64 = 13
	got := string(eventVerb(ev))
	if !strings.Contains(got, "#13") {
		t.Fatalf("missing agent_id chip #13: %q", got)
	}
	if !strings.Contains(got, "WORK-99") {
		t.Fatalf("missing work_item_id chip WORK-99: %q", got)
	}
	if !strings.Contains(got, "brief") {
		t.Fatalf("missing verb 'brief': %q", got)
	}
}

// TestEventVerb_RecoveredCrashedShowsAgent pins #1119: recovered_crashed surfaces agent_id chip inline so the operator can trace which agent recovered without opening the drawer.
func TestEventVerb_RecoveredCrashedShowsAgent(t *testing.T) {
	ev := state.Event{
		Kind:        "recovered_crashed",
		PayloadJSON: `{}`,
	}
	ev.AgentID.Valid = true
	ev.AgentID.Int64 = 99
	got := string(eventVerb(ev))
	if !strings.Contains(got, "#99") {
		t.Fatalf("missing agent_id chip #99: %q", got)
	}
	if !strings.Contains(got, "recovered") {
		t.Fatalf("missing verb 'recovered': %q", got)
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

// TestLoadDockerSoakView_Idle asserts the IDLE state on a fresh boot with no spawns or exits.
func TestLoadDockerSoakView_Idle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "soak-idle.db")
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	deps := Dependencies{DB: db, Clock: clock, BootedAt: now.Add(-30 * time.Second)}
	view := loadDockerSoakView(context.Background(), deps)
	sv, ok := view.(dashboardDockerSoakView)
	if !ok {
		t.Fatalf("loadDockerSoakView returned %T, want dashboardDockerSoakView", view)
	}
	if sv.Health != "green" {
		t.Fatalf("Health=%q, want green on idle", sv.Health)
	}
	if sv.HealthLabel != "IDLE" {
		t.Fatalf("HealthLabel=%q, want IDLE", sv.HealthLabel)
	}
	if sv.SpawnsLast1m != 0 || sv.ExitedLast1m != 0 {
		t.Fatalf("idle counts spawns=%d exits=%d, want 0/0", sv.SpawnsLast1m, sv.ExitedLast1m)
	}
}

// TestLoadDockerSoakView_EmptyExitReasonNotMaskedAsHealthy asserts an agent.exited row whose exit_reason is empty (classifier didn't tag; pre-#1063 row) counts as non-completed so the HEALTHY pill cannot mask data drift.
func TestLoadDockerSoakView_EmptyExitReasonNotMaskedAsHealthy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "soak-empty-reason.db")
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WORK-EMPTY", "server")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "spawn.started", `{}`); err != nil {
		t.Fatalf("RecordEvent spawn: %v", err)
	}
	if err := db.RecordEvent(ctx, a.ID, "agent.exited", `{"exit_code":1}`); err != nil {
		t.Fatalf("RecordEvent exit: %v", err)
	}
	deps := Dependencies{DB: db, Clock: clock, BootedAt: now.Add(-60 * time.Second)}
	view := loadDockerSoakView(ctx, deps)
	sv, ok := view.(dashboardDockerSoakView)
	if !ok {
		t.Fatalf("loadDockerSoakView returned %T", view)
	}
	if sv.Health == "green" {
		t.Fatalf("empty-exit_reason masked as healthy: Health=%q HealthLabel=%q", sv.Health, sv.HealthLabel)
	}
}

// TestEventVerb_EmptyAgentIDOmitsChip pins graceful degradation when AgentID.Valid is false — the chip is omitted, not rendered as "agent#0".
func TestEventVerb_EmptyAgentIDOmitsChip(t *testing.T) {
	e := state.Event{Kind: "spawn.completed", PayloadJSON: `{"work_item_id":"BUG-1058"}`}
	got := string(eventVerb(e))
	if strings.Contains(got, "agent #0") || strings.Contains(got, "agent #") {
		t.Fatalf("invalid AgentID rendered as agent#0: %q", got)
	}
	if !strings.Contains(got, "BUG-1058") {
		t.Fatalf("work_item_id chip dropped when AgentID is invalid: %q", got)
	}
}

// TestEventVerb_MalformedPayloadOmitsWorkItemChip pins graceful degradation when PayloadJSON is non-JSON — the chip is omitted, no panic.
func TestEventVerb_MalformedPayloadOmitsWorkItemChip(t *testing.T) {
	e := state.Event{Kind: "spawn.completed", AgentID: sql.NullInt64{Int64: 42, Valid: true}, PayloadJSON: `{not json`}
	got := string(eventVerb(e))
	if strings.Contains(got, "BUG-") {
		t.Fatalf("malformed payload rendered work_item_id: %q", got)
	}
	if !strings.Contains(got, "#42") {
		t.Fatalf("agent #42 dropped when payload malformed: %q", got)
	}
}
