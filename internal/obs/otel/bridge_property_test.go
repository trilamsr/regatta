package otel_test

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
	"pgregory.net/rapid"

	obsotel "github.com/trilamsr/regatta/internal/obs/otel"
	"github.com/trilamsr/regatta/internal/obstest"
)

// Spec §3.2 + upstream otelslog v0.19.0 doc the kind→Value contract:
//
//   slog.Kind  → log.Value
//   Bool       → BoolValue
//   Int64      → Int64Value
//   Uint64     → Int64Value (Float64Value when u > math.MaxInt64)
//   Float64    → Float64Value
//   String     → StringValue
//   Duration   → Int64Value(nanoseconds)
//   Time       → Int64Value(UnixNano)
//
// Deferred (not covered here, called out in issue #176 Acceptance):
//   Group attr, LogValuer, Any with non-primitive payload.
//
// The property test generates random mixes of the seven primitive kinds,
// fans them through the bridge, and asserts (a) the primary leg
// preserves the slog.Value byte-equal and (b) the OTel leg holds the
// kind-translated value per the table. Rapid shrinking surfaces
// boundary cases: NaN, +-Inf, math.Max/MinInt64, math.MaxUint64
// (forced Float64 fallback), time zero, and negative Duration.

// primitiveAttr is a generated slog.Attr plus a witness of the kind so
// the assertion arm knows which AsXxx accessor to call without
// re-deriving the type at runtime.
type primitiveAttr struct {
	attr slog.Attr
	kind slog.Kind
}

// genPrimitiveAttr draws one of the seven primitive slog.Value kinds
// uniformly. Generators include the boundary values otelslog's convert
// path is most likely to mishandle (NaN, +-Inf, math limits).
func genPrimitiveAttr(key string) *rapid.Generator[primitiveAttr] {
	return rapid.Custom(func(t *rapid.T) primitiveAttr {
		kind := rapid.SampledFrom([]slog.Kind{
			slog.KindBool,
			slog.KindInt64,
			slog.KindUint64,
			slog.KindFloat64,
			slog.KindString,
			slog.KindDuration,
			slog.KindTime,
		}).Draw(t, "kind")

		switch kind {
		case slog.KindBool:
			return primitiveAttr{slog.Bool(key, rapid.Bool().Draw(t, "bool")), kind}
		case slog.KindInt64:
			return primitiveAttr{slog.Int64(key, rapid.Int64().Draw(t, "i64")), kind}
		case slog.KindUint64:
			return primitiveAttr{slog.Uint64(key, rapid.Uint64().Draw(t, "u64")), kind}
		case slog.KindFloat64:
			// Rapid's Float64 hits NaN / +-Inf naturally; we do not
			// filter those because the bridge contract is "preserve
			// whatever slog.Float64Value held".
			return primitiveAttr{slog.Float64(key, rapid.Float64().Draw(t, "f64")), kind}
		case slog.KindString:
			return primitiveAttr{slog.String(key, rapid.String().Draw(t, "str")), kind}
		case slog.KindDuration:
			return primitiveAttr{slog.Duration(key, time.Duration(rapid.Int64().Draw(t, "dur"))), kind}
		case slog.KindTime:
			// Bound to ±100y around Unix epoch — UnixNano overflows
			// outside roughly year 1678..2262, and the bridge inherits
			// that overflow from time.Time itself. Bounding here keeps
			// the property focused on the conversion contract, not
			// time.Time's own range limits.
			sec := rapid.Int64Range(-3155760000, 3155760000).Draw(t, "tsec")
			nsec := rapid.Int64Range(0, 999_999_999).Draw(t, "tnsec")
			return primitiveAttr{slog.Time(key, time.Unix(sec, nsec).UTC()), kind}
		}
		panic("unreachable kind in genPrimitiveAttr")
	})
}

// TestBridge_PrimitiveAttrRoundTrip_Property pins issue #176 — every primitive slog.Value kind fans through the bridge to both legs with the documented conversion intact.
func TestBridge_PrimitiveAttrRoundTrip_Property(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 8).Draw(rt, "n_attrs")
		gens := make([]primitiveAttr, n)
		for i := 0; i < n; i++ {
			// Distinct keys per attr — slog.Record.AddAttrs preserves
			// duplicates but the OTel side WalkAttributes returns them
			// in emission order, so unique keys keep findAttr's
			// first-match semantics aligned with the slog leg.
			gens[i] = genPrimitiveAttr(uniqueKey(i)).Draw(rt, "attr")
		}

		primary := obstest.New()
		lp, mem := newTestProvider()
		defer func() { _ = lp.Shutdown(context.Background()) }()

		bridge := obsotel.NewBridgeHandler(primary, "regatta-prop", obsotel.WithLoggerProvider(lp))
		lg := slog.New(bridge)

		lg.LogAttrs(context.Background(), slog.LevelInfo, "round-trip", asSlogAttrs(gens)...)

		// Primary leg: slog.Record carries the same Attrs we emitted.
		primaryRecs := primary.Records()
		if len(primaryRecs) != 1 {
			rt.Fatalf("primary leg: want 1 record, got %d", len(primaryRecs))
		}
		gotPrimary := collectAttrs(primaryRecs[0])
		for _, g := range gens {
			pv, ok := gotPrimary[g.attr.Key]
			if !ok {
				rt.Fatalf("primary leg dropped attr %q (kind=%s)", g.attr.Key, g.kind)
			}
			assertSlogValueEqual(rt, g.attr.Key, g.attr.Value, pv)
		}

		// OTel leg: kind-translated values per the §3.2 table.
		otelRecs := mem.snapshot()
		if len(otelRecs) != 1 {
			rt.Fatalf("OTel leg: want 1 record, got %d", len(otelRecs))
		}
		for _, g := range gens {
			v, ok := findAttr(otelRecs[0], g.attr.Key)
			if !ok {
				rt.Fatalf("OTel leg dropped attr %q (kind=%s)", g.attr.Key, g.kind)
			}
			assertOTelValueMatches(rt, g.attr.Key, g.attr.Value, g.kind, v)
		}
	})
}

func asSlogAttrs(gs []primitiveAttr) []slog.Attr {
	out := make([]slog.Attr, len(gs))
	for i, g := range gs {
		out[i] = g.attr
	}
	return out
}

// uniqueKey returns a stable distinct key per attr index. Stays inside
// the printable ASCII range so a shrunk counterexample is readable.
func uniqueKey(i int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	return string(alphabet[i%len(alphabet)]) + "_" + strconv.Itoa(i)
}

// collectAttrs flattens slog.Record.Attrs to a key→Value map. Property
// generator emits unique keys so the map preserves every value.
func collectAttrs(r slog.Record) map[string]slog.Value {
	out := make(map[string]slog.Value, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value
		return true
	})
	return out
}

// assertSlogValueEqual checks the primary leg preserved the slog.Value
// exactly. NaN is compared by IsNaN because != on NaN is always true.
func assertSlogValueEqual(rt *rapid.T, key string, want, got slog.Value) {
	if want.Kind() != got.Kind() {
		rt.Fatalf("primary attr %q: kind mismatch want=%s got=%s", key, want.Kind(), got.Kind())
	}
	switch want.Kind() {
	case slog.KindFloat64:
		w, g := want.Float64(), got.Float64()
		if math.IsNaN(w) && math.IsNaN(g) {
			return
		}
		if w != g {
			rt.Fatalf("primary attr %q: float64 want=%v got=%v", key, w, g)
		}
	case slog.KindTime:
		if !want.Time().Equal(got.Time()) {
			rt.Fatalf("primary attr %q: time want=%v got=%v", key, want.Time(), got.Time())
		}
	default:
		if want.Equal(got) {
			return
		}
		rt.Fatalf("primary attr %q: value want=%v got=%v", key, want.Any(), got.Any())
	}
}

// assertOTelValueMatches encodes the §3.2 conversion table. Any
// deviation is a contract break — either fix the bridge or update the
// table comment at top-of-file.
func assertOTelValueMatches(rt *rapid.T, key string, want slog.Value, wantKind slog.Kind, got log.Value) {
	switch wantKind {
	case slog.KindBool:
		if g, w := got.AsBool(), want.Bool(); g != w {
			rt.Fatalf("otel %q: bool want=%v got=%v", key, w, g)
		}
	case slog.KindInt64:
		if g, w := got.AsInt64(), want.Int64(); g != w {
			rt.Fatalf("otel %q: int64 want=%d got=%d", key, w, g)
		}
	case slog.KindUint64:
		// Documented overflow path: u > math.MaxInt64 ⇒ Float64Value.
		u := want.Uint64()
		if u > math.MaxInt64 {
			if got.Kind() != log.KindFloat64 {
				rt.Fatalf("otel %q: uint64 overflow want Float64Value, got kind=%s", key, got.Kind())
			}
			if g, w := got.AsFloat64(), float64(u); g != w {
				rt.Fatalf("otel %q: uint64 overflow float64 want=%v got=%v", key, w, g)
			}
			return
		}
		if g, w := got.AsInt64(), int64(u); g != w {
			rt.Fatalf("otel %q: uint64 want=%d got=%d", key, w, g)
		}
	case slog.KindFloat64:
		g, w := got.AsFloat64(), want.Float64()
		if math.IsNaN(w) && math.IsNaN(g) {
			return
		}
		if w != g {
			rt.Fatalf("otel %q: float64 want=%v got=%v", key, w, g)
		}
	case slog.KindString:
		if g, w := got.AsString(), want.String(); g != w {
			rt.Fatalf("otel %q: string want=%q got=%q", key, w, g)
		}
	case slog.KindDuration:
		if g, w := got.AsInt64(), want.Duration().Nanoseconds(); g != w {
			rt.Fatalf("otel %q: duration ns want=%d got=%d", key, w, g)
		}
	case slog.KindTime:
		if g, w := got.AsInt64(), want.Time().UnixNano(); g != w {
			rt.Fatalf("otel %q: time UnixNano want=%d got=%d", key, w, g)
		}
	default:
		rt.Fatalf("otel %q: untracked kind %s in test table", key, wantKind)
	}
}
