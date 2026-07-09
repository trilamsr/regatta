package main

import (
	"bytes"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestStartHTTPServer_ReturnsBindErrorSynchronously asserts EADDRINUSE surfaces at call time, not from a background goroutine (wave-a bug 8).
func TestStartHTTPServer_ReturnsBindErrorSynchronously(t *testing.T) {
	// Pre-bind a listener on 127.0.0.1:0, capture the resolved addr,
	// then point the HTTP server at that same addr — Serve MUST fail
	// with a bind error, not a background goroutine leak.
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pre-bind: %v", err)
	}
	t.Cleanup(func() { _ = blocker.Close() })
	addr := blocker.Addr().String()

	srv := &http.Server{
		Addr:              addr,
		Handler:           http.NewServeMux(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	stop, err := startHTTPServer(srv, log.New(io.Discard, "", 0))
	if err == nil {
		if stop != nil {
			stop()
		}
		t.Fatal("startHTTPServer err=nil on collided port; expected bind error")
	}
	if stop != nil {
		t.Fatal("startHTTPServer returned non-nil stop func alongside a bind error; caller cannot tell which path took")
	}
	// Error must name the addr and hint at remediation so the operator
	// does not need to grep stderr for context.
	msg := err.Error()
	if !strings.Contains(msg, addr) {
		t.Errorf("err=%q does not mention addr %q", msg, addr)
	}
	if !strings.Contains(msg, "--addr") && !strings.Contains(msg, "port") {
		t.Errorf("err=%q lacks actionable remediation hint (--addr / port)", msg)
	}
}

// TestStartHTTPServer_HappyPath asserts free-port bind + clean stop drain within shutdown budget (wave-a bug 8).
func TestStartHTTPServer_HappyPath(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := log.New(buf, "", 0)

	srv := &http.Server{
		Addr:              "127.0.0.1:0",
		Handler:           http.NewServeMux(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	stop, err := startHTTPServer(srv, logger)
	if err != nil {
		t.Fatalf("startHTTPServer: %v", err)
	}
	if stop == nil {
		t.Fatal("startHTTPServer returned nil stop func on happy path")
	}

	// stop drains cleanly within the listenerShutdownBudget.
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(listenerShutdownBudget + 2*time.Second):
		t.Fatal("stop() did not return within shutdown budget; goroutine leaked")
	}

	// Clean shutdown must NOT log a listener error — ErrServerClosed is
	// swallowed by the helper on the graceful path.
	if got := buf.String(); strings.Contains(got, "listener:") {
		t.Errorf("stop() logged listener error on graceful shutdown: %q", got)
	}
}

// TestStartHTTPServer_EmptyAddrDefaults asserts empty srv.Addr falls back to defaultListenerAddr in the error path (wave-a bug 8).
func TestStartHTTPServer_EmptyAddrDefaults(t *testing.T) {
	// Pre-bind defaultListenerAddr to force the bind error path, then
	// point srv at "" — the helper MUST substitute defaultListenerAddr
	// and the resulting error MUST name that literal so the operator
	// sees which default they hit.
	blocker, err := net.Listen("tcp", defaultListenerAddr)
	if err != nil {
		// The default port is already in use by something outside the
		// test — skip rather than flake.
		t.Skipf("cannot pre-bind %s: %v", defaultListenerAddr, err)
	}
	t.Cleanup(func() { _ = blocker.Close() })

	srv := &http.Server{Addr: "", Handler: http.NewServeMux(), ReadHeaderTimeout: 5 * time.Second}
	stop, err := startHTTPServer(srv, log.New(io.Discard, "", 0))
	if err == nil {
		if stop != nil {
			stop()
		}
		t.Fatal("startHTTPServer err=nil; expected bind collision on defaultListenerAddr")
	}
	if !strings.Contains(err.Error(), defaultListenerAddr) {
		t.Errorf("err=%q does not mention defaultListenerAddr %q", err.Error(), defaultListenerAddr)
	}
}

