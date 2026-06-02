// Package testdata carries known-bad pricing-table fixtures the boot
// validator MUST reject. The cost-governor runbook "Pricing-table
// rollback" cites TestPricing_BootRejectsKnownBadTable as the falsifier
// — without this fixture, the rollback procedure has no test that proves
// the validator works.
//
// This package lives under testdata/ so `go build ./internal/cost/...`
// skips it during ordinary directory walks; the pricing test binary
// imports it explicitly, which the Go toolchain permits.
package testdata

import (
	"time"

	"github.com/trilamsr/regatta/internal/cost/pricing"
)

// BadAnthropicZeroRow is an Anthropic-shaped pricing table whose
// `claude-opus-4-7` row carries `InputUSDPerMTok: 0` while still being
// active (RetiredAfter is the zero Time). Loading this into a real
// pricing.Anthropic at boot would silently zero out every opus call's
// recorded spend — the Portkey trap spec §3.1 calls out.
//
// pricing.Validate("anthropic", BadAnthropicZeroRow) MUST return an
// ErrPricingZeroRow-wrapped error. Every sibling row carries valid
// rates so the failure isolates to the named SKU + field.
var BadAnthropicZeroRow = map[string]pricing.Row{
	"claude-opus-4-7": {
		InputUSDPerMTok:         0, // <-- the bad row: zero rate on an active SKU.
		CacheReadUSDPerMTok:     1.50,
		CacheCreationUSDPerMTok: 18.75,
		OutputUSDPerMTok:        75.00,
		RetiredAfter:            time.Time{},
	},
	"claude-sonnet-4-7": {
		InputUSDPerMTok:         3.00,
		CacheReadUSDPerMTok:     0.30,
		CacheCreationUSDPerMTok: 3.75,
		OutputUSDPerMTok:        15.00,
		RetiredAfter:            time.Time{},
	},
}
