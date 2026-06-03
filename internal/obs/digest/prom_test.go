package digest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPromSource_FetchHappyPath locks the spec §6.2 PromQL → Snapshot mapping against a stub HTTP backend.
func TestPromSource_FetchHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		switch {
		case strings.Contains(q, "regatta_cost_usd_total"):
			// Today vs week-to-date both hit the same emitter; the
			// stub returns a scalar so the digest body sees a number.
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1717286400,"87.42"]}]}}`))
		case strings.Contains(q, "regatta_scheduler_tick_latency_ms"):
			// T3 emitter — when present in Prom, the source picks the
			// p95 quantile. When missing, source falls back to zero
			// (covered by the unreachable / empty-vector test below).
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1717286400,"4321"]}]}}`))
		default:
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		}
	}))
	defer srv.Close()

	src := NewPromSource(srv.URL)
	snap, up, err := src.Fetch("2026-06-03")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !up {
		t.Errorf("backendUp = false; want true on 2xx response")
	}
	if snap.CostUSDToday <= 0 {
		t.Errorf("CostUSDToday = %v; want >0 from stub", snap.CostUSDToday)
	}
}

// TestPromSource_FetchBackendDown locks the soft-fail contract: dial failure → backendUp=false, no error.
func TestPromSource_FetchBackendDown(t *testing.T) {
	// Use a guaranteed-unreachable URL — port 1 is reserved and any
	// dial will fail immediately on a normal host.
	src := NewPromSource("http://127.0.0.1:1")
	_, up, err := src.Fetch("2026-06-03")
	if err != nil {
		t.Fatalf("Fetch should soft-fail; got err: %v", err)
	}
	if up {
		t.Errorf("backendUp = true; want false on dial failure")
	}
}

// TestPromSource_FetchSanitizesNaN locks the sentinel: Prom returning NaN must not propagate into the Snapshot.
func TestPromSource_FetchSanitizesNaN(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prom emits literal "NaN" for empty histograms (e.g. zero
		// observations under histogram_quantile). Renderer would
		// otherwise spit "NaN" through %.2f / convert to MinInt64 via
		// int() — both poison the YAML front-matter.
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1717286400,"NaN"]}]}}`))
	}))
	defer srv.Close()

	src := NewPromSource(srv.URL)
	snap, up, err := src.Fetch("2026-06-03")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !up {
		t.Fatalf("backendUp = false; want true (stub returns 200)")
	}
	if snap.TickP95Ms != 0 {
		t.Errorf("TickP95Ms = %d; want 0 when Prom returns NaN", snap.TickP95Ms)
	}
	if snap.CostUSDToday != 0 {
		t.Errorf("CostUSDToday = %v; want 0 when Prom returns NaN", snap.CostUSDToday)
	}
	if snap.CostUSDWeek != 0 {
		t.Errorf("CostUSDWeek = %v; want 0 when Prom returns NaN", snap.CostUSDWeek)
	}
}

// TestPromSource_FetchSanitizesInf locks the sentinel: Prom returning +Inf must not propagate (int(Inf)→MaxInt64, %.2f→"+Inf").
func TestPromSource_FetchSanitizesInf(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1717286400,"+Inf"]}]}}`))
	}))
	defer srv.Close()

	src := NewPromSource(srv.URL)
	snap, up, err := src.Fetch("2026-06-03")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !up {
		t.Fatalf("backendUp = false; want true (stub returns 200)")
	}
	if snap.TickP95Ms != 0 {
		t.Errorf("TickP95Ms = %d; want 0 when Prom returns +Inf (raw int(+Inf) = MaxInt64)", snap.TickP95Ms)
	}
	if snap.CostUSDToday != 0 {
		t.Errorf("CostUSDToday = %v; want 0 when Prom returns +Inf", snap.CostUSDToday)
	}
	if snap.CostUSDWeek != 0 {
		t.Errorf("CostUSDWeek = %v; want 0 when Prom returns +Inf", snap.CostUSDWeek)
	}
}

// TestPromSource_Fetch404IsBackendDown locks the misconfig signal: 404 (operator URL typo) must flip backendUp=false.
func TestPromSource_Fetch404IsBackendDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	src := NewPromSource(srv.URL)
	_, up, err := src.Fetch("2026-06-03")
	if err != nil {
		t.Fatalf("Fetch should soft-fail; got err: %v", err)
	}
	if up {
		t.Errorf("backendUp = true; want false on 404 (operator typo'd DIGEST_PROM_URL → renderer needs banner)")
	}
}

// TestPromSource_Fetch5xxIsBackendDown locks the dual non-2xx contract: 5xx flips backendUp=false (paired with 404).
func TestPromSource_Fetch5xxIsBackendDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	src := NewPromSource(srv.URL)
	_, up, err := src.Fetch("2026-06-03")
	if err != nil {
		t.Fatalf("Fetch should soft-fail; got err: %v", err)
	}
	if up {
		t.Errorf("backendUp = true; want false on 500")
	}
}

// TestPromSource_FetchPartialOutageTickP95 locks issue #512: 404 on tick_p95 query (cost-today 200) must flip backendUp=false.
func TestPromSource_FetchPartialOutageTickP95(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if strings.Contains(q, "regatta_scheduler_tick_latency_ms") {
			// Histogram endpoint missing — operator removed the recording
			// rule but cost-today is still emitted. Without AND-folding
			// `up` across all 3 queries, the digest renders zeros and
			// claims the backend is healthy.
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1717286400,"87.42"]}]}}`))
	}))
	defer srv.Close()

	src := NewPromSource(srv.URL)
	_, up, err := src.Fetch("2026-06-03")
	if err != nil {
		t.Fatalf("Fetch should soft-fail; got err: %v", err)
	}
	if up {
		t.Errorf("backendUp = true; want false on partial outage (cost-today 200 + tick_p95 404)")
	}
}

// TestPromSource_FetchPartialOutageCostWeek locks issue #512: 500 on cost-week query (cost-today 200) must flip backendUp=false.
func TestPromSource_FetchPartialOutageCostWeek(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// First call is cost-today (24h) — return success. Second call
		// is cost-week (7d) which uses the same metric; fail it with
		// 500 to model a partial recording-rule outage.
		if calls == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1717286400,"87.42"]}]}}`))
	}))
	defer srv.Close()

	src := NewPromSource(srv.URL)
	_, up, err := src.Fetch("2026-06-03")
	if err != nil {
		t.Fatalf("Fetch should soft-fail; got err: %v", err)
	}
	if up {
		t.Errorf("backendUp = true; want false on partial outage (cost-today 200 + cost-week 500)")
	}
}

// TestPromSource_FetchSanitizesNegInf locks the symmetric guard against -Inf
// (G1 finding). int(-math.Inf) saturates to
// MinInt64; %.2f formats to "-Inf" which is invalid YAML in strict parsers.
// Future narrowing of the IsInf check to (f, 1) would let -Inf slip through —
// this test fails before that regression ships.
func TestPromSource_FetchSanitizesNegInf(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1717286400,"-Inf"]}]}}`))
	}))
	defer srv.Close()

	src := NewPromSource(srv.URL)
	snap, up, err := src.Fetch("2026-06-03")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !up {
		t.Fatalf("backendUp = false; want true (stub returns 200)")
	}
	if snap.TickP95Ms != 0 {
		t.Errorf("TickP95Ms = %d; want 0 when Prom returns -Inf (raw int(-Inf) = MinInt64)", snap.TickP95Ms)
	}
	if snap.CostUSDToday != 0 {
		t.Errorf("CostUSDToday = %v; want 0 when Prom returns -Inf", snap.CostUSDToday)
	}
	if snap.CostUSDWeek != 0 {
		t.Errorf("CostUSDWeek = %v; want 0 when Prom returns -Inf", snap.CostUSDWeek)
	}
}
