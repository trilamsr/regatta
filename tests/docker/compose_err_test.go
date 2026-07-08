package docker_test

import (
	"errors"
	"strings"
	"testing"
)

// TestComposeUpErrorSurfacesStderr asserts the compose-up failure formatter
// preserves captured stderr in the returned error so future infra breakage
// is diagnosable (closes obs 18471: compose stderr silently swallowed).
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

// TestComposeUpErrorNilPassthrough asserts a nil base error yields nil so the
// caller can drop-in replace the existing `.Run()` err check.
func TestComposeUpErrorNilPassthrough(t *testing.T) {
	if got := composeUpError(nil, []byte("noise")); got != nil {
		t.Errorf("composeUpError(nil, ...) = %v, want nil", got)
	}
}
