package reconcile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/cost/pricing"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/obs"
)

// ErrUpstreamPersistent5xx is the sentinel returned by Tick when 5xx
// errors have crossed the persistent-failure threshold (spec §3.4
// failure-mode table line 248: 5 consecutive attempts). Run() keeps
// looping; ad-hoc Tick callers see the error and decide.
var ErrUpstreamPersistent5xx = errors.New("upstream 5xx persistent")

// ErrUpstreamPersistent429 is the sentinel for sustained rate-limit
// failures. Same dispatch contract as ErrUpstreamPersistent5xx.
var ErrUpstreamPersistent429 = errors.New("upstream 429 persistent")

// defaultRetryAttempts is the persistent-failure threshold from spec
// §3.4 line 247-248. Hard-coded rather than configured because the
// number drives operator-facing failure semantics and is part of the
// observability contract — operators tune via reconcile_interval, not
// retry attempts.
const defaultRetryAttempts = 5

// Appender is the substrate-write seam. Production wires this to
// substrate.AppendEvent inside a tx; tests inject a fake that records
// rows in-memory. Decoupling here keeps T4 file-disjoint from T3's
// validator update — the reconciler emits payload bytes, the substrate
// layer accepts them, the spec §3.5 shape is the contract.
type Appender interface {
	Append(ctx context.Context, tenantID, kind string, payload json.RawMessage, writtenAt time.Time) error
}

// RecordedReader returns the locally-recorded spend (micro-USD) for
// the reconcile window; func-shape lets tests inject deterministic
// values while production passes (*spend.Reader).RecordedUSDForWindow.
type RecordedReader func(ctx context.Context, tenantID string, start, end time.Time) (spend.USDMicro, error)

// Config holds the Reconciler's wiring. Mirrors spec §3.4 + plan §3
// "T4 exports reconcile.Config" verbatim.
type Config struct {
	// Clock is the wall-clock source used to compute the reconcile
	// window. Tests inject frozen-time closures; production passes
	// time.Now.
	Clock func() time.Time

	// HTTPClient is shared with the Client below; injection lets tests
	// swap httptest.Server's Client transparently.
	HTTPClient *http.Client

	// BaseURL points at the Anthropic admin endpoint. Tests swap to
	// httptest.Server URL.
	BaseURL string

	// BucketWidth pins the Anthropic bucket size; spec default 1h.
	BucketWidth time.Duration

	// ReconcileInterval drives Run()'s sleep cadence. Tick() does not
	// read this; the interval is operator-tunable per spec §3.4 line 224.
	ReconcileInterval time.Duration

	// DriftAlertThresholdPct is the % above which a drift alert fires.
	// 10 means drift_pct > 0.10 triggers a WARN. Spec §3.4 line 240.
	DriftAlertThresholdPct float64

	// UsageAPIKeyEnv names the env var holding the admin API key.
	// Default ANTHROPIC_ADMIN_KEY.
	UsageAPIKeyEnv string

	// UserAgent for the outbound HTTP request — Anthropic recommends
	// integrations set this for usage-pattern attribution.
	UserAgent string

	// Tracer mirrors W6's normalisation pattern: nil falls back to
	// otel.Tracer("internal/cost/reconcile") which resolves to the
	// global provider (noop until obs/otel.Setup runs).
	Tracer trace.Tracer

	// Logger emits the cost.reconcile_* + cost.drift_alert events.
	// nil falls back to slog.Default().
	Logger *slog.Logger

	// Appender is the substrate-write seam. Must be non-nil; the
	// reconciler is useless without it.
	Appender Appender

	// RecordedReader returns the cumulative locally-recorded spend
	// for the window. Must be non-nil.
	RecordedReader RecordedReader

	// TenantID scopes the substrate writes. Production passes
	// substrate.DefaultTenantID until W8 ships tenant propagation.
	TenantID string

	// Sleep is the seam tests use to assert backoff delays without
	// real time.Sleep. nil falls back to time.Sleep.
	Sleep func(time.Duration)

	// Pricing is used ONLY on the Usage API fallback path — when the
	// Cost API is unavailable, we apply pricing locally per spec
	// §3.4 line 236. nil falls back to pricing.Lookup.
	Pricing func(model string) (pricing.Row, error)
}

// Reconciler runs the post-hoc reconciliation tick.
type Reconciler struct {
	cfg    Config
	client *Client
	tracer trace.Tracer
	log    *slog.Logger
	sleep  func(time.Duration)

	// alertMu guards alertDedupe. Tick() can in principle be called
	// concurrently — spec §3.4 calls out the dedup invariant as A4.
	alertMu     sync.Mutex
	alertDedupe map[string]float64
}

// NewReconciler constructs a Reconciler. All nil-defaults run here so
// callsites never check before use.
func NewReconciler(cfg Config) *Reconciler {
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.BucketWidth == 0 {
		cfg.BucketWidth = time.Hour
	}
	if cfg.DriftAlertThresholdPct <= 0 {
		cfg.DriftAlertThresholdPct = 10
	}
	if cfg.UsageAPIKeyEnv == "" {
		cfg.UsageAPIKeyEnv = defaultAdminKeyEnv
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("internal/cost/reconcile")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	if cfg.Pricing == nil {
		cfg.Pricing = pricing.Lookup
	}
	client := NewClient(ClientConfig{
		BaseURL:        cfg.BaseURL,
		HTTPClient:     cfg.HTTPClient,
		UsageAPIKeyEnv: cfg.UsageAPIKeyEnv,
		UserAgent:      cfg.UserAgent,
	})
	return &Reconciler{
		cfg:         cfg,
		client:      client,
		tracer:      tracer,
		log:         log,
		sleep:       sleep,
		alertDedupe: map[string]float64{},
	}
}

// Tick runs one reconciliation pass. Idempotent across re-invocations
// per A4 (drift-alert dedup by period_start + drift_pct@2dp).
//
// Failure-mode contract (spec §3.4):
//   - admin key unset → log WARN cost.reconcile_skipped, return ErrAdminKeyUnset
//   - 5xx persistent → log ERROR cost.reconcile_failing, return ErrUpstreamPersistent5xx
//   - 429 persistent → same, ErrUpstreamPersistent429
//   - Cost API 403/404 → fallback to Usage API + local pricing, emit
//     WARN cost.reconcile_fallback
func (r *Reconciler) Tick(ctx context.Context) error {
	now := r.cfg.Clock()
	start, end := WindowForTick(now, r.cfg.BucketWidth)

	spanCtx, span := r.tracer.Start(ctx, "cost.reconcile",
		trace.WithAttributes(
			attribute.Int64("regatta.cost.period_start", start.UnixMilli()),
			attribute.Int64("regatta.cost.period_end", end.UnixMilli()),
		),
	)
	defer span.End()

	// Try Cost API (preferred). On 403/404 fall back to Usage API.
	apiSource := "cost"
	costResp, body, attempts, err := r.fetchWithBackoff(spanCtx, func(c context.Context) ([]byte, *bytes.Buffer, error) {
		resp, b, e := r.client.FetchCost(c, start, end, r.cfg.BucketWidth)
		if e != nil {
			return nil, nil, e
		}
		buf := bytes.NewBuffer(nil)
		_ = json.NewEncoder(buf).Encode(resp) // unused; preserve shape
		return b, buf, nil
	})
	var actualUSD float64
	var modelBreakdown []spend.ModelBreakdownRow

	switch {
	case errors.Is(err, ErrAdminKeyUnset):
		r.log.WarnContext(spanCtx, string(obs.EventCostReconcileSkipped),
			slog.String(string(obs.KeyReason), "no_admin_key"),
			slog.String(string(obs.KeyEventName), string(obs.EventCostReconcileSkipped)),
		)
		span.SetAttributes(attribute.String("regatta.cost.api_source", "skipped"))
		return ErrAdminKeyUnset

	case errors.Is(err, ErrCostAPIUnavailable):
		r.log.WarnContext(spanCtx, string(obs.EventCostReconcileFallback),
			slog.String(string(obs.KeyReason), "cost_api_unavailable"),
			slog.String(string(obs.KeyEventName), string(obs.EventCostReconcileFallback)),
		)
		apiSource = "usage_fallback"
		usageResp, ub, uAttempts, ferr := r.fetchUsageWithBackoff(spanCtx, start, end)
		if ferr != nil {
			return r.persistentFailure(spanCtx, span, start, uAttempts, ferr)
		}
		body = ub
		actualUSD, modelBreakdown, err = r.usageToActualUSD(spanCtx, usageResp)
		if err != nil {
			return err
		}

	case err != nil:
		return r.persistentFailure(spanCtx, span, start, attempts, err)

	default:
		// Cost API happy path — Anthropic returned USD directly.
		for _, row := range costResp.Data {
			actualUSD += row.CostUSD
			modelBreakdown = append(modelBreakdown, spend.ModelBreakdownRow{
				Model:    row.Model,
				USDMicro: spend.FromUSD(row.CostUSD),
				USD:      row.CostUSD,
			})
		}
	}

	// Locally-recorded spend over the same window. Returned in
	// micro-USD (#554); convert at the diff-math boundary here — the
	// reconciler's drift_pct is a float ratio and the substrate row
	// dual-emits both.
	recordedMicro, rerr := r.cfg.RecordedReader(spanCtx, r.cfg.TenantID, start, end)
	if rerr != nil {
		return fmt.Errorf("recorded reader: %w", rerr)
	}
	recordedUSD := recordedMicro.USD()

	deltaUSD := actualUSD - recordedUSD
	driftPct := 0.0
	if actualUSD > 0 {
		driftPct = math.Abs(deltaUSD) / math.Max(actualUSD, 0.01)
	} else if recordedUSD > 0 {
		// Anthropic says 0, we say >0 — drift is undefined but the
		// signal still matters; force a non-zero drift so the alert
		// fires. Clamp to 1.0 (100%) so the dashboard math stays sane.
		driftPct = 1.0
	}

	span.SetAttributes(
		attribute.String("regatta.cost.api_source", apiSource),
		attribute.Float64("regatta.cost.drift_pct", driftPct),
	)

	// Compute the canonical sha256 of the response body for audit
	// replay. Canonical = decode then re-encode with sorted keys
	// (encoding/json sorts map keys alphabetically by default — we
	// pass through a generic map to force the canonical shape).
	sig, sigErr := canonicalHashHex(body)
	if sigErr != nil {
		return fmt.Errorf("canonicalise response: %w", sigErr)
	}

	payload := spend.BudgetReconciledPayload{
		PeriodStart:      start.UnixMilli(),
		PeriodEnd:        end.UnixMilli(),
		ActualUSDMicro:   spend.FromUSD(actualUSD),
		RecordedUSDMicro: recordedMicro,
		DeltaUSDMicro:    spend.FromUSD(actualUSD) - recordedMicro,
		ActualUSD:        actualUSD,
		RecordedUSD:      recordedUSD,
		DeltaUSD:         deltaUSD,
		DriftPct:         driftPct,
		ModelBreakdown:   modelBreakdown,
		APIResponseSig:   sig,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	if err := r.cfg.Appender.Append(spanCtx, r.cfg.TenantID, "budget_reconciled", raw, r.cfg.Clock()); err != nil {
		return fmt.Errorf("append budget_reconciled: %w", err)
	}

	// Drift alert: emit at most one per (period_start, drift_pct@2dp).
	if driftPct*100 > r.cfg.DriftAlertThresholdPct {
		r.maybeEmitDriftAlert(spanCtx, payload)
	}
	return nil
}

// Run is the long-loop driver. Sleeps until the next aligned tick,
// calls Tick, never gives up (per spec §3.4 + R6). Exits when ctx is
// done.
func (r *Reconciler) Run(ctx context.Context) error {
	if r.cfg.ReconcileInterval <= 0 {
		r.cfg.ReconcileInterval = time.Hour
	}
	for {
		now := r.cfg.Clock()
		next := NextTickTime(now, r.cfg.ReconcileInterval, 2*time.Minute)
		wait := next.Sub(now)
		if wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = r.Tick(ctx) // logging is part of Tick — we just keep going
	}
}

// fetchWithBackoff retries 429 + 5xx up to defaultRetryAttempts. On
// ErrAdminKeyUnset or ErrCostAPIUnavailable we return immediately —
// those are caller-handled signals, not retryable failures.
//
// Returned attempts counts the actual attempts made (1-indexed). On the
// admin-key / cost-api-unavailable shortcut paths it is the iteration
// the signal was seen on. On the exhaustion path it equals
// defaultRetryAttempts. The cost.reconcile_failing emit pins this value
// so dashboards joining on attempt_count see real numbers, not the
// retry-budget ceiling (issue #439).
func (r *Reconciler) fetchWithBackoff(ctx context.Context, fetcher func(context.Context) ([]byte, *bytes.Buffer, error)) (CostResponse, []byte, int, error) {
	backoff := NewBackoff(time.Second, 5*time.Minute)
	attempt := 0
	for ; attempt < defaultRetryAttempts; attempt++ {
		body, _, err := fetcher(ctx)
		if err == nil {
			var resp CostResponse
			if jerr := json.Unmarshal(body, &resp); jerr != nil {
				return CostResponse{}, nil, attempt + 1, fmt.Errorf("decode cost: %w", jerr)
			}
			return resp, body, attempt + 1, nil
		}
		if errors.Is(err, ErrAdminKeyUnset) || errors.Is(err, ErrCostAPIUnavailable) {
			return CostResponse{}, nil, attempt + 1, err
		}
		var rl *RateLimitedError
		if errors.As(err, &rl) {
			r.sleep(backoff.NextWithRetryAfter(rl.RetryAfter))
			continue
		}
		var up *UpstreamError
		if errors.As(err, &up) {
			r.sleep(backoff.Next())
			continue
		}
		// Unknown — propagate so the caller does not silently retry.
		return CostResponse{}, nil, attempt + 1, err
	}
	return CostResponse{}, nil, attempt, ErrUpstreamPersistent5xx
}

func (r *Reconciler) fetchUsageWithBackoff(ctx context.Context, start, end time.Time) (UsageResponse, []byte, int, error) {
	backoff := NewBackoff(time.Second, 5*time.Minute)
	attempt := 0
	for ; attempt < defaultRetryAttempts; attempt++ {
		resp, body, err := r.client.FetchUsage(ctx, start, end, r.cfg.BucketWidth)
		if err == nil {
			return resp, body, attempt + 1, nil
		}
		if errors.Is(err, ErrAdminKeyUnset) {
			return UsageResponse{}, nil, attempt + 1, err
		}
		var rl *RateLimitedError
		if errors.As(err, &rl) {
			r.sleep(backoff.NextWithRetryAfter(rl.RetryAfter))
			continue
		}
		var up *UpstreamError
		if errors.As(err, &up) {
			r.sleep(backoff.Next())
			continue
		}
		return UsageResponse{}, nil, attempt + 1, err
	}
	return UsageResponse{}, nil, attempt, ErrUpstreamPersistent5xx
}

// usageToActualUSD applies local pricing to the Usage API response.
// Per R-A4 caveat, this path is pricing-self-referential: if our
// pricing table is wrong, drift will appear zero even when actual
// billing differs. The fallback emit covers stream-json-parser drift
// only; the operator runbook documents the limitation.
func (r *Reconciler) usageToActualUSD(ctx context.Context, resp UsageResponse) (float64, []spend.ModelBreakdownRow, error) {
	var total float64
	var rows []spend.ModelBreakdownRow
	for _, b := range resp.Data {
		row, err := r.cfg.Pricing(b.Model)
		if err != nil {
			// Missing pricing on the fallback path — surface a single
			// alert and continue; the reconciler still writes a
			// best-effort row (R6 graceful degradation).
			r.log.WarnContext(ctx, string(obs.EventCostReconcileFallback),
				slog.String(string(obs.KeyReason), "pricing_missing"),
				slog.String("model", b.Model),
				slog.String(string(obs.KeyEventName), string(obs.EventCostReconcileFallback)),
			)
			continue
		}
		usd := perMillion(b.UncachedInputTokens, row.InputUSDPerMTok) +
			perMillion(b.CacheReadInputTokens, row.CacheReadUSDPerMTok) +
			perMillion(b.CacheCreationInputTokens, row.CacheCreationUSDPerMTok) +
			perMillion(b.OutputTokens, row.OutputUSDPerMTok)
		total += usd
		rows = append(rows, spend.ModelBreakdownRow{
			Model:               b.Model,
			InputTokens:         b.UncachedInputTokens,
			OutputTokens:        b.OutputTokens,
			CacheReadTokens:     b.CacheReadInputTokens,
			CacheCreationTokens: b.CacheCreationInputTokens,
			USDMicro:            spend.FromUSD(usd),
			USD:                 usd,
		})
	}
	return total, rows, nil
}

func perMillion(tokens int64, ratePerMTok float64) float64 {
	return float64(tokens) * ratePerMTok / 1_000_000.0
}

// persistentFailure handles the 5xx-or-429 exhaustion path and the
// non-retryable immediate-fail path (issue #439). The period_start +
// attempt_count attrs ride along so the OTel ERROR record (bridged via
// internal/obs/otel) carries the same join keys dashboards already use
// on the happy-path BudgetReconciledPayload row — operators reading
// Honeycomb/Loki/Datadog see the failed window without cross-referencing
// slog stderr (issue #289). attempts is the real number of attempts the
// retry loop made; dashboards alerting on attempt_count == retry budget
// or aggregating by attempt_count see the actual value, not the ceiling.
func (r *Reconciler) persistentFailure(ctx context.Context, span trace.Span, periodStart time.Time, attempts int, cause error) error {
	reason := "upstream_down"
	sentinel := ErrUpstreamPersistent5xx
	var rl *RateLimitedError
	if errors.As(cause, &rl) {
		reason = "rate_limited"
		sentinel = ErrUpstreamPersistent429
	}
	if errors.Is(cause, ErrUpstreamPersistent5xx) {
		reason = "upstream_down"
	}
	r.log.ErrorContext(ctx, string(obs.EventCostReconcileFailing),
		slog.String(string(obs.KeyReason), reason),
		slog.Int64(string(obs.KeyPeriodStart), periodStart.UnixMilli()),
		slog.Int64(string(obs.KeyAttemptCount), int64(attempts)),
		slog.String(string(obs.KeyEventName), string(obs.EventCostReconcileFailing)),
	)
	span.SetAttributes(attribute.String("regatta.cost.api_source", "failed"))
	return sentinel
}

// maybeEmitDriftAlert is the (period_start, drift_pct@2dp) dedup gate.
func (r *Reconciler) maybeEmitDriftAlert(ctx context.Context, p spend.BudgetReconciledPayload) {
	round := math.Round(p.DriftPct*100) / 100 // 2dp
	key := fmt.Sprintf("%d|%.2f", p.PeriodStart, round)
	r.alertMu.Lock()
	if prev, ok := r.alertDedupe[key]; ok && math.Abs(prev-round) < 0.005 {
		r.alertMu.Unlock()
		return
	}
	r.alertDedupe[key] = round
	r.alertMu.Unlock()
	r.log.WarnContext(ctx, string(obs.EventCostDriftAlert),
		slog.Float64("drift_pct", round),
		slog.Float64("actual_usd", p.ActualUSD),
		slog.Float64("recorded_usd", p.RecordedUSD),
		slog.Int64("period_start", p.PeriodStart),
		slog.String(string(obs.KeyEventName), string(obs.EventCostDriftAlert)),
	)
}

// canonicalHashHex returns the lowercase hex sha256 of the canonical
// JSON form of raw — the `api_response_sig` audit anchor (spec §3.5
// line 288 + A2). Routes through canon.CanonicaliseJSON so reconcile
// shares the same dup-key-rejecting canonicaliser as every other signed
// surface (issue #553 unified four forks).
func canonicalHashHex(raw []byte) (string, error) {
	c, err := canon.CanonicaliseJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(c)
	return hex.EncodeToString(sum[:]), nil
}
