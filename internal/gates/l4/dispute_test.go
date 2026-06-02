package l4

import (
	"context"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// ParseDisputes extracts finding IDs from a PR body's [L4-DISPUTE]
// markers. Empty body returns nil. Marker shape: "[L4-DISPUTE] <id>"
// or "[L4-DISPUTE] id1, id2".
func TestL4_ParseDisputes_BasicMarker(t *testing.T) {
	body := "Some PR description.\n\n[L4-DISPUTE] L4-CORR-OFFBYONE\n\nRationale: not a bug because..."
	got := ParseDisputes(body)
	if len(got) != 1 || got[0] != "L4-CORR-OFFBYONE" {
		t.Fatalf("ParseDisputes: got %v, want [L4-CORR-OFFBYONE]", got)
	}
}

// Multiple disputes on separate lines collect into a flat list.
func TestL4_ParseDisputes_MultipleMarkers(t *testing.T) {
	body := "[L4-DISPUTE] L4-CORR-OFFBYONE\n[L4-DISPUTE] L4-REFACTOR-NAMING\n"
	got := ParseDisputes(body)
	if len(got) != 2 || got[0] != "L4-CORR-OFFBYONE" || got[1] != "L4-REFACTOR-NAMING" {
		t.Fatalf("ParseDisputes: got %v, want [L4-CORR-OFFBYONE L4-REFACTOR-NAMING]", got)
	}
}

// Comma-separated IDs on one marker line all extract.
func TestL4_ParseDisputes_CommaSeparated(t *testing.T) {
	body := "[L4-DISPUTE] L4-CORR-OFFBYONE, L4-REFACTOR-NAMING"
	got := ParseDisputes(body)
	if len(got) != 2 || got[0] != "L4-CORR-OFFBYONE" || got[1] != "L4-REFACTOR-NAMING" {
		t.Fatalf("ParseDisputes: got %v, want both IDs", got)
	}
}

// No marker present returns nil.
func TestL4_ParseDisputes_NoMarker(t *testing.T) {
	body := "Plain PR body with no dispute marker."
	if got := ParseDisputes(body); len(got) != 0 {
		t.Fatalf("ParseDisputes: got %v, want nil", got)
	}
}

// DefaultSecondOpinionModel is the alt-model escalation default.
func TestL4_DefaultSecondOpinionModel(t *testing.T) {
	if DefaultSecondOpinionModel != "claude-opus-4-7" {
		t.Fatalf("default second-opinion model drift: got %q, want claude-opus-4-7", DefaultSecondOpinionModel)
	}
}

// ResolveSecondOpinionModel honours yaml > env > default precedence.
func TestL4_SecondOpinionModelResolutionOrder(t *testing.T) {
	cases := []struct {
		name    string
		yamlVal string
		envVal  string
		want    string
	}{
		{"yaml-wins", "claude-haiku-4-5", "claude-sonnet-4-6", "claude-haiku-4-5"},
		{"env-fills-when-yaml-empty", "", "claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"default-when-both-empty", "", "", "claude-opus-4-7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(EnvSecondOpinionModel, c.envVal)
			got := ResolveSecondOpinionModel(c.yamlVal)
			if got != c.want {
				t.Fatalf("ResolveSecondOpinionModel(%q) env=%q: got %q, want %q",
					c.yamlVal, c.envVal, got, c.want)
			}
		})
	}
}

// When the PR body disputes a critical finding and the second-opinion
// model does NOT reproduce it, the finding drops and the gate passes.
func TestL4_Run_DisputedFindingDropped_WhenSecondOpinionClears(t *testing.T) {
	primary := []schemas.Finding{
		{ID: "L4-CORR-OFFBYONE", Severity: schemas.FindingCritical, Claim: "off-by-one"},
	}
	secondCalls := 0
	cfg := Config{
		GateID:              "l4_adversarial",
		Model:               DefaultModel,
		SecondOpinionModel:  DefaultSecondOpinionModel,
		SeverityBlock:       []string{RuleCritical, RuleTwoHigh},
		Invoker: func(_ context.Context, req InvokeRequest) (InvokeResponse, error) {
			if req.Model == DefaultSecondOpinionModel {
				secondCalls++
				// Second opinion clears: returns no findings.
				return InvokeResponse{Findings: nil}, nil
			}
			return InvokeResponse{Findings: primary}, nil
		},
	}
	in := Input{
		PRSHA:  "deadbeef",
		RunID:  "run-disp-1",
		PRBody: "[L4-DISPUTE] L4-CORR-OFFBYONE",
	}
	gr, err := Run(context.Background(), cfg, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secondCalls != 1 {
		t.Fatalf("second-opinion invoker not called: got %d calls, want 1", secondCalls)
	}
	// Disputed critical was dropped; verdict must be pass.
	if gr.Verdict != schemas.VerdictPass {
		t.Fatalf("verdict: got %s, want pass (disputed critical cleared)", gr.Verdict)
	}
	if gr.Blocking {
		t.Fatalf("must not block: disputed finding was cleared by second opinion")
	}
	// Original critical must not be present in the merged findings.
	for _, f := range gr.Findings {
		if f.ID == "L4-CORR-OFFBYONE" && f.Severity == schemas.FindingCritical {
			t.Fatalf("disputed critical finding still present after second opinion cleared it: %+v", f)
		}
	}
}

// When the second-opinion model confirms the disputed finding, the
// finding stays and the gate keeps blocking.
func TestL4_Run_DisputedFindingKept_WhenSecondOpinionConfirms(t *testing.T) {
	primary := []schemas.Finding{
		{ID: "L4-CORR-OFFBYONE", Severity: schemas.FindingCritical, Claim: "off-by-one"},
	}
	cfg := Config{
		GateID:             "l4_adversarial",
		Model:              DefaultModel,
		SecondOpinionModel: DefaultSecondOpinionModel,
		SeverityBlock:      []string{RuleCritical, RuleTwoHigh},
		Invoker: func(_ context.Context, req InvokeRequest) (InvokeResponse, error) {
			if req.Model == DefaultSecondOpinionModel {
				// Second opinion confirms the same finding.
				return InvokeResponse{Findings: []schemas.Finding{
					{ID: "L4-CORR-OFFBYONE", Severity: schemas.FindingCritical, Claim: "confirmed"},
				}}, nil
			}
			return InvokeResponse{Findings: primary}, nil
		},
	}
	in := Input{
		PRSHA:  "deadbeef",
		RunID:  "run-disp-2",
		PRBody: "[L4-DISPUTE] L4-CORR-OFFBYONE",
	}
	gr, err := Run(context.Background(), cfg, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gr.Verdict != schemas.VerdictFail {
		t.Fatalf("verdict: got %s, want fail (disputed critical confirmed)", gr.Verdict)
	}
	if !gr.Blocking {
		t.Fatalf("must block: disputed finding was confirmed by second opinion")
	}
}

// No dispute marker means the second-opinion model is never called.
func TestL4_Run_NoDispute_NoSecondOpinionCall(t *testing.T) {
	secondCalls := 0
	cfg := Config{
		GateID:             "l4_adversarial",
		Model:              DefaultModel,
		SecondOpinionModel: DefaultSecondOpinionModel,
		SeverityBlock:      []string{RuleCritical, RuleTwoHigh},
		Invoker: func(_ context.Context, req InvokeRequest) (InvokeResponse, error) {
			if req.Model == DefaultSecondOpinionModel {
				secondCalls++
			}
			return InvokeResponse{Findings: []schemas.Finding{
				{ID: "L4-CORR-OFFBYONE", Severity: schemas.FindingCritical},
			}}, nil
		},
	}
	in := Input{PRSHA: "deadbeef", RunID: "run-disp-3", PRBody: "no marker here"}
	gr, _ := Run(context.Background(), cfg, in)
	if secondCalls != 0 {
		t.Fatalf("second-opinion called without dispute marker: %d calls", secondCalls)
	}
	if gr.Verdict != schemas.VerdictFail {
		t.Fatalf("undisputed critical must fail: got %s", gr.Verdict)
	}
}

// A dispute for a finding ID that the primary never reported is a
// no-op; second-opinion model is not invoked.
func TestL4_Run_DisputeForUnknownFinding_NoSecondOpinionCall(t *testing.T) {
	secondCalls := 0
	cfg := Config{
		GateID:             "l4_adversarial",
		Model:              DefaultModel,
		SecondOpinionModel: DefaultSecondOpinionModel,
		SeverityBlock:      []string{RuleCritical, RuleTwoHigh},
		Invoker: func(_ context.Context, req InvokeRequest) (InvokeResponse, error) {
			if req.Model == DefaultSecondOpinionModel {
				secondCalls++
			}
			return InvokeResponse{Findings: []schemas.Finding{
				{ID: "L4-CORR-OFFBYONE", Severity: schemas.FindingCritical},
			}}, nil
		},
	}
	in := Input{
		PRSHA:  "deadbeef",
		RunID:  "run-disp-4",
		PRBody: "[L4-DISPUTE] L4-NONEXISTENT-FINDING",
	}
	_, _ = Run(context.Background(), cfg, in)
	if secondCalls != 0 {
		t.Fatalf("second-opinion called for unknown finding id: %d calls", secondCalls)
	}
}

// When second-opinion model errors, the primary findings are kept
// (fail-closed): a dispute that cannot be confirmed must not silently
// pass.
func TestL4_Run_SecondOpinionError_KeepsPrimaryFindings(t *testing.T) {
	cfg := Config{
		GateID:             "l4_adversarial",
		Model:              DefaultModel,
		SecondOpinionModel: DefaultSecondOpinionModel,
		SeverityBlock:      []string{RuleCritical, RuleTwoHigh},
		Invoker: func(_ context.Context, req InvokeRequest) (InvokeResponse, error) {
			if req.Model == DefaultSecondOpinionModel {
				return InvokeResponse{}, context.DeadlineExceeded
			}
			return InvokeResponse{Findings: []schemas.Finding{
				{ID: "L4-CORR-OFFBYONE", Severity: schemas.FindingCritical},
			}}, nil
		},
	}
	in := Input{
		PRSHA:  "deadbeef",
		RunID:  "run-disp-5",
		PRBody: "[L4-DISPUTE] L4-CORR-OFFBYONE",
	}
	gr, _ := Run(context.Background(), cfg, in)
	if gr.Verdict != schemas.VerdictFail {
		t.Fatalf("second-opinion error must fail-closed: got %s, want fail", gr.Verdict)
	}
	if !gr.Blocking {
		t.Fatalf("second-opinion error must keep blocking")
	}
}

// SecondOpinionModel empty in config falls back to the default model
// via ResolveSecondOpinionModel during Run.
func TestL4_Run_SecondOpinionModelDefault_AppliesWhenUnset(t *testing.T) {
	requestedModels := []string{}
	cfg := Config{
		GateID:        "l4_adversarial",
		Model:         DefaultModel,
		SeverityBlock: []string{RuleCritical, RuleTwoHigh},
		Invoker: func(_ context.Context, req InvokeRequest) (InvokeResponse, error) {
			requestedModels = append(requestedModels, req.Model)
			if req.Model == DefaultSecondOpinionModel {
				return InvokeResponse{Findings: nil}, nil
			}
			return InvokeResponse{Findings: []schemas.Finding{
				{ID: "L4-CORR-OFFBYONE", Severity: schemas.FindingCritical},
			}}, nil
		},
	}
	in := Input{
		PRSHA:  "deadbeef",
		RunID:  "run-disp-6",
		PRBody: "[L4-DISPUTE] L4-CORR-OFFBYONE",
	}
	_, _ = Run(context.Background(), cfg, in)
	if len(requestedModels) != 2 {
		t.Fatalf("expected 2 invoker calls (primary + second-opinion), got %d", len(requestedModels))
	}
	if requestedModels[1] != DefaultSecondOpinionModel {
		t.Fatalf("second invoker call model: got %q, want %q", requestedModels[1], DefaultSecondOpinionModel)
	}
}
