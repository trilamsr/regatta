package main

import (
	"path/filepath"
	"testing"
)

// TestLintSubstrateQueries_RejectsUnscopedRead: an SQL string selecting
// from substrate_events with no WHERE-clause kind filter must produce a
// finding. Spec §6 mandates kind=? on every read.
func TestLintSubstrateQueries_RejectsUnscopedRead(t *testing.T) {
	findings, err := runLinter(filepath.Join("testdata", "unscoped"))
	if err != nil {
		t.Fatalf("runLinter: %v", err)
	}
	if len(findings) == 0 {
		t.Fatalf("expected at least one finding for unscoped SELECT * FROM substrate_events, got 0")
	}
}

// TestLintSubstrateQueries_AllowsKindScopedRead: an SQL string with
// `kind = ?` in the WHERE clause and a `run_id = ?` filter passes lint.
func TestLintSubstrateQueries_AllowsKindScopedRead(t *testing.T) {
	findings, err := runLinter(filepath.Join("testdata", "kind_scoped"))
	if err != nil {
		t.Fatalf("runLinter: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected zero findings for kind-scoped read, got %d: %v", len(findings), findings)
	}
}

// TestLintSubstrateQueries_RequiresTenantOnCrossRunRead: a SELECT with
// kind=? but no run_id=? (cross-run audit/billing read) must also carry
// tenant_id=?. Spec §6.
func TestLintSubstrateQueries_RequiresTenantOnCrossRunRead(t *testing.T) {
	findings, err := runLinter(filepath.Join("testdata", "cross_run_no_tenant"))
	if err != nil {
		t.Fatalf("runLinter: %v", err)
	}
	if len(findings) == 0 {
		t.Fatalf("expected finding for cross-run read missing tenant_id=?, got 0")
	}
}
