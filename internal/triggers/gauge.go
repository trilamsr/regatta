package triggers

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Provider exposes the live State through OTel ObservableGauges so
// dashboards can read the day-count and days-remaining views without
// polling the substrate themselves. The compute closure runs on each
// scrape; it must return quickly (< 100 ms budget) — keep substrate
// queries indexed.
type Provider struct {
	mu sync.RWMutex
	// Each trigger name maps to its current State. The Source closure
	// stamps fresh State on each scrape via Update.
	states map[string]State
}

// NewProvider constructs an empty Provider. The caller registers
// instruments via Register and seeds state via Update.
func NewProvider() *Provider {
	return &Provider{states: map[string]State{}}
}

// Update overwrites the state for one trigger name. Safe for
// concurrent reads from the gauge callback.
func (p *Provider) Update(trigger string, s State) {
	p.mu.Lock()
	p.states[trigger] = s
	p.mu.Unlock()
}

// snapshot returns a copy of the current state map for the gauge
// callback. Internal; do not export — callers should not depend on
// the map identity.
func (p *Provider) snapshot() map[string]State {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cp := make(map[string]State, len(p.states))
	for k, v := range p.states {
		cp[k] = v
	}
	return cp
}

// Register installs both ObservableGauges
// (regatta.green_clock.day_count and regatta.trigger.days_remaining)
// against cfg's meter. One callback fires per scrape and observes
// every known trigger.
func (p *Provider) Register(cfg MeterConfig) error {
	m := cfg.ResolveMeter()
	dayCount, err := m.Float64ObservableGauge(
		"regatta.green_clock.day_count",
		metric.WithDescription("Consecutive green days in the rolling Phase GREEN-CLOCK window."),
	)
	if err != nil {
		return fmt.Errorf("triggers: day_count gauge: %w", err)
	}
	daysRem, err := m.Float64ObservableGauge(
		"regatta.trigger.days_remaining",
		metric.WithDescription("Days remaining until each trigger's window completes."),
	)
	if err != nil {
		return fmt.Errorf("triggers: days_remaining gauge: %w", err)
	}
	_, err = m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		for name, s := range p.snapshot() {
			attrs := metric.WithAttributes(attribute.String("trigger", name))
			o.ObserveFloat64(dayCount, float64(s.DayCount), attrs)
			o.ObserveFloat64(daysRem, float64(s.DaysRemaining), attrs)
		}
		return nil
	}, dayCount, daysRem)
	if err != nil {
		return fmt.Errorf("triggers: register callback: %w", err)
	}
	return nil
}
