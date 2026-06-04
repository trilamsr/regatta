package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil"
)

// listenerHarness fans the bootListener seam under a t.TempDir DB so
// tests exercise the real boot path, not an httptest.NewServer shortcut.
type listenerHarness struct {
	t   *testing.T
	cfg listenerConfig
	db  *state.DB
}

func newListenerHarness(t *testing.T, ui bool, addr string) *listenerHarness {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "listener.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &listenerHarness{
		t:  t,
		db: db,
		cfg: listenerConfig{
			UI:    ui,
			Addr:  addr,
			DB:    db,
			Clock: time.Now,
		},
	}
}

// B1: --ui=true binds the listener; /healthz returns 200 + JSON readiness envelope.
func TestServe_UITrue_BindsListener(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "test-key")
	h := newListenerHarness(t, true, "127.0.0.1:0")
	srv, err := bootListener(h.cfg)
	if err != nil {
		t.Fatalf("bootListener: %v", err)
	}
	if srv == nil {
		t.Fatal("srv=nil with --ui=true")
	}
	addr, errChan := startListener(t, srv)
	t.Cleanup(func() {
		_ = srv.Shutdown(context.Background())
		<-errChan
	})

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status=%d want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"status"`) {
		t.Errorf("body=%q missing JSON status field", string(body))
	}
}

// B2: --ui=false returns nil server; no port is bound.
func TestServe_UIFalse_SkipsListener(t *testing.T) {
	h := newListenerHarness(t, false, "127.0.0.1:0")
	srv, err := bootListener(h.cfg)
	if err != nil {
		t.Fatalf("bootListener: %v", err)
	}
	if srv != nil {
		t.Fatal("srv non-nil with --ui=false; listener should be skipped entirely")
	}
}

// B3: --ui=true with REGATTA_HMAC_KEY unset fails boot before any DB open or listener bind.
func TestServe_UITrue_FailsBootIfHMACKeyUnset(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "")
	t.Setenv("REGATTA_HMAC_KEY_ENV", "")
	// pre-flight check must trip BEFORE DB open: prove by passing a nil DB
	// — if pre-flight ran second, NewHTTPCallback would dereference nil.
	cfg := listenerConfig{UI: true, Addr: "127.0.0.1:0", DB: nil, Clock: time.Now}
	_, err := bootListener(cfg)
	if err == nil {
		t.Fatal("bootListener err=nil; expected fail-boot when REGATTA_HMAC_KEY unset")
	}
	if !strings.Contains(err.Error(), "REGATTA_HMAC_KEY") {
		t.Errorf("err=%q want error mentioning REGATTA_HMAC_KEY", err.Error())
	}
}

// B4: graceful shutdown completes within budget; no inflight-request leak.
func TestServe_GracefulShutdown(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "test-key")
	h := newListenerHarness(t, true, "127.0.0.1:0")
	srv, err := bootListener(h.cfg)
	if err != nil {
		t.Fatalf("bootListener: %v", err)
	}
	addr, errChan := startListener(t, srv)

	preResp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("pre-shutdown GET: %v", err)
	}
	_ = preResp.Body.Close()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Shutdown took %v; want ≤ 5s", d)
	}
	// ListenAndServe goroutine must exit cleanly with ErrServerClosed.
	select {
	case err := <-errChan:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("ListenAndServe err=%v want ErrServerClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("ListenAndServe goroutine leaked past Shutdown")
	}

	// post-shutdown GET fails: connection refused (listener torn down).
	postResp, postErr := http.Get("http://" + addr + "/healthz")
	if postErr == nil {
		_ = postResp.Body.Close()
		t.Error("post-shutdown GET succeeded; listener still bound")
	}
}

// A8: callback route registered only when --ui=true; with --ui=false the listener is absent so a POST cannot connect at all.
func TestServe_CallbackRouteRegisteredOnlyWhenUITrue(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "test-key")
	// --ui=false: no listener bound; dial-fixed-port returns ECONNREFUSED.
	hOff := newListenerHarness(t, false, "127.0.0.1:0")
	srvOff, err := bootListener(hOff.cfg)
	if err != nil {
		t.Fatalf("bootListener ui=false: %v", err)
	}
	if srvOff != nil {
		t.Fatal("srvOff non-nil with --ui=false; bind syscall should not fire")
	}

	// --ui=true: callback path returns 405 on GET (POST-only per spec).
	hOn := newListenerHarness(t, true, "127.0.0.1:0")
	srvOn, err := bootListener(hOn.cfg)
	if err != nil {
		t.Fatalf("bootListener ui=true: %v", err)
	}
	addr, errChan := startListener(t, srvOn)
	t.Cleanup(func() {
		_ = srvOn.Shutdown(context.Background())
		<-errChan
	})

	resp, err := http.Get("http://" + addr + "/api/approval/callback")
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status=%d want 405 (POST-only)", resp.StatusCode)
	}
}

// startListener spawns ListenAndServe in a goroutine and returns the
// bound addr (after resolving :0) plus the error channel so the caller
// can assert clean shutdown.
func startListener(t testing.TB, srv *http.Server) (string, chan error) {
	t.Helper()
	errChan := make(chan error, 1)
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		errChan <- err
		close(errChan)
		return srv.Addr, errChan
	}
	go func() {
		errChan <- srv.Serve(ln)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addr := ln.Addr().String()
	testutil.Eventually(t, ctx, 5*time.Millisecond, func() bool {
		c, derr := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if derr != nil {
			return false
		}
		_ = c.Close()
		return true
	}, "Serve goroutine did not accept on "+addr)
	return addr, errChan
}
