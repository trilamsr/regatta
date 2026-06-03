package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestTick_UsesInjectedClock_NotWallClock asserts injected clock drives tick latency, not wall clock.
func TestTick_UsesInjectedClock_NotWallClock(t *testing.T) {
	dbClock := time.Unix(1_700_000_000, 0).UTC()
	db, err := state.OpenWithClock(
		context.Background(),
		state.DSN(filepath.Join(t.TempDir(), "tick.db")),
		func() time.Time { return dbClock },
	)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// stepClock advances by exactly 1s each call so the test is
	// invariant under future steps-loop changes — any positive
	// integer-second value proves the injected clock won over
	// wall-clock fallback (sub-ms in a unit test).
	stepClock := dbClock
	clockFn := func() time.Time {
		now := stepClock
		stepClock = stepClock.Add(1 * time.Second)
		return now
	}

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	sch := New(db, Config{
		LockTTL: time.Minute,
		Meter:   mp.Meter("scheduler-clock-injection-test"),
		Clock:   clockFn,
	})
	if _, err := sch.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	got, ok := histogramSumMs(rm, "regatta.scheduler.tick.latency_ms")
	if !ok {
		t.Fatalf("histogram %q missing — instrument not recorded", "regatta.scheduler.tick.latency_ms")
	}
	if got < 1000 {
		t.Fatalf("tick latency = %.1fms; want >= 1000ms — proves wall clock used not injected clock", got)
	}
	// Each Clock() call advances 1 000 ms; an exact-ms-integer value
	// proves time.Since(tickStart) operated on injected timestamps
	// (real wall clock would yield sub-ms fractional remainder).
	rem := got - float64(int64(got/1000))*1000
	if rem != 0 {
		t.Fatalf("tick latency = %.3fms; want integer-second multiple — wall-clock leak suspected", got)
	}
}

// TestTick_NilClock_DefaultsToWallClock asserts zero-value Config falls back to time.Now for back-compat.
func TestTick_NilClock_DefaultsToWallClock(t *testing.T) {
	dbClock := time.Unix(1_700_000_000, 0).UTC()
	db, err := state.OpenWithClock(
		context.Background(),
		state.DSN(filepath.Join(t.TempDir(), "tick.db")),
		func() time.Time { return dbClock },
	)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	sch := New(db, Config{
		LockTTL: time.Minute,
		Meter:   mp.Meter("scheduler-clock-default-test"),
		// Clock left nil — must default to time.Now.
	})
	if _, err := sch.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	got, ok := histogramSumMs(rm, "regatta.scheduler.tick.latency_ms")
	if !ok {
		t.Fatalf("histogram missing")
	}
	if got < 0 {
		t.Fatalf("tick latency = %.3fms; want >= 0", got)
	}
	// Wall-clock test: tick should complete in well under one second.
	if got > 5_000 {
		t.Fatalf("tick latency = %.1fms; want < 5000 — default clock should be wall clock", got)
	}
}

// histogramSumMs returns the sum field of the first histogram data
// point for name, or (0, false) if absent. Use Sum (not bucket
// counts) because the regatta scheduler instruments record one
// observation per tick and Sum is exact for a single observation.
func histogramSumMs(rm metricdata.ResourceMetrics, name string) (float64, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || len(hist.DataPoints) == 0 {
				return 0, false
			}
			return hist.DataPoints[0].Sum, true
		}
	}
	return 0, false
}
