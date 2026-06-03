package spend

import (
	"math"
)

// USDMicro is money expressed as integer micro-USD: 1 USD = 1_000_000
// micro-USD. Substrate payloads sign + sum money in this unit so a
// chain of 10^N small charges aggregates to the same byte sequence
// across replays — float64 SUM drifts at the ULP, and that drift
// crosses the cap boundary at high event volume (issue #554).
//
// Range: int64 holds ±9.22 × 10^18 micro-USD = ±9.22 × 10^12 USD.
// JSON marshal stays byte-stable up to 2^53 micro (~$9 billion) which
// is the integer-exact range of a round-tripped float64; beyond that
// the canonical-JSON unmarshal-then-marshal in
// contracts/schemas.CanonicalJSON would normalize the digit shape
// (the encoding/json scanner stores all JSON numbers as float64).
// Every per-event amount Regatta records sits well below that bound.
type USDMicro int64

// scale is the exact USD→micro-USD multiplier. Kept as a const so the
// boundary conversion is single-source-of-truth across writer,
// reader, gate, and reconciler.
const scale = 1_000_000

// FromUSD converts a float USD amount into integer micro-USD using
// half-away-from-zero (math.Round). This is the ONLY place float math touches
// the money path; downstream cap comparisons + sums are pure int64. Round
// (not Floor / Trunc) preserves the operator-intuitive "0.005 → 1 cent"
// behaviour an invoice reviewer expects (over-recording by 1 micro is the
// safe-rounding direction for cap-defending budget enforcement).
func FromUSD(usd float64) USDMicro {
	// NaN/Inf collapse to 0 — a non-finite spend is a writer-side bug,
	// not a recordable amount. Returning 0 keeps the substrate sum
	// integer-stable; the open-span error contract surfaces the
	// upstream cause.
	if math.IsNaN(usd) || math.IsInf(usd, 0) {
		return 0
	}
	return USDMicro(math.Round(usd * scale))
}

// USD returns the float USD value for display / OTel-attr / dashboard
// emission. Boundary conversion at the rendering edge; not used in
// any cap comparison or accumulating sum.
func (m USDMicro) USD() float64 {
	return float64(m) / scale
}

// Add is the in-domain accumulator: int64 sum, no rounding. Returned
// type stays USDMicro so callers cannot accidentally drop precision
// by stuffing the result back into a float64.
func (m USDMicro) Add(other USDMicro) USDMicro {
	return m + other
}
