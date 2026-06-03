// Package dispatch instruments subagent dispatches with a span,
// outcome counter, and duration histogram. Spec OBS-WAVE-C-T1.
//
// Cardinality fence: kind ∈ {implementer, reviewer, designer, triage}
// and outcome ∈ {success, refused, error, timeout} are closed enums.
// task_id, model, pr_number ride span attributes only — never metric
// labels. The operator drills counter → exemplar trace_id → span.
package dispatch

import (
	"context"
	"regexp"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Kind enumerates the 4 subagent roles the spawner can dispatch.
type Kind string

// Closed enum — adding a 5th kind MUST update TestDispatchKindEnum_Closed.
const (
	KindImplementer Kind = "implementer"
	KindReviewer    Kind = "reviewer"
	KindDesigner    Kind = "designer"
	KindTriage      Kind = "triage"
)

// Outcome enumerates the 4 terminal states a dispatch reaches.
type Outcome string

// Closed enum — adding a 5th outcome MUST update TestDispatchOutcomeEnum_Closed.
const (
	OutcomeSuccess Outcome = "success"
	OutcomeRefused Outcome = "refused"
	OutcomeError   Outcome = "error"
	OutcomeTimeout Outcome = "timeout"
)

// AllKinds returns the canonical kind list in declaration order.
func AllKinds() []Kind {
	return []Kind{KindImplementer, KindReviewer, KindDesigner, KindTriage}
}

// AllOutcomes returns the canonical outcome list in declaration order.
func AllOutcomes() []Outcome {
	return []Outcome{OutcomeSuccess, OutcomeRefused, OutcomeError, OutcomeTimeout}
}

// scopeName pins the OTel instrumentation scope.
const scopeName = "github.com/trilamsr/regatta/internal/obs/dispatch"

// Config holds DI handles for tracer + meter. Nil falls back to the
// global providers — a test that swaps the global noop wins.
type Config struct {
	Tracer trace.Tracer
	Meter  metric.Meter
}

// EndOpts collects post-run attributes the caller stamps onto the span
// and counter via the EndFunc returned from Start.
type EndOpts struct {
	Outcome      Outcome
	InputTokens  int64
	OutputTokens int64
	// PRNumber rides the span only when the dispatch produced a PR.
	// Zero means "not yet known" — the attribute is omitted.
	PRNumber int64
	// Model rides span only; sanitized to whitelist shape before emit.
	Model string
}

// EndFunc is the closure Start returns; the caller invokes it on
// subagent exit to stamp terminal attributes + emit counter + histogram.
// Idempotent on second call (defer-friendly).
type EndFunc func(opts EndOpts)

// modelAttrPattern whitelists model attribute shape — `^[a-z0-9._-]{1,40}$`.
// Anything outside becomes the literal "invalid_model" so a typo'd
// API-key fragment cannot leak through the span attribute set (R4).
var modelAttrPattern = regexp.MustCompile(`^[a-z0-9._-]{1,40}$`)

// sanitizeModel folds non-whitelist input to "invalid_model".
func sanitizeModel(s string) string {
	if modelAttrPattern.MatchString(s) {
		return s
	}
	return "invalid_model"
}

// Start opens the dispatch span and returns a child context + an EndFunc.
// Callers MUST invoke the EndFunc exactly once (defer ok). kind and taskID
// are stamped on span open so a crashed/abandoned subagent still surfaces
// in the trace tree.
func Start(ctx context.Context, cfg Config, kind Kind, taskID string) (context.Context, EndFunc) {
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer(scopeName)
	}
	meter := cfg.Meter
	if meter == nil {
		meter = otel.Meter(scopeName)
	}

	start := time.Now()
	ctx, span := tracer.Start(ctx, "regatta.dispatch.subagent",
		trace.WithAttributes(
			attribute.String("kind", string(kind)),
			attribute.String("task_id", taskID),
		))

	var done bool
	return ctx, func(opts EndOpts) {
		if done {
			return
		}
		done = true
		dur := time.Since(start)

		// Span attributes — span-only set carries the high-cardinality
		// drill keys (task_id stamped at open; model + pr_number + tokens
		// stamp on close).
		attrs := []attribute.KeyValue{
			attribute.String("outcome", string(opts.Outcome)),
			attribute.Int64("input_tokens", opts.InputTokens),
			attribute.Int64("output_tokens", opts.OutputTokens),
			attribute.Float64("duration_seconds", dur.Seconds()),
		}
		if opts.Model != "" {
			attrs = append(attrs, attribute.String("model", sanitizeModel(opts.Model)))
		}
		if opts.PRNumber != 0 {
			attrs = append(attrs, attribute.Int64("pr_number", opts.PRNumber))
		}
		span.SetAttributes(attrs...)
		span.End()

		// Counter — kind × outcome closed enum cross-product = 16 cells.
		ctr, err := meter.Int64Counter("regatta.dispatch.subagent")
		if err == nil {
			ctr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("kind", string(kind)),
				attribute.String("outcome", string(opts.Outcome)),
			))
		}
		// Histogram — kind enum closed; duration in seconds per OTel
		// semconv (unit "s"). Default bucket boundaries cover the
		// SLO-5 le="120" threshold via the SDK exponential view.
		hist, err := meter.Float64Histogram("regatta.dispatch.subagent_duration_seconds",
			metric.WithUnit("s"))
		if err == nil {
			hist.Record(ctx, dur.Seconds(), metric.WithAttributes(
				attribute.String("kind", string(kind)),
			))
		}
	}
}
