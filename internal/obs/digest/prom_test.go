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
