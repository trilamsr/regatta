package rejectionrouter

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// scopeName is the OTel instrumentation scope the rejection-router
// emits label-failure counters under. Matches the W6 per-scope query
// slice ("orchestrator/rejectionrouter") so the metric is grep-able
// from dashboard PromQL.
const scopeName = "github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"

// LabelFailureMetricName is the OTel meter-API name for the counter.
// Exported so dashboard JSON + alert rules + the drift-guard test
// share one constant. The Prom exporter renders this as
// `regatta_rejection_router_label_failures_total` — dotted-name →
// underscore plus auto-appended `_total` for counters, per the obs
// spec §2.1 convention. The meter-API name therefore omits `_total`
// to avoid the double-suffix the exporter would otherwise produce.
const LabelFailureMetricName = "regatta.rejection_router.label_failures"

// Reason label values for LabelFailureMetricName. The mapping is
// load-bearing — dashboards + on-call runbooks key off these literals.
// Cardinality cap: 4 reasons total; expanding requires an obs-roadmap
// amendment.
const (
	ReasonAbsent      = "absent"       // label literal missing from the repo's label set
	ReasonRateLimited = "rate_limited" // gh CLI hit GitHub's REST/GraphQL rate cap
	ReasonNotFound    = "not_found"    // PR/branch/repo not found (distinct from absent-label)
	ReasonUnknown     = "unknown"      // every other failure (network, gh-CLI bug, parse)
)

// instruments bundles the rejection-router metric handles behind a
// nil-safe surface. Telemetry is observability, not a correctness gate:
// a misconfigured meter must never panic the labeler path.
type instruments struct {
	labelFailures metric.Int64Counter
}

// newInstruments wires the failure counter against a meter. Int64Counter
// only errors on duplicate registration with conflicting attributes —
// fall through to a nil handle (every record() is nil-safe) so a
// re-registration race cannot brick escalation labeling.
func newInstruments(m metric.Meter) *instruments {
	c, _ := m.Int64Counter(LabelFailureMetricName)
	return &instruments{labelFailures: c}
}

// recordLabelFailure emits one (reason) data point. nil-safe at every
// layer: nil receiver + nil counter both short-circuit cleanly.
func (i *instruments) recordLabelFailure(ctx context.Context, reason string) {
	if i == nil || i.labelFailures == nil {
		return
	}
	i.labelFailures.Add(ctx, 1,
		metric.WithAttributes(attribute.String("reason", reason)))
}

// resolveMeter returns the configured meter or falls back to the global
// MeterProvider's scoped meter. Lazy so a global-provider swap (test
// injection of a noop provider, runtime swap on reload) takes effect on
// the next call.
func (c Config) resolveMeter() metric.Meter {
	if c.Meter != nil {
		return c.Meter
	}
	return otel.Meter(scopeName)
}

// ClassifyGHError maps a gh-CLI / labeler error onto one of four reason
// buckets. The classifier is conservative: ambiguous strings fall
// through to ReasonUnknown so dashboards do not inflate one bucket on
// novel error shapes.
//
// Reason precedence — checked in order so a substring overlap
// (e.g. an error string that contains both "rate limit" and "not found")
// resolves to the more-specific bucket:
//
//  1. ReasonRateLimited — "rate limit" / "HTTP 403 ... rate limit"
//  2. ReasonAbsent — "could not add label" + "not found" (the github
//     CLI emits this exact pair when the label literal is absent from
//     the repo's label set; see PR #480 reviewer finding #498)
//  3. ReasonNotFound — bare "not found" / "no open PR for branch" /
//     "Could not resolve" (PR-not-found, branch-not-found, 404)
//  4. ReasonUnknown — every other surface
func ClassifyGHError(err error) string {
	if err == nil {
		return ReasonUnknown
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "rate limit"):
		return ReasonRateLimited
	case strings.Contains(s, "could not add label") && strings.Contains(s, "not found"):
		return ReasonAbsent
	case strings.Contains(s, "no open pr for branch"),
		strings.Contains(s, "could not resolve"),
		strings.Contains(s, "not found"):
		return ReasonNotFound
	default:
		return ReasonUnknown
	}
}

// metricLabeler wraps a PRLabeler and emits LabelFailureMetricName on
// every AddLabel error. Construction lives in Router.New so Config.Meter
// flows through to emission without callers having to thread a meter
// into every PRLabeler implementation.
type metricLabeler struct {
	inner       PRLabeler
	instruments *instruments
}

func (m *metricLabeler) AddLabel(ctx context.Context, agentID int64, label string) error {
	err := m.inner.AddLabel(ctx, agentID, label)
	if err != nil {
		m.instruments.recordLabelFailure(ctx, ClassifyGHError(err))
	}
	return err
}
