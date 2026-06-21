package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/merge/lowrisk"
)

// TestLoadSchedulerParallelCap_ReturnsConfiguredCap asserts wire reads regatta.yaml::scheduler.parallel_cap (#1169).
func TestLoadSchedulerParallelCap_ReturnsConfiguredCap(t *testing.T) {
	dir := t.TempDir()
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
scheduler:
  parallel_cap: 7
gates:
  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`)
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), yaml, 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	if got := loadSchedulerParallelCap(dir); got != 7 {
		t.Fatalf("loadSchedulerParallelCap=%d, want 7", got)
	}
}

// TestLoadSchedulerParallelCap_MissingYAML_ReturnsZero asserts best-effort contract (#1169).
func TestLoadSchedulerParallelCap_MissingYAML_ReturnsZero(t *testing.T) {
	if got := loadSchedulerParallelCap(t.TempDir()); got != 0 {
		t.Fatalf("loadSchedulerParallelCap=%d, want 0 on missing yaml", got)
	}
}

// TestLoadDestructiveOpLists_DefaultAllowlist asserts the CUE-default allow surfaces for brief injection (MAY-97, MAY-258).
func TestLoadDestructiveOpLists_DefaultAllowlist(t *testing.T) {
	dir := t.TempDir()
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
  destructive_ops_deny: ['git push --force']
  agent_creds_scope: dev_only
`)
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), yaml, 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	deny, allow := loadDestructiveOpLists(dir)
	if len(deny) == 0 || len(allow) == 0 {
		t.Fatalf("expected resolved deny+allow lists; got deny=%v allow=%v", deny, allow)
	}
}

// TestLoadDestructiveOpLists_MissingYAML_ReturnsNil asserts best-effort contract (MAY-258).
func TestLoadDestructiveOpLists_MissingYAML_ReturnsNil(t *testing.T) {
	if deny, allow := loadDestructiveOpLists(t.TempDir()); deny != nil || allow != nil {
		t.Fatalf("missing yaml should yield nil lists; got deny=%v allow=%v", deny, allow)
	}
}

const lowRiskBaseYAML = `version: 1
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
`

// TestBuildLowRiskGate_AutoMergeOffReturnsNil asserts the gate is nil when --auto-merge=false (MAY-86).
func TestBuildLowRiskGate_AutoMergeOffReturnsNil(t *testing.T) {
	if g := buildLowRiskGate(t.TempDir(), false, discardLogger()); g != nil {
		t.Fatalf("auto-merge off must yield nil gate; got %T", g)
	}
}

// TestBuildLowRiskGate_ConservativeDefaultHoldsAll asserts --auto-merge=true with the gate disabled wires HoldAll (MAY-86).
func TestBuildLowRiskGate_ConservativeDefaultHoldsAll(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), []byte(lowRiskBaseYAML), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	g := buildLowRiskGate(dir, true, discardLogger())
	if _, ok := g.(lowrisk.HoldAll); !ok {
		t.Fatalf("conservative default must wire HoldAll; got %T", g)
	}
}

// TestBuildLowRiskGate_EnabledWiresRealGate asserts the double opt-in (auto-merge + enabled) wires a real *lowrisk.Gate (MAY-86).
func TestBuildLowRiskGate_EnabledWiresRealGate(t *testing.T) {
	dir := t.TempDir()
	yaml := lowRiskBaseYAML + `low_risk_automerge:
  enabled: true
  loc_cap: 40
  hold_window: "10m"
`
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	g := buildLowRiskGate(dir, true, discardLogger())
	if _, ok := g.(*lowrisk.Gate); !ok {
		t.Fatalf("enabled double opt-in must wire *lowrisk.Gate; got %T", g)
	}
}

// TestBuildLowRiskGate_BadHoldWindowHoldsAll asserts a malformed hold_window reverts to HoldAll (fail-closed) rather than wiring a real gate (MAY-86 reviewer).
func TestBuildLowRiskGate_BadHoldWindowHoldsAll(t *testing.T) {
	dir := t.TempDir()
	yaml := lowRiskBaseYAML + `low_risk_automerge:
  enabled: true
  loc_cap: 40
  hold_window: "0s"
`
	if err := os.WriteFile(filepath.Join(dir, "regatta.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	g := buildLowRiskGate(dir, true, discardLogger())
	if _, ok := g.(lowrisk.HoldAll); !ok {
		t.Fatalf("bad hold_window must revert to HoldAll; got %T", g)
	}
}
