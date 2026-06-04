package rejectionrouter

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// scopeName matches the W6 per-scope query slice so the metric is
// grep-able from dashboard PromQL.
const scopeName = "github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"

// LabelFailureMetricName is the OTel meter-API name for the counter.
// Exported so dashboard JSON + alert rules + drift-guard test share one
// constant. Prom exporter renders this as
// `regatta_rejection_router_label_failures_total` (dotted→underscore +
// auto `_total` suffix per obs spec §2.1); we omit `_total` here to
// avoid the double-suffix the exporter would otherwise produce.
const LabelFailureMetricName = "regatta.rejection_router.label_failures"

// Reason label values for LabelFailureMetricName — load-bearing:
// dashboards + on-call runbooks key off these literals. Cardinality
// cap: 4 reasons; expanding requires an obs-roadmap amendment.
const (
	ReasonAbsent      = "absent"       // label literal missing from repo label set
	ReasonRateLimited = "rate_limited" // gh CLI hit GitHub REST/GraphQL rate cap
	ReasonNotFound    = "not_found"    // PR/branch/repo not found (distinct from absent-label)
	ReasonUnknown     = "unknown"      // every other failure
)

// instruments bundles the rejection-router metric handles behind a
// nil-safe surface — telemetry is observability, not a correctness
// gate; a misconfigured meter must never panic the labeler path.
type instruments struct {
	labelFailures metric.Int64Counter
}

// newInstruments wires the failure counter; Int64Counter only errors
// on duplicate registration with conflicting attributes — fall through
// to a nil handle (record() is nil-safe) so a re-registration race
// cannot brick escalation labeling.
func newInstruments(m metric.Meter) *instruments {
	c, _ := m.Int64Counter(LabelFailureMetricName)
	return &instruments{labelFailures: c}
}

func (i *instruments) recordLabelFailure(ctx context.Context, reason string) {
	if i == nil || i.labelFailures == nil {
		return
	}
	i.labelFailures.Add(ctx, 1,
		metric.WithAttributes(attribute.String("reason", reason)))
}

// resolveMeter falls back to the global MeterProvider so a provider
// swap (test injection, runtime reload) takes effect on the next call.
func (c Config) resolveMeter() metric.Meter {
	if c.Meter != nil {
		return c.Meter
	}
	return otel.Meter(scopeName)
}

// ClassifyGHError maps a gh-CLI error onto one of four reason buckets;
// ambiguous strings fall through to ReasonUnknown so novel error
// shapes do not inflate one bucket on dashboards.
//
// Precedence (more-specific first; absent-label BEFORE rate-limit so
// an operator-configured EscalationLabel literally containing "rate
// limit" cannot misclassify as rate_limited when the repo lacks it):
//  1. ReasonAbsent — "could not add label" + "not found" (gh CLI emits
//     this exact pair when the label is absent from the repo, #498)
//  2. ReasonRateLimited — "rate limit"
//  3. ReasonNotFound — "no open pr for branch" / "could not resolve" /
//     bare "not found"
//  4. ReasonUnknown — everything else
func ClassifyGHError(err error) string {
	if err == nil {
		return ReasonUnknown
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "could not add label") && strings.Contains(s, "not found"):
		return ReasonAbsent
	case strings.Contains(s, "rate limit"):
		return ReasonRateLimited
	case strings.Contains(s, "no open pr for branch"),
		strings.Contains(s, "could not resolve"),
		strings.Contains(s, "not found"):
		return ReasonNotFound
	default:
		return ReasonUnknown
	}
}

// metricLabeler wraps a PRLabeler to emit LabelFailureMetricName on
// every AddLabel error. Construction lives in Router.New so
// Config.Meter flows through without callers threading a meter into
// every PRLabeler impl.
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
