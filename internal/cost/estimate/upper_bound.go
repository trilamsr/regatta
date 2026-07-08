// Package estimate prices a pending LLM call before it spawns using a
// deterministic upper-bound; predicted-mean would undercount and let
// spawning pass the cap (the Waxell-$47K trap). Determinism is load-bearing
// for W9 replay: pure function of (input_tokens, max_tokens, price_in,
// price_out) — no map iteration, no time.Now, no mutable state.
package estimate

import (
	"context"

	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/cost/pricing"
)

// UpperBound is the conservative-cap pricing estimator picked pre-call to fail-fast on runaway-spend risk: est_usd = (input·price_in + max·price_out) / 1e6.
type UpperBound struct{}

// Estimate returns ErrPricingMissing for unknown SKUs (Portkey-trap defense at the estimator seam).
func (UpperBound) Estimate(_ context.Context, hint gate.EstHint, model string) (float64, error) {
	row, err := pricing.Lookup(model)
	if err != nil {
		return 0, err
	}
	// Divide by 1e6 last so all terms stay in token×rate space (rate<100, tokens<10^7) within float64 mantissa.
	return (float64(hint.InputTokens)*row.InputUSDPerMTok + float64(hint.MaxTokens)*row.OutputUSDPerMTok) / 1e6, nil
}
