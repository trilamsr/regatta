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
			b.RecordFailure(context.Background(), id)
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
		b.RecordFailure(context.Background(), id)
	}
	b.RecordSuccess(id)
	for i := 0; i < 3; i++ {
		if !b.Admit(id) {
			t.Fatalf("post-success tick %d: Admit=false; want true (budget reset)", i)
		}
		b.RecordFailure(context.Background(), id)
	}
}

// TestRecheckBackoff_PerOrphanIsolation asserts backoff state is keyed per id (#773).
func TestRecheckBackoff_PerOrphanIsolation(t *testing.T) {
	b := newRecheckBackoff()
	for i := 0; i < 3; i++ {
		b.Admit("wi-a")
		b.RecordFailure(context.Background(), "wi-a")
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
		b.RecordFailure(context.Background(), id)
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
		b.RecordFailure(context.Background(), "wi-1")
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
		entered := b.RecordFailure(context.Background(), id)
		if entered {
			warns++
		}
	}
	if warns != 1 {
		t.Fatalf("entered-backoff signal fired %d times; want exactly 1", warns)
	}
}
