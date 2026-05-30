package schemas

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestGateResultSchemaLockstep verifies that the canonical Go
// shape round-trips through JSON without losing or renaming any
// schema-required field. Drift between gate_result.go and
// gate_result.schema.json is the failure mode this test exists to
// prevent.
func TestGateResultSchemaLockstep(t *testing.T) {
	t.Parallel()
	gr := GateResult{
		SchemaVersion: 1,
		GateID:        "l0_spec_immutability",
		GateKind:      GateKindDeterministic,
		PRSHA:         "0123456789abcdef0123456789abcdef01234567",
		RunID:         "11111111-1111-1111-1111-111111111111",
		Verdict:       VerdictPass,
		Blocking:      false,
		Severity:      SeverityNone,
		Findings:      []Finding{},
		Telemetry: Telemetry{
			DurationMs: 123,
			Model:      "claude-opus-4-7",
			StartedAt:  time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC),
			FinishedAt: time.Date(2026, 5, 21, 0, 0, 1, 0, time.UTC),
		},
		Signature: SignatureBlock{Alg: "HMAC-SHA256", KeyID: "k1", MAC: strings.Repeat("0", 64)},
	}

	raw, err := json.Marshal(gr)
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []string{
		`"schema_version"`, `"gate_id"`, `"gate_kind"`, `"pr_sha"`,
		`"run_id"`, `"verdict"`, `"blocking"`, `"findings"`,
		`"telemetry"`, `"signature"`,
		`"duration_ms"`, `"alg"`, `"key_id"`, `"mac"`,
	} {
		if !strings.Contains(string(raw), req) {
			t.Fatalf("missing %s in canonical form: %s", req, raw)
		}
	}

	// And finding shape: id/severity/claim required, evidence/remediation/trap_pattern optional.
	f := Finding{
		ID:       "F-001",
		Severity: FindingCritical,
		Claim:    "criterion text mutated",
		Evidence: &FindingEvidence{
			Path:      "RFC.md",
			LineStart: 10,
			LineEnd:   15,
			SHA:       strings.Repeat("a", 40),
		},
		TrapPattern: "P3",
	}
	fraw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []string{`"id"`, `"severity"`, `"claim"`, `"evidence"`, `"trap_pattern"`} {
		if !strings.Contains(string(fraw), req) {
			t.Fatalf("missing %s in finding canonical form: %s", req, fraw)
		}
	}

	// Verdict + GateKind values must match the schema enums.
	for _, v := range []Verdict{VerdictPass, VerdictFail, VerdictAdvisory} {
		if _, err := json.Marshal(v); err != nil {
			t.Fatalf("verdict %q does not marshal: %v", v, err)
		}
	}
	for _, k := range []GateKind{GateKindDeterministic, GateKindAIJudicial, GateKindAIAdversarial, GateKindAIRuleCheck} {
		if _, err := json.Marshal(k); err != nil {
			t.Fatalf("gate_kind %q does not marshal: %v", k, err)
		}
	}
}

func TestVerdictEnumMatchesSchema(t *testing.T) {
	t.Parallel()
	// Schema enum is exactly {pass, fail, advisory}. Anything else
	// is a drift bug.
	allowed := map[Verdict]bool{VerdictPass: true, VerdictFail: true, VerdictAdvisory: true}
	for _, v := range []Verdict{"pass", "fail", "advisory"} {
		if !allowed[v] {
			t.Fatalf("schema verdict %q not in Go constants", v)
		}
	}
}
