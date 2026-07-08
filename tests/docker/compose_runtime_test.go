//go:build docker

package docker_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestComposeRuntimeHTTPMatrix asserts the compose stack's HTTP surface after stack-up: liveness/readiness/UI/panel routes return expected status, error responses do not leak internal-state strings (R-MEGA-3 INT-1).
func TestComposeRuntimeHTTPMatrix(t *testing.T) {
	requireDocker(t)

	repoRoot := repoRootFromCWD(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	out, err := compose(ctx, repoRoot, "up", "-d", "--wait").CombinedOutput()
	if err := composeUpError(err, out); err != nil {
		t.Fatalf("compose up: %v", err)
	}
	t.Cleanup(func() {
		downCtx, c := context.WithTimeout(context.Background(), 60*time.Second)
		defer c()
		_ = compose(downCtx, repoRoot, "down", "-v").Run()
	})
	assertServicesHealthy(ctx, t, repoRoot, []string{"regatta"})

	base := "http://127.0.0.1:8080"

	type probe struct {
		name       string
		method     string
		path       string
		wantStatus int
		mustNotContain []string // forbidden in body
	}
	cases := []probe{
		{name: "healthz", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK},
		{name: "readyz", method: http.MethodGet, path: "/readyz", wantStatus: http.StatusOK},
		{name: "ui_root_redirect", method: http.MethodGet, path: "/ui", wantStatus: http.StatusMovedPermanently},
		{name: "ui_slash_redirect", method: http.MethodGet, path: "/ui/", wantStatus: http.StatusMovedPermanently},
		{name: "root_get_ok", method: http.MethodGet, path: "/", wantStatus: http.StatusOK},
		{name: "root_post_405", method: http.MethodPost, path: "/", wantStatus: http.StatusMethodNotAllowed},
		{name: "panel_agents", method: http.MethodGet, path: "/ui/panels/agents", wantStatus: http.StatusOK},
		{name: "panel_events", method: http.MethodGet, path: "/ui/panels/events", wantStatus: http.StatusOK},
		{name: "panel_pipeline", method: http.MethodGet, path: "/ui/panels/pipeline", wantStatus: http.StatusOK},
		{name: "panel_workitems", method: http.MethodGet, path: "/ui/panels/work-items", wantStatus: http.StatusOK},
	}

	// LIVE-5 leak-check: any internal-error path must produce a generic
	// message, not paths, sql snippets, panics, or goroutine traces.
	leakSubstrings := []string{"/etc/", "sql:", "goroutine ", "panic:"}

	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for _, p := range cases {
		t.Run(p.name, func(t *testing.T) {
			req, _ := http.NewRequestWithContext(ctx, p.method, base+p.path, nil)
			resp, err := httpClient.Do(req)
			if err != nil {
				t.Fatalf("HTTP %s %s: %v", p.method, p.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != p.wantStatus {
				t.Errorf("status=%d want %d", resp.StatusCode, p.wantStatus)
			}
			body, _ := io.ReadAll(resp.Body)
			for _, leak := range leakSubstrings {
				if strings.Contains(string(body), leak) {
					t.Errorf("body leaks %q under %s %s", leak, p.method, p.path)
				}
			}
			for _, leak := range p.mustNotContain {
				if strings.Contains(string(body), leak) {
					t.Errorf("body contains forbidden %q under %s %s", leak, p.method, p.path)
				}
			}
		})
	}
}
