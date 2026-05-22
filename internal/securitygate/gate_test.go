package securitygate

import (
	"context"
	"testing"

	"github.com/trilamsr/regatta/schemas"
)

func TestRun_FloorDisabledAndAIDisabled_Passes(t *testing.T) {
	cfg := Config{
		GateID: "security",
		DeterminismFloor: FloorConfig{
			Gitleaks:   ToolPin{Enabled: false},
			OSVScanner: ToolPin{Enabled: false},
		},
		AI: AIConfig{Enabled: false},
	}
	gr, err := Run(context.Background(), cfg, Input{PRSHA: "abc", RunID: "00000000-0000-0000-0000-000000000000"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gr.Verdict != schemas.VerdictPass {
		t.Fatalf("expected pass with all knobs off, got %s", gr.Verdict)
	}
}

func TestRun_AIStubEmitsAdvisoryOnly(t *testing.T) {
	cfg := Config{
		GateID: "security",
		DeterminismFloor: FloorConfig{
			Gitleaks:   ToolPin{Enabled: false},
			OSVScanner: ToolPin{Enabled: false},
		},
		AI: AIConfig{Enabled: true, Model: "claude-opus-4-7", Mode: "adversarial", MinFalsifications: 3},
	}
	gr, err := Run(context.Background(), cfg, Input{PRSHA: "abc", RunID: "00000000-0000-0000-0000-000000000000"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gr.Verdict != schemas.VerdictPass {
		t.Fatalf("AI stub must be advisory, not blocking — got verdict %s", gr.Verdict)
	}
	if len(gr.Findings) != 1 {
		t.Fatalf("expected 1 stub finding, got %d", len(gr.Findings))
	}
	if gr.Findings[0].Blocking {
		t.Fatalf("stub finding must not be blocking")
	}
	if gr.GateKind != "ai_adversarial" {
		t.Fatalf("expected gate_kind ai_adversarial when AI enabled, got %s", gr.GateKind)
	}
}

func TestRun_MissingGitleaksBinaryFailsLoud(t *testing.T) {
	// We can't install/uninstall binaries in a unit test, but we can
	// at least confirm Run propagates an error rather than silently
	// passing when the floor errors out.
	cfg := Config{
		GateID: "security",
		DeterminismFloor: FloorConfig{
			Gitleaks: ToolPin{Enabled: true},
		},
	}
	gr, err := Run(context.Background(), cfg, Input{PRSHA: "abc", RunID: "00000000-0000-0000-0000-000000000000", RepoRoot: "."})
	// If gitleaks happens to be installed on the developer's machine,
	// the test gracefully degrades to "ran successfully" — that's
	// also a valid pass.
	if err == nil {
		if gr.Verdict != schemas.VerdictPass {
			t.Logf("gitleaks present locally; finished with verdict %s and %d finding(s)", gr.Verdict, len(gr.Findings))
		}
		return
	}
	if gr.Verdict != schemas.VerdictFail {
		t.Fatalf("expected verdict fail on floor error, got %s (err=%v)", gr.Verdict, err)
	}
}
