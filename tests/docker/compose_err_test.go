package docker_test

import (
	"errors"
	"strings"
	"testing"
)

// TestComposeUpErrorSurfacesStderr asserts composeUpError preserves captured stderr in the returned error (obs 18471).
func TestComposeUpErrorSurfacesStderr(t *testing.T) {
	base := errors.New("exit status 1")
	stderr := []byte("regatta_regatta_1 exited: preflight failed: CLAUDE_CODE_OAUTH_TOKEN empty")
	got := composeUpError(base, stderr)
	if got == nil {
		t.Fatalf("composeUpError returned nil for non-nil base error")
	}
	msg := got.Error()
	if !strings.Contains(msg, "exit status 1") {
		t.Errorf("error message drops base error cause: %q", msg)
	}
	if !strings.Contains(msg, "preflight failed") {
		t.Errorf("error message drops captured stderr snippet: %q", msg)
	}
}

// TestComposeUpErrorNilPassthrough asserts composeUpError(nil, ...) returns nil for drop-in replacement of .Run() err check.
func TestComposeUpErrorNilPassthrough(t *testing.T) {
	if got := composeUpError(nil, []byte("noise")); got != nil {
		t.Errorf("composeUpError(nil, ...) = %v, want nil", got)
	}
}

// TestComposeUpErrorWithLogsSurfacesContainerOutput asserts container stdout/stderr snippets land in the error under per-service headers (MAY-container-logs).
func TestComposeUpErrorWithLogsSurfacesContainerOutput(t *testing.T) {
	base := errors.New("exit status 1")
	stderr := []byte("container regatta-regatta-1 is unhealthy")
	fetcher := func(service string) []byte {
		switch service {
		case "regatta":
			return []byte("preflight: CLAUDE_CODE_OAUTH_TOKEN missing\nboot failed")
		case "prometheus":
			return []byte("scrape target refused")
		default:
			return nil
		}
	}
	got := composeUpErrorWithLogs(base, stderr, fetcher, []string{"regatta", "prometheus", "grafana"})
	if got == nil {
		t.Fatalf("composeUpErrorWithLogs returned nil for non-nil base error")
	}
	msg := got.Error()
	for _, want := range []string{
		"exit status 1",
		"container regatta-regatta-1 is unhealthy",
		"[regatta]",
		"CLAUDE_CODE_OAUTH_TOKEN missing",
		"[prometheus]",
		"scrape target refused",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q; got: %q", want, msg)
		}
	}
	if strings.Contains(msg, "[grafana]") {
		t.Errorf("error message should omit sections for services with empty logs; got: %q", msg)
	}
}

// TestComposeUpErrorWithLogsNilFetcher asserts a nil fetcher degrades to composeUpError shape.
func TestComposeUpErrorWithLogsNilFetcher(t *testing.T) {
	base := errors.New("boom")
	got := composeUpErrorWithLogs(base, []byte("cli stderr"), nil, []string{"regatta"})
	if got == nil {
		t.Fatalf("composeUpErrorWithLogs returned nil for non-nil base error")
	}
	msg := got.Error()
	if !strings.Contains(msg, "boom") || !strings.Contains(msg, "cli stderr") {
		t.Errorf("nil fetcher lost base+stderr; got: %q", msg)
	}
	if strings.Contains(msg, "[regatta]") {
		t.Errorf("nil fetcher must not emit service sections; got: %q", msg)
	}
}
