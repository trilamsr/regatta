package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
