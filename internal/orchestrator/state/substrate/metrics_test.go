package substrate_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// metricsTestMu serializes the package-level meter swap so parallel tests
// in the same suite do not stomp each other's manual reader. Substrate
// uses a sync.Once + package-level meter override; concurrent test
// goroutines must serialise their override windows.
var metricsTestMu sync.Mutex

// newSubstrateReader installs a fresh manual reader as the substrate
// package meter and returns (reader, cleanup). Cleanup restores the
// previous meter and releases the suite-wide lock.
func newSubstrateReader(t *testing.T) (sdkmetric.Reader, func()) {
	t.Helper()
	metricsTestMu.Lock()
	r := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(r))
	restore := substrate.SetMeterForTesting(mp.Meter("substrate-test"))
	return r, func() {
		restore()
		metricsTestMu.Unlock()
	}
}

// collectMetrics drains the reader into a snapshot for assertion.
func collectMetrics(t *testing.T, r sdkmetric.Reader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return rm
}

// findCounterByAttrSlice returns the int64 sum of data points on the
// named counter whose attribute set contains every (k,v) in wantPairs.
// Pairs are passed as [][2]string so callers can list them inline
// without a literal map allocation per call.
func findCounterByAttrSlice(t *testing.T, rm metricdata.ResourceMetrics, name string, wantPairs [][2]string) int64 {
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
			for _, dp := range sum.DataPoints {
				if attrSetContains(dp.Attributes.ToSlice(), wantPairs) {
					return dp.Value
				}
			}
		}
	}
	return 0
}

// attrSetContains is true when every (k,v) in want appears in got. Used
// to look up a counter data point by its tag combination.
func attrSetContains(got []attribute.KeyValue, want [][2]string) bool {
	for _, kv := range want {
		found := false
		for _, g := range got {
			if string(g.Key) == kv[0] && g.Value.AsString() == kv[1] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// findHistogramCount returns the total count across data points for `name`.
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

// histogramHasAttr returns true if any data point of the histogram
// `name` carries the (k,v) attribute pair.
func histogramHasAttr(t *testing.T, rm metricdata.ResourceMetrics, name, k, v string) bool {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			for _, dp := range hist.DataPoints {
				for _, kv := range dp.Attributes.ToSlice() {
					if string(kv.Key) == k && kv.Value.AsString() == v {
						return true
					}
				}
			}
		}
	}
	return false
}

// openMigratedFileDB returns (db, path) for tests that need both a *sql.DB
// handle and the underlying file path (sweeper opens a second read-only
// connection against the same file).
func openMigratedFileDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "raw.db")
	raw, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open file DB: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	raw.SetMaxOpenConns(1)
	if err := state.Migrate(context.Background(), raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	substrate.ResetClockForTesting()
	return raw, dbPath
}

// TestT1_EventRateCounter_IncrementsOnAppend pins one append → one counter increment.
func TestT1_EventRateCounter_IncrementsOnAppend(t *testing.T) {
	reader, cleanup := newSubstrateReader(t)
	defer cleanup()

	substrate.ResetClockForTesting()
	db := openMigratedDB(t)
	ctx := testCtx()
	at := testTime()
	e := mkEvent(0xA1, "run-T1", substrate.KindHeartbeat,
		`{"work_item_id":"WI-T1","timestamp":1}`, at)
	if err := appendEventTx(ctx, t, db, e); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	rm := collectMetrics(t, reader)
	got := findCounterByAttrSlice(t, rm, "regatta.substrate.events.appended",
		[][2]string{{"kind", string(substrate.KindHeartbeat)}})
	if got != 1 {
		t.Fatalf("events.appended (heartbeat): got %d, want 1", got)
	}
}

// TestT1_UnknownKindRoutesToOther pins the enum-guard "other" fallthrough.
func TestT1_UnknownKindRoutesToOther(t *testing.T) {
	reader, cleanup := newSubstrateReader(t)
	defer cleanup()
	ctx := testCtx()
	substrate.ExportedRecordEventForTesting(ctx, substrate.EventKind("not-a-real-kind"))
	substrate.ExportedRecordEventForTesting(ctx, substrate.KindHeartbeat)
	rm := collectMetrics(t, reader)
	otherCount := findCounterByAttrSlice(t, rm, "regatta.substrate.events.appended",
		[][2]string{{"kind", "other"}})
	if otherCount != 1 {
		t.Fatalf("events.appended (other): got %d, want 1", otherCount)
	}
	knownCount := findCounterByAttrSlice(t, rm, "regatta.substrate.events.appended",
		[][2]string{{"kind", string(substrate.KindHeartbeat)}})
	if knownCount != 1 {
		t.Fatalf("events.appended (heartbeat): got %d, want 1", knownCount)
	}
}

// TestT1_NilMeterFallback_NoPanic pins the no-panic contract on the global noop fallback.
func TestT1_NilMeterFallback_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on nil-meter fallback: %v", r)
		}
	}()
	substrate.ExportedRecordEventForTesting(context.Background(), substrate.KindHeartbeat)
}

// TestT2_ChainBreakCounter_OnVerifyMismatch pins read-path increment on MAC mismatch.
func TestT2_ChainBreakCounter_OnVerifyMismatch(t *testing.T) {
	reader, cleanup := newSubstrateReader(t)
	defer cleanup()

	at := testTime()
	e := substrate.Event{
		ID: substrate.Mint(at), RunID: "run-T2", TenantID: substrate.DefaultTenantID,
		Kind: substrate.KindHeartbeat, PayloadJSON: []byte(`{}`),
		WrittenBy: "tester", WrittenAt: at.UnixMilli(), SchemaVersion: 1,
		Nonce: fixedNonce(0xB2),
	}
	if err := substrate.Sign(&e, testKey, testKeyID); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	e.SigMAC = strings.Repeat("00", 32)
	if err := substrate.Verify(e, testKeyring()); err == nil {
		t.Fatalf("Verify: want error, got nil")
	}
	rm := collectMetrics(t, reader)
	got := findCounterByAttrSlice(t, rm, "regatta.substrate.chain.break",
		[][2]string{{"event_kind", string(substrate.KindHeartbeat)}})
	if got != 1 {
		t.Fatalf("chain.break (heartbeat): got %d, want 1", got)
	}
}

// TestT2_ChainBreakCounter_UnknownKeyDoesNotIncrement pins missing-key path is quiet.
func TestT2_ChainBreakCounter_UnknownKeyDoesNotIncrement(t *testing.T) {
	reader, cleanup := newSubstrateReader(t)
	defer cleanup()

	at := testTime()
	e := substrate.Event{
		ID: substrate.Mint(at), RunID: "run-T2b", TenantID: substrate.DefaultTenantID,
		Kind: substrate.KindHeartbeat, PayloadJSON: []byte(`{}`),
		WrittenBy: "tester", WrittenAt: at.UnixMilli(), SchemaVersion: 1,
		Nonce: fixedNonce(0xB3),
	}
	if err := substrate.Sign(&e, testKey, testKeyID); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	_ = substrate.Verify(e, map[string][]byte{})
	rm := collectMetrics(t, reader)
	got := findCounterByAttrSlice(t, rm, "regatta.substrate.chain.break",
		[][2]string{{"event_kind", string(substrate.KindHeartbeat)}})
	if got != 0 {
		t.Fatalf("chain.break: got %d, want 0 (unknown key is not a break)", got)
	}
}

// TestT2_ChainSweeper_DetectsBreak_LogsOnly pins sweeper logs+counters, never halts.
func TestT2_ChainSweeper_DetectsBreak_LogsOnly(t *testing.T) {
	reader, cleanup := newSubstrateReader(t)
	defer cleanup()

	db, path := openMigratedFileDB(t)
	substrate.ResetClockForTesting()
	at := testTime()
	ctx := testCtx()
	e := mkEvent(0xC4, "run-T2c", substrate.KindHeartbeat,
		`{"work_item_id":"WI-T2c","timestamp":1}`, at)
	if err := appendEventTx(ctx, t, db, e); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	// Reset T1 counter side-effect so we measure only the sweeper bump.
	// The previous AppendEvent emitted one event-rate count; we assert
	// chain.break separately so this is fine.

	if _, err := db.Exec(`UPDATE substrate_events SET sig_mac=? WHERE id=?`,
		strings.Repeat("00", 32), e.ID); err != nil {
		t.Fatalf("UPDATE sig_mac: %v", err)
	}

	sw, err := substrate.NewSweeper(substrate.SweeperConfig{
		DBPath:          path,
		Keyring:         testKeyring(),
		Interval:        time.Hour,
		Window:          24 * time.Hour,
		BatchSize:       100,
		InterBatchPause: time.Millisecond,
		Now:             func() time.Time { return at.Add(time.Minute) },
	})
	if err != nil {
		t.Fatalf("NewSweeper: %v", err)
	}
	defer func() { _ = sw.Close() }()
	sw.Start(ctx)

	if err := sw.SweepOnce(ctx); err != nil {
		t.Fatalf("SweepOnce: want nil (log-only), got %v", err)
	}
	rm := collectMetrics(t, reader)
	got := findCounterByAttrSlice(t, rm, "regatta.substrate.chain.break",
		[][2]string{{"event_kind", string(substrate.KindHeartbeat)}})
	if got < 1 {
		t.Fatalf("chain.break: got %d, want >= 1 (sweeper detected break)", got)
	}
}

// TestT2_ChainSweeper_ExitsOnContextCancel pins clean goroutine shutdown.
func TestT2_ChainSweeper_ExitsOnContextCancel(t *testing.T) {
	_, cleanup := newSubstrateReader(t)
	defer cleanup()

	_, path := openMigratedFileDB(t)
	sw, err := substrate.NewSweeper(substrate.SweeperConfig{
		DBPath:   path,
		Keyring:  testKeyring(),
		Interval: 10 * time.Millisecond,
		Window:   24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewSweeper: %v", err)
	}
	ctx, cancel := context.WithCancel(testCtx())
	sw.Start(ctx)
	cancel()
	done := make(chan struct{})
	go func() {
		_ = sw.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sweeper did not exit within 2s of context cancel")
	}
}

// TestT3_DivergenceCounter_IncrementsOnAuditRow pins audit-table reader emits.
func TestT3_DivergenceCounter_IncrementsOnAuditRow(t *testing.T) {
	reader, cleanup := newSubstrateReader(t)
	defer cleanup()

	db := openMigratedDB(t)
	ctx := testCtx()
	if _, err := db.Exec(`INSERT INTO substrate_divergence_audit
		(detected_at, detector, store, primary_key, diff_summary)
		VALUES (?, 'layer1_read', 'approvals', 'pk-1', 'test-diff')`,
		time.Now().UnixMilli()); err != nil {
		t.Fatalf("insert audit row: %v", err)
	}
	r, err := substrate.NewDivergenceReader(substrate.DivergenceReaderConfig{
		DB: db,
	})
	if err != nil {
		t.Fatalf("NewDivergenceReader: %v", err)
	}
	if err := r.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	rm := collectMetrics(t, reader)
	got := findCounterByAttrSlice(t, rm, "regatta.substrate.divergence.detected",
		[][2]string{{"program_kind", "other"}, {"layer", "layer1_read"}})
	if got != 1 {
		t.Fatalf("divergence.detected (program_kind=other,layer=layer1_read): got %d, want 1", got)
	}
}

// TestT3_DivergenceCounter_ProgramKindOther_FallsThrough pins enum guard fallthrough.
func TestT3_DivergenceCounter_ProgramKindOther_FallsThrough(t *testing.T) {
	reader, cleanup := newSubstrateReader(t)
	defer cleanup()

	db := openMigratedDB(t)
	ctx := testCtx()
	if _, err := db.Exec(`INSERT INTO substrate_divergence_audit
		(detected_at, detector, store, primary_key, diff_summary)
		VALUES (?, 'layer1_write', 'token_spend', 'pk-X', 'd')`,
		time.Now().UnixMilli()); err != nil {
		t.Fatalf("insert audit row: %v", err)
	}
	r, err := substrate.NewDivergenceReader(substrate.DivergenceReaderConfig{
		DB: db,
		ProgramKindResolver: func(_ substrate.DivergenceAuditRow) string {
			return "bogus-kind-not-in-enum"
		},
	})
	if err != nil {
		t.Fatalf("NewDivergenceReader: %v", err)
	}
	if err := r.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	rm := collectMetrics(t, reader)
	got := findCounterByAttrSlice(t, rm, "regatta.substrate.divergence.detected",
		[][2]string{{"program_kind", "other"}})
	if got < 1 {
		t.Fatalf("divergence.detected (program_kind=other): got %d, want >= 1", got)
	}
}

// TestT4_ReplayLatencyHistogram_BucketsMatchSpec pins the 13-bucket grid.
func TestT4_ReplayLatencyHistogram_BucketsMatchSpec(t *testing.T) {
	want := []float64{
		0.005, 0.010, 0.025, 0.050, 0.100, 0.250,
		0.500, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0,
	}
	got := substrate.ReplayLatencyBuckets()
	if len(got) != len(want) {
		t.Fatalf("bucket count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("bucket[%d]: got %v, want %v", i, got[i], want[i])
		}
	}
}

// TestT4_ReplayLatency_RecordedExactlyOnce pins one Record per invocation.
func TestT4_ReplayLatency_RecordedExactlyOnce(t *testing.T) {
	reader, cleanup := newSubstrateReader(t)
	defer cleanup()
	substrate.RecordReplayDuration(context.Background(), "dag", "match", 0.123)
	rm := collectMetrics(t, reader)
	count := findHistogramCount(t, rm, "regatta.substrate.replay.duration_seconds")
	if count != 1 {
		t.Fatalf("histogram count: got %d, want 1", count)
	}
}

// TestT4_ReplayLatency_OutcomeOtherFallsThrough pins outcome enum guard.
func TestT4_ReplayLatency_OutcomeOtherFallsThrough(t *testing.T) {
	reader, cleanup := newSubstrateReader(t)
	defer cleanup()
	substrate.RecordReplayDuration(context.Background(), "dag", "weird-outcome", 0.05)
	rm := collectMetrics(t, reader)
	if !histogramHasAttr(t, rm, "regatta.substrate.replay.duration_seconds",
		"outcome", "other") {
		t.Fatal("histogram missing outcome=other tag (enum guard not routing fallthrough)")
	}
}

// TestProgramKindEnum_Closed pins the closed-enum guard for divergence.
func TestProgramKindEnum_Closed(t *testing.T) {
	reader, cleanup := newSubstrateReader(t)
	defer cleanup()
	substrate.ExportedRecordDivergenceForTesting(context.Background(),
		"definitely-not-a-real-kind", "layer1_read")
	rm := collectMetrics(t, reader)
	got := findCounterByAttrSlice(t, rm, "regatta.substrate.divergence.detected",
		[][2]string{{"program_kind", "other"}})
	if got != 1 {
		t.Fatalf("program_kind enum guard: got %d for 'other', want 1", got)
	}
}

// BenchmarkSubstrate_AppendUnderT1Meter measures counter overhead on hot path.
func BenchmarkSubstrate_AppendUnderT1Meter(b *testing.B) {
	r := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(r))
	restore := substrate.SetMeterForTesting(mp.Meter("substrate-bench"))
	defer restore()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		substrate.ExportedRecordEventForTesting(context.Background(), substrate.KindHeartbeat)
	}
}
