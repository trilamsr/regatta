package scheduler

import (
	"context"
	"sync"

	"github.com/trilamsr/regatta/internal/obs"
	"go.opentelemetry.io/otel/metric"
)

const (
	recheckBackoffDefaultK             = 3
	recheckBackoffDefaultSuppressTicks = 10
	recheckBackoffDefaultStaleTicks    = 20
)

// recheckBackoffTicks is the legacy in-package alias retained for the
// pre-#794 `TestRecheckBackoff_BackoffExpiresAfterNTicks` cross-ref;
// runtime path consults b.suppressTicks so Config overrides flow
// through (#794).
const recheckBackoffTicks = recheckBackoffDefaultSuppressTicks

// recheckBackoffConfig is the tuple of operator-tunable knobs threaded
// from scheduler.Config into the per-orphan backoff helper (#794).
type recheckBackoffConfig struct {
	K             int
	SuppressTicks int
	StaleTicks    int
}

// recheckBackoff is the per-orphan K-strike fetch-failure budget +
// N-tick suppression window for fetchWorkItemForRecheck (#773). Stale
// idle entries (failures>0 but not suppressed) evict after staleTicks
// to bound map growth (#794).
type recheckBackoff struct {
	mu            sync.Mutex
	entries       map[string]*recheckEntry
	unavailable   metric.Int64Counter
	k             int
	suppressTicks int
	staleTicks    int
}

type recheckEntry struct {
	failures      int
	suppressTicks int
	idleTicks     int
}

func newRecheckBackoff() *recheckBackoff {
	return newRecheckBackoffWithConfig(recheckBackoffConfig{})
}

func newRecheckBackoffWithMeter(meter metric.Meter) *recheckBackoff {
	return newRecheckBackoffWithMeterAndConfig(meter, recheckBackoffConfig{})
}

func newRecheckBackoffWithConfig(cfg recheckBackoffConfig) *recheckBackoff {
	cfg = resolveRecheckBackoffConfig(cfg)
	return &recheckBackoff{
		entries:       make(map[string]*recheckEntry),
		k:             cfg.K,
		suppressTicks: cfg.SuppressTicks,
		staleTicks:    cfg.StaleTicks,
	}
}

func newRecheckBackoffWithMeterAndConfig(meter metric.Meter, cfg recheckBackoffConfig) *recheckBackoff {
	cfg = resolveRecheckBackoffConfig(cfg)
	if meter == nil {
		meter = obs.Meter(obs.MeterScopeSchedulerFallback)
	}
	c, err := meter.Int64Counter("regatta.scheduler.gate_recheck_unavailable")
	if err != nil {
		c, _ = obs.Meter(obs.MeterScopeSchedulerFallback).Int64Counter("regatta.scheduler.gate_recheck_unavailable")
	}
	return &recheckBackoff{
		entries:       make(map[string]*recheckEntry),
		unavailable:   c,
		k:             cfg.K,
		suppressTicks: cfg.SuppressTicks,
		staleTicks:    cfg.StaleTicks,
	}
}

// resolveRecheckBackoffConfig clamps invalid knob values to defaults so
// a partial or zero-value Config never produces a no-op gate or
// unbounded entry growth (#794).
func resolveRecheckBackoffConfig(cfg recheckBackoffConfig) recheckBackoffConfig {
	if cfg.K < 1 {
		cfg.K = recheckBackoffDefaultK
	}
	if cfg.SuppressTicks < 1 {
		cfg.SuppressTicks = recheckBackoffDefaultSuppressTicks
	}
	if cfg.StaleTicks < cfg.K {
		cfg.StaleTicks = recheckBackoffDefaultStaleTicks
		if cfg.StaleTicks < cfg.K {
			cfg.StaleTicks = cfg.K
		}
	}
	return cfg
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
// flips id into the suppression window. ctx threads the caller's span
// onto the unavailable counter so the Add lands on the right scope
// rather than context.Background() (#793).
func (b *recheckBackoff) RecordFailure(ctx context.Context, id string) (enteredBackoff bool) {
	b.incr(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	e := b.entries[id]
	if e == nil {
		e = &recheckEntry{}
		b.entries[id] = e
	}
	e.failures++
	e.idleTicks = 0
	if e.failures == b.k {
		e.suppressTicks = b.suppressTicks
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
			continue
		}
		e.idleTicks++
		if e.idleTicks >= b.staleTicks {
			delete(b.entries, id)
		}
	}
}
