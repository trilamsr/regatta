// serve_test verifies the scheduler-side approval-gate wiring spec §3.1
// step 0.5 expects from `regatta serve`. Heavyweight end-to-end coverage
// lives in serve_claude_test.go; this file pins the minimal contract that
// regatta.yaml gates load + appear in the scheduler config.
package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/config"
	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

const serveTestGateYAML = `version: 1
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
  - id: prod-deploy-approval
    type: approval_gate
    name: prod
    risk_class: low
    reviewers: [alice, bob]
    quorum: 1
    timeout: 1h
    decision_window: 30m
    on_timeout: fail
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`

// TestBuildApprovalGate_LoadsAndResolvesByLane pins the MVP-2 W3
// resolution policy: gate.Name == work_item.Lane. A wi on lane "prod"
// matches the "prod" gate; lanes without a matching gate resolve as
// non-gated.
func TestBuildApprovalGate_LoadsAndResolvesByLane(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(serveTestGateYAML), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}
	db := openSchedulerTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gate, resolver, err := buildApprovalGate(db, repoRoot, logger)
	if err != nil {
		t.Fatalf("buildApprovalGate: %v", err)
	}
	if gate == nil {
		t.Fatal("gate is nil; want a non-nil ApprovalGate when regatta.yaml declares one")
	}
	if resolver == nil {
		t.Fatal("resolver is nil; want a non-nil GateResolver")
	}

	cfg, ok := resolver(state.WorkItem{ID: "F-1", Lane: "prod"})
	if !ok {
		t.Fatal("resolver returned !ok for lane=prod; want matched")
	}
	if cfg.Name != "prod" {
		t.Errorf("cfg.Name=%q; want prod", cfg.Name)
	}
	if cfg.Quorum != 1 {
		t.Errorf("cfg.Quorum=%d; want 1", cfg.Quorum)
	}

	if _, ok := resolver(state.WorkItem{ID: "F-2", Lane: "server"}); ok {
		t.Error("resolver matched lane=server; want !ok (no gate for that lane)")
	}
}

// TestBuildApprovalGate_NoConfigFileDisabled pins the zero-config path:
// repos without a regatta.yaml load with gate=nil so the scheduler
// gate-pass is a no-op. Operators who have not adopted approval gates
// pay zero runtime cost.
func TestBuildApprovalGate_NoConfigFileDisabled(t *testing.T) {
	repoRoot := t.TempDir()
	db := openSchedulerTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gate, resolver, err := buildApprovalGate(db, repoRoot, logger)
	if err != nil {
		t.Fatalf("buildApprovalGate: %v", err)
	}
	if gate != nil || resolver != nil {
		t.Fatalf("gate=%v resolver=%v; want both nil when regatta.yaml absent", gate, resolver)
	}
}

// TestBuildApprovalGate_NoGatesDisabled pins the other zero-cost path:
// regatta.yaml exists but declares zero approval_gate rows. Same
// outcome as no-config — scheduler gate-pass stays disabled.
func TestBuildApprovalGate_NoGatesDisabled(t *testing.T) {
	repoRoot := t.TempDir()
	emptyYAML := `version: 1
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
  - id: ci-build
    type: deterministic
    command: "go build ./..."
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`
	if err := os.WriteFile(filepath.Join(repoRoot, "regatta.yaml"), []byte(emptyYAML), 0o600); err != nil {
		t.Fatalf("write regatta.yaml: %v", err)
	}
	db := openSchedulerTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gate, resolver, err := buildApprovalGate(db, repoRoot, logger)
	if err != nil {
		t.Fatalf("buildApprovalGate: %v", err)
	}
	if gate != nil || resolver != nil {
		t.Fatalf("gate=%v resolver=%v; want both nil when zero approval_gates configured", gate, resolver)
	}
}

// TestConvertApprovalGateConfig_RoundTrips pins the field-for-field
// mapping between config.ApprovalGateConfig (YAML loader) and
// approval.Config (runtime). Drift here means a gate operator typed in
// regatta.yaml gets silently dropped at the scheduler boundary.
func TestConvertApprovalGateConfig_RoundTrips(t *testing.T) {
	// Compile-time + runtime sanity: build, convert, compare.
	in := canonicalApprovalGateConfig()
	out := convertApprovalGateConfig(in)
	if out.Name != in.Name {
		t.Errorf("Name: in=%q out=%q", in.Name, out.Name)
	}
	if out.Quorum != in.Quorum {
		t.Errorf("Quorum: in=%d out=%d", in.Quorum, out.Quorum)
	}
	if out.Timeout != in.Timeout {
		t.Errorf("Timeout: in=%v out=%v", in.Timeout, out.Timeout)
	}
	if out.OnTimeout != in.OnTimeout {
		t.Errorf("OnTimeout: in=%q out=%q", in.OnTimeout, out.OnTimeout)
	}
	if len(out.Reviewers) != len(in.Reviewers) {
		t.Errorf("Reviewers len mismatch: in=%d out=%d", len(in.Reviewers), len(out.Reviewers))
	}
	if len(out.EscalationChain) != len(in.EscalationChain) {
		t.Errorf("EscalationChain len: in=%d out=%d", len(in.EscalationChain), len(out.EscalationChain))
	}
	// Sanity check: round-tripped Config validates per spec §5.5.
	if err := out.Validate(); err != nil {
		t.Errorf("converted Config fails approval.Config.Validate: %v", err)
	}
	// Compile-time assertion that the seam wires through; touches the
	// approval import so the test stays robust if Config grows fields
	// the converter must learn to copy.
	_ = approval.ResultPause
}

// canonicalApprovalGateConfig is a small valid config row used to test
// the converter in isolation from the YAML loader.
func canonicalApprovalGateConfig() config.ApprovalGateConfig {
	return config.ApprovalGateConfig{
		Name:           "prod",
		RiskClass:      config.RiskLow,
		Reviewers:      []string{"alice", "bob"},
		Quorum:         1,
		Timeout:        time.Hour,
		DecisionWindow: 30 * time.Minute,
		OnTimeout:      config.OnTimeoutFail,
		EscalationChain: []config.ApprovalTierConfig{{
			Reviewers:      []string{"carol"},
			Quorum:         1,
			Timeout:        time.Hour,
			DecisionWindow: 30 * time.Minute,
		}},
	}
}

func openSchedulerTestDB(t *testing.T) *state.DB {
	t.Helper()
	dsn := state.DSN(filepath.Join(t.TempDir(), "serve.db"))
	db, err := state.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
