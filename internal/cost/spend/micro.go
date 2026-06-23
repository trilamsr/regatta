package spend

import (
	"math"
)

// USDMicro is money expressed as integer micro-USD (1 USD = 1_000_000). Substrate payloads sign + sum in this unit so a chain of 10^N small charges aggregates to the same byte sequence across replays — float64 SUM drifts at the ULP and crosses the cap boundary at high event volume (#554).
type USDMicro int64

const scale = 1_000_000

// FromUSD converts float USD into integer micro-USD using half-away-from-zero — sole place float math touches the money path; over-recording by 1 micro is the safe-rounding direction for cap-defending budget enforcement. Negative inputs clamp to 0 (R-MEGA-2 C4): a refund-shaped charge would shrink the accumulated budget and let a subsequent spawn slip past the cap.
func FromUSD(usd float64) USDMicro {
	if math.IsNaN(usd) || math.IsInf(usd, 0) {
		return 0
	}
	if usd < 0 {
		return 0
	}
	return USDMicro(math.Round(usd * scale))
}

// USD renders the float USD value at the display / OTel-attr edge; never used in cap comparison or accumulating sum.
func (m USDMicro) USD() float64 {
	return float64(m) / scale
}

// Add accumulates in-domain (int64, no rounding); returns USDMicro so callers cannot accidentally drop precision through float64.
func (m USDMicro) Add(other USDMicro) USDMicro {
	return m + other
}
