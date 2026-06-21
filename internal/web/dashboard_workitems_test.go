package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

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

// TestLoadWorkItemsView_EmptyHintWhenNoItems asserts the work-items view carries an EmptyHint that points to the adapter selector when no items are present, so the operator knows to check regatta.yaml.
func TestLoadWorkItemsView_EmptyHintWhenNoItems(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wi-empty.db")
	clock := func() time.Time { return time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	view := loadWorkItemsView(context.Background(), Dependencies{DB: db, Clock: clock})
	v, ok := view.(dashboardWorkItemsView)
	if !ok {
		t.Fatalf("loadWorkItemsView returned %T, want dashboardWorkItemsView", view)
	}
	total := 0
	for _, b := range v.Buckets {
		total += b.Count
	}
	if total != 0 {
		t.Fatalf("bucket total=%d, want 0 on empty DB", total)
	}
	if v.EmptyHint == "" {
		t.Fatalf("EmptyHint empty; want operator-friendly copy when zero work-items")
	}
	if !strings.Contains(v.EmptyHint, "spec_adapter.selector") {
		t.Fatalf("EmptyHint missing selector pointer: %q", v.EmptyHint)
	}
}

// TestLoadWorkItemsView_HintHidesBucketsWhenEmpty asserts the work-items template renders ONLY the EmptyHint when set — bucket grid stays hidden so the panel does not show contradictory "empty" + four zero-count cards (#1146 reviewer REVISE).
func TestLoadWorkItemsView_HintHidesBucketsWhenEmpty(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	labels := []string{"Planned", bucketLabelRunning, statusLabelPROpen, "merged-bucket-sentinel"}
	buckets := make([]dashboardBucket, len(labels))
	for i, l := range labels {
		buckets[i] = dashboardBucket{Label: l, Count: 0}
	}
	view := dashboardWorkItemsView{EmptyHint: emptyHintWorkItems, Buckets: buckets}
	rec := httptest.NewRecorder()
	if err := tmpls.Render(rec, "_work_items", view); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, emptyHintWorkItems) {
		t.Fatalf("body missing EmptyHint copy: %s", body)
	}
	for _, label := range labels {
		if strings.Contains(body, label) {
			t.Fatalf("bucket label %q present alongside EmptyHint; want either/or rendering: %s", label, body)
		}
	}
}

// TestLoadWorkItemsView_RunningReflectsAgents asserts work-items panel running-bucket reflects active agents not work_items.status (#1217).
func TestLoadWorkItemsView_RunningReflectsAgents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wi-running.db")
	clock := func() time.Time { return time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		workID := fmt.Sprintf("WORK-%d", i+1)
		wi := state.WorkItem{ID: workID, Kind: state.KindFeature, Title: "title", Lane: "server", Status: state.WorkStatusPlanned}
		if err := db.UpsertWorkItem(ctx, wi, state.SourceAdapter, clock()); err != nil {
			t.Fatalf("UpsertWorkItem %s: %v", workID, err)
		}
		a, err := db.UpsertPending(ctx, workID, "server")
		if err != nil {
			t.Fatalf("UpsertPending %s: %v", workID, err)
		}
		pid := 1000 + i
		sess := fmt.Sprintf("sess-%d", i)
		if _, err := db.TransitionAgent(ctx, a.ID, state.AgentSpawning, state.AgentMutation{PID: &pid, SessionID: &sess}); err != nil {
			t.Fatalf("TransitionAgent spawning %s: %v", workID, err)
		}
		if _, err := db.TransitionAgent(ctx, a.ID, state.AgentRunning, state.AgentMutation{}); err != nil {
			t.Fatalf("TransitionAgent running %s: %v", workID, err)
		}
	}
	view := loadWorkItemsView(ctx, Dependencies{DB: db, Clock: clock})
	v, ok := view.(dashboardWorkItemsView)
	if !ok {
		t.Fatalf("loadWorkItemsView returned %T, want dashboardWorkItemsView", view)
	}
	var running *dashboardBucket
	for i := range v.Buckets {
		if v.Buckets[i].Label == bucketLabelRunning {
			running = &v.Buckets[i]
			break
		}
	}
	if running == nil {
		t.Fatalf("Running bucket not found in %v", v.Buckets)
	}
	if running.Count != 3 {
		t.Fatalf("Running bucket count=%d, want 3 (agents alive while work_items.status stays planned)", running.Count)
	}
}
