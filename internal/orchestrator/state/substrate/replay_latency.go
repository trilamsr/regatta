// Replay-duration histogram (OBS Wave-B T4). Substrate owns the metric
// (it owns the event log) even though internal/history sources the
// timer. Explicit buckets per spec §6 — OTel defaults have one bucket
// between 500ms and 5s, which leaves histogram_quantile(60s SLO) with
// too few sample points across the warn-critical band.

package substrate

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
)

// validOutcomes is the closed-enum guard for the replay histogram's `outcome` tag; unknown values become "other" so cardinality stays bounded (5 × program_kind = 25 cells × 13 buckets = 325 series ceiling).
var validOutcomes = map[string]struct{}{
	"match":     {},
	"divergent": {},
	"error":     {},
	"cancelled": {},
	"success":   {},
}

// outcomeOrOther sanitizes a replay outcome string — "other" fallthrough is a belt-and-braces guard for the cardinality budget.
func outcomeOrOther(o string) string {
	if _, ok := validOutcomes[o]; ok {
		return o
	}
	return tagOther
}

// replayLatencyBuckets — spec §6 boundaries; 60s SLO critical, 30s warn, ≥ 3 buckets across the warn-critical band for clean P95.
var replayLatencyBuckets = []float64{
	0.005, 0.010, 0.025, 0.050, 0.100, 0.250,
	0.500, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0,
}

var (
	replayHistOnce sync.Once
	replayHistVal  metric.Float64Histogram
)

// resolveReplayHistogram lazily binds the histogram against the package meter; SetMeterForTesting resets both this and metrics.go counters in one shot.
func resolveReplayHistogram() metric.Float64Histogram {
	replayHistOnce.Do(func() {
		m := resolveMeter()
		h, err := m.Float64Histogram("regatta.substrate.replay.duration_seconds",
			metric.WithDescription("substrate replay duration; outcome ∈ {match, divergent, error, cancelled}"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(replayLatencyBuckets...))
		if err != nil {
			h, _ = noopmetric.NewMeterProvider().Meter("noop").Float64Histogram(
				"regatta.substrate.replay.duration_seconds")
		}
		replayHistVal = h
	})
	return replayHistVal
}

// resetReplayHistogramForTesting clears the sync.Once so a meter swap rebinds.
func resetReplayHistogramForTesting() {
	replayHistOnce = sync.Once{}
	replayHistVal = nil
}

// RecordReplayDuration emits one histogram observation. Exported so internal/history Replay can record without importing substrate internals — both program_kind and outcome are enum-guarded to bound cardinality.
func RecordReplayDuration(ctx context.Context, programKind, outcome string, durationSeconds float64) {
	resolveReplayHistogram().Record(ctx, durationSeconds,
		metric.WithAttributes(
			attribute.String("program_kind", programKindOrOther(programKind)),
			attribute.String("outcome", outcomeOrOther(outcome)),
		))
}

// ReplayLatencyBuckets returns the explicit bucket boundaries — exported so the SLO YAML validator and dashboard schema checker can assert without importing OTel internals.
func ReplayLatencyBuckets() []float64 {
	out := make([]float64, len(replayLatencyBuckets))
	copy(out, replayLatencyBuckets)
	return out
}
