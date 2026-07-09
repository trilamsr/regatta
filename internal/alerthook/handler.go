package alerthook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/ghclient"
	"github.com/trilamsr/regatta/internal/obs"
)

// WebhookAuthHeader names the inbound header carrying the shared secret.
// Network-localhost binding is not a defence — any local process can POST
// (R-MEGA-3 LIVE-12). Operators set WEBHOOK_AUTH_TOKEN in docker-compose
// and AlertManager mirrors it via webhook_configs[].http_config.
const (
	WebhookAuthHeader = "X-Webhook-Token" //nolint:gosec // header NAME, not a secret
	WebhookAuthEnv    = "WEBHOOK_AUTH_TOKEN"
)

// checkWebhookAuth fails-closed when WEBHOOK_AUTH_TOKEN is unset OR the
// inbound header does not match. constant-time compare is mandatory so
// an attacker cannot byte-by-byte time-attack the token.
func checkWebhookAuth(r *http.Request) bool {
	want := os.Getenv(WebhookAuthEnv)
	if want == "" {
		return false
	}
	got := r.Header.Get(WebhookAuthHeader)
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// MaxBodyBytes caps an incoming webhook body. 4 MiB covers any realistic
// AlertManager batch (the upstream default group_by produces payloads
// under 100 KiB; a large grouping with 10K alerts + annotations still
// fits comfortably) while staying well below anything an attacker could
// use to OOM the receiver. Oversize bodies surface as 500 so AlertManager
// retries — a 4xx would be treated as permanent and the alert dropped.
const MaxBodyBytes = 4 << 20

// mutexReapInterval and mutexReapTTL bound how aggressively idle alertname
// mutexes are evicted. The TTL is comfortably longer than any realistic
// AlertManager group_interval so a still-firing alert keeps its mutex
// pinned; the interval keeps each pass cheap even on a receiver tracking
// thousands of alertnames.
const (
	mutexReapInterval = 1 * time.Hour
	mutexReapTTL      = 24 * time.Hour
)

// alertnameLock wraps the per-alertname mutex with a lastSeenAt stamp the
// reaper consults to evict idle entries. UnixNano in atomic.Int64 keeps
// reads lock-free so the storm-collapse hot path pays no extra cost.
type alertnameLock struct {
	mu         sync.Mutex
	lastSeenAt atomic.Int64
}

// Handler is the HTTP handler that owns one AlertManager receiver
// endpoint. Wire one Handler per (repo, label-set); the dedup cache
// keys on alertname so two Handlers pointed at different repos do not
// share state.
type Handler struct {
	// Client is the GitHub seam. Required; the constructor enforces it.
	Client ghclient.Client
	// Logger is the slog drain. Nil falls back to slog.Default().
	Logger *slog.Logger
	// Meter is the OTel instrument factory. Nil resolves lazily at
	// ServeHTTP so a global MeterProvider swap (Setup-after-construct)
	// still takes effect.
	Meter metric.Meter
	// Tracer is the OTel tracer. Nil resolves lazily.
	Tracer trace.Tracer
	// Now is the clock seam. Nil = time.Now. Set by tests for
	// deterministic dedup-cache expiry.
	Now func() time.Time
	// CacheTTL overrides the dedup-cache TTL. Zero = 60s default.
	CacheTTL time.Duration

	initOnce sync.Once
	cache    *dedupCache

	// fallbackMeter overrides the package-level obs.Meter fallback used
	// when the primary Int64Counter call fails. Nil = production
	// obs.Meter(MeterScopeAlerthookFallback). Test-only seam so the
	// double-fail path (both primary AND fallback error) is coverable.
	fallbackMeter metric.Meter

	alertCounter metric.Int64Counter

	// perAlertname serialises concurrent route() calls sharing an
	// alertname so the first request's create-or-find decision lands
	// in the cache before sibling requests start their own lookup.
	// Without it an alert-storm of N simultaneous firings would race
	// past the empty cache, see zero open issues, and create N
	// duplicate issues — the very dedup property the receiver exists
	// to enforce.
	// Idle entries are reaped after mutexReapTTL by the goroutine
	// startReaper launches from Serve — without that the map would
	// grow unbounded as production accumulates distinct alertnames.
	perAlertname sync.Map // alertname (string) -> *alertnameLock
}

// ResolveMeter returns the configured meter, falling back lazily to the
// global MeterProvider when nil. Matches the scheduler pattern at
// internal/orchestrator/scheduler/scheduler.go::ResolveMeter so a
// post-construct SetupMeter still wires through.
func (h *Handler) resolveMeter() metric.Meter {
	return obs.ResolveMeter(h.Meter, obs.MeterScopeAlerthook)
}

func (h *Handler) resolveTracer() trace.Tracer {
	if h.Tracer != nil {
		return h.Tracer
	}
	return otel.Tracer("alerthook")
}

func (h *Handler) resolveLogger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

func (h *Handler) resolveFallbackMeter() metric.Meter {
	if h.fallbackMeter != nil {
		return h.fallbackMeter
	}
	return obs.Meter(obs.MeterScopeAlerthookFallback)
}

// init wires meter-derived instruments lazily so a caller mutation of
// h.Meter between construction and first request still binds.
// Guarded by sync.Once so concurrent first-requests cannot race past
// each other and double-install the cache or counter.
func (h *Handler) init() {
	h.initOnce.Do(func() {
		h.cache = newDedupCache(h.Now, h.CacheTTL)
		log := h.resolveLogger()
		const counterName = "regatta.alerthook.alerts.total"
		c, err := h.resolveMeter().Int64Counter(counterName)
		if err != nil {
			log.Warn("alerthook.meter_primary_failed",
				"scope", string(obs.MeterScopeAlerthook),
				"counter", counterName,
				"err", err)
			var ferr error
			c, ferr = h.resolveFallbackMeter().Int64Counter(counterName)
			if ferr != nil {
				log.Error("alerthook.meter_fallback_failed",
					"scope", string(obs.MeterScopeAlerthookFallback),
					"counter", counterName,
					"err", ferr)
			}
		}
		h.alertCounter = c
	})
}

// lockAlertname returns the lock unique to this alertname, allocating on
// first use and stamping lastSeenAt so the reaper can evict idle entries
// after mutexReapTTL.
func (h *Handler) lockAlertname(name string) *alertnameLock {
	now := h.nowUnixNano()
	if v, ok := h.perAlertname.Load(name); ok {
		if lk, ok := v.(*alertnameLock); ok {
			lk.lastSeenAt.Store(now)
			return lk
		}
	}
	fresh := &alertnameLock{}
	fresh.lastSeenAt.Store(now)
	actual, _ := h.perAlertname.LoadOrStore(name, fresh)
	lk, ok := actual.(*alertnameLock)
	if !ok {
		return fresh
	}
	lk.lastSeenAt.Store(now)
	return lk
}

// acquireAlertnameLock returns a locked alertnameLock that is still the
// canonical map entry for name. Looping closes the reaper-evict race:
// if the reaper deleted our lock between lockAlertname and mu.Lock, the
// post-lock map check fails and we retry with a fresh allocation.
func (h *Handler) acquireAlertnameLock(name string) *alertnameLock {
	for {
		lk := h.lockAlertname(name)
		lk.mu.Lock()
		if cur, ok := h.perAlertname.Load(name); ok && cur == lk {
			return lk
		}
		lk.mu.Unlock()
	}
}

// nowUnixNano resolves the injected clock once per call so tests with a
// fakeClock advance reaper time deterministically.
func (h *Handler) nowUnixNano() int64 {
	if h.Now != nil {
		return h.Now().UnixNano()
	}
	return time.Now().UnixNano()
}

// reapStale evicts perAlertname entries idle past mutexReapTTL. Entries
// whose mutex is currently held are skipped — TryLock fails for live
// route() calls, so a concurrent storm never races the reaper into
// dropping a lock another goroutine has just decided to use.
func (h *Handler) reapStale(now time.Time) {
	cutoff := now.Add(-mutexReapTTL).UnixNano()
	h.perAlertname.Range(func(key, value any) bool {
		lk, ok := value.(*alertnameLock)
		if !ok {
			return true
		}
		if lk.lastSeenAt.Load() >= cutoff {
			return true
		}
		if !lk.mu.TryLock() {
			return true
		}
		// Re-check under the lock: a parallel lockAlertname may have just
		// refreshed lastSeenAt between our Load and TryLock.
		if lk.lastSeenAt.Load() >= cutoff {
			lk.mu.Unlock()
			return true
		}
		h.perAlertname.CompareAndDelete(key, value)
		lk.mu.Unlock()
		return true
	})
}

// startReaper launches the background eviction loop and returns a stop
// func the caller invokes on shutdown to drain the goroutine.
func (h *Handler) startReaper(interval time.Duration) func() {
	if interval <= 0 {
		interval = mutexReapInterval
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				h.reapStale(h.resolveNow()())
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

// resolveNow returns the clock seam, defaulting to time.Now when unset.
func (h *Handler) resolveNow() func() time.Time {
	if h.Now != nil {
		return h.Now
	}
	return time.Now
}

// ServeHTTP is the AlertManager webhook entrypoint. Only POST is
// accepted; everything else returns 405 immediately so a misconfigured
// liveness probe never costs a GitHub API call.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.init()
	ctx, span := h.resolveTracer().Start(r.Context(), "alerthook.webhook")
	defer span.End()

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// LIVE-12: shared-secret auth — fail-closed when WEBHOOK_AUTH_TOKEN is
	// unset or the inbound header does not match. The bind addr is not a
	// security boundary: localhost bound is reachable from every container
	// on the same host.
	if !checkWebhookAuth(r) {
		h.resolveLogger().WarnContext(ctx, "alerthook.auth_rejected",
			"remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	defer func() { _ = r.Body.Close() }()

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		// MaxBytesError surfaces as 500 (not 400) so AlertManager retries.
		// AM treats 4xx as permanent — a 1 MiB cap historically caused
		// large batches to silently drop. The cap should not be hit at
		// 4 MiB in practice, but if it is, the operator needs the alert
		// re-delivered, not swallowed.
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.resolveLogger().ErrorContext(ctx, "alerthook.payload_too_large",
				"limit_bytes", MaxBodyBytes, "err", err)
			http.Error(w, "payload exceeds "+fmt.Sprintf("%d", MaxBodyBytes)+" bytes; retry advised", http.StatusInternalServerError)
			return
		}
		h.resolveLogger().WarnContext(ctx, "alerthook.read_body_failed", "err", err)
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		h.resolveLogger().WarnContext(ctx, "alerthook.decode_failed", "err", err)
		http.Error(w, "decode payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(p.Alerts) == 0 {
		h.resolveLogger().InfoContext(ctx, "alerthook.no_alerts")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var routeErr error
	for i := range p.Alerts {
		if err := h.route(ctx, p, p.Alerts[i]); err != nil {
			routeErr = errors.Join(routeErr, err)
		}
	}

	if routeErr != nil {
		span.RecordError(routeErr)
		span.SetStatus(codes.Error, "route failed")
		h.resolveLogger().ErrorContext(ctx, "alerthook.route_failed", "err", routeErr)
		http.Error(w, "route: "+routeErr.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// route resolves one alert to either CreateIssue or CommentOnIssue per
// the dedup rule. Errors propagate via errors.Join in ServeHTTP so the
// failure of one alert does not silently swallow neighbors.
//
// The per-alertname mutex serialises sibling firings so concurrent
// requests that share an alertname always observe the same find-or-
// create outcome — without it two simultaneous firings would both
// see an empty search result and create duplicate issues.
func (h *Handler) route(ctx context.Context, p Payload, a Alert) error {
	name := a.Alertname()
	severity := a.Severity()
	if name == "" {
		h.bump(ctx, name, severity, "error")
		return errors.New("alert missing labels.alertname")
	}

	lk := h.acquireAlertnameLock(name)
	defer lk.mu.Unlock()

	issueNumber, exists, err := findExistingIssue(ctx, h.Client, h.cache, name)
	if err != nil {
		h.bump(ctx, name, severity, "error")
		return fmt.Errorf("dedup lookup: %w", err)
	}

	if exists {
		if err := h.Client.CommentOnIssue(ctx, issueNumber, renderCommentBody(p, a)); err != nil {
			h.bump(ctx, name, severity, "error")
			return fmt.Errorf("comment on #%d: %w", issueNumber, err)
		}
		h.bump(ctx, name, severity, "comment_added")
		h.resolveLogger().InfoContext(ctx, "alerthook.comment_added",
			"alertname", name, "issue", issueNumber)
		return nil
	}

	title := fmt.Sprintf("[obs-alert] %s firing (%s)", name, severity)
	body := renderIssueBody(p, a)
	labels := []string{autonomousLabel, dedupLabel, severity}
	num, err := h.Client.CreateIssue(ctx, title, body, labels)
	if err != nil {
		h.bump(ctx, name, severity, "error")
		return fmt.Errorf("create issue: %w", err)
	}
	if h.cache != nil {
		h.cache.put(name, num, true)
	}
	h.bump(ctx, name, severity, "issue_created")
	h.resolveLogger().InfoContext(ctx, "alerthook.issue_created",
		"alertname", name, "issue", num, "severity", severity)
	return nil
}

func (h *Handler) bump(ctx context.Context, alertname, severity, action string) {
	if h.alertCounter == nil {
		return
	}
	h.alertCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("alertname", alertname),
		attribute.String("severity", severity),
		attribute.String("action", action),
	))
}
