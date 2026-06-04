// Package estimate prices a pending LLM call before it spawns.
// Strategy: upper-bound (deterministic, conservative). Predicted-mean
// undercounts → spawning continues past the cap → the Waxell-$47K-trap.
// Upper-bound never undercounts; worst case is "soft-cap fires
// pessimistically" — acceptable failure mode.
//
// Determinism is load-bearing for W9 replay: pure function of
// (input_tokens, max_tokens, price_in, price_out) — no map iteration
// over non-keyed data, no time.Now, no mutable global state.
package estimate

import (
	"context"

	"github.com/trilamsr/regatta/internal/cost/pricing"
)

// Hint lets the planner override (input, max). A non-zero field
// overrides; zero value means "use the caller-supplied parameter".
type Hint struct {
	InputTokens int64
	MaxTokens   int64
}

// UpperBound is the conservative-cap pricing estimator picked pre-call to fail-fast on runaway-spend risk: est_usd = (input·price_in + max·price_out) / 1e6.
type UpperBound struct{}

// Estimate returns ErrPricingMissing for unknown SKUs (Portkey-trap
// defense at the estimator seam).
func (UpperBound) Estimate(_ context.Context, model string, inputTokens, maxTokens int64, hint Hint) (float64, error) {
	row, err := pricing.Lookup(model)
	if err != nil {
		return 0, err
	}
	if hint.InputTokens > 0 {
		inputTokens = hint.InputTokens
	}
	if hint.MaxTokens > 0 {
		maxTokens = hint.MaxTokens
	}
	// Division by 1e6 last keeps every term in tokens-times-rate space,
	// where magnitudes (rate < 100, tokens < 10^7) fit float64 mantissa.
	return (float64(inputTokens)*row.InputUSDPerMTok + float64(maxTokens)*row.OutputUSDPerMTok) / 1e6, nil
}
