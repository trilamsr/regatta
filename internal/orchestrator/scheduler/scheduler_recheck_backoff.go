package scheduler

import (
	"context"
	"sync"

	"github.com/trilamsr/regatta/internal/obs"
	"go.opentelemetry.io/otel/metric"
)

const recheckBackoffK = 3
const recheckBackoffTicks = 10

// recheckBackoff is the per-orphan K-strike fetch-failure budget +
// N-tick suppression window for fetchWorkItemForRecheck (#773).
type recheckBackoff struct {
	mu          sync.Mutex
	entries     map[string]*recheckEntry
	unavailable metric.Int64Counter
}

type recheckEntry struct {
	failures      int
	suppressTicks int
}

func newRecheckBackoff() *recheckBackoff {
	return &recheckBackoff{entries: make(map[string]*recheckEntry)}
}

func newRecheckBackoffWithMeter(meter metric.Meter) *recheckBackoff {
	if meter == nil {
		meter = obs.Meter(obs.MeterScopeSchedulerFallback)
	}
	c, err := meter.Int64Counter("regatta.scheduler.gate_recheck_unavailable")
	if err != nil {
		c, _ = obs.Meter(obs.MeterScopeSchedulerFallback).Int64Counter("regatta.scheduler.gate_recheck_unavailable")
	}
	return &recheckBackoff{entries: make(map[string]*recheckEntry), unavailable: c}
}

func (b *recheckBackoff) incr(ctx context.Context) {
	if b.unavailable == nil {
		return
	}
	b.unavailable.Add(ctx, 1)
}

func (b *recheckBackoff) Admit(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entries[id]
	if e == nil {
		return true
	}
	return e.suppressTicks == 0
}

// RecordFailure returns true exactly once — on the Kth strike that
// flips id into the suppression window.
func (b *recheckBackoff) RecordFailure(id string) (enteredBackoff bool) {
	b.incr(context.Background())
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entries[id]
	if e == nil {
		e = &recheckEntry{}
		b.entries[id] = e
	}
	e.failures++
	if e.failures == recheckBackoffK {
		e.suppressTicks = recheckBackoffTicks
		return true
	}
	return false
}

func (b *recheckBackoff) RecordSuccess(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.entries, id)
}

func (b *recheckBackoff) Tick() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, e := range b.entries {
		if e.suppressTicks > 0 {
			e.suppressTicks--
			if e.suppressTicks == 0 {
				delete(b.entries, id)
			}
		}
	}
}
