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
