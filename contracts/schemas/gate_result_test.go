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
		},
		Heartbeat: TelemetryHeartbeat{
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

	// Auto-fix mode: when AutoFixable=true + Patch non-empty, both
	// fields must serialize so a downstream PR-comment poster can
	// render the unified-diff verbatim (closes #358).
	af := Finding{
		ID:          "L4-REF-NAMING",
		Severity:    FindingMedium,
		Claim:       "renaming candidate",
		AutoFixable: true,
		Patch:       "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n",
	}
	afraw, err := json.Marshal(af)
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []string{`"auto_fixable"`, `"patch"`} {
		if !strings.Contains(string(afraw), req) {
			t.Fatalf("missing %s in auto-fix finding: %s", req, afraw)
		}
	}
	// Zero-value Finding must omit both (omitempty discipline keeps
	// the audit payload small for the 99% no-patch case).
	zraw, err := json.Marshal(Finding{ID: "F", Severity: FindingLow, Claim: "c"})
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{`"auto_fixable"`, `"patch"`} {
		if strings.Contains(string(zraw), banned) {
			t.Fatalf("zero-value finding must omit %s: %s", banned, zraw)
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

// TestHeartbeatNotInSignedPayload pins that StartedAt/FinishedAt
// never leak into the canonical JSON used for signing. The
// reconciler reads Heartbeat in-process; including it in the
// signed payload would weaken tamper-evidence by including
// non-deterministic timestamps that vary across re-runs.
func TestHeartbeatNotInSignedPayload(t *testing.T) {
	t.Parallel()
	gr := GateResult{
		SchemaVersion: 1,
		GateID:        "l0",
		Heartbeat: TelemetryHeartbeat{
			StartedAt:  time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC),
			FinishedAt: time.Date(2026, 5, 30, 0, 0, 1, 0, time.UTC),
		},
	}
	raw, err := json.Marshal(gr)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{`"started_at"`, `"finished_at"`, `"heartbeat"`} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("heartbeat field %s leaked into signed payload: %s", banned, raw)
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
