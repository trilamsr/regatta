package config

import (
	"errors"
	"strings"
	"testing"
	"time"

	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/gates/approval"
)

// minimalConfigWithGate returns a regatta.yaml string that validates
// against the v1 CUE schema, parameterised by the gates: block. Tests
// vary the block to exercise the approval_gate parser entry without
// re-paying the per-test boilerplate.
func minimalConfigWithGate(gateYAML string) string {
	return `
version: 1
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
` + gateYAML + `
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`
}

// canonicalApprovalGateYAML is the spec §5.3 canonical YAML shape. The
// test that pins this loads cleanly through CUE + LoadApprovalGates is
// the load-bearing happy path; later tests assume the same template
// with a single field mutated.
const canonicalApprovalGateYAML = `  - id: prod-deploy-approval
    type: approval_gate
    name: prod-deploy-approval
    risk_class: high
    reviewers: [alice, bob]
    roles: [sre]
    quorum: 2
    prevent_self_review: true
    timeout: 24h
    decision_window: 4h
    on_timeout: fail
    escalation_chain:
      - reviewers: [carol]
        quorum: 1
        timeout: 1h
        decision_window: 30m
`

func TestLoadApprovalGates_HappyPath(t *testing.T) {
	yaml := minimalConfigWithGate(canonicalApprovalGateYAML)
	// CUE must accept it.
	if err := validateconfig.LoadBytes([]byte(yaml)); err != nil {
		t.Fatalf("CUE rejected canonical approval_gate: %v", err)
	}
	// Go loader must accept it and surface a typed Config.
	gates, err := LoadApprovalGates([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadApprovalGates: %v", err)
	}
	if len(gates) != 1 {
		t.Fatalf("want 1 approval_gate; got %d", len(gates))
	}
	g := gates[0]
	if g.Name != "prod-deploy-approval" {
		t.Errorf("Name: want prod-deploy-approval, got %q", g.Name)
	}
	if g.RiskClass != approval.RiskHigh {
		t.Errorf("RiskClass: want high, got %q", g.RiskClass)
	}
	if g.Quorum != 2 {
		t.Errorf("Quorum: want 2, got %d", g.Quorum)
	}
	if !g.PreventSelfReview {
		t.Error("PreventSelfReview: want true")
	}
	if g.Timeout != 24*time.Hour {
		t.Errorf("Timeout: want 24h, got %v", g.Timeout)
	}
	if g.DecisionWindow != 4*time.Hour {
		t.Errorf("DecisionWindow: want 4h, got %v", g.DecisionWindow)
	}
	if g.OnTimeout != approval.OnTimeoutFail {
		t.Errorf("OnTimeout: want fail, got %q", g.OnTimeout)
	}
	if len(g.Reviewers) != 2 || g.Reviewers[0] != "alice" || g.Reviewers[1] != "bob" {
		t.Errorf("Reviewers: want [alice bob], got %v", g.Reviewers)
	}
	if len(g.Roles) != 1 || g.Roles[0] != "sre" {
		t.Errorf("Roles: want [sre], got %v", g.Roles)
	}
	if len(g.EscalationChain) != 1 {
		t.Fatalf("EscalationChain: want 1 tier, got %d", len(g.EscalationChain))
	}
	tier := g.EscalationChain[0]
	if tier.Timeout != 1*time.Hour {
		t.Errorf("tier.Timeout: want 1h, got %v", tier.Timeout)
	}
	if tier.DecisionWindow != 30*time.Minute {
		t.Errorf("tier.DecisionWindow: want 30m, got %v", tier.DecisionWindow)
	}
}

// TestLoadApprovalGates_IgnoresNonApprovalGates filters non-approval gate rows.
func TestLoadApprovalGates_IgnoresNonApprovalGates(t *testing.T) {
	yaml := minimalConfigWithGate(`  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
  - id: license_check
    type: deterministic
    command: ./scripts/license.sh
` + canonicalApprovalGateYAML)
	gates, err := LoadApprovalGates([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadApprovalGates: %v", err)
	}
	if len(gates) != 1 {
		t.Fatalf("want 1 approval_gate (filtered); got %d", len(gates))
	}
	if gates[0].Name != "prod-deploy-approval" {
		t.Errorf("wrong gate returned; got Name=%q", gates[0].Name)
	}
}

// TestLoadApprovalGates_NoApprovalGates returns (nil, nil) when no approval gates exist.
func TestLoadApprovalGates_NoApprovalGates(t *testing.T) {
	yaml := minimalConfigWithGate(`  - id: spec_conformance
    type: ai
    model: claude-opus-4-7
    severity_block: [fail]
`)
	gates, err := LoadApprovalGates([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadApprovalGates: %v", err)
	}
	if len(gates) != 0 {
		t.Errorf("want 0 approval_gates; got %d", len(gates))
	}
}

// invariantCase pairs a mutation against the sentinel the loader must
// surface AND the substring that must appear in the CUE rejection. A
// single fixture exercises BOTH validation surfaces, enforcing the
// "CUE and Go disagree" parity invariant per spec §5.5.
type invariantCase struct {
	name        string
	yaml        string // canonical yaml with one mutation
	wantSentinel error
	// cueRejects: true if this fixture is also rejectable at the CUE
	// layer (most are; V5/V7/V3/V4 require Go because CUE can't
	// cross-reference fields). When true, the parity test asserts CUE
	// errors as well.
	cueRejects bool
	cueSubstring string // substring that must appear in CUE error if cueRejects
}

// mutate replaces the first occurrence of `old` with `new` in the
// canonical yaml; tests fail loud if `old` is absent.
func mutate(t *testing.T, old, newStr string) string {
	t.Helper()
	if !strings.Contains(canonicalApprovalGateYAML, old) {
		t.Fatalf("mutate: %q not found in canonical yaml", old)
	}
	return strings.Replace(canonicalApprovalGateYAML, old, newStr, 1)
}

func TestLoadApprovalGates_Invariants(t *testing.T) {
	cases := []invariantCase{
		{
			name:         "V11_NameBadChars",
			yaml:         mutate(t, "name: prod-deploy-approval", "name: PROD!Deploy"),
			wantSentinel: approval.ErrInvalidGateName,
			cueRejects:   true,
			cueSubstring: "name",
		},
		{
			name:         "V11_NameEmpty",
			yaml:         mutate(t, "name: prod-deploy-approval", `name: ""`),
			wantSentinel: approval.ErrInvalidGateName,
			cueRejects:   true,
		},
		{
			name:         "V1_DecisionWindowZero",
			yaml:         mutate(t, "decision_window: 4h", "decision_window: 0s"),
			wantSentinel: approval.ErrInvalidDecisionWindow,
		},
		{
			// Timeout=0 also trips V3 (window>timeout) via errors.Join;
			// asserting V2 is the primary signal.
			name:         "V2_TimeoutZero",
			yaml:         mutate(t, "timeout: 24h", "timeout: 0s"),
			wantSentinel: approval.ErrInvalidTimeout,
		},
		{
			name:         "V3_WindowExceedsTimeout",
			yaml:         mutate(t, "decision_window: 4h", "decision_window: 48h"),
			wantSentinel: approval.ErrWindowExceedsTimeout,
		},
		{
			name: "V4_ChainTierWindowExceedsTimeout",
			yaml: strings.Replace(canonicalApprovalGateYAML,
				`      - reviewers: [carol]
        quorum: 1
        timeout: 1h
        decision_window: 30m`,
				`      - reviewers: [carol]
        quorum: 1
        timeout: 30m
        decision_window: 1h`, 1),
			wantSentinel: approval.ErrWindowExceedsTimeout,
		},
		{
			// auto_approve + risk_class=high (unchanged from canonical)
			// trips V5. Spec §5.5 calls this the foot-gun; enforced at
			// config-load, not runtime.
			name: "V5_AutoApproveRequiresLowRisk",
			yaml: strings.Replace(canonicalApprovalGateYAML,
				"on_timeout: fail",
				"on_timeout: auto_approve", 1),
			wantSentinel: approval.ErrAutoApproveRequiresLowRisk,
		},
		{
			name:         "V6_InvalidRiskClass",
			yaml:         mutate(t, "risk_class: high", "risk_class: ultra"),
			wantSentinel: approval.ErrInvalidRiskClass,
			cueRejects:   true,
			cueSubstring: "ultra",
		},
		{
			name:         "V7_QuorumZero",
			yaml:         mutate(t, "quorum: 2", "quorum: 0"),
			wantSentinel: approval.ErrQuorumExceedsReviewers,
			cueRejects:   true,
		},
		{
			name:         "V7_QuorumExceedsSet",
			yaml:         mutate(t, "quorum: 2", "quorum: 99"),
			wantSentinel: approval.ErrQuorumExceedsReviewers,
		},
		{
			name:         "V8_InvalidOnTimeout",
			yaml:         mutate(t, "on_timeout: fail", "on_timeout: maybe"),
			wantSentinel: approval.ErrInvalidOnTimeout,
			cueRejects:   true,
			cueSubstring: "maybe",
		},
		{
			name: "V9_EscalateMissingChain",
			yaml: strings.Replace(
				strings.Replace(canonicalApprovalGateYAML,
					"on_timeout: fail",
					"on_timeout: escalate", 1),
				`    escalation_chain:
      - reviewers: [carol]
        quorum: 1
        timeout: 1h
        decision_window: 30m
`, "", 1),
			wantSentinel: approval.ErrEscalateMissingChain,
		},
		{
			name:         "V10_InvalidReviewerID",
			yaml:         mutate(t, "reviewers: [alice, bob]", `reviewers: ["alice!!", "bob spaces"]`),
			wantSentinel: approval.ErrInvalidReviewerID,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			full := minimalConfigWithGate(tc.yaml)

			// LoadApprovalGates runs CUE first, then the Go validator. For
			// CUE-expressible invariants the loader returns
			// ErrSchemaValidation; for cross-field invariants the loader
			// returns the Go sentinel. Either rejection is acceptable.
			// The parity assertion below pins that both layers agree on
			// the fixture being invalid.
			gates, err := LoadApprovalGates([]byte(full))
			if err == nil {
				t.Fatalf("LoadApprovalGates: want error (%v); got nil (gates=%+v)",
					tc.wantSentinel, gates)
			}
			if !errors.Is(err, tc.wantSentinel) && !errors.Is(err, ErrSchemaValidation) {
				t.Fatalf("LoadApprovalGates: want sentinel %v or ErrSchemaValidation; got %v",
					tc.wantSentinel, err)
			}

			// CUE parity. When the invariant is CUE-expressible, the bare
			// CUE validator must also reject (proving the schema catches
			// it before Go runs).
			if tc.cueRejects {
				cueErr := validateconfig.LoadBytes([]byte(full))
				if cueErr == nil {
					t.Fatalf("parity violation: loader rejected but CUE-only validator accepted; "+
						"fixture must be rejectable by both layers")
				}
				if tc.cueSubstring != "" && !strings.Contains(cueErr.Error(), tc.cueSubstring) {
					t.Errorf("CUE error should mention %q; got: %s",
						tc.cueSubstring, cueErr.Error())
				}
			}
		})
	}
}

// TestLoadApprovalGates_AccumulatesAllErrors surfaces V3+V7 together via errors.Join (not fail-on-first).
func TestLoadApprovalGates_AccumulatesAllErrors(t *testing.T) {
	yaml := strings.Replace(canonicalApprovalGateYAML, "quorum: 2", "quorum: 99", 1)
	yaml = strings.Replace(yaml, "decision_window: 4h", "decision_window: 48h", 1)

	_, err := LoadApprovalGates([]byte(minimalConfigWithGate(yaml)))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, approval.ErrQuorumExceedsReviewers) {
		t.Errorf("error chain missing approval.ErrQuorumExceedsReviewers; got %v", err)
	}
	if !errors.Is(err, approval.ErrWindowExceedsTimeout) {
		t.Errorf("error chain missing approval.ErrWindowExceedsTimeout; got %v", err)
	}
}

// TestLoadApprovalGates_CUE_V5_FootGun pins V5 at the CUE layer (operator validate-config path).
func TestLoadApprovalGates_CUE_V5_FootGun(t *testing.T) {
	yaml := strings.Replace(canonicalApprovalGateYAML,
		"on_timeout: fail", "on_timeout: auto_approve", 1)
	full := minimalConfigWithGate(yaml)

	if err := validateconfig.LoadBytes([]byte(full)); err == nil {
		t.Fatal("CUE must reject auto_approve + risk_class=high (V5 foot-gun); got nil")
	}
}

// TestLoadApprovalGates_RoleOnlyReviewerSet pins V7 union semantics for role-only reviewer sets.
func TestLoadApprovalGates_RoleOnlyReviewerSet(t *testing.T) {
	// Quorum stays 2 (canonical). Reviewers drops to empty; roles
	// expands to two entries so the V7 union (|reviewers|+|roles|=2)
	// matches quorum.
	yaml := strings.Replace(canonicalApprovalGateYAML,
		"reviewers: [alice, bob]", "reviewers: []", 1)
	yaml = strings.Replace(yaml, "roles: [sre]", "roles: [sre, security]", 1)
	gates, err := LoadApprovalGates([]byte(minimalConfigWithGate(yaml)))
	if err != nil {
		t.Fatalf("role-only reviewer set must validate; got %v", err)
	}
	if len(gates) != 1 {
		t.Fatalf("want 1 gate; got %d", len(gates))
	}
}

// TestLoadApprovalGates_MalformedDuration rejects unparseable durations via CUE or Go.
func TestLoadApprovalGates_MalformedDuration(t *testing.T) {
	yaml := mutate(t, "timeout: 24h", "timeout: forever")
	_, err := LoadApprovalGates([]byte(minimalConfigWithGate(yaml)))
	if err == nil {
		t.Fatal("expected error for malformed duration")
	}
	if !errors.Is(err, ErrInvalidDuration) && !errors.Is(err, ErrSchemaValidation) {
		t.Errorf("want ErrInvalidDuration or ErrSchemaValidation; got %v", err)
	}
}

// TestLoadApprovalGates_GoDurationParseRejection surfaces int64-overflow durations via ErrInvalidDuration.
func TestLoadApprovalGates_GoDurationParseRejection(t *testing.T) {
	yaml := mutate(t, "timeout: 24h", "timeout: 99999999999999999999h")
	_, err := LoadApprovalGates([]byte(minimalConfigWithGate(yaml)))
	if err == nil {
		t.Fatal("expected error for out-of-range duration")
	}
	if !errors.Is(err, ErrInvalidDuration) {
		t.Errorf("want ErrInvalidDuration; got %v", err)
	}
}
