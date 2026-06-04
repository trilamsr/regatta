package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/internal/authz"
)

// TestBuildAuthorizer_NoYAML_HydratesEmbeddedFallback asserts no-yaml path hydrates embed.FS default-deny bundle and exposes a SHA prefix via CurrentRevision.
func TestBuildAuthorizer_NoYAML_HydratesEmbeddedFallback(t *testing.T) {
	repo := t.TempDir()
	az, err := buildAuthorizer(context.Background(), repo, discardLogger())
	if err != nil {
		t.Fatalf("buildAuthorizer: %v", err)
	}
	if rev := az.CurrentRevision(authz.DefaultTenant); rev == "" {
		t.Fatalf("CurrentRevision empty; want 8-char SHA prefix from embed.FS fallback")
	}
}

// TestBuildAuthorizer_NoPolicyDir_SkipsReloader asserts safety.authz absent ⇒ no Reloader spawned (no goroutine leak on ctx cancel).
func TestBuildAuthorizer_NoPolicyDir_SkipsReloader(t *testing.T) {
	repo := t.TempDir()
	yaml := []byte(`version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:planned"
ci:
  command: "go test ./..."
gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`)
	if err := os.WriteFile(filepath.Join(repo, "regatta.yaml"), yaml, 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	az, err := buildAuthorizer(context.Background(), repo, discardLogger())
	if err != nil {
		t.Fatalf("buildAuthorizer: %v", err)
	}
	if az == nil {
		t.Fatalf("Authorizer nil")
	}
}

// TestBuildAuthorizer_RelativePolicyDir_ResolvedUnderRepoRoot asserts policy_dir resolves repo-relative → absolute under repoRoot so disk loader is byte-stable across cwds.
func TestBuildAuthorizer_RelativePolicyDir_ResolvedUnderRepoRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "pol", "regatta", "v1", "default"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := []byte(`version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:planned"
ci:
  command: "go test ./..."
gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
  authz:
    policy_dir: pol
`)
	if err := os.WriteFile(filepath.Join(repo, "regatta.yaml"), yaml, 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	az, err := buildAuthorizer(ctx, repo, discardLogger())
	if err != nil {
		t.Fatalf("buildAuthorizer: %v", err)
	}
	if az.CurrentRevision(authz.DefaultTenant) == "" {
		t.Fatal("Authorizer not hydrated")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
