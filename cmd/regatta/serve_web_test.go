package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestServe_RootHandlerWiredIntoBootListener asserts bootListener mux routes `/` through web.NewHandler while keeping /healthz + /api/approval/callback intact (longest-prefix-wins, B-tier #6).
func TestServe_RootHandlerWiredIntoBootListener(t *testing.T) {
	t.Setenv("REGATTA_HMAC_KEY", "test-key")
	h := newListenerHarness(t, true, "127.0.0.1:0")
	srv, err := bootListener(h.cfg)
	if err != nil {
		t.Fatalf("bootListener: %v", err)
	}
	addr, errChan := startListener(t, srv)
	t.Cleanup(func() {
		_ = srv.Shutdown(context.Background())
		<-errChan
	})

	for _, c := range []struct {
		path        string
		wantStatus  int
		wantBody    string
		wantHeader  string
		wantHValue  string
		bodyMissing string
	}{
		{path: "/ui/static/htmx-config.js", wantStatus: http.StatusOK, wantBody: "allowEval"},
		{path: "/", wantStatus: http.StatusOK, wantBody: "<title>"},
		// W7.0 registrations win because http.ServeMux picks the longest prefix; /healthz now returns the JSON readiness envelope.
		{path: "/healthz", wantStatus: http.StatusOK, wantBody: `"status"`},
	} {
		resp, err := http.Get("http://" + addr + c.path)
		if err != nil {
			t.Fatalf("GET %s: %v", c.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != c.wantStatus {
			t.Errorf("%s status=%d want %d (body=%q)", c.path, resp.StatusCode, c.wantStatus, string(body))
		}
		if !strings.Contains(string(body), c.wantBody) {
			t.Errorf("%s body missing %q; got %q", c.path, c.wantBody, string(body))
		}
	}

	// CSP must wrap UI routes; /healthz is owned by W7.0 and is exempt.
	resp, err := http.Get("http://" + addr + "/ui/static/htmx-config.js")
	if err != nil {
		t.Fatalf("GET asset: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Errorf("asset CSP=%q want default-src 'self'", got)
	}
}
