package pricing_test

import (
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/cost/pricing"
)

// errorsIs wraps errors.Is for a one-line call site; lets retired-SKU test in
// anthropic_test.go share a helper without forcing a circular import.
func errorsIs(err, target error) bool { return errors.Is(err, target) }

// TestPricing_Lookup_UnknownModelErrors pins the hard-fail invariant.
func TestPricing_Lookup_UnknownModelErrors(t *testing.T) {
	row, err := pricing.Lookup("gpt-4")
	if err == nil {
		t.Fatalf("Lookup(unknown) returned no error; got %+v", row)
	}
	if !errors.Is(err, pricing.ErrPricingMissing) {
		t.Fatalf("Lookup(unknown) = %v; want ErrPricingMissing", err)
	}
}

// TestPricing_Lookup_KnownModelReturnsRow sanity-checks the happy path so the
// retired-SKU and unknown-SKU branches are not the only coverage.
func TestPricing_Lookup_KnownModelReturnsRow(t *testing.T) {
	row, err := pricing.Lookup("claude-sonnet-4-7")
	if err != nil {
		t.Fatalf("Lookup(known) returned error %v", err)
	}
	if row.InputUSDPerMTok <= 0 || row.OutputUSDPerMTok <= 0 {
		t.Fatalf("Lookup(known) returned zero-rate row %+v", row)
	}
}
