package adversarial

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/trilamsr/regatta/internal/obs"
)

// Finding is one item produced by a reviewer-subagent. Severity/Scope
// are pre-classified by the reviewer; Pattern is derived from Text via
// ClassifyPattern at recording time so callers never pass tag values
// directly.
type Finding struct {
	SpanID   string
	Index    int
	Severity Severity
	Scope    Scope
	Text     string
}

// Config wires the counter to a meter. Nil Meter resolves through
// ResolveMeter to obs.MeterScopeObsAdversarial at first use so a
// global provider swap (test injection) takes effect on the next
// call.
type Config struct {
	Meter metric.Meter
}

// ResolveMeter returns the configured meter or the global-scoped
// fallback. Matches orchestrator.Config.ResolveMeter shape.
func (c Config) ResolveMeter() metric.Meter {
	return obs.ResolveMeter(c.Meter, obs.MeterScopeObsAdversarial)
}

// Recorder owns the counter instrument and the dedupe set that makes
// re-export idempotent on span retry (spec R12).
type Recorder struct {
	counter metric.Int64Counter

	mu   sync.Mutex
	seen map[string]struct{}
}

// NewRecorder constructs a Recorder backed by cfg's meter. The counter
// name is regatta.adversarial.findings; the Prometheus exporter
// suffixes _total per its naming convention.
func NewRecorder(cfg Config) (*Recorder, error) {
	c, err := cfg.ResolveMeter().Int64Counter(
		"regatta.adversarial.findings",
		metric.WithDescription("Adversarial reviewer findings, bucketed by severity x scope x pattern."),
	)
	if err != nil {
		return nil, fmt.Errorf("adversarial: counter init: %w", err)
	}
	return &Recorder{counter: c, seen: map[string]struct{}{}}, nil
}

// Record increments the counter for f. Invalid severity/scope short-
// circuits to an error so a buggy caller cannot pollute the time
// series. Duplicate (SpanID, Index) calls are dropped so span retry
// stays idempotent (spec R12).
func (r *Recorder) Record(ctx context.Context, f Finding) error {
	if !ValidSeverity(f.Severity) {
		return fmt.Errorf("adversarial: invalid severity %q", f.Severity)
	}
	if !ValidScope(f.Scope) {
		return fmt.Errorf("adversarial: invalid scope %q", f.Scope)
	}
	pattern := ClassifyPattern(f.Text)

	if f.SpanID != "" {
		key := fmt.Sprintf("%s/%d", f.SpanID, f.Index)
		r.mu.Lock()
		if _, ok := r.seen[key]; ok {
			r.mu.Unlock()
			return nil
		}
		r.seen[key] = struct{}{}
		r.mu.Unlock()
	}

	r.counter.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("severity", string(f.Severity)),
			attribute.String("scope", string(f.Scope)),
			attribute.String("pattern", string(pattern)),
		),
	)
	return nil
}
