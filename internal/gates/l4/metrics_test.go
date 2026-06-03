package l4_test

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/gates/l4"
	"github.com/trilamsr/regatta/internal/gates/severity"
)

// stubInvoker returns a canned-finding Invoker for metric assertions.
func stubInvoker(findings []schemas.Finding) l4.Invoker {
	return func(_ context.Context, _ l4.InvokeRequest) (l4.InvokeResponse, error) {
		return l4.InvokeResponse{Findings: findings}, nil
	}
}

// newReader returns a freshly scoped Meter backed by a ManualReader.
func newReader(t *testing.T) (sdkmetric.Reader, *sdkmetric.MeterProvider) {
	t.Helper()
	r := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(r))
	return r, mp
}

// collect drains the reader into a metricdata.ResourceMetrics snapshot.
func collect(t *testing.T, r sdkmetric.Reader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	return rm
}

// findSum returns the int64 sum across data points for a counter metric.
func findSum(t *testing.T, rm metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s: data is %T, want Sum[int64]", name, m.Data)
			}
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total
		}
	}
	return 0
}

// findHistogramCount returns the total count across data points for a histogram metric.
func findHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string) uint64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s: data is %T, want Histogram[float64]", name, m.Data)
			}
			var total uint64
			for _, dp := range hist.DataPoints {
				total += dp.Count
			}
			return total
		}
	}
	return 0
}

// verdictsByCategory returns the (verdict,category) tuples emitted for regatta.l4.invocations.
func verdictsByCategory(t *testing.T, rm metricdata.ResourceMetrics) []struct{ Verdict, Category string } {
	t.Helper()
	out := []struct{ Verdict, Category string }{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "regatta.l4.invocations" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("invocations: data is %T", m.Data)
			}
			for _, dp := range sum.DataPoints {
				var v, c string
				for _, kv := range dp.Attributes.ToSlice() {
					switch string(kv.Key) {
					case "verdict":
						v = kv.Value.AsString()
					case "category":
						c = kv.Value.AsString()
					}
				}
				out = append(out, struct{ Verdict, Category string }{v, c})
			}
		}
	}
	return out
}

// TestL4_Metrics_InvocationsByVerdictAndCategory pins the per-category emit with the gate verdict.
func TestL4_Metrics_InvocationsByVerdictAndCategory(t *testing.T) {
	reader, mp := newReader(t)
	cfg := l4.Config{
		GateID:        "l4_adversarial",
		Model:         l4.DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		Invoker:       stubInvoker(nil),
		Meter:         mp.Meter("test"),
	}
	if _, err := l4.Run(context.Background(), cfg, l4.Input{PRSHA: "deadbeef", RunID: "r1"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rm := collect(t, reader)
	got := verdictsByCategory(t, rm)
	if len(got) != len(l4.AllCategories) {
		t.Fatalf("emit count: got %d, want %d (one per category)", len(got), len(l4.AllCategories))
	}
	for _, kv := range got {
		if kv.Verdict != "allow" {
			t.Errorf("verdict: got %q, want allow", kv.Verdict)
		}
	}
}

// TestL4_Metrics_VerdictMapping_Fail asserts VerdictFail maps to label deny.
func TestL4_Metrics_VerdictMapping_Fail(t *testing.T) {
	reader, mp := newReader(t)
	cfg := l4.Config{
		GateID:        "l4_adversarial",
		Model:         l4.DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		Invoker: stubInvoker([]schemas.Finding{{
			ID: "L4-CORR-X", Severity: schemas.FindingCritical,
		}}),
		Meter: mp.Meter("test"),
	}
	if _, err := l4.Run(context.Background(), cfg, l4.Input{PRSHA: "x", RunID: "r2"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rm := collect(t, reader)
	for _, kv := range verdictsByCategory(t, rm) {
		if kv.Verdict != "deny" {
			t.Errorf("verdict: got %q, want deny", kv.Verdict)
		}
	}
}

// TestL4_Metrics_VerdictMapping_Advisory asserts VerdictAdvisory maps to label needs_review.
func TestL4_Metrics_VerdictMapping_Advisory(t *testing.T) {
	reader, mp := newReader(t)
	cfg := l4.Config{
		GateID:        "l4_adversarial",
		Model:         l4.DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		AdvisoryMode:  true,
		Invoker: stubInvoker([]schemas.Finding{{
			ID: "L4-SEC-AB", Severity: schemas.FindingCritical,
		}}),
		Meter: mp.Meter("test"),
	}
	if _, err := l4.Run(context.Background(), cfg, l4.Input{PRSHA: "y", RunID: "r3"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rm := collect(t, reader)
	for _, kv := range verdictsByCategory(t, rm) {
		if kv.Verdict != "needs_review" {
			t.Errorf("verdict: got %q, want needs_review", kv.Verdict)
		}
	}
}

// TestL4_Metrics_VerdictMapping_NilInvoker_Skip asserts nil-Invoker maps to label skip.
func TestL4_Metrics_VerdictMapping_NilInvoker_Skip(t *testing.T) {
	reader, mp := newReader(t)
	cfg := l4.Config{
		GateID: "l4_adversarial",
		Model:  l4.DefaultModel,
		Meter:  mp.Meter("test"),
	}
	if _, err := l4.Run(context.Background(), cfg, l4.Input{PRSHA: "z", RunID: "r4"}); err == nil {
		t.Fatalf("nil-invoker: want error, got nil")
	}
	rm := collect(t, reader)
	got := verdictsByCategory(t, rm)
	if len(got) == 0 {
		t.Fatalf("nil-invoker: want at least one emit, got 0")
	}
	for _, kv := range got {
		if kv.Verdict != "skip" {
			t.Errorf("nil-invoker verdict: got %q, want skip", kv.Verdict)
		}
	}
}

// TestL4_Metrics_LatencyRecorded pins the latency histogram emit per Run.
func TestL4_Metrics_LatencyRecorded(t *testing.T) {
	reader, mp := newReader(t)
	cfg := l4.Config{
		GateID:        "l4_adversarial",
		Model:         l4.DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		Invoker:       stubInvoker(nil),
		Meter:         mp.Meter("test"),
	}
	if _, err := l4.Run(context.Background(), cfg, l4.Input{PRSHA: "a", RunID: "r5"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rm := collect(t, reader)
	if got := findHistogramCount(t, rm, "regatta.l4.latency_ms"); got != 1 {
		t.Errorf("latency count: got %d, want 1", got)
	}
}

// TestL4_Metrics_CacheHitsAndMisses pins counter emits on the cached-invoker path.
func TestL4_Metrics_CacheHitsAndMisses(t *testing.T) {
	reader, mp := newReader(t)
	meter := mp.Meter("test")
	base := stubInvoker(nil)
	cached := l4.NewCachedInvoker(base, 8, meter)
	cfg := l4.Config{
		GateID:        "l4_adversarial",
		Model:         l4.DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		Invoker:       cached,
		Meter:         meter,
	}
	in := l4.Input{PRSHA: "a", RunID: "r6", Diff: "diff-A", Spec: "spec-A"}
	if _, err := l4.Run(context.Background(), cfg, in); err != nil {
		t.Fatalf("Run #1: %v", err)
	}
	if _, err := l4.Run(context.Background(), cfg, in); err != nil {
		t.Fatalf("Run #2: %v", err)
	}
	rm := collect(t, reader)
	if got := findSum(t, rm, "regatta.l4.cache.misses"); got != 1 {
		t.Errorf("cache.misses: got %d, want 1", got)
	}
	if got := findSum(t, rm, "regatta.l4.cache.hits"); got != 1 {
		t.Errorf("cache.hits: got %d, want 1", got)
	}
}

// TestL4_Metrics_SecondOpinionFired pins the counter on dispute-driven SO re-invoke.
func TestL4_Metrics_SecondOpinionFired(t *testing.T) {
	reader, mp := newReader(t)
	primary := []schemas.Finding{{ID: "L4-CORR-OBO", Severity: schemas.FindingHigh}}
	cfg := l4.Config{
		GateID:        "l4_adversarial",
		Model:         l4.DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		Invoker:       stubInvoker(primary),
		Meter:         mp.Meter("test"),
	}
	in := l4.Input{
		PRSHA:  "a",
		RunID:  "r7",
		PRBody: "[L4-DISPUTE] L4-CORR-OBO",
	}
	if _, err := l4.Run(context.Background(), cfg, in); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rm := collect(t, reader)
	if got := findSum(t, rm, "regatta.l4.second_opinion.fired"); got != 1 {
		t.Errorf("second_opinion.fired: got %d, want 1", got)
	}
}

// TestL4_Metrics_VerdictMapping_EscalateOnSOFlip pins escalate when second-opinion drops a disputed finding.
func TestL4_Metrics_VerdictMapping_EscalateOnSOFlip(t *testing.T) {
	reader, mp := newReader(t)
	calls := 0
	cfg := l4.Config{
		GateID:        "l4_adversarial",
		Model:         l4.DefaultModel,
		SeverityBlock: []string{severity.Critical, severity.TwoHigh},
		// First call (primary) returns one critical that the SO will refuse to confirm.
		Invoker: func(_ context.Context, _ l4.InvokeRequest) (l4.InvokeResponse, error) {
			calls++
			if calls == 1 {
				return l4.InvokeResponse{Findings: []schemas.Finding{{
					ID: "L4-RISK-FLIP", Severity: schemas.FindingCritical,
				}}}, nil
			}
			return l4.InvokeResponse{}, nil
		},
		Meter: mp.Meter("test"),
	}
	in := l4.Input{PRSHA: "a", RunID: "r8", PRBody: "[L4-DISPUTE] L4-RISK-FLIP"}
	if _, err := l4.Run(context.Background(), cfg, in); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rm := collect(t, reader)
	for _, kv := range verdictsByCategory(t, rm) {
		if kv.Verdict != "escalate" {
			t.Errorf("escalate verdict: got %q, want escalate", kv.Verdict)
		}
	}
}

// TestL4_Metrics_CategoryCardinality_AtMost12 pins the AllCategories slice size against the spec cap.
func TestL4_Metrics_CategoryCardinality_AtMost12(t *testing.T) {
	if got := len(l4.AllCategories); got > 12 {
		t.Fatalf("AllCategories len %d exceeds the 12-value cap from spec §3 / issue #388", got)
	}
}
