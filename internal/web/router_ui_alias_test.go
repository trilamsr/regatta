package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRouter_UIRootServes200 asserts /ui and /ui/ redirect to the dashboard root (R-MEGA-3 LIVE-1).
func TestRouter_UIRootServes200(t *testing.T) {
	h := newTestHandler(t)
	for _, path := range []string{"/ui", "/ui/"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMovedPermanently {
			t.Errorf("path=%s status=%d want 301; body=%q", path, rec.Code, rec.Body.String())
			continue
		}
		if loc := rec.Header().Get("Location"); loc != "/" {
			t.Errorf("path=%s Location=%q want %q", path, loc, "/")
		}
	}
}

// TestRouter_UIPanelsStillWin asserts /ui/panels/* + /ui/static/* longest-prefix routes are not shadowed by the /ui/ redirect (R-MEGA-3 LIVE-1).
func TestRouter_UIPanelsStillWin(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ui/static/htmx-config.js", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("/ui/static asset returned %d (body=%q); /ui/ redirect shadowed longest-prefix", rec.Code, rec.Body.String())
	}
}

// TestRootHandler_405OnPost asserts POST to / returns 405 with Allow header (R-MEGA-3 LIVE-4).
func TestRootHandler_405OnPost(t *testing.T) {
	h := newTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Errorf("Allow=%q want %q", allow, "GET, HEAD")
	}
}
