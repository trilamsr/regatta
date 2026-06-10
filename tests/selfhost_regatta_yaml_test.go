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

// TestSelfHost_RegattaYAML_DeclaresGithubIssues pins the self-host adapter (brief §3).
func TestSelfHost_RegattaYAML_DeclaresGithubIssues(t *testing.T) {
	path := filepath.Join(repoRoot(t), "regatta.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	cfg, err := validate.LoadConfig(data)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.SpecAdapter == nil {
		t.Fatal("repo-root regatta.yaml does not declare spec_adapter; brief §3 requires github_issues for self-host")
	}
	if got, want := cfg.SpecAdapter.Type, validate.SpecAdapterTypeGitHubIssues; got != want {
		t.Fatalf("spec_adapter.type = %q; want %q (brief §3 self-host pin)", got, want)
	}
	if got, want := cfg.SpecAdapter.Selector, "label:autonomous"; got != want {
		t.Fatalf("spec_adapter.selector = %q; want %q (FEED phase intake gate per PR #1206)", got, want)
	}
	if got, want := cfg.SpecAdapter.AcceptanceSection, "## Acceptance criteria"; got != want {
		t.Fatalf("spec_adapter.acceptance_section = %q; want %q", got, want)
	}
	if got, want := cfg.SpecAdapter.DefaultLane, "self-host"; got != want {
		t.Fatalf("spec_adapter.default_lane = %q; want %q (lane backfill for unlabelled issues per #1117)", got, want)
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
