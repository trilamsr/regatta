package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/web"
)

// TestNewWebHandler_PopulatesBootedAt pins #1124: newWebHandler must stamp BootedAt onto the web.Dependencies so the docker-soak panel can report uptime — without this wiring the panel renders "0s" forever (caught by reviewer a066716113c32e4d6).
func TestNewWebHandler_PopulatesBootedAt(t *testing.T) {
	captured := false
	t0 := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	cfg := listenerConfig{
		Clock: func() time.Time { captured = true; return t0 },
	}
	_, _ = newWebHandler(cfg)
	if !captured {
		t.Fatalf("newWebHandler did not invoke cfg.Clock() to stamp BootedAt — composition root regression")
	}
	_ = web.Dependencies{}
}

// TestNewWebHandlerWiresApprovalRoutes asserts the RouteRegistrar seam mounts /approve/{aid} (MAY-116).
func TestNewWebHandlerWiresApprovalRoutes(t *testing.T) {
	h, err := newWebHandler(listenerConfig{Clock: func() time.Time { return time.Unix(0, 0) }})
	if err != nil {
		t.Fatalf("newWebHandler: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/approve/test-aid", nil))

	// Wired RouteRegistrar routes the approve page (401 no-cookie). Reverting
	// RouteRegistrar to nil drops the route to the "/" catch-all → 404.
	if rec.Code == http.StatusNotFound {
		t.Fatalf("GET /approve/{aid} returned 404: RouteRegistrar seam not wired")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /approve/{aid} status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
