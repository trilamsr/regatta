package obs

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// AdversarialFinding is one reviewer-subagent item; Severity/Scope
// are pre-classified by the reviewer; Pattern is derived from Text
// via ClassifyPattern at record time.
type AdversarialFinding struct {
	SpanID   string
	Index    int
	Severity Severity
	Scope    Scope
	Text     string
}

// AdversarialConfig wires the counter to a meter; nil Meter resolves
// via ResolveMeter(MeterScopeObsAdversarial) so a test noop swap wins.
type AdversarialConfig struct {
	Meter metric.Meter
}

// ResolveMeter returns the configured meter or the global-scoped fallback.
func (c AdversarialConfig) ResolveMeter() metric.Meter {
	return ResolveMeter(c.Meter, MeterScopeObsAdversarial)
}

// AdversarialRecorder owns the findings counter + a dedupe set that
// keeps re-export idempotent on span retry (spec R12).
type AdversarialRecorder struct {
	counter metric.Int64Counter

	mu   sync.Mutex
	seen map[string]struct{}
}

// NewAdversarialRecorder constructs a Recorder backed by cfg's meter;
// counter name is regatta.adversarial.findings.
func NewAdversarialRecorder(cfg AdversarialConfig) (*AdversarialRecorder, error) {
	c, err := cfg.ResolveMeter().Int64Counter(
		"regatta.adversarial.findings",
		metric.WithDescription("Adversarial reviewer findings, bucketed by severity x scope x pattern."),
	)
	if err != nil {
		return nil, fmt.Errorf("adversarial: counter init: %w", err)
	}
	return &AdversarialRecorder{counter: c, seen: map[string]struct{}{}}, nil
}

// Record increments the counter for f — invalid severity/scope short-
// circuit to error; duplicate (SpanID, Index) drops keep span retry
// idempotent (spec R12).
func (r *AdversarialRecorder) Record(ctx context.Context, f AdversarialFinding) error {
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
