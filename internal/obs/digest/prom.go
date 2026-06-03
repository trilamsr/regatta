package digest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// PromSource pulls digest inputs from a Prometheus HTTP API. The query
// shape (instant vector at `time=now`) is the lowest-cardinality path —
// each digest field maps to one PromQL string and the renderer never
// sees the wire format. Unreachable backend returns backendUp=false (no
// error) so the renderer's R5-mirror banner can fire instead of the
// digest failing outright. UX > velocity: a stale digest with a banner
// beats no digest at all.
type PromSource struct {
	baseURL string
	client  *http.Client
}

// NewPromSource returns a PromSource pointed at baseURL (e.g.
// "http://prom:9090"). Caller-supplied URL keeps env-resolution at the
// CLI boundary.
func NewPromSource(baseURL string) *PromSource {
	return &PromSource{
		baseURL: baseURL,
		// 5s per query is generous for an instant vector against a
		// healthy Prom; tighter than the default http client (none).
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// promQueryResult is the documented Prom HTTP API v1 instant-vector
// envelope. We only consume the scalar fields the digest needs.
type promQueryResult struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// Fetch issues one query per digest field. Failures on individual
// queries fall back to zero values rather than aborting the digest —
// per spec §6.2 the operator gets partial data with a banner rather
// than no digest at all. A total dial failure (any single query) flips
// backendUp to false so the renderer banner fires.
func (s *PromSource) Fetch(date string) (Snapshot, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// AND-fold `up` across all 3 queries — a partial outage (cost-today
	// 200 + tick_p95 404) would otherwise render zeros as authoritative
	// with no backend-down banner. Short-circuit on the first query so
	// a fully-unreachable Prom still skips the remaining round-trips.
	costToday, up := s.queryScalar(ctx, "sum(increase(regatta_cost_usd_total[24h]))")
	if !up {
		return Snapshot{}, false, nil
	}
	costWeek, upWeek := s.queryScalar(ctx, "sum(increase(regatta_cost_usd_total[7d]))")
	tickP95, upTick := s.queryScalar(ctx, "histogram_quantile(0.95, sum(rate(regatta_scheduler_tick_latency_ms_bucket[1h])) by (le))")
	backendUp := up && upWeek && upTick

	return Snapshot{
		TickP95Ms:    int(tickP95),
		CostUSDToday: costToday,
		CostUSDWeek:  costWeek,
		// PRsLandedCount / Adversarial / Substrate / Triggers / Followups
		// stay at zero values until the C-T2 / D-T1 emitters and the
		// trigger-clock (A-T?) land. The renderer's degraded-contract
		// placeholders cover sections 2 + 3.
	}, backendUp, nil
}

// queryScalar runs an instant query and returns the first sample value
// as a float, with `up` reflecting whether the HTTP call reached Prom.
// Empty result sets return (0, true) — "Prom is up but the metric has
// not been emitted yet" is the expected pre-emitter state.
func (s *PromSource) queryScalar(ctx context.Context, query string) (float64, bool) {
	u := s.baseURL + "/api/v1/query?query=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, false
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		// Any non-2xx flips backendUp=false. 404 in particular catches
		// an operator typo in DIGEST_PROM_URL — silently returning
		// up=true on 404 would hide the misconfig behind all-zero
		// metrics rendered as authoritative.
		return 0, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, true
	}
	var pr promQueryResult
	if err := json.Unmarshal(body, &pr); err != nil {
		return 0, true
	}
	if pr.Status != "success" || len(pr.Data.Result) == 0 {
		return 0, true
	}
	val := pr.Data.Result[0].Value[1]
	str, ok := val.(string)
	if !ok {
		return 0, true
	}
	f, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return 0, true
	}
	// NaN / ±Inf sanitize to zero at the wire boundary. Prom emits
	// these legitimately (empty histogram → histogram_quantile = NaN;
	// zero-rate division → +Inf) but the renderer would either spit
	// "NaN"/"+Inf" through %.2f (invalid YAML in strict parsers) or
	// saturate int(+Inf) to MaxInt64 in tick_p95_ms. Treat as
	// missing-metric: same shape as the empty-result-set branch above.
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, true
	}
	return f, true
}

// errEmptyEndpoint signals NewPromSourceFromEnv was called without an
// endpoint env var; cmd-layer callers branch to a no-source path.
var errEmptyEndpoint = errors.New("digest: no Prom endpoint env var set")

// NewPromSourceFromEnv resolves a PromSource from the env-var stack the
// operator-facing OTel exporters already use. Returns errEmptyEndpoint
// when nothing is set; the cmd layer falls through to a noop source so
// the digest still renders (with the backend-down banner).
func NewPromSourceFromEnv(getenv func(string) string) (*PromSource, error) {
	// Priority: explicit DIGEST_PROM_URL (operator override) > the
	// shared OTLP endpoint > the Prom port the SetupMeter exporter
	// binds locally. Last form assumes the cron runs on the same host
	// as serve.
	if v := getenv("DIGEST_PROM_URL"); v != "" {
		return NewPromSource(v), nil
	}
	if v := getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"); v != "" {
		return NewPromSource(v), nil
	}
	if v := getenv("OTEL_METRICS_PROMETHEUS_PORT"); v != "" {
		return NewPromSource(fmt.Sprintf("http://127.0.0.1:%s", v)), nil
	}
	return nil, errEmptyEndpoint
}

// IsNoEndpoint reports whether err signals "no Prom endpoint configured"
// so cmd-layer callers can branch to the noop-source path without
// importing the sentinel.
func IsNoEndpoint(err error) bool { return errors.Is(err, errEmptyEndpoint) }
