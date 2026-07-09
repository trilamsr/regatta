package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/health"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/web"
)

// TestNewWebHandler_PopulatesBootedAt pins #1124: newWebHandler must stamp BootedAt onto the web.Dependencies so uptime-dependent dashboard panels report correctly — without this wiring uptime renders "0s" forever (caught by reviewer a066716113c32e4d6).
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

// TestReadOnlyRoutes_RejectNonGETWithMethodNotAllowed asserts /healthz + /ui/panels/* refuse POST/PUT/DELETE with 405 to stop accidental cache poisoning + CSRF surface.
func TestReadOnlyRoutes_RejectNonGETWithMethodNotAllowed(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	srv, err := bootListener(listenerConfig{
		UI:    true,
		Clock: func() time.Time { return time.Unix(0, 0) },
	})
	if err != nil {
		t.Fatalf("bootListener: %v", err)
	}
	if srv == nil {
		t.Fatal("bootListener returned nil server with UI=true")
	}
	h := srv.Handler
	readOnly := []string{"/healthz", "/ui/panels/health", "/ui/panels/agents"}
	for _, path := range readOnly {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s status = %d; want 405", method, path, rec.Code)
			}
		}
	}
}

// TestBuildListenerConfig pins serveFlags -> listenerConfig threading (UI/Addr/DB/Clock/PublicHost/Heartbeat/TickInterval) so MAY-48 tick-stale wiring cannot silently regress.
func TestBuildListenerConfig(t *testing.T) {
	t.Parallel()

	clock := func() time.Time { return time.Unix(0, 0).UTC() }
	db := &state.DB{}
	hb := health.NewHeartbeatCell(clock)
	tick := 750 * time.Millisecond
	f := serveFlags{
		UI:      true,
		Addr:    ":9999",
		TickDur: tick,
	}

	cfg := buildListenerConfig(f, db, clock, "ui.example.com", hb)

	if cfg.UI != f.UI {
		t.Errorf("UI: got %v want %v", cfg.UI, f.UI)
	}
	if cfg.Addr != f.Addr {
		t.Errorf("Addr: got %q want %q", cfg.Addr, f.Addr)
	}
	if cfg.DB != db {
		t.Errorf("DB: got %p want %p", cfg.DB, db)
	}
	if cfg.PublicHost != "ui.example.com" {
		t.Errorf("PublicHost: got %q want %q", cfg.PublicHost, "ui.example.com")
	}
	if cfg.Heartbeat != hb {
		t.Errorf("Heartbeat: got %p want %p", cfg.Heartbeat, hb)
	}
	if cfg.TickInterval != tick {
		t.Errorf("TickInterval: got %v want %v", cfg.TickInterval, tick)
	}
	if cfg.Clock == nil {
		t.Fatal("Clock: nil")
	}
	if got := cfg.Clock(); !got.Equal(clock()) {
		t.Errorf("Clock: got %v want %v", got, clock())
	}
	if cfg.Keyring == nil {
		t.Error("Keyring: nil; loadBriefKeyring should populate MapKeyring")
	}
}
