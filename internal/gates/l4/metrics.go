package l4

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Verdict label values for regatta.l4.invocations. The mapping is
// load-bearing: dashboards + alarms key off these literals, so the
// brief pins them as the only allowed values (cap 5 × 12 categories).
const (
	verdictLabelAllow       = "allow"        // schemas.VerdictPass
	verdictLabelDeny        = "deny"         // schemas.VerdictFail (blocking)
	verdictLabelNeedsReview = "needs_review" // schemas.VerdictAdvisory
	verdictLabelEscalate    = "escalate"     // second-opinion flipped a disputed finding
	verdictLabelSkip        = "skip"         // nil-Invoker or oversize-diff short-circuit
)

// instruments bundles the five L4 metrics behind a nil-safe handle.
// Construction errors fall back to noop counters so a misconfigured
// meter cannot brick the gate path — telemetry is observability, not
// a correctness gate.
type instruments struct {
	invocations   metric.Int64Counter
	latency       metric.Float64Histogram
	cacheHits     metric.Int64Counter
	cacheMisses   metric.Int64Counter
	secondOpinion metric.Int64Counter
}

// newInstruments wires the five L4 metrics against a meter. The
// Int64Counter constructor only errors on duplicate registration with
// conflicting attributes — drop the err and fall through to a noop
// counter when that happens so a meter mid-flux cannot panic the gate.
func newInstruments(m metric.Meter) *instruments {
	inv, _ := m.Int64Counter("regatta.l4.invocations")
	lat, _ := m.Float64Histogram("regatta.l4.latency_ms")
	hits, _ := m.Int64Counter("regatta.l4.cache.hits")
	misses, _ := m.Int64Counter("regatta.l4.cache.misses")
	so, _ := m.Int64Counter("regatta.l4.second_opinion.fired")
	return &instruments{
		invocations:   inv,
		latency:       lat,
		cacheHits:     hits,
		cacheMisses:   misses,
		secondOpinion: so,
	}
}

// recordInvocation emits one (verdict, category) cell. Callers loop
// over the relevant category slice so a single Run with N categories
// produces N data points sharing one verdict label.
func (i *instruments) recordInvocation(ctx context.Context, verdict, category string) {
	if i == nil || i.invocations == nil {
		return
	}
	i.invocations.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("verdict", verdict),
			attribute.String("category", category),
		))
}

func (i *instruments) recordLatency(ctx context.Context, ms float64) {
	if i == nil || i.latency == nil {
		return
	}
	i.latency.Record(ctx, ms)
}

func (i *instruments) recordCacheHit(ctx context.Context) {
	if i == nil || i.cacheHits == nil {
		return
	}
	i.cacheHits.Add(ctx, 1)
}

func (i *instruments) recordCacheMiss(ctx context.Context) {
	if i == nil || i.cacheMisses == nil {
		return
	}
	i.cacheMisses.Add(ctx, 1)
}

func (i *instruments) recordSecondOpinion(ctx context.Context) {
	if i == nil || i.secondOpinion == nil {
		return
	}
	i.secondOpinion.Add(ctx, 1)
}

// emitInvocations records one regatta.l4.invocations data point per
// category with the same verdict label. Categories empty falls back
// to "_unknown" so a degenerate path still surfaces in dashboards.
func emitInvocations(ctx context.Context, inst *instruments, verdict string, categories []string) {
	if len(categories) == 0 {
		inst.recordInvocation(ctx, verdict, "_unknown")
		return
	}
	for _, c := range categories {
		inst.recordInvocation(ctx, verdict, c)
	}
}
