package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// moduleRoot walks up from the current test file to the directory
// containing go.mod. Returns "" if not found (e.g. out-of-tree run);
// callers should t.Skip in that case.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func TestEmbeddedYamlMatchesExample(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	if root == "" {
		t.Skip("module root not resolvable; skipping drift test")
	}
	canonical, err := os.ReadFile(filepath.Join(root, "examples/minimal/regatta.yaml"))
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	embedded, err := os.ReadFile(filepath.Join(root, "cmd/regatta/init_assets/regatta.yaml"))
	if err != nil {
		t.Fatalf("read embedded: %v. Did you run Task 2 of the init+smoke plan?", err)
	}
	if string(canonical) != string(embedded) {
		t.Fatalf("drift: cmd/regatta/init_assets/regatta.yaml diverges from examples/minimal/regatta.yaml. Re-sync with: cp examples/minimal/regatta.yaml cmd/regatta/init_assets/regatta.yaml")
	}
}

func TestEmbeddedSampleMatchesFixture(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	if root == "" {
		t.Skip("module root not resolvable; skipping drift test")
	}
	canonical, err := os.ReadFile(filepath.Join(root, "testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff"))
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	embedded, err := os.ReadFile(filepath.Join(root, "cmd/regatta/init_assets/sample.diff"))
	if err != nil {
		t.Fatalf("read embedded: %v. Did you run Task 2 of the init+smoke plan?", err)
	}
	if string(canonical) != string(embedded) {
		t.Fatalf("drift: cmd/regatta/init_assets/sample.diff diverges from testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff. Re-sync with: cp testdata/gates/l0/fail/17_homoglyph_cyrillic_a.diff cmd/regatta/init_assets/sample.diff")
	}
}
