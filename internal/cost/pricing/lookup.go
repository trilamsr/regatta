// Package pricing holds the hardcoded Anthropic per-model USD rate table
// and the Lookup function that callers use to price a token-count.
//
// Pricing is critical-path data that MUST be hermetic (no boot-time network
// call) and MUST be reviewable in the diff (every change is a code-review
// event). The override-file surface is intentionally deferred per spec §10 S2
// — refresh-via-code-change matches Helicone/Portkey/LiteLLM v1.
//
// Lookup hard-fails on unknown SKUs: silent-zero is the Portkey trap called
// out in spec §3.1. Retired SKUs (RetiredAfter non-zero AND in the past) are
// also rejected so a stale-SKU caller cannot drift past pricing changes.
package pricing

import (
	"errors"
	"time"
)

// ErrPricingMissing is returned by Lookup when the model SKU is unknown or
// retired. Callers MUST hard-fail; silent-zero is the Portkey trap.
var ErrPricingMissing = errors.New("pricing missing for model")

// Row holds per-million-token USD rates for one model SKU plus a sunset
// marker. The zero value of RetiredAfter means the SKU is still active.
type Row struct {
	InputUSDPerMTok         float64
	CacheReadUSDPerMTok     float64
	CacheCreationUSDPerMTok float64
	OutputUSDPerMTok        float64
	RetiredAfter            time.Time
}

// now is a seam so tests can pin "current time" for retired-SKU coverage
// without leaking time.Now into the pure-function callers.
var now = time.Now

// Lookup returns the priced row for a model SKU. ErrPricingMissing when the
// SKU is unknown OR retired (RetiredAfter non-zero and in the past).
func Lookup(model string) (Row, error) {
	row, ok := Anthropic[model]
	if !ok {
		return Row{}, ErrPricingMissing
	}
	if !row.RetiredAfter.IsZero() && now().After(row.RetiredAfter) {
		return Row{}, ErrPricingMissing
	}
	return row, nil
}
