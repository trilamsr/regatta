// Package estimate prices a pending LLM call before it spawns.
//
// Strategy: upper-bound (deterministic, conservative, cold-start-friendly).
// Predicted-mean undercounts in the worst case → spawning continues past the
// actual cap → the Waxell-$47K-trap. Upper-bound never undercounts; worst case
// is "soft-cap fires pessimistically" — acceptable user-facing failure mode.
//
// Determinism is load-bearing for W9 replay: pure function of
// (input_tokens, max_tokens, price_in, price_out). No map iteration over
// non-keyed data, no time.Now, no mutable global state.
package estimate

import (
	"context"

	"github.com/trilamsr/regatta/internal/cost/pricing"
)

// Estimator is the seam Gate uses to price a pending call. Kept as an
// interface so T1 can mock without pulling the real pricing table; the
// production type is the concrete UpperBound.
type Estimator interface {
	Estimate(ctx context.Context, model string, inputTokens, maxTokens int64, hint Hint) (float64, error)
}

// Hint lets the planner override the (input, max) values the spawner would
// otherwise probe. A non-zero field overrides; the zero value means "use the
// caller-supplied parameter as-is".
type Hint struct {
	InputTokens int64
	MaxTokens   int64
}

// UpperBound is the deterministic, conservative concrete Estimator.
// Formula: est_usd = (input × price_in + max × price_out) / 1e6.
type UpperBound struct{}

// Estimate prices the upper bound of one call. Returns ErrPricingMissing for
// unknown SKUs (Portkey-trap defense at the estimator seam).
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
	// Division by 1e6 last would not change the math but keeps every term
	// in tokens-times-rate space, where the magnitudes are small enough
	// (rate < 100, tokens < 10^7) that float64 mantissa precision holds.
	return (float64(inputTokens)*row.InputUSDPerMTok + float64(maxTokens)*row.OutputUSDPerMTok) / 1e6, nil
}
