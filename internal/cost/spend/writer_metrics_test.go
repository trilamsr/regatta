package spend_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/trilamsr/regatta/internal/cost/spend"
)

// newMeterAndReader builds an SDK MeterProvider + ManualReader pair so
// emit sites can be inspected without a wire-level exporter.
func newMeterAndReader(t *testing.T) (*metric.ManualReader, *metric.MeterProvider) {
	t.Helper()
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	return reader, mp
}

// collectScope returns the scope metrics for the spend package.
func collectScope(t *testing.T, reader *metric.ManualReader) metricdata.ScopeMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	const scope = "github.com/trilamsr/regatta/internal/cost/spend"
	for _, sm := range rm.ScopeMetrics {
		if sm.Scope.Name == scope {
			return sm
		}
	}
	t.Fatalf("scope %q absent; got %d scopes", scope, len(rm.ScopeMetrics))
	return metricdata.ScopeMetrics{}
}

// findSum returns the Sum data for the named instrument or fails.
func findSum[T int64 | float64](t *testing.T, sm metricdata.ScopeMetrics, name string) metricdata.Sum[T] {
	t.Helper()
	for _, m := range sm.Metrics {
		if m.Name == name {
			s, ok := m.Data.(metricdata.Sum[T])
			if !ok {
				t.Fatalf("metric %q data %T; want Sum[T]", name, m.Data)
			}
			return s
		}
	}
	t.Fatalf("metric %q absent; have %v", name, metricNames(sm))
	return metricdata.Sum[T]{}
}

func metricNames(sm metricdata.ScopeMetrics) []string {
	out := make([]string, 0, len(sm.Metrics))
	for _, m := range sm.Metrics {
		out = append(out, m.Name)
	}
	return out
}

// attrSetEqual reports whether got matches want set-wise, ignoring order.
func attrSetEqual(got attribute.Set, want []attribute.KeyValue) bool {
	if got.Len() != len(want) {
		return false
	}
	for _, kv := range want {
		v, ok := got.Value(kv.Key)
		if !ok || v.AsString() != kv.Value.AsString() {
			return false
		}
	}
	return true
}

// TestRecordCall_EmitsCostUSDCounter — one RecordCall produces one regatta.cost.usd datapoint with dag_id+operator_id only.
func TestRecordCall_EmitsCostUSDCounter(t *testing.T) {
	db := openWriterDB(t)
	reader, mp := newMeterAndReader(t)
	meter := mp.Meter("github.com/trilamsr/regatta/internal/cost/spend")

	cfg := spend.Config{Meter: meter}
	r := baseRecord()
	if err := recordOneWithCfg(t, context.Background(), db, r, cfg); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}

	sm := collectScope(t, reader)
	sum := findSum[float64](t, sm, "regatta.cost.usd")
	if len(sum.DataPoints) != 1 {
		t.Fatalf("regatta.cost.usd datapoints=%d; want 1", len(sum.DataPoints))
	}
	dp := sum.DataPoints[0]
	want := []attribute.KeyValue{
		attribute.String("dag_id", "DAG-A"),
		attribute.String("operator_id", "agent-7"),
	}
	if !attrSetEqual(dp.Attributes, want) {
		t.Errorf("attrs = %v; want %v", dp.Attributes.ToSlice(), want)
	}
	// 1M input @ $3 + 0.5M output @ $15 = 10.50.
	if dp.Value < 10.49 || dp.Value > 10.51 {
		t.Errorf("value=%.4f; want ~10.50", dp.Value)
	}
}

// TestRecordCall_EmitsTokenCounterPerDirection — one RecordCall yields one datapoint per non-zero direction with input|output|cache_read.
func TestRecordCall_EmitsTokenCounterPerDirection(t *testing.T) {
	db := openWriterDB(t)
	reader, mp := newMeterAndReader(t)
	meter := mp.Meter("github.com/trilamsr/regatta/internal/cost/spend")

	cfg := spend.Config{Meter: meter}
	r := baseRecord()
	r.CacheReadTokens = 200_000
	if err := recordOneWithCfg(t, context.Background(), db, r, cfg); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}

	sm := collectScope(t, reader)
	sum := findSum[int64](t, sm, "regatta.cost.tokens")
	if len(sum.DataPoints) != 3 {
		t.Fatalf("token datapoints=%d; want 3 (input+output+cache_read)", len(sum.DataPoints))
	}

	gotByDir := map[string]int64{}
	for _, dp := range sum.DataPoints {
		d, ok := dp.Attributes.Value("direction")
		if !ok {
			t.Errorf("datapoint missing direction attr: %v", dp.Attributes.ToSlice())
			continue
		}
		gotByDir[d.AsString()] = dp.Value
		// Each datapoint must carry dag_id + operator_id + direction (exactly 3 attrs).
		if dp.Attributes.Len() != 3 {
			t.Errorf("direction=%s attrs=%d; want 3", d.AsString(), dp.Attributes.Len())
		}
	}
	wantByDir := map[string]int64{
		"input":      1_000_000,
		"output":     500_000,
		"cache_read": 200_000,
	}
	for dir, want := range wantByDir {
		if gotByDir[dir] != want {
			t.Errorf("direction=%s sum=%d; want %d", dir, gotByDir[dir], want)
		}
	}
}

// TestRecordCall_TokenCounter_SkipsZeroDirections — directions with zero tokens emit no datapoint (cardinality hygiene).
func TestRecordCall_TokenCounter_SkipsZeroDirections(t *testing.T) {
	db := openWriterDB(t)
	reader, mp := newMeterAndReader(t)
	meter := mp.Meter("github.com/trilamsr/regatta/internal/cost/spend")

	cfg := spend.Config{Meter: meter}
	r := baseRecord()
	r.CacheReadTokens = 0
	r.CacheCreationTokens = 0
	if err := recordOneWithCfg(t, context.Background(), db, r, cfg); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}

	sm := collectScope(t, reader)
	sum := findSum[int64](t, sm, "regatta.cost.tokens")
	if len(sum.DataPoints) != 2 {
		t.Fatalf("datapoints=%d; want 2 (input+output, cache_read skipped)", len(sum.DataPoints))
	}
	for _, dp := range sum.DataPoints {
		d, _ := dp.Attributes.Value("direction")
		if d.AsString() == "cache_read" {
			t.Errorf("cache_read emitted with zero tokens")
		}
	}
}

// TestRecordCall_NoEmitOnPricingMissing — pricing-missing path writes no metric (open span already smokes).
func TestRecordCall_NoEmitOnPricingMissing(t *testing.T) {
	db := openWriterDB(t)
	reader, mp := newMeterAndReader(t)
	meter := mp.Meter("github.com/trilamsr/regatta/internal/cost/spend")

	cfg := spend.Config{Meter: meter}
	r := baseRecord()
	r.Model = "no-such-model-9000"
	_ = recordOneWithCfg(t, context.Background(), db, r, cfg) // err expected

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "regatta.cost.usd" || m.Name == "regatta.cost.tokens" {
				if sum, ok := m.Data.(metricdata.Sum[float64]); ok && len(sum.DataPoints) > 0 {
					t.Errorf("metric %q emitted on pricing-missing", m.Name)
				}
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok && len(sum.DataPoints) > 0 {
					t.Errorf("metric %q emitted on pricing-missing", m.Name)
				}
			}
		}
	}
}

// TestRecordCall_NilMeter_NoPanic — empty Config resolves to global noop and does not panic.
func TestRecordCall_NilMeter_NoPanic(t *testing.T) {
	db := openWriterDB(t)
	r := baseRecord()
	if err := recordOneWithCfg(t, context.Background(), db, r, spend.Config{}); err != nil {
		t.Fatalf("RecordCall with nil meter: %v", err)
	}
}

// TestDashboardMetricNames_MatchEmitted — dashboard JSON refs must match emitter names exactly (drift gate).
func TestDashboardMetricNames_MatchEmitted(t *testing.T) {
	path := filepath.Join(repoRootForTest(t), "docs", "operator", "dashboards", "per-dag-cost.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	var dash map[string]any
	if err := json.Unmarshal(raw, &dash); err != nil {
		t.Fatalf("parse dashboard: %v", err)
	}
	// Prometheus suffix-rule: regatta.cost.usd → regatta_cost_usd_total;
	// regatta.cost.tokens → regatta_cost_tokens_total.
	wantNames := []string{"regatta_cost_usd_total", "regatta_cost_tokens_total"}
	bannedNames := []string{
		// Old or speculative spellings — drift would land here silently.
		"regatta_cost_usd", "regatta_cost_tokens",
		"regatta_cost_token_total", "regatta_cost_usd_dollars_total",
	}
	for _, want := range wantNames {
		if !strings.Contains(string(raw), want) {
			t.Errorf("dashboard JSON missing emitted metric ref %q", want)
		}
	}
	for _, bad := range bannedNames {
		// substring guard: a banned literal is allowed only when it is a strict prefix of a want name.
		if strings.Contains(string(raw), bad) && !prefixOfAny(bad, wantNames) {
			t.Errorf("dashboard JSON contains banned metric name %q (drift)", bad)
		}
	}
}

func prefixOfAny(s string, set []string) bool {
	for _, x := range set {
		if strings.HasPrefix(x, s) {
			return true
		}
	}
	return false
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/cost/spend/writer_metrics_test.go → repo root is 4 levels up.
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
}

// recordOneWithCfg mirrors recordOne but threads a spend.Config through WriteOptions.
func recordOneWithCfg(t *testing.T, ctx context.Context, db *sql.DB, r spend.CallRecord, cfg spend.Config) error {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := spend.RecordCall(ctx, tx, r, spend.WriteOptions{
		Now:   func() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC) },
		Key:   testKey,
		KeyID: testKeyID,
		Cfg:   cfg,
	}); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
