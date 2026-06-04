package scheduler

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestRecheckBackoff_BacksOffAfterKFailures asserts 10 failing ticks admit at most K=3 (#773).
func TestRecheckBackoff_BacksOffAfterKFailures(t *testing.T) {
	b := newRecheckBackoff()
	const id = "wi-1"
	admits := 0
	for tick := 0; tick < 10; tick++ {
		if b.Admit(id) {
			admits++
			b.RecordFailure(id)
		}
	}
	if admits > 3 {
		t.Fatalf("Admit fired %d times in 10 ticks; want <=3 (K=3 budget then 7 ticks suppressed)", admits)
	}
	if admits != 3 {
		t.Fatalf("Admit fired %d times in 10 ticks; want exactly 3 (K=3 then suppressed)", admits)
	}
}

// TestRecheckBackoff_SuccessResetsBudget asserts RecordSuccess clears the failure budget (#773).
func TestRecheckBackoff_SuccessResetsBudget(t *testing.T) {
	b := newRecheckBackoff()
	const id = "wi-1"
	for i := 0; i < 2; i++ {
		if !b.Admit(id) {
			t.Fatalf("tick %d: Admit=false; want true (under budget)", i)
		}
		b.RecordFailure(id)
	}
	b.RecordSuccess(id)
	for i := 0; i < 3; i++ {
		if !b.Admit(id) {
			t.Fatalf("post-success tick %d: Admit=false; want true (budget reset)", i)
		}
		b.RecordFailure(id)
	}
}

// TestRecheckBackoff_PerOrphanIsolation asserts backoff state is keyed per id (#773).
func TestRecheckBackoff_PerOrphanIsolation(t *testing.T) {
	b := newRecheckBackoff()
	for i := 0; i < 3; i++ {
		b.Admit("wi-a")
		b.RecordFailure("wi-a")
	}
	if b.Admit("wi-a") {
		t.Fatalf("wi-a Admit=true after K failures; want false (in backoff)")
	}
	if !b.Admit("wi-b") {
		t.Fatalf("wi-b Admit=false; want true (independent orphan)")
	}
}

// TestRecheckBackoff_BackoffExpiresAfterNTicks asserts re-admit after N Tick() calls elapse (#773).
func TestRecheckBackoff_BackoffExpiresAfterNTicks(t *testing.T) {
	b := newRecheckBackoff()
	const id = "wi-1"
	for i := 0; i < 3; i++ {
		b.Admit(id)
		b.RecordFailure(id)
	}
	for i := 0; i < recheckBackoffTicks; i++ {
		if b.Admit(id) {
			t.Fatalf("admit at suppressed tick %d; want false until N elapses", i)
		}
		b.Tick()
	}
	if !b.Admit(id) {
		t.Fatalf("Admit=false after N=%d ticks elapsed; want true (re-admitted)", recheckBackoffTicks)
	}
}

// TestRecheckBackoff_UnavailableCounterIncrements asserts gate_recheck_unavailable bumps per RecordFailure (#773).
func TestRecheckBackoff_UnavailableCounterIncrements(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	b := newRecheckBackoffWithMeter(mp.Meter("test"))
	for i := 0; i < 4; i++ {
		b.RecordFailure("wi-1")
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var got int64 = -1
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "regatta.scheduler.gate_recheck_unavailable" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("counter data type = %T; want Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				got = dp.Value
			}
		}
	}
	if got != 4 {
		t.Fatalf("counter value = %d; want 4 (one per RecordFailure)", got)
	}
}

// TestRecheckBackoff_WarnOnceOnEntry asserts RecordFailure returns true exactly once per backoff entry (#773).
func TestRecheckBackoff_WarnOnceOnEntry(t *testing.T) {
	b := newRecheckBackoff()
	const id = "wi-1"
	warns := 0
	for tick := 0; tick < 10; tick++ {
		if !b.Admit(id) {
			continue
		}
		entered := b.RecordFailure(id)
		if entered {
			warns++
		}
	}
	if warns != 1 {
		t.Fatalf("entered-backoff signal fired %d times; want exactly 1", warns)
	}
}

// TestRecheckBackoff_RespectsConfigK_N asserts the config-driven K/N override fires backoff on the Kth strike with N-tick suppression (#794).
func TestRecheckBackoff_RespectsConfigK_N(t *testing.T) {
	b := newRecheckBackoffWithConfig(recheckBackoffConfig{K: 5, SuppressTicks: 7, StaleTicks: 20})
	const id = "wi-1"
	for i := 0; i < 4; i++ {
		if !b.Admit(id) {
			t.Fatalf("strike %d: Admit=false; want true (under K=5)", i)
		}
		if entered := b.RecordFailure(id); entered {
			t.Fatalf("strike %d: enteredBackoff=true; want false (K=5 not yet hit)", i)
		}
	}
	if !b.Admit(id) {
		t.Fatalf("5th strike: Admit=false; want true (still under K=5 pre-record)")
	}
	if !b.RecordFailure(id) {
		t.Fatalf("5th strike: enteredBackoff=false; want true (hits K=5)")
	}
	for i := 0; i < 7; i++ {
		if b.Admit(id) {
			t.Fatalf("admit at suppressed tick %d; want false until N=7 elapses", i)
		}
		b.Tick()
	}
	if !b.Admit(id) {
		t.Fatalf("Admit=false after N=7 ticks elapsed; want true (re-admitted)")
	}
}

// TestRecheckBackoff_DefaultsMatchLegacyConstants asserts the zero-value config produces K=3, N=10, stale=20 (#794).
func TestRecheckBackoff_DefaultsMatchLegacyConstants(t *testing.T) {
	b := newRecheckBackoffWithConfig(recheckBackoffConfig{})
	if b.k != recheckBackoffDefaultK {
		t.Fatalf("k=%d; want %d (default)", b.k, recheckBackoffDefaultK)
	}
	if b.suppressTicks != recheckBackoffDefaultSuppressTicks {
		t.Fatalf("suppressTicks=%d; want %d (default)", b.suppressTicks, recheckBackoffDefaultSuppressTicks)
	}
	if b.staleTicks != recheckBackoffDefaultStaleTicks {
		t.Fatalf("staleTicks=%d; want %d (default)", b.staleTicks, recheckBackoffDefaultStaleTicks)
	}
}

// TestRecheckBackoff_StaleTicksEvictsIdleEntries asserts sub-K entries are evicted after stale ticks of inactivity (#794).
func TestRecheckBackoff_StaleTicksEvictsIdleEntries(t *testing.T) {
	b := newRecheckBackoffWithConfig(recheckBackoffConfig{K: 3, SuppressTicks: 10, StaleTicks: 4})
	const id = "wi-1"
	b.RecordFailure(id)
	b.RecordFailure(id)
	for i := 0; i < 4; i++ {
		b.Tick()
	}
	b.mu.Lock()
	_, present := b.entries[id]
	b.mu.Unlock()
	if present {
		t.Fatalf("entry still present after stale=4 ticks of idleness; want evicted")
	}
}

// TestRecheckBackoff_ConfigValidationClamps asserts invalid K/N/stale fall back to defaults (#794).
func TestRecheckBackoff_ConfigValidationClamps(t *testing.T) {
	b := newRecheckBackoffWithConfig(recheckBackoffConfig{K: 0, SuppressTicks: -1, StaleTicks: 1})
	if b.k != recheckBackoffDefaultK {
		t.Fatalf("k=%d; want %d (K=0 invalid, clamped to default)", b.k, recheckBackoffDefaultK)
	}
	if b.suppressTicks != recheckBackoffDefaultSuppressTicks {
		t.Fatalf("suppressTicks=%d; want %d (N<1 invalid, clamped to default)", b.suppressTicks, recheckBackoffDefaultSuppressTicks)
	}
	if b.staleTicks < b.k {
		t.Fatalf("staleTicks=%d < k=%d; want stale >= k (clamped)", b.staleTicks, b.k)
	}
}
