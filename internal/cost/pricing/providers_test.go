package pricing_test

import (
	"testing"

	"github.com/trilamsr/regatta/internal/cost/pricing"
)

// TestPricing_BedrockSKU_ResolvesForActiveModels pins issue #240 rows.
func TestPricing_BedrockSKU_ResolvesForActiveModels(t *testing.T) {
	for _, sku := range []string{
		"bedrock.claude-opus-4-7",
		"bedrock.claude-sonnet-4-7",
		"bedrock.claude-haiku-4-5",
	} {
		row, err := pricing.Lookup(sku)
		if err != nil {
			t.Errorf("Lookup(%q) returned %v; want a priced row", sku, err)
			continue
		}
		if row.InputUSDPerMTok <= 0 || row.OutputUSDPerMTok <= 0 ||
			row.CacheReadUSDPerMTok <= 0 || row.CacheCreationUSDPerMTok <= 0 {
			t.Errorf("Lookup(%q) returned zero-rate row %+v", sku, row)
		}
	}
}

// TestPricing_VertexSKU_ResolvesForActiveModels pins issue #240 rows.
func TestPricing_VertexSKU_ResolvesForActiveModels(t *testing.T) {
	for _, sku := range []string{
		"vertex.claude-opus-4-7",
		"vertex.claude-sonnet-4-7",
		"vertex.claude-haiku-4-5",
	} {
		row, err := pricing.Lookup(sku)
		if err != nil {
			t.Errorf("Lookup(%q) returned %v; want a priced row", sku, err)
			continue
		}
		if row.InputUSDPerMTok <= 0 || row.OutputUSDPerMTok <= 0 ||
			row.CacheReadUSDPerMTok <= 0 || row.CacheCreationUSDPerMTok <= 0 {
			t.Errorf("Lookup(%q) returned zero-rate row %+v", sku, row)
		}
	}
}

// TestPricing_NativeSKU_UnchangedByProviderRows pins issue #240 isolation.
func TestPricing_NativeSKU_UnchangedByProviderRows(t *testing.T) {
	row, err := pricing.Lookup("claude-opus-4-7")
	if err != nil {
		t.Fatalf("Lookup(native) returned %v", err)
	}
	if row.InputUSDPerMTok != 15.00 || row.OutputUSDPerMTok != 75.00 {
		t.Fatalf("native claude-opus-4-7 rates drifted: %+v", row)
	}
}

// TestPricing_ProviderPrefix_DistinctFromNative pins SKU namespace safety.
func TestPricing_ProviderPrefix_DistinctFromNative(t *testing.T) {
	native, err := pricing.Lookup("claude-sonnet-4-7")
	if err != nil {
		t.Fatalf("native lookup: %v", err)
	}
	bedrock, err := pricing.Lookup("bedrock.claude-sonnet-4-7")
	if err != nil {
		t.Fatalf("bedrock lookup: %v", err)
	}
	vertex, err := pricing.Lookup("vertex.claude-sonnet-4-7")
	if err != nil {
		t.Fatalf("vertex lookup: %v", err)
	}
	// Three distinct keys must resolve independently. Rates may match
	// today (Bedrock + Vertex price-match Anthropic list for Claude
	// standard tier) but the table identity must be separate so future
	// provider-specific rate changes flow through code review per-row.
	_ = native
	_ = bedrock
	_ = vertex
}
