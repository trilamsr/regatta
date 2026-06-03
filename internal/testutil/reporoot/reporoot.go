// Package reporoot resolves the module root from a test's working
// directory — walks upward until go.mod is found. Centralised so
// every test that needs the repo root reaches for one helper.
package reporoot

import (
	"os"
	"path/filepath"
	"testing"
)

// Must returns the absolute module root walked from t's cwd. Fatals
// on >10 hops or filesystem error so a misconfigured test surfaces
// fast.
func Must(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("reporoot.Must: abs cwd: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("reporoot.Must: go.mod not found upward from %s", dir)
	return ""
}
