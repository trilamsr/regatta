// tests/selfhost_regatta_yaml_test.go anchors the self-host bootstrap
// against the schema + parser. If either drifts, this test fires before
// an operator hits the failure on the live serve loop.
//
// Per spec docs/engineer/specs/2026-06-02-s1-t1-self-host-regatta-yaml.md
// §7 #4 + #5.
package tests

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	validate "github.com/trilamsr/regatta/internal/config/validate"
	adapter "github.com/trilamsr/regatta/internal/orchestrator/adapter"
)

// repoRoot resolves the regatta repo root by walking up from this
// source file's location. The tests/ directory sits two levels under
// the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(file))
}

// TestSelfHost_RegattaYAML_Validates pins the repo-root regatta.yaml against the CUE schema. The yaml is the single source
func TestSelfHost_RegattaYAML_Validates(t *testing.T) {
	path := filepath.Join(repoRoot(t), "regatta.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := validate.LoadBytes(data); err != nil {
		t.Fatalf("repo-root regatta.yaml fails CUE schema:\n%v", err)
	}
}

// TestSelfHost_RegattaYAML_DeclaresMarkdownCatalog pins the self-host adapter choice: the brief §3 calls out markdown adap
func TestSelfHost_RegattaYAML_DeclaresMarkdownCatalog(t *testing.T) {
	path := filepath.Join(repoRoot(t), "regatta.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	cfg, err := validate.LoadConfig(data)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	root := cfg.MarkdownCatalogRoot()
	if root == "" {
		t.Fatal("repo-root regatta.yaml does not declare spec_adapter.type=markdown_catalog; brief §3 requires it for self-host")
	}
}

// TestSelfHost_SeedItems_Parse anchors every .regatta/items/*.md file at the repo root against the markdown adapter's pars
func TestSelfHost_SeedItems_Parse(t *testing.T) {
	dir := filepath.Join(repoRoot(t), ".regatta", "items")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	seen := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if strings.HasPrefix(e.Name(), "_") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if _, err := adapter.ParseMarkdownItem(data); err != nil {
			t.Errorf("%s: parse: %v", e.Name(), err)
		}
		seen++
	}
	if seen < 2 {
		t.Fatalf("found %d seed items under .regatta/items/; brief §3 S1-T1 acceptance needs ≥2", seen)
	}
}
