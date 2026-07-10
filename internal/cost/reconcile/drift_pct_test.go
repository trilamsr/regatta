package reconcile

import (
	"math"
	"testing"
)

// TestComputeDriftPct covers actual>0, actual==0 && recorded>0, and both-zero (R5 helper #1432).
func TestComputeDriftPct(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		actualUSD  float64
		recorded   float64
		wantMin    float64
		wantMax    float64
		wantExact  bool
		wantExactV float64
	}{
		{
			name:      "actual_gt_zero_small_delta",
			actualUSD: 10.0,
			recorded:  9.5,
			wantMin:   0.049,
			wantMax:   0.051,
		},
		{
			name:      "actual_gt_zero_denominator_floor",
			actualUSD: 0.001,
			recorded:  0.5,
			wantMin:   49.0,
			wantMax:   51.0,
		},
		{
			name:       "actual_zero_recorded_positive_clamps_to_one",
			actualUSD:  0,
			recorded:   0.42,
			wantExact:  true,
			wantExactV: 1.0,
		},
		{
			name:       "actual_negative_recorded_positive_clamps_to_one",
			actualUSD:  -0.01,
			recorded:   0.42,
			wantExact:  true,
			wantExactV: 1.0,
		},
		{
			name:       "both_zero",
			actualUSD:  0,
			recorded:   0,
			wantExact:  true,
			wantExactV: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeDriftPct(tc.actualUSD, tc.recorded)
			if tc.wantExact {
				if math.Abs(got-tc.wantExactV) > 1e-9 {
					t.Fatalf("computeDriftPct(%v, %v) = %v, want %v",
						tc.actualUSD, tc.recorded, got, tc.wantExactV)
				}
				return
			}
			if got < tc.wantMin || got > tc.wantMax {
				t.Fatalf("computeDriftPct(%v, %v) = %v, want in [%v, %v]",
					tc.actualUSD, tc.recorded, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}
