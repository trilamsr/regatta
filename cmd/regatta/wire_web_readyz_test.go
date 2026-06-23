package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/health"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestReadyz_ReturnsOKAfterFirstAdapterPoll asserts /readyz is 503 until the heartbeat is touched, then 200 (R-MEGA-3 LIVE-2).
func TestReadyz_ReturnsOKAfterFirstAdapterPoll(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	dir := t.TempDir()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(dir, "regatta.db")))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	hb := health.NewHeartbeatCell(time.Now)
	srv, err := bootListener(listenerConfig{
		UI:        true,
		Addr:      "127.0.0.1:0",
		DB:        db,
		Clock:     time.Now,
		Heartbeat: hb,
	})
	if err != nil {
		t.Fatalf("bootListener: %v (env REGATTA_HMAC_KEY needed?)", err)
	}
	if srv == nil {
		t.Fatalf("nil server")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("pre-touch /readyz status=%d want 503; body=%q", rec.Code, rec.Body.String())
	}

	hb.Touch()
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("post-touch /readyz status=%d want 200; body=%q", rec.Code, rec.Body.String())
	}
}

// TestReadyz_RejectsPost asserts /readyz returns 405 on POST (R-MEGA-3 LIVE-2).
func TestReadyz_RejectsPost(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "test-hmac-key")
	dir := t.TempDir()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(dir, "regatta.db")))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()
	srv, err := bootListener(listenerConfig{
		UI: true, Addr: "127.0.0.1:0", DB: db, Clock: time.Now,
		Heartbeat: health.NewHeartbeatCell(time.Now),
	})
	if err != nil {
		t.Fatalf("bootListener: %v", err)
	}
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/readyz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", rec.Code)
	}
}
