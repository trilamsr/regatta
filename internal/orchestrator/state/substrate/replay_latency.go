// OBS Wave-B T4 replay-duration histogram. Lives in substrate because
// the metric's semantic owner is the substrate-spine (replay reads the
// event log; substrate is the log) — even though the timer source is
// the history package's Replay implementation. The explicit-bucket
// boundary set is the spec §6 table verbatim: 13 buckets giving ≥ 3
// boundaries across the warn-to-critical band (30s warn, 60s critical).
//
// Why explicit buckets instead of OTel defaults: OTel defaults are
// biased toward web-request latencies and have one bucket between
// 500ms and 5s. histogram_quantile against the SLO threshold (60s)
// needs ≥ 3 sample points across the warn-critical band; explicit
// boundaries give clean P95 readings.

package substrate

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
)

// validOutcomes is the closed-enum guard for the replay histogram's
// `outcome` tag. Spec §6 names {match, divergent, error} plus a
// "cancelled" entry for context-cancel paths; unknown values become
// "other" so cardinality stays bounded at 5 × program_kind = 25 cells
// × 13 buckets = 325 series ceiling.
var validOutcomes = map[string]struct{}{
	"match":     {},
	"divergent": {},
	"error":     {},
	"cancelled": {},
	"success":   {},
}

// outcomeOrOther sanitizes a replay outcome string. The validator never
// returns an empty string upstream; the "other" fallthrough is a
// belt-and-braces guard for the cardinality budget.
func outcomeOrOther(o string) string {
	if _, ok := validOutcomes[o]; ok {
		return o
	}
	return tagOther
}

// Spec §6 explicit bucket boundaries — 13 buckets covering 5ms → 60s.
// The 60s top bucket is the SLO critical threshold; the 30s entry is
// the warn threshold; ≥ 3 buckets sit across the warn-critical band
// for clean P95 reads.
var replayLatencyBuckets = []float64{
	0.005, 0.010, 0.025, 0.050, 0.100, 0.250,
	0.500, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0,
}

var (
	replayHistOnce sync.Once
	replayHistVal  metric.Float64Histogram
)

// resolveReplayHistogram lazily binds the histogram against the
// package meter. Same sync.Once shape as the counter resolver in
// metrics.go so SetMeterForTesting drives both with one reset.
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

// resetReplayHistogramForTesting clears the sync.Once so a meter swap
// rebinds the histogram. Called from SetMeterForTesting's reset closure.
func resetReplayHistogramForTesting() {
	replayHistOnce = sync.Once{}
	replayHistVal = nil
}

// RecordReplayDuration emits one histogram observation. Exported so
// the internal/history Replay path can record without importing the
// substrate metrics internals. The tag set is fixed at (program_kind,
// outcome) — both closed-enum guarded so cardinality stays bounded
// regardless of caller drift.
//
// Why exported: substrate is the metric's semantic owner but
// internal/history is the timer source. Keeping the bucket boundaries
// + enum guards inside substrate (one source of truth) requires an
// exported emit seam.
func RecordReplayDuration(ctx context.Context, programKind, outcome string, durationSeconds float64) {
	resolveReplayHistogram().Record(ctx, durationSeconds,
		metric.WithAttributes(
			attribute.String("program_kind", programKindOrOther(programKind)),
			attribute.String("outcome", outcomeOrOther(outcome)),
		))
}

// ReplayLatencyBuckets returns the explicit bucket boundaries used by
// the histogram. Exported so the SLO YAML validator and dashboard
// schema checker can assert the boundary set without importing OTel
// metric internals.
func ReplayLatencyBuckets() []float64 {
	out := make([]float64, len(replayLatencyBuckets))
	copy(out, replayLatencyBuckets)
	return out
}
