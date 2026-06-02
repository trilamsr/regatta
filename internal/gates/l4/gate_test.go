package l4

import (
	"context"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/gates/severity"
)

// Run with no findings returns a clean pass.
func TestL4_Run_CleanPass(t *testing.T) {
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		Invoker:       stubInvoker(nil),
	}
	gr, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gr.Verdict != schemas.VerdictPass {
		t.Fatalf("clean-pass: got %s, want pass", gr.Verdict)
	}
	if gr.Blocking {
		t.Fatalf("clean-pass must not block")
	}
	if gr.GateKind != schemas.GateKindAIAdversarial {
		t.Fatalf("gate_kind: got %s, want ai_adversarial", gr.GateKind)
	}
	if gr.Telemetry.Model != DefaultModel {
		t.Fatalf("telemetry.model: got %q, want %q", gr.Telemetry.Model, DefaultModel)
	}
}

// One critical finding routes to fail+blocking.
func TestL4_Run_OneCritical_Blocks(t *testing.T) {
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		Invoker: stubInvoker([]schemas.Finding{{
			ID:       "L4-CORR-OFFBYONE",
			Severity: schemas.FindingCritical,
			Claim:    "scheduler tick increments by 2 instead of 1",
		}}),
	}
	gr, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gr.Verdict != schemas.VerdictFail {
		t.Fatalf("one-critical: got %s, want fail", gr.Verdict)
	}
	if !gr.Blocking {
		t.Fatalf("one-critical must block")
	}
}

// One high finding stays advisory; two high findings block.
func TestL4_Run_TwoHigh_Blocks(t *testing.T) {
	highs := []schemas.Finding{
		{ID: "L4-RISK-LOAD", Severity: schemas.FindingHigh, Claim: "load-bearing change w/o rollback"},
		{ID: "L4-DOC-BANNED", Severity: schemas.FindingHigh, Claim: "banned phrase"},
	}
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		Invoker:       stubInvoker(highs),
	}
	gr, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gr.Verdict != schemas.VerdictFail {
		t.Fatalf("two-high: got %s, want fail", gr.Verdict)
	}
	if !gr.Blocking {
		t.Fatalf("two-high must block")
	}
}

// Advisory mode demotes a would-be fail to advisory verdict + non-blocking.
func TestL4_Run_AdvisoryMode_NeverBlocks(t *testing.T) {
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		AdvisoryMode:  true,
		Invoker: stubInvoker([]schemas.Finding{{
			ID:       "L4-SEC-AUTHBYPASS",
			Severity: schemas.FindingCritical,
		}}),
	}
	gr, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-4"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gr.Verdict != schemas.VerdictAdvisory {
		t.Fatalf("advisory-mode: got %s, want advisory", gr.Verdict)
	}
	if gr.Blocking {
		t.Fatalf("advisory-mode must not block")
	}
}

// Auto-fixable finding round-trips through gate when AutoFix=true (closes #358).
func TestL4_Run_AutoFix_PatchSurfacesInVerdict(t *testing.T) {
	patch := "--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n-old\n+new\n"
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		AutoFix:       true,
		Invoker: stubInvoker([]schemas.Finding{{
			ID:          "L4-REF-NAMING",
			Severity:    schemas.FindingMedium,
			Claim:       "exported func name shadows stdlib io.Reader",
			AutoFixable: true,
			Patch:       patch,
		}}),
	}
	gr, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-autofix"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gr.Findings) != 1 {
		t.Fatalf("findings count: got %d, want 1", len(gr.Findings))
	}
	if !gr.Findings[0].AutoFixable {
		t.Fatalf("auto_fixable: got false, want true")
	}
	if gr.Findings[0].Patch != patch {
		t.Fatalf("patch: got %q, want %q", gr.Findings[0].Patch, patch)
	}
}

// AutoFix=false strips Patch + AutoFixable off findings (operator opt-in).
func TestL4_Run_AutoFix_OffStripsPatch(t *testing.T) {
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		AutoFix:       false,
		Invoker: stubInvoker([]schemas.Finding{{
			ID:          "L4-REF-DEAD",
			Severity:    schemas.FindingMedium,
			Claim:       "unreachable branch after early-return",
			AutoFixable: true,
			Patch:       "--- a/x\n+++ b/x\n@@ -1 +0,0 @@\n-dead\n",
		}}),
	}
	gr, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-autofix-off"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gr.Findings[0].Patch != "" {
		t.Fatalf("patch should be stripped when AutoFix=false; got %q", gr.Findings[0].Patch)
	}
	if gr.Findings[0].AutoFixable {
		t.Fatalf("auto_fixable should be cleared when AutoFix=false")
	}
}

// Nil Invoker fails loud rather than silently passing.
func TestL4_Run_NilInvoker_FailsLoud(t *testing.T) {
	cfg := Config{GateID: "l4_adversarial", Model: DefaultModel}
	gr, err := Run(context.Background(), cfg, Input{PRSHA: "deadbeef", RunID: "run-5"})
	if err == nil {
		t.Fatalf("nil-invoker: expected error, got nil")
	}
	if gr.Verdict != schemas.VerdictFail || !gr.Blocking {
		t.Fatalf("nil-invoker: got verdict=%s blocking=%v, want fail+blocking", gr.Verdict, gr.Blocking)
	}
}

// stubInvoker returns a canned-finding Invoker for tests.
func stubInvoker(findings []schemas.Finding) Invoker {
	return func(_ context.Context, _ InvokeRequest) (InvokeResponse, error) {
		return InvokeResponse{Findings: findings}, nil
	}
}
