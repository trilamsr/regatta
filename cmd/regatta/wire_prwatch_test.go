package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/prwatch"
)

// TestBuildPRLister_FallsBackToCLIWithoutToken keeps gh-CLI lister when no GH_TOKEN (MAY-52).
func TestBuildPRLister_FallsBackToCLIWithoutToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	got := buildPRLister(t.TempDir(), nopLogger(t))
	if _, ok := got.(*prwatch.GHCLILister); !ok {
		t.Fatalf("want *GHCLILister, got %T", got)
	}
}

// TestBuildPRLister_FallsBackToCLIWithoutRepoConfig keeps gh-CLI when repo owner/name missing.
func TestBuildPRLister_FallsBackToCLIWithoutRepoConfig(t *testing.T) {
	t.Setenv("GH_TOKEN", "tok")

	got := buildPRLister(t.TempDir(), nopLogger(t))
	if _, ok := got.(*prwatch.GHCLILister); !ok {
		t.Fatalf("want *GHCLILister (no repo config), got %T", got)
	}
}

// TestBuildPRLister_ETagWhenTokenAndRepoConfigPresent selects ETagLister with token+repo config.
func TestBuildPRLister_ETagWhenTokenAndRepoConfigPresent(t *testing.T) {
	t.Setenv("GH_TOKEN", "tok")
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "regatta.yaml")); err != nil {
		t.Skipf("regatta.yaml not at expected location %s: %v", repoRoot, err)
	}
	if owner, repo, ok := loadRepoOwnerName(repoRoot); !ok {
		t.Fatalf("loadRepoOwnerName rejected live regatta.yaml; owner=%q repo=%q", owner, repo)
	}

	got := buildPRLister(repoRoot, nopLogger(t))
	if _, ok := got.(*prwatch.ETagLister); !ok {
		t.Fatalf("want *ETagLister, got %T", got)
	}
}

// nopLogger returns a slog.Logger discarding everything — keeps the
// test free of log noise without depending on test/testlog injection.
func nopLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
