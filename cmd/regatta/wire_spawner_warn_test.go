package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const warnStubGitHubIssuesYAML = `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
work_item_source:
  type: github_issues
  selector: "label:autonomous"
ci:
  command: "go test ./..."
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

const warnStubMarkdownYAML = `version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
work_item_source:
  type: markdown_catalog
  root: .regatta/items
ci:
  command: "go test ./..."
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

// TestWarnStubWithGitHubIssues_FiresWhenMisconfigured: stub spawner + github_issues adapter triggers operator-visible WARN (#1090 c1).
func TestWarnStubWithGitHubIssues_FiresWhenMisconfigured(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(warnStubGitHubIssuesYAML), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	warnIfStubWithGitHubIssues("stub", repoRoot, logger)

	if !strings.Contains(buf.String(), "spawner.stub_with_github_issues") {
		t.Fatalf("missing operator WARN: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("WARN not at WARN level: %q", buf.String())
	}
}

// TestWarnStubWithGitHubIssues_SilentWhenClaude: claude spawner + github_issues = correct combo, no warning (#1090 c2).
func TestWarnStubWithGitHubIssues_SilentWhenClaude(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(warnStubGitHubIssuesYAML), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	warnIfStubWithGitHubIssues("claude", repoRoot, logger)

	if buf.Len() != 0 {
		t.Fatalf("unexpected log on claude+github_issues happy-path: %q", buf.String())
	}
}

// TestWarnStubWithGitHubIssues_SilentWhenMarkdownCatalog: stub + markdown_catalog is the smoke-test fixture, no warning (#1090 c3).
func TestWarnStubWithGitHubIssues_SilentWhenMarkdownCatalog(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(warnStubMarkdownYAML), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	warnIfStubWithGitHubIssues("stub", repoRoot, logger)

	if buf.Len() != 0 {
		t.Fatalf("unexpected log on stub+markdown smoke-test combo: %q", buf.String())
	}
}

// TestWarnStubWithGitHubIssues_SilentWhenNoYaml: zero-config deployment stays silent (#1090 c4).
func TestWarnStubWithGitHubIssues_SilentWhenNoYaml(t *testing.T) {
	repoRoot := t.TempDir()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	warnIfStubWithGitHubIssues("stub", repoRoot, logger)

	if buf.Len() != 0 {
		t.Fatalf("unexpected log when regatta.yaml absent: %q", buf.String())
	}
}
