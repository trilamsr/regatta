package pricing_test

import (
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/cost/pricing"
	"github.com/trilamsr/regatta/internal/cost/pricing/testdata"
)

// TestPricing_BootRejectsKnownBadTable pins §3.8 + §7 B7 + #290 fixture.
func TestPricing_BootRejectsKnownBadTable(t *testing.T) {
	err := pricing.Validate("anthropic", testdata.BadAnthropicZeroRow)
	if err == nil {
		t.Fatal("Validate(BadAnthropicZeroRow) returned nil; want ErrPricingZeroRow")
	}
	if !errors.Is(err, pricing.ErrPricingZeroRow) {
		t.Fatalf("Validate(BadAnthropicZeroRow) returned %v; want wrapped ErrPricingZeroRow", err)
	}
}

// TestPricing_BootAcceptsRealTables pins the negative case for #290.
func TestPricing_BootAcceptsRealTables(t *testing.T) {
	for _, tc := range []struct {
		provider string
		table    map[string]pricing.Row
	}{
		{"anthropic", pricing.Anthropic},
		{"bedrock", pricing.Bedrock},
		{"vertex", pricing.Vertex},
	} {
		if err := pricing.Validate(tc.provider, tc.table); err != nil {
			t.Errorf("Validate(%s) returned %v; in-tree tables MUST validate clean", tc.provider, err)
		}
	}
}

// TestPricing_ValidateRejectsNilTable pins #445: a truncated table MUST fail boot.
func TestPricing_ValidateRejectsNilTable(t *testing.T) {
	err := pricing.Validate("anthropic", nil)
	if err == nil {
		t.Fatal("Validate(nil) returned nil; want ErrPricingZeroRow (empty-table guard)")
	}
	if !errors.Is(err, pricing.ErrPricingZeroRow) {
		t.Fatalf("Validate(nil) returned %v; want wrapped ErrPricingZeroRow", err)
	}
}

// TestPricing_ValidateRejectsEmptyTable pins #445: an empty map MUST fail boot.
func TestPricing_ValidateRejectsEmptyTable(t *testing.T) {
	err := pricing.Validate("anthropic", map[string]pricing.Row{})
	if err == nil {
		t.Fatal("Validate(empty) returned nil; want ErrPricingZeroRow (empty-table guard)")
	}
	if !errors.Is(err, pricing.ErrPricingZeroRow) {
		t.Fatalf("Validate(empty) returned %v; want wrapped ErrPricingZeroRow", err)
	}
}
