package spend

import "testing"

// TestFromUSD_NegativeClamped asserts negative USD inputs clamp to 0 (R-MEGA-2 C4).
func TestFromUSD_NegativeClamped(t *testing.T) {
	cases := []float64{-0.00001, -1, -1234.56, -1e9}
	for _, c := range cases {
		if got := FromUSD(c); got != 0 {
			t.Errorf("FromUSD(%v)=%d; want 0", c, got)
		}
	}
}

// TestTokensToMicro_NegativeClamped asserts negative token counts clamp to 0 (R-MEGA-2 C4).
func TestTokensToMicro_NegativeClamped(t *testing.T) {
	cases := []int64{-1, -1000, -1 << 30}
	for _, c := range cases {
		if got := tokensToMicro(c, 3.0); got != 0 {
			t.Errorf("tokensToMicro(%d, 3.0)=%d; want 0", c, got)
		}
	}
}
