package estimate_test

import (
	"context"
	"testing"

	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/cost/estimate"
	"github.com/trilamsr/regatta/internal/cost/pricing"
)

// TestEstimate_UpperBound_Deterministic pins replay-safety (W9 forward-fit).
func TestEstimate_UpperBound_Deterministic(t *testing.T) {
	ub := estimate.UpperBound{}
	ctx := context.Background()
	const model = "claude-sonnet-4-7"

	first, err := ub.Estimate(ctx, model, 1000, 4096, estimate.Hint{})
	if err != nil {
		t.Fatalf("Estimate first call: %v", err)
	}
	for i := 0; i < 100; i++ {
		got, err := ub.Estimate(ctx, model, 1000, 4096, estimate.Hint{})
		if err != nil {
			t.Fatalf("Estimate iter %d: %v", i, err)
		}
		if got != first {
			t.Fatalf("non-deterministic: iter %d got %v, first %v", i, got, first)
		}
	}
}

// TestEstimate_UpperBound_NeverUndercountsActual pins the conservative
// invariant: UpperBound ≥ actual cost for every actual_out ∈ [0, max_tokens].
func TestEstimate_UpperBound_NeverUndercountsActual(t *testing.T) {
	ub := estimate.UpperBound{}
	ctx := context.Background()
	const model = "claude-haiku-4-5"
	row, err := pricing.Lookup(model)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", model, err)
	}

	rapid.Check(t, func(rt *rapid.T) {
		input := rapid.Int64Range(0, 1_000_000).Draw(rt, "input")
		maxTok := rapid.Int64Range(1, 200_000).Draw(rt, "max")
		actualOut := rapid.Int64Range(0, maxTok).Draw(rt, "actual_out")

		est, err := ub.Estimate(ctx, model, input, maxTok, estimate.Hint{})
		if err != nil {
			rt.Fatalf("Estimate: %v", err)
		}
		// Mirror the impl's evaluation order so float64 mantissa drift does
		// not synthesize a "1 ULP under" false positive; the invariant is
		// algebraic — input × p_in + max × p_out ≥ input × p_in + actual_out × p_out
		// for all actual_out ∈ [0, max] — and the same-order evaluation pins it
		// to the bit.
		actual := (float64(input)*row.InputUSDPerMTok + float64(actualOut)*row.OutputUSDPerMTok) / 1e6
		if est < actual {
			rt.Fatalf("undercount: est=%v actual=%v input=%d max=%d out=%d", est, actual, input, maxTok, actualOut)
		}
	})
}

// TestEstimate_UpperBound_HintOverridesInputTokens pins planner-supplied Hint
// precedence used by T1's Request.EstHint path.
func TestEstimate_UpperBound_HintOverridesInputTokens(t *testing.T) {
	ub := estimate.UpperBound{}
	ctx := context.Background()
	const model = "claude-sonnet-4-7"

	base, err := ub.Estimate(ctx, model, 1000, 4096, estimate.Hint{})
	if err != nil {
		t.Fatalf("Estimate base: %v", err)
	}
	withHint, err := ub.Estimate(ctx, model, 1000, 4096, estimate.Hint{InputTokens: 50_000, MaxTokens: 8192})
	if err != nil {
		t.Fatalf("Estimate with hint: %v", err)
	}
	if withHint <= base {
		t.Fatalf("Hint did not override: base=%v withHint=%v (expected withHint > base)", base, withHint)
	}

	hintOnlyInput, err := ub.Estimate(ctx, model, 1000, 4096, estimate.Hint{InputTokens: 50_000})
	if err != nil {
		t.Fatalf("Estimate hint-input-only: %v", err)
	}
	hintOnlyMax, err := ub.Estimate(ctx, model, 1000, 4096, estimate.Hint{MaxTokens: 8192})
	if err != nil {
		t.Fatalf("Estimate hint-max-only: %v", err)
	}
	if hintOnlyInput <= base {
		t.Fatalf("Hint.InputTokens did not override: base=%v hint=%v", base, hintOnlyInput)
	}
	if hintOnlyMax <= base {
		t.Fatalf("Hint.MaxTokens did not override: base=%v hint=%v", base, hintOnlyMax)
	}
}

// TestEstimate_UpperBound_UnknownModelErrors covers the Portkey-trap path at
// the estimator seam.
func TestEstimate_UpperBound_UnknownModelErrors(t *testing.T) {
	ub := estimate.UpperBound{}
	if _, err := ub.Estimate(context.Background(), "gpt-4", 100, 100, estimate.Hint{}); err == nil {
		t.Fatal("Estimate(unknown) returned no error")
	}
}
