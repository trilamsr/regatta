package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestDrawer_404ReturnsHTMLFragment asserts drawer 404 serves a styled HTML fragment (R-MEGA-2 P5).
func TestDrawer_404ReturnsHTMLFragment(t *testing.T) {
	tmpls, err := LoadTemplates(AssetsFS())
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "drawer.db")
	clock := func() time.Time { return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := NewHandler(Dependencies{Templates: tmpls, DB: db, Clock: clock})

	cases := []struct {
		name string
		path string
	}{
		{"agent-bad-id", "/ui/drawer/agent/not-a-number"},
		{"agent-missing", "/ui/drawer/agent/9999"},
		{"work-item-empty", "/ui/drawer/work-item/"},
		{"work-item-missing", "/ui/drawer/work-item/WI-MISSING"},
		{"event-bad-id", "/ui/drawer/event/bogus"},
		{"event-missing", "/ui/drawer/event/9999"},
		{"pipeline-unknown", "/ui/drawer/pipeline/nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status=%d want 404", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("Content-Type=%q want text/html prefix", ct)
			}
			body := rec.Body.String()
			if strings.Contains(body, "404 page not found") {
				t.Fatalf("body still contains stdlib 404 leak: %q", body)
			}
			if !strings.Contains(body, "Item not found") {
				t.Fatalf("body missing fragment text: %q", body)
			}
			if !strings.Contains(body, "drawer-empty") {
				t.Fatalf("body missing drawer-empty class hook: %q", body)
			}
		})
	}
}
