// serve_lane_default_test pins BUG-1048 c1+c3: regatta serve auto-
// applies -lane server:1 when spec_adapter.type=github_issues and no
// --lane was passed, and the resulting scheduler reserves exactly one
// agent per tick for two planned items on the shared lane.
package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

const serveLaneDefaultGatesBlock = `
gates:
  - id: human_merge
    type: approval_gate
    name: human-merge
    risk_class: low
    reviewers: [trilamsr]
    quorum: 1
    timeout: 24h
    decision_window: 12h
    on_timeout: fail
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`

const serveLaneDefaultGitHubIssuesYAML = `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:autonomous"
ci:
  command: "go test ./..."
` + serveLaneDefaultGatesBlock

const serveLaneDefaultMarkdownYAML = `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: markdown_catalog
  root: .regatta/items
ci:
  command: "go test ./..."
` + serveLaneDefaultGatesBlock

// TestApplyDefaultLaneCap_GitHubIssuesDefaultsToOne asserts c1: the
// helper installs server:1 + emits scheduler.default_lane_cap_applied
// when github_issues is wired and no --lane was passed.
func TestApplyDefaultLaneCap_GitHubIssuesDefaultsToOne(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(serveLaneDefaultGitHubIssuesYAML), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	f := serveFlags{RepoRoot: repoRoot, LaneCaps: laneCapsFlag{}}
	applyDefaultLaneCap(&f, logger)

	if got, want := f.LaneCaps["server"], 1; got != want {
		t.Fatalf("LaneCaps[server]=%d, want %d (BUG-1048 c1)", got, want)
	}
	if !strings.Contains(logBuf.String(), "scheduler.default_lane_cap_applied") {
		t.Fatalf("startup log missing scheduler.default_lane_cap_applied marker: %q", logBuf.String())
	}
}

// TestApplyDefaultLaneCap_ExplicitFlagWins asserts the helper is a
// no-op when the operator passed --lane — explicit choice overrides
// the safe default.
func TestApplyDefaultLaneCap_ExplicitFlagWins(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(serveLaneDefaultGitHubIssuesYAML), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	f := serveFlags{RepoRoot: repoRoot, LaneCaps: laneCapsFlag{"server": 3}}
	applyDefaultLaneCap(&f, logger)

	if got, want := f.LaneCaps["server"], 3; got != want {
		t.Fatalf("LaneCaps[server]=%d, want %d (operator override should survive)", got, want)
	}
}

// TestApplyDefaultLaneCap_MarkdownCatalogUnaffected asserts the helper
// is a no-op for markdown_catalog (single-operator local workflow does
// not need the cap).
func TestApplyDefaultLaneCap_MarkdownCatalogUnaffected(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(serveLaneDefaultMarkdownYAML), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	f := serveFlags{RepoRoot: repoRoot, LaneCaps: laneCapsFlag{}}
	applyDefaultLaneCap(&f, logger)

	if len(f.LaneCaps) != 0 {
		t.Fatalf("LaneCaps=%v, want empty for markdown_catalog", f.LaneCaps)
	}
}

// TestApplyDefaultLaneCap_NoRegattaYamlUnaffected asserts the helper
// is a no-op when regatta.yaml is missing entirely (zero-config path).
func TestApplyDefaultLaneCap_NoRegattaYamlUnaffected(t *testing.T) {
	repoRoot := t.TempDir() // no regatta.yaml inside
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	f := serveFlags{RepoRoot: repoRoot, LaneCaps: laneCapsFlag{}}
	applyDefaultLaneCap(&f, logger)

	if len(f.LaneCaps) != 0 {
		t.Fatalf("LaneCaps=%v, want empty when regatta.yaml absent", f.LaneCaps)
	}
}

// TestServe_NoLaneFlag_OnlyOneSpawnPerTick is c3: end-to-end through
// buildScheduler — with no --lane flag + 2 planned items on the same
// `server` lane, scheduler reserves exactly 1 per tick.
func TestServe_NoLaneFlag_OnlyOneSpawnPerTick(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(serveLaneDefaultGitHubIssuesYAML), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	f := serveFlags{RepoRoot: repoRoot, LaneCaps: laneCapsFlag{}, LockTTL: time.Minute}
	applyDefaultLaneCap(&f, logger)

	db := openSchedulerTestDB(t)
	ctx := context.Background()
	seedServeLanePlanned(t, db, "WORK-1", "server")
	seedServeLanePlanned(t, db, "WORK-2", "server")

	sched := buildScheduler(db, f, schedulerDeps{Clock: time.Now})
	if sched == nil {
		t.Fatal("buildScheduler returned nil")
	}
	ids, err := sched.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("reserved %d, want 1 (default lane cap should serialize)", len(ids))
	}
}

func seedServeLanePlanned(t *testing.T, db *state.DB, id, lane string) {
	t.Helper()
	w := state.WorkItem{
		ID:     id,
		Kind:   state.KindFeature,
		Title:  id,
		Lane:   lane,
		Status: state.WorkStatusPlanned,
	}
	if err := db.UpsertWorkItem(context.Background(), w, state.SourceBrief, time.Now()); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}
