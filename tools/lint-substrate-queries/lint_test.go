package main

import (
	"path/filepath"
	"testing"
)

// TestLintSubstrateQueries_RejectsUnscopedRead pins spec §6: SELECT FROM substrate_events without `kind=?` ⇒ finding.
func TestLintSubstrateQueries_RejectsUnscopedRead(t *testing.T) {
	findings, err := runLinter(filepath.Join("testdata", "unscoped"))
	if err != nil {
		t.Fatalf("runLinter: %v", err)
	}
	if len(findings) == 0 {
		t.Fatalf("expected at least one finding for unscoped SELECT * FROM substrate_events, got 0")
	}
}

// TestLintSubstrateQueries_AllowsKindScopedRead pins spec §6: `kind=?` + `run_id=?` scope ⇒ clean.
func TestLintSubstrateQueries_AllowsKindScopedRead(t *testing.T) {
	findings, err := runLinter(filepath.Join("testdata", "kind_scoped"))
	if err != nil {
		t.Fatalf("runLinter: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected zero findings for kind-scoped read, got %d: %v", len(findings), findings)
	}
}

// TestLintSubstrateQueries_RequiresTenantOnCrossRunRead pins spec §6: cross-run reads without `tenant_id=?` ⇒ finding.
func TestLintSubstrateQueries_RequiresTenantOnCrossRunRead(t *testing.T) {
	findings, err := runLinter(filepath.Join("testdata", "cross_run_no_tenant"))
	if err != nil {
		t.Fatalf("runLinter: %v", err)
	}
	if len(findings) == 0 {
		t.Fatalf("expected finding for cross-run read missing tenant_id=?, got 0")
	}
}
