package alarmwebhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// MaxBodyBytes caps an incoming webhook body. 1 MiB is well above any
// realistic AlertManager group (the upstream default group_by produces
// payloads under 100 KiB even with verbose annotations) and well below
// anything an attacker could use to OOM the receiver.
const MaxBodyBytes = 1 << 20

// Handler is the HTTP handler that owns one AlertManager receiver
// endpoint. Wire one Handler per (repo, label-set); the dedup cache
// keys on alertname so two Handlers pointed at different repos do not
// share state.
type Handler struct {
	// Client is the GitHub seam. Required; the constructor enforces it.
	Client GitHubClient
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

	alertCounter metric.Int64Counter

	// perAlertname serialises concurrent route() calls sharing an
	// alertname so the first request's create-or-find decision lands
	// in the cache before sibling requests start their own lookup.
	// Without it an alert-storm of N simultaneous firings would race
	// past the empty cache, see zero open issues, and create N
	// duplicate issues — the very dedup property the receiver exists
	// to enforce.
	perAlertname sync.Map // alertname (string) -> *sync.Mutex
}

// ResolveMeter returns the configured meter, falling back lazily to the
// global MeterProvider when nil. Matches the scheduler pattern at
// internal/orchestrator/scheduler/scheduler.go::ResolveMeter so a
// post-construct SetupMeter still wires through.
func (h *Handler) resolveMeter() metric.Meter {
	if h.Meter != nil {
		return h.Meter
	}
	return otel.Meter("alarmwebhook")
}

func (h *Handler) resolveTracer() trace.Tracer {
	if h.Tracer != nil {
		return h.Tracer
	}
	return otel.Tracer("alarmwebhook")
}

func (h *Handler) resolveLogger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

// init wires meter-derived instruments lazily so a caller mutation of
// h.Meter between construction and first request still binds.
// Guarded by sync.Once so concurrent first-requests cannot race past
// each other and double-install the cache or counter.
func (h *Handler) init() {
	h.initOnce.Do(func() {
		h.cache = newDedupCache(h.Now, h.CacheTTL)
		m := h.resolveMeter()
		c, err := m.Int64Counter("regatta.alarm_webhook.alerts.total")
		if err != nil {
			c, _ = otel.Meter("alarmwebhook-fallback").Int64Counter("regatta.alarm_webhook.alerts.total")
		}
		h.alertCounter = c
	})
}

// lockAlertname returns the mutex unique to this alertname, allocating
// on first use. Holders serialise the find-or-create decision so an
// alert-storm collapses to one CreateIssue even when N requests arrive
// in the same tick before any cache write has landed.
func (h *Handler) lockAlertname(name string) *sync.Mutex {
	if v, ok := h.perAlertname.Load(name); ok {
		if mu, ok := v.(*sync.Mutex); ok {
			return mu
		}
	}
	fresh := &sync.Mutex{}
	actual, _ := h.perAlertname.LoadOrStore(name, fresh)
	if mu, ok := actual.(*sync.Mutex); ok {
		return mu
	}
	return fresh
}

// ServeHTTP is the AlertManager webhook entrypoint. Only POST is
// accepted; everything else returns 405 immediately so a misconfigured
// liveness probe never costs a GitHub API call.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.init()
	ctx, span := h.resolveTracer().Start(r.Context(), "alarmwebhook.webhook")
	defer span.End()

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	defer func() { _ = r.Body.Close() }()

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		h.resolveLogger().WarnContext(ctx, "alarmwebhook.read_body_failed", "err", err)
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		h.resolveLogger().WarnContext(ctx, "alarmwebhook.decode_failed", "err", err)
		http.Error(w, "decode payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(p.Alerts) == 0 {
		h.resolveLogger().InfoContext(ctx, "alarmwebhook.no_alerts")
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
		h.resolveLogger().ErrorContext(ctx, "alarmwebhook.route_failed", "err", routeErr)
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

	mu := h.lockAlertname(name)
	mu.Lock()
	defer mu.Unlock()

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
		h.resolveLogger().InfoContext(ctx, "alarmwebhook.comment_added",
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
	h.resolveLogger().InfoContext(ctx, "alarmwebhook.issue_created",
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
