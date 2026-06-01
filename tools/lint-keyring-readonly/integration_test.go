package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestKeyringReadOnly_LintIntegrationCI runs the lint against the repo's
// internal/ tree and asserts zero findings. Pins spec §9 A+-tier: the
// CI gate fires on every PR, so a runtime KeyringSet sneaking into main
// is impossible.
//
// The test invokes `go run` so the harness exercises exactly what CI
// runs (no in-process shortcut). A flake here means either:
//   - a new caller has a KeyringSet outside init/Setup (legitimate
//     finding ⇒ fix the caller, not the test);
//   - the lint allowlist needs a new encloser name (legitimate spec
//     evolution ⇒ update allowedEnclosers + spec §5).
func TestKeyringReadOnly_LintIntegrationCI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test skipped in -short mode")
	}
	repoRoot := findRepoRoot(t)
	internalDir := filepath.Join(repoRoot, "internal")
	if _, err := os.Stat(internalDir); err != nil {
		t.Fatalf("internal/ not found at %s: %v", internalDir, err)
	}

	cmd := exec.Command("go", "run",
		"./tools/lint-keyring-readonly",
		"./internal",
	)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err == nil {
		return // clean run, no findings.
	}
	// `go run` exits 1 when the linted code has findings — emit them in
	// the test failure message so the developer can fix or allowlist.
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		t.Fatalf("lint-keyring-readonly found violations (exit %d) in ./internal:\n%s",
			ee.ExitCode(), strings.TrimSpace(string(out)))
	}
	t.Fatalf("lint-keyring-readonly invocation failed: %v\noutput:\n%s", err, out)
}

// findRepoRoot walks upward from the test's cwd until it finds a go.mod.
// Co-located with the test so the lint tool itself doesn't need to know
// about its caller's directory shape.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("go.mod not found upward from %s", wd)
	return ""
}
