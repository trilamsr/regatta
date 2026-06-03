package spend_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/trilamsr/regatta/internal/cost/spend"
)

// TestMicroUSD_FromUSD_RoundsHalfAwayFromZero asserts math.Round semantics at the USD→micro conversion boundary.
func TestMicroUSD_FromUSD_RoundsHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		usd  float64
		want spend.USDMicro
	}{
		{0, 0},
		{1.0, 1_000_000},
		{0.000001, 1},     // 1 micro-USD
		{0.0000005, 1},    // half rounds up
		{0.0000004, 0},    // below-half rounds down
		{-0.0000005, -1},  // half away from zero (negative half rounds DOWN)
		{99.99, 99_990_000},
		{1234.567891, 1_234_567_891},
		{math.NaN(), 0},               // non-finite collapses to 0
		{math.Inf(1), 0},
		{math.Inf(-1), 0},
	}
	for _, c := range cases {
		got := spend.FromUSD(c.usd)
		if got != c.want {
			t.Errorf("FromUSD(%v) = %d; want %d", c.usd, got, c.want)
		}
	}
}

// TestMicroUSD_USD_RoundTripsAtMillionthPrecision asserts micro→USD display preserves millionth precision for operators.
func TestMicroUSD_USD_RoundTripsAtMillionthPrecision(t *testing.T) {
	cases := []struct {
		micro spend.USDMicro
		want  float64
	}{
		{0, 0},
		{1, 0.000001},
		{1_000_000, 1.0},
		{1_234_567_891, 1234.567891},
		{-500_000, -0.5},
	}
	for _, c := range cases {
		got := c.micro.USD()
		if got != c.want {
			t.Errorf("USDMicro(%d).USD() = %v; want %v", c.micro, got, c.want)
		}
	}
}

// TestMicroUSD_CanonicalJSON_ByteStableAcrossRoundTrip asserts integer micro JSON round-trips byte-stable for HMAC signing.
func TestMicroUSD_CanonicalJSON_ByteStableAcrossRoundTrip(t *testing.T) {
	// Round-trip every magnitude up to ~$9B per single event (the
	// float64 mantissa-exact bound of 2^53 micro). Production rows
	// never approach this; the test pins the band that holds.
	microValues := []spend.USDMicro{
		1,
		999_999,
		1_000_000,
		1_234_567_891,
		1_000_000_000,         // $1,000
		9_000_000_000_000,     // $9M
		9_007_199_254_740_992, // 2^53 — last exact float repr
	}
	for _, m := range microValues {
		in := map[string]any{"usd_micro": int64(m)}
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// Round-trip through the CanonicalJSON shape (unmarshal-then-
		// marshal via any) — the path substrate.signedPayload exercises.
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		raw2, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("re-marshal: %v", err)
		}
		if string(raw) != string(raw2) {
			t.Errorf("byte drift on round-trip for micro=%d: in=%s out=%s", m, raw, raw2)
		}
	}
}

// TestMicroUSD_IntegerSum_BeatsFloatRoundingAtBoundary asserts integer-micro sum hits cap exactly where float ULP drift undershoots.
func TestMicroUSD_IntegerSum_BeatsFloatRoundingAtBoundary(t *testing.T) {
	const perEventUSD = 0.10
	const events = 11

	// Float-space accumulation — the path SQLite SUM(json_extract($.usd)) walks.
	var floatSum float64
	for i := 0; i < events; i++ {
		floatSum += perEventUSD
	}
	if floatSum == 1.10 {
		t.Fatalf("test setup defunct: 0.1×11 = 1.10 exactly in this Go runtime; pick a different boundary")
	}

	// Integer-micro accumulation — the path the new schema walks.
	perMicro := spend.FromUSD(perEventUSD) // 100_000
	if perMicro != 100_000 {
		t.Fatalf("FromUSD(0.10) = %d; want 100_000", perMicro)
	}
	intSum := spend.USDMicro(events) * perMicro // 1_100_000

	// The operator-typed cap.
	const capUSD = 1.10
	capMicro := spend.FromUSD(capUSD) // 1_100_000

	// Float comparator (existing buggy path):
	floatPasses := floatSum <= capUSD // true — bug surface
	// Integer comparator (post-fix path):
	intPasses := intSum <= capMicro // true at equality boundary too

	// The load-bearing assertion: floatSum != cap exactly, integer
	// sum DOES equal cap exactly. The cap-enforcement gate switches
	// from "approximately under" to "exactly equal" — eliminating the
	// ULP-drift slop that lets one extra spend slip past.
	if floatSum == capUSD {
		t.Errorf("floatSum == capUSD unexpectedly; ULP drift fixture broke")
	}
	if int64(intSum) != int64(capMicro) {
		t.Errorf("intSum=%d != capMicro=%d; integer math must be exact at boundary", intSum, capMicro)
	}
	if !floatPasses {
		t.Errorf("float path unexpectedly denied; the bug surface is supposed to pass")
	}
	if !intPasses {
		t.Errorf("integer path denied at equality boundary; expected pass (cap <= rule)")
	}
}
