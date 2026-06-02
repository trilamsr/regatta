package pricing_test

import (
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/cost/pricing"
)

// TestPricing_AllActiveSKUsHavePositiveRows pins B7 Portkey-trap defense.
func TestPricing_AllActiveSKUsHavePositiveRows(t *testing.T) {
	catalog := pricing.Catalog()
	if len(catalog) == 0 {
		t.Fatal("pricing.Catalog() is empty")
	}
	// Each per-provider source-of-truth table MUST be non-empty so a
	// silent table-deletion regression cannot pass review by hiding
	// behind another provider's coverage.
	if len(pricing.Anthropic) == 0 {
		t.Error("pricing.Anthropic table is empty")
	}
	if len(pricing.Bedrock) == 0 {
		t.Error("pricing.Bedrock table is empty")
	}
	if len(pricing.Vertex) == 0 {
		t.Error("pricing.Vertex table is empty")
	}
	for model, row := range catalog {
		if !row.RetiredAfter.IsZero() {
			continue
		}
		if row.InputUSDPerMTok <= 0 {
			t.Errorf("active SKU %q has non-positive InputUSDPerMTok=%v", model, row.InputUSDPerMTok)
		}
		if row.CacheReadUSDPerMTok <= 0 {
			t.Errorf("active SKU %q has non-positive CacheReadUSDPerMTok=%v", model, row.CacheReadUSDPerMTok)
		}
		if row.CacheCreationUSDPerMTok <= 0 {
			t.Errorf("active SKU %q has non-positive CacheCreationUSDPerMTok=%v", model, row.CacheCreationUSDPerMTok)
		}
		if row.OutputUSDPerMTok <= 0 {
			t.Errorf("active SKU %q has non-positive OutputUSDPerMTok=%v", model, row.OutputUSDPerMTok)
		}
	}
}

// TestPricing_RetiredSKURejected_IfStrictMode pins R1 pricing-drift defense.
func TestPricing_RetiredSKURejected_IfStrictMode(t *testing.T) {
	// Inject a retired-in-the-past SKU; Lookup must reject it, not return a zero row.
	const sku = "test-retired-sku"
	pricing.Anthropic[sku] = pricing.Row{
		InputUSDPerMTok:         1.0,
		CacheReadUSDPerMTok:     0.1,
		CacheCreationUSDPerMTok: 1.25,
		OutputUSDPerMTok:        5.0,
		RetiredAfter:            time.Now().Add(-24 * time.Hour),
	}
	t.Cleanup(func() { delete(pricing.Anthropic, sku) })

	row, err := pricing.Lookup(sku)
	if err == nil {
		t.Fatalf("Lookup(%q) for retired SKU returned no error; got row=%+v", sku, row)
	}
	if !errorsIs(err, pricing.ErrPricingMissing) {
		t.Fatalf("Lookup(%q) for retired SKU returned %v; want ErrPricingMissing", sku, err)
	}
}
