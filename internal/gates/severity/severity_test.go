package severity_test

import (
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/gates/severity"
)

// Blocks parses `critical` + `2*high` per spec §3.6 R1/R2.
func TestBlocksRouting(t *testing.T) {
	rules := []string{severity.Critical, severity.TwoHigh}
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
			got := severity.Blocks(rules, c.findings)
			if got != c.want {
				t.Fatalf("Blocks(%v, %d findings): got %v, want %v", rules, len(c.findings), got, c.want)
			}
		})
	}
}

// Malformed mini-DSL tokens fail Validate rather than silently passing.
func TestValidateMalformedRule(t *testing.T) {
	if err := severity.Validate([]string{"not-a-rule"}); err == nil {
		t.Fatalf("expected error on unknown severity token, got nil")
	}
	if err := severity.Validate([]string{"0*high"}); err == nil {
		t.Fatalf("expected error on zero-count NxSEV, got nil")
	}
}

// Validate is the load-time gate so misconfig fails at startup.
func TestValidate(t *testing.T) {
	if err := severity.Validate([]string{severity.Critical, severity.TwoHigh}); err != nil {
		t.Fatalf("default rules must validate: %v", err)
	}
	if err := severity.Validate([]string{"garbage"}); err == nil {
		t.Fatalf("expected validation error on garbage rule")
	}
}
