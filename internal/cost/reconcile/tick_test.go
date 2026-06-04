package reconcile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/obs"
)

// adminKeyEnv names the env var holding the Anthropic admin API key.
// Tests set this via t.Setenv; the fixture value never appears in the
// PR diff (gosec G101 false-positive on the literal constant — the
// constant is the env-var name, not the key).
const adminKeyEnv = "ANTHROPIC_ADMIN_KEY" //nolint:gosec // env var name, not a credential

// recordedAppend captures one Appender call. The fake Appender stores
// every call so tests assert payload + tenant + kind invariants.
type recordedAppend struct {
	tenantID  string
	kind      string
	payload   json.RawMessage
	writtenAt time.Time
}

type fakeAppender struct {
	mu      sync.Mutex
	rows    []recordedAppend
	failNth int // 0 = never fail; N>0 = fail on Nth call (1-indexed)
}

func (f *fakeAppender) Append(ctx context.Context, tenantID, kind string, payload json.RawMessage, writtenAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNth > 0 && len(f.rows)+1 == f.failNth {
		f.failNth = 0
		return errors.New("synthetic append failure")
	}
	f.rows = append(f.rows, recordedAppend{
		tenantID:  tenantID,
		kind:      kind,
		payload:   append(json.RawMessage(nil), payload...),
		writtenAt: writtenAt,
	})
	return nil
}

func (f *fakeAppender) snapshot() []recordedAppend {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedAppend, len(f.rows))
	copy(out, f.rows)
	return out
}

// recordedReader returns a fixed cumulative recorded_usd for the
// reconcile window. Gate-side spend.Reader is not on the Reconciler
// hot path; an interface keeps the file-disjoint contract pure.
// Per #554 the returned value is micro-USD; the fixture stores
// USD and converts at the seam so test sites still type natural
// dollar amounts.
type fakeRecordedReader struct {
	usd float64
	err error
}

func (f *fakeRecordedReader) RecordedUSDForWindow(ctx context.Context, tenantID string, start, end time.Time) (spend.USDMicro, error) {
	return spend.FromUSD(f.usd), f.err
}

// capturingHandler captures slog records so R15 + drift-alert tests
// can assert event names + payload shape without serialising through
// an io.Writer.
type capturingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (c *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Clone())
	return nil
}
func (c *capturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return c }
func (c *capturingHandler) WithGroup(name string) slog.Handler       { return c }

func (c *capturingHandler) snapshot() []slog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]slog.Record, len(c.records))
	copy(out, c.records)
	return out
}

func (c *capturingHandler) eventCounts() map[string]int {
	m := map[string]int{}
	for _, r := range c.snapshot() {
		m[r.Message]++
	}
	return m
}

func mkRecorder(t *testing.T, recorded float64) *fakeRecordedReader {
	t.Helper()
	return &fakeRecordedReader{usd: recorded}
}

func mkCapturingLogger() (*slog.Logger, *capturingHandler) {
	h := &capturingHandler{}
	return slog.New(h), h
}

// frozenClock returns a clock function pinned to now.
func frozenClock(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

// fixedTime is the "current" reconciler clock — Tick will resolve the
// window [00:00, 01:00) just-closed at 01:02.
func fixedTime() time.Time {
	return time.Date(2026, 6, 1, 1, 2, 0, 0, time.UTC)
}

// B-tier — spec §6 T4 / §7 B.

func TestReconciler_TickEmitsBudgetReconciled_CostAPIPreferred(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)

	var costHits, usageHits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/organizations/cost_report/messages") {
			atomic.AddInt64(&costHits, 1)
			_, _ = w.Write(mustReadTestdata(t, "anthropic_cost_2026_06_01_01h.json"))
			return
		}
		atomic.AddInt64(&usageHits, 1)
		_, _ = w.Write(mustReadTestdata(t, "anthropic_usage_2026_06_01_01h.json"))
	}))
	t.Cleanup(srv.Close)

	app := &fakeAppender{}
	logger, _ := mkCapturingLogger()
	r := NewReconciler(Config{
		Clock:                  frozenClock(fixedTime()),
		HTTPClient:             srv.Client(),
		BaseURL:                srv.URL,
		BucketWidth:            time.Hour,
		DriftAlertThresholdPct: 10,
		UsageAPIKeyEnv:         adminKeyEnv,
		Appender:               app,
		RecordedReader:         mkRecorder(t, 13.25), // recorded == actual, drift 0
		TenantID:               "default",
		Logger:                 logger,
	})

	if err := r.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if atomic.LoadInt64(&costHits) != 1 {
		t.Errorf("costHits=%d want 1", costHits)
	}
	if atomic.LoadInt64(&usageHits) != 0 {
		t.Errorf("usageHits=%d want 0 (Cost API preferred)", usageHits)
	}
	rows := app.snapshot()
	if len(rows) != 1 {
		t.Fatalf("len(rows)=%d want 1", len(rows))
	}
	if rows[0].kind != "budget_reconciled" {
		t.Errorf("kind=%q want budget_reconciled", rows[0].kind)
	}
	var p spend.BudgetReconciledPayload
	if err := json.Unmarshal(rows[0].payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	want := 12.50 + 0.75
	if p.ActualUSD != want {
		t.Errorf("ActualUSD=%v want %v", p.ActualUSD, want)
	}
	if p.PeriodStart != time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("PeriodStart=%d", p.PeriodStart)
	}
	if p.PeriodEnd != time.Date(2026, 6, 1, 1, 0, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("PeriodEnd=%d", p.PeriodEnd)
	}
	if len(p.ModelBreakdown) != 2 {
		t.Errorf("len(ModelBreakdown)=%d want 2", len(p.ModelBreakdown))
	}
}

func TestReconciler_FallsBackToUsageAPI_WhenCostAPI404(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)

	var costHits, usageHits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/organizations/cost_report/messages") {
			atomic.AddInt64(&costHits, 1)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		atomic.AddInt64(&usageHits, 1)
		_, _ = w.Write(mustReadTestdata(t, "anthropic_usage_2026_06_01_01h.json"))
	}))
	t.Cleanup(srv.Close)

	app := &fakeAppender{}
	logger, capH := mkCapturingLogger()
	r := NewReconciler(Config{
		Clock:                  frozenClock(fixedTime()),
		HTTPClient:             srv.Client(),
		BaseURL:                srv.URL,
		BucketWidth:            time.Hour,
		DriftAlertThresholdPct: 10,
		UsageAPIKeyEnv:         adminKeyEnv,
		Appender:               app,
		RecordedReader:         mkRecorder(t, 0),
		TenantID:               "default",
		Logger:                 logger,
	})
	if err := r.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if costHits != 1 || usageHits != 1 {
		t.Errorf("costHits=%d usageHits=%d want 1/1", costHits, usageHits)
	}
	if capH.eventCounts()[string(obs.EventCostReconcileFallback)] == 0 {
		t.Errorf("expected EventCostReconcileFallback in log; got=%v", capH.eventCounts())
	}
	if len(app.snapshot()) != 1 {
		t.Errorf("len(rows)=%d want 1", len(app.snapshot()))
	}
}

func TestReconciler_DriftBelowThreshold_NoAlert(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// actual = 100
		body := `{"data":[{"bucket_start":"2026-06-01T00:00:00Z","bucket_end":"2026-06-01T01:00:00Z","model":"claude-sonnet-4-7","cost_usd":100.0}],"has_more":false}`
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	logger, capH := mkCapturingLogger()
	app := &fakeAppender{}
	r := NewReconciler(Config{
		Clock:                  frozenClock(fixedTime()),
		HTTPClient:             srv.Client(),
		BaseURL:                srv.URL,
		BucketWidth:            time.Hour,
		DriftAlertThresholdPct: 10,
		UsageAPIKeyEnv:         adminKeyEnv,
		Appender:               app,
		RecordedReader:         mkRecorder(t, 95), // drift=5% (below 10%)
		TenantID:               "default",
		Logger:                 logger,
	})
	if err := r.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if capH.eventCounts()[string(obs.EventCostDriftAlert)] != 0 {
		t.Errorf("expected zero EventCostDriftAlert; got=%v", capH.eventCounts())
	}
	if len(app.snapshot()) != 1 {
		t.Errorf("len(rows)=%d want 1", len(app.snapshot()))
	}
}

func TestReconciler_DriftAboveThreshold_EmitsAlert(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := `{"data":[{"bucket_start":"2026-06-01T00:00:00Z","bucket_end":"2026-06-01T01:00:00Z","model":"claude-sonnet-4-7","cost_usd":100.0}],"has_more":false}`
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	logger, capH := mkCapturingLogger()
	app := &fakeAppender{}
	r := NewReconciler(Config{
		Clock:                  frozenClock(fixedTime()),
		HTTPClient:             srv.Client(),
		BaseURL:                srv.URL,
		BucketWidth:            time.Hour,
		DriftAlertThresholdPct: 10,
		UsageAPIKeyEnv:         adminKeyEnv,
		Appender:               app,
		RecordedReader:         mkRecorder(t, 80), // drift=20% (above 10%)
		TenantID:               "default",
		Logger:                 logger,
	})
	if err := r.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if capH.eventCounts()[string(obs.EventCostDriftAlert)] != 1 {
		t.Errorf("expected exactly 1 EventCostDriftAlert; got=%v", capH.eventCounts())
	}
	if len(app.snapshot()) != 1 {
		t.Errorf("len(rows)=%d want 1", len(app.snapshot()))
	}
}

func TestReconciler_AdminKeyUnset_LogsAndSkips(t *testing.T) {
	// Explicitly unset.
	t.Setenv(adminKeyEnv, "")

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		_, _ = w.Write(mustReadTestdata(t, "anthropic_cost_2026_06_01_01h.json"))
	}))
	t.Cleanup(srv.Close)

	logger, capH := mkCapturingLogger()
	app := &fakeAppender{}
	r := NewReconciler(Config{
		Clock:                  frozenClock(fixedTime()),
		HTTPClient:             srv.Client(),
		BaseURL:                srv.URL,
		BucketWidth:            time.Hour,
		DriftAlertThresholdPct: 10,
		UsageAPIKeyEnv:         adminKeyEnv,
		Appender:               app,
		RecordedReader:         mkRecorder(t, 0),
		TenantID:               "default",
		Logger:                 logger,
	})
	err := r.Tick(context.Background())
	if !errors.Is(err, ErrAdminKeyUnset) {
		t.Fatalf("err=%v want ErrAdminKeyUnset", err)
	}
	if atomic.LoadInt64(&hits) != 0 {
		t.Errorf("hits=%d want 0 (no HTTP call when admin key unset)", hits)
	}
	if capH.eventCounts()[string(obs.EventCostReconcileSkipped)] == 0 {
		t.Errorf("expected EventCostReconcileSkipped; got=%v", capH.eventCounts())
	}
	if len(app.snapshot()) != 0 {
		t.Errorf("len(rows)=%d want 0", len(app.snapshot()))
	}
}

// TestReconciler_TickWindowQueryParamsMatchSpec complements window_test.go by pinning Tick-side HTTP query params.
func TestReconciler_TickWindowQueryParamsMatchSpec(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	var seenQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		_, _ = w.Write(mustReadTestdata(t, "anthropic_cost_empty.json"))
	}))
	t.Cleanup(srv.Close)
	app := &fakeAppender{}
	logger, _ := mkCapturingLogger()
	r := NewReconciler(Config{
		Clock:                  frozenClock(fixedTime()),
		HTTPClient:             srv.Client(),
		BaseURL:                srv.URL,
		BucketWidth:            time.Hour,
		DriftAlertThresholdPct: 10,
		UsageAPIKeyEnv:         adminKeyEnv,
		Appender:               app,
		RecordedReader:         mkRecorder(t, 0),
		TenantID:               "default",
		Logger:                 logger,
	})
	if err := r.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	for _, want := range []string{
		"starting_at=2026-06-01T00%3A00%3A00Z",
		"ending_at=2026-06-01T01%3A00%3A00Z",
		"bucket_width=1h",
	} {
		if !strings.Contains(seenQuery, want) {
			t.Errorf("query=%q missing %q", seenQuery, want)
		}
	}
}

// A-tier — spec §7 A3 + A4 + audit invariant.

// TestReconciler_DriftAlertDedupedAcrossTicks pins A4 — one alert per (period_start, drift@2dp).
func TestReconciler_DriftAlertDedupedAcrossTicks(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := `{"data":[{"bucket_start":"2026-06-01T00:00:00Z","bucket_end":"2026-06-01T01:00:00Z","model":"claude-sonnet-4-7","cost_usd":100.0}],"has_more":false}`
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	logger, capH := mkCapturingLogger()
	app := &fakeAppender{}
	r := NewReconciler(Config{
		Clock:                  frozenClock(fixedTime()),
		HTTPClient:             srv.Client(),
		BaseURL:                srv.URL,
		BucketWidth:            time.Hour,
		DriftAlertThresholdPct: 10,
		UsageAPIKeyEnv:         adminKeyEnv,
		Appender:               app,
		RecordedReader:         mkRecorder(t, 80), // drift=20%
		TenantID:               "default",
		Logger:                 logger,
	})
	for i := 0; i < 3; i++ {
		if err := r.Tick(context.Background()); err != nil {
			t.Fatalf("Tick #%d: %v", i, err)
		}
	}
	if got := capH.eventCounts()[string(obs.EventCostDriftAlert)]; got != 1 {
		t.Errorf("EventCostDriftAlert count=%d want 1 (deduped)", got)
	}
	// All 3 ticks still emit budget_reconciled rows (LWW lets the
	// substrate sort it out — see test 11).
	if len(app.snapshot()) != 3 {
		t.Errorf("len(rows)=%d want 3", len(app.snapshot()))
	}
}

// TestReconciler_429Backoff_RespectsRetryAfterHeader pins R3 + A3 — total wait ≥ 3×retry-after.
func TestReconciler_429Backoff_RespectsRetryAfterHeader(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	var attempt int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt64(&attempt, 1)
		if n <= 3 {
			w.Header().Set("retry-after", "12")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(mustReadTestdata(t, "anthropic_cost_2026_06_01_01h.json"))
	}))
	t.Cleanup(srv.Close)

	var sleepCalls []time.Duration
	var sleepMu sync.Mutex
	app := &fakeAppender{}
	logger, _ := mkCapturingLogger()
	r := NewReconciler(Config{
		Clock:                  frozenClock(fixedTime()),
		HTTPClient:             srv.Client(),
		BaseURL:                srv.URL,
		BucketWidth:            time.Hour,
		DriftAlertThresholdPct: 10,
		UsageAPIKeyEnv:         adminKeyEnv,
		Appender:               app,
		RecordedReader:         mkRecorder(t, 13.25),
		TenantID:               "default",
		Logger:                 logger,
		Sleep: func(d time.Duration) {
			sleepMu.Lock()
			sleepCalls = append(sleepCalls, d)
			sleepMu.Unlock()
		},
	})
	if err := r.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if atomic.LoadInt64(&attempt) != 4 {
		t.Errorf("attempt=%d want 4", attempt)
	}
	sleepMu.Lock()
	total := time.Duration(0)
	for _, d := range sleepCalls {
		total += d
	}
	sleepMu.Unlock()
	if total < 36*time.Second {
		t.Errorf("total sleep=%v want >= 36s (3 × retry-after=12s)", total)
	}
}

// TestReconciler_Network5xx_KeepsTickingAndNeverPanics pins persistent-5xx sentinel + no goroutine leak.
func TestReconciler_Network5xx_KeepsTickingAndNeverPanics(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	app := &fakeAppender{}
	logger, capH := mkCapturingLogger()
	r := NewReconciler(Config{
		Clock:                  frozenClock(fixedTime()),
		HTTPClient:             srv.Client(),
		BaseURL:                srv.URL,
		BucketWidth:            time.Hour,
		DriftAlertThresholdPct: 10,
		UsageAPIKeyEnv:         adminKeyEnv,
		Appender:               app,
		RecordedReader:         mkRecorder(t, 0),
		TenantID:               "default",
		Logger:                 logger,
		Sleep:                  func(time.Duration) {},
	})

	before := runtime.NumGoroutine()
	for i := 0; i < 5; i++ {
		err := r.Tick(context.Background())
		if !errors.Is(err, ErrUpstreamPersistent5xx) {
			t.Fatalf("Tick #%d err=%v want ErrUpstreamPersistent5xx", i, err)
		}
	}
	// httptest may park transient handler goroutines briefly; GC +
	// Gosched until the count stops shrinking so the no-leak invariant
	// is structural (delta bounded), not race-y (delta exactly zero).
	after := gcSettleGoroutines()
	if delta := after - before; delta > 5 {
		t.Errorf("goroutine delta=%d want <= 5 (no per-tick growth)", delta)
	}
	if capH.eventCounts()[string(obs.EventCostReconcileFailing)] == 0 {
		t.Errorf("expected EventCostReconcileFailing; got=%v", capH.eventCounts())
	}
	// Issue #289 — the failing emit must carry period_start +
	// attempt_count so the OTel bridge surfaces the same join keys to
	// Honeycomb/Loki/Datadog as the happy-path BudgetReconciledPayload.
	// Issue #439 — exhausted-retry path reports the full attempt budget.
	wantStart, _ := WindowForTick(fixedTime(), time.Hour)
	assertFailingEventAttrs(t, capH.snapshot(), wantStart.UnixMilli(), "upstream_down", int64(defaultRetryAttempts))
}

// TestReconciler_ImmediateFail_AttemptCountIsActual pins issue #439: non-exhaustion failures report the real attempt count, not defaultRetryAttempts.
func TestReconciler_ImmediateFail_AttemptCountIsActual(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	// HTTP 200 + malformed JSON triggers a decode error inside
	// FetchCost. fetchWithBackoff treats unknown errors as non-retryable
	// and returns after the first attempt — attempt_count must be 1.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "not-json{")
	}))
	t.Cleanup(srv.Close)

	app := &fakeAppender{}
	logger, capH := mkCapturingLogger()
	r := NewReconciler(Config{
		Clock:                  frozenClock(fixedTime()),
		HTTPClient:             srv.Client(),
		BaseURL:                srv.URL,
		BucketWidth:            time.Hour,
		DriftAlertThresholdPct: 10,
		UsageAPIKeyEnv:         adminKeyEnv,
		Appender:               app,
		RecordedReader:         mkRecorder(t, 0),
		TenantID:               "default",
		Logger:                 logger,
		Sleep:                  func(time.Duration) {},
	})
	if err := r.Tick(context.Background()); err == nil {
		t.Fatal("Tick err=nil want non-nil (decode failure path)")
	}
	wantStart, _ := WindowForTick(fixedTime(), time.Hour)
	assertFailingEventAttrs(t, capH.snapshot(), wantStart.UnixMilli(), "upstream_down", 1)
}

// assertFailingEventAttrs scans capH for the cost.reconcile_failing record and pins period_start, reason, attempt_count.
func assertFailingEventAttrs(t *testing.T, recs []slog.Record, wantStart int64, wantReason string, wantAttempts int64) {
	t.Helper()
	for _, r := range recs {
		if r.Message != string(obs.EventCostReconcileFailing) {
			continue
		}
		attrs := map[string]slog.Value{}
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value
			return true
		})
		if v, ok := attrs[string(obs.KeyPeriodStart)]; !ok || v.Int64() != wantStart {
			t.Errorf("period_start attr: want %d got (%v, found=%v)", wantStart, v, ok)
		}
		if v, ok := attrs[string(obs.KeyAttemptCount)]; !ok || v.Int64() != wantAttempts {
			t.Errorf("attempt_count attr: want %d got (%v, found=%v)", wantAttempts, v, ok)
		}
		if v, ok := attrs[string(obs.KeyReason)]; !ok || v.String() != wantReason {
			t.Errorf("reason attr: want %q got (%v, found=%v)", wantReason, v, ok)
		}
		return
	}
	t.Errorf("no %q record in capture", obs.EventCostReconcileFailing)
}

// TestReconciler_AnthropicResponseSig_IsSHA256OfCanonicalBody pins A2 audit-replay invariant.
func TestReconciler_AnthropicResponseSig_IsSHA256OfCanonicalBody(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	body := mustReadTestdata(t, "anthropic_cost_2026_06_01_01h.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	app := &fakeAppender{}
	logger, _ := mkCapturingLogger()
	r := NewReconciler(Config{
		Clock:                  frozenClock(fixedTime()),
		HTTPClient:             srv.Client(),
		BaseURL:                srv.URL,
		BucketWidth:            time.Hour,
		DriftAlertThresholdPct: 10,
		UsageAPIKeyEnv:         adminKeyEnv,
		Appender:               app,
		RecordedReader:         mkRecorder(t, 13.25),
		TenantID:               "default",
		Logger:                 logger,
	})
	if err := r.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	rows := app.snapshot()
	if len(rows) != 1 {
		t.Fatalf("len(rows)=%d", len(rows))
	}
	var p spend.BudgetReconciledPayload
	if err := json.Unmarshal(rows[0].payload, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := canonicalSHA256(t, body)
	if p.APIResponseSig != want {
		t.Errorf("APIResponseSig=%s want %s", p.APIResponseSig, want)
	}
}

func canonicalSHA256(t *testing.T, body []byte) string {
	t.Helper()
	canonical, err := canon.CanonicaliseJSON(body)
	if err != nil {
		t.Fatalf("canonicalise: %v", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// A+-tier — spec §7 A+ + R15 + LWW pin.

// TestReconciler_LWWCorrectionEmitsNewRow pins R6 — two raw rows per period; Fold picks later.
func TestReconciler_LWWCorrectionEmitsNewRow(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)
	var attempt int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt64(&attempt, 1)
		// First Tick sees partial bucket; second Tick sees corrected total.
		if n == 1 {
			body := `{"data":[{"bucket_start":"2026-06-01T00:00:00Z","bucket_end":"2026-06-01T01:00:00Z","model":"claude-sonnet-4-7","cost_usd":50.0}],"has_more":false}`
			_, _ = io.WriteString(w, body)
			return
		}
		body := `{"data":[{"bucket_start":"2026-06-01T00:00:00Z","bucket_end":"2026-06-01T01:00:00Z","model":"claude-sonnet-4-7","cost_usd":75.0}],"has_more":false}`
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	app := &fakeAppender{}
	logger, _ := mkCapturingLogger()
	r := NewReconciler(Config{
		Clock:                  frozenClock(fixedTime()),
		HTTPClient:             srv.Client(),
		BaseURL:                srv.URL,
		BucketWidth:            time.Hour,
		DriftAlertThresholdPct: 10,
		UsageAPIKeyEnv:         adminKeyEnv,
		Appender:               app,
		RecordedReader:         mkRecorder(t, 70),
		TenantID:               "default",
		Logger:                 logger,
	})
	if err := r.Tick(context.Background()); err != nil {
		t.Fatalf("Tick #1: %v", err)
	}
	if err := r.Tick(context.Background()); err != nil {
		t.Fatalf("Tick #2: %v", err)
	}
	rows := app.snapshot()
	if len(rows) != 2 {
		t.Fatalf("len(rows)=%d want 2 (LWW per substrate §4)", len(rows))
	}
	var first, second spend.BudgetReconciledPayload
	_ = json.Unmarshal(rows[0].payload, &first)
	_ = json.Unmarshal(rows[1].payload, &second)
	if first.PeriodStart != second.PeriodStart {
		t.Errorf("both rows must share period_start; got %d vs %d", first.PeriodStart, second.PeriodStart)
	}
	if first.ActualUSD == second.ActualUSD {
		t.Errorf("ActualUSD did not advance on Tick #2; got %v vs %v", first.ActualUSD, second.ActualUSD)
	}
	if second.ActualUSD != 75 {
		t.Errorf("second ActualUSD=%v want 75", second.ActualUSD)
	}
}

// TestReconciler_NeverLogsKeyValue pins R15 across 6 error paths × text+JSON handlers.
func TestReconciler_NeverLogsKeyValue(t *testing.T) {
	t.Setenv(adminKeyEnv, adminKeyFixture)

	mkSrv := func(status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("retry-after", "1")
			w.WriteHeader(status)
		}))
	}

	// Each case runs against a fresh server returning the given status.
	// network-down is modelled by closing the server before Tick.
	cases := []struct {
		name   string
		status int
		closed bool
	}{
		{"401", 401, false},
		{"403", 403, false},
		{"404", 404, false},
		{"429", 429, false},
		{"500", 500, false},
		{"network-down", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var srv *httptest.Server
			var base string
			if c.closed {
				srv = mkSrv(500)
				base = srv.URL
				srv.Close()
			} else {
				srv = mkSrv(c.status)
				t.Cleanup(srv.Close)
				base = srv.URL
			}

			// Use stderr-routed JSON handler so we capture both human
			// and structured form; R15 means BOTH paths stay clean.
			var buf bytes.Buffer
			textHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
			structuredHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
			logger := slog.New(slogMulti{textHandler, structuredHandler})

			app := &fakeAppender{}
			r := NewReconciler(Config{
				Clock:                  frozenClock(fixedTime()),
				HTTPClient:             &http.Client{Timeout: 2 * time.Second},
				BaseURL:                base,
				BucketWidth:            time.Hour,
				DriftAlertThresholdPct: 10,
				UsageAPIKeyEnv:         adminKeyEnv,
				Appender:               app,
				RecordedReader:         mkRecorder(t, 0),
				TenantID:               "default",
				Logger:                 logger,
				Sleep:                  func(time.Duration) {},
			})
			_ = r.Tick(context.Background())
			if strings.Contains(buf.String(), adminKeyFixture) {
				t.Errorf("admin key fixture leaked into log output for case %s\n%s", c.name, buf.String())
			}
		})
	}
}

// slogMulti fans a slog record to multiple handlers — used by R15 to
// pin both text + JSON shapes.
type slogMulti []slog.Handler

func (m slogMulti) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range m {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}
func (m slogMulti) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m {
		_ = h.Handle(ctx, r.Clone())
	}
	return nil
}
func (m slogMulti) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(slogMulti, len(m))
	for i, h := range m {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}
func (m slogMulti) WithGroup(name string) slog.Handler {
	out := make(slogMulti, len(m))
	for i, h := range m {
		out[i] = h.WithGroup(name)
	}
	return out
}

// gcSettleGoroutines drives GC + Gosched until runtime.NumGoroutine stops
// shrinking across two consecutive samples, returning the settled count.
// Replaces a bare time.Sleep settle window with a state-driven loop bounded
// by a hard pass cap so a true leak cannot loop forever.
func gcSettleGoroutines() int {
	const maxPasses = 32
	prev := runtime.NumGoroutine()
	for i := 0; i < maxPasses; i++ {
		runtime.GC()
		runtime.Gosched()
		cur := runtime.NumGoroutine()
		if cur >= prev {
			return cur
		}
		prev = cur
	}
	return prev
}
