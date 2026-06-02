package l4

import (
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// Blocks parses `critical` + `2*high` per spec §3.6 R1/R2.
func TestL4_SeverityBlockRouting(t *testing.T) {
	rules := []string{RuleCritical, RuleTwoHigh}
	cases := []struct {
		name     string
		findings []schemas.Finding
		want     bool
	}{
		{"no-findings-passes", nil, false},
		{"one-high-advisory", []schemas.Finding{{Severity: schemas.FindingHigh}}, false},
		{"two-high-blocks", []schemas.Finding{{Severity: schemas.FindingHigh}, {Severity: schemas.FindingHigh}}, true},
		{"one-critical-blocks", []schemas.Finding{{Severity: schemas.FindingCritical}}, true},
		{"medium-only-passes", []schemas.Finding{{Severity: schemas.FindingMedium}, {Severity: schemas.FindingMedium}}, false},
		{"critical-plus-high-blocks", []schemas.Finding{{Severity: schemas.FindingCritical}, {Severity: schemas.FindingHigh}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Blocks(rules, c.findings)
			if got != c.want {
				t.Fatalf("Blocks(%v, %d findings): got %v, want %v", rules, len(c.findings), got, c.want)
			}
		})
	}
}

// Malformed mini-DSL tokens fail loud rather than silently passing.
func TestL4_SeverityBlockMalformedRule(t *testing.T) {
	if _, err := parseRules([]string{"not-a-rule"}); err == nil {
		t.Fatalf("expected error on unknown severity token, got nil")
	}
	if _, err := parseRules([]string{"0*high"}); err == nil {
		t.Fatalf("expected error on zero-count NxSEV, got nil")
	}
}

// ValidateRules is the load-time gate so misconfig fails at startup.
func TestL4_ValidateRules(t *testing.T) {
	if err := ValidateRules([]string{RuleCritical, RuleTwoHigh}); err != nil {
		t.Fatalf("default rules must validate: %v", err)
	}
	if err := ValidateRules([]string{"garbage"}); err == nil {
		t.Fatalf("expected validation error on garbage rule")
	}
}
