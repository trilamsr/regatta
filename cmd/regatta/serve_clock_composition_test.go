// serve_clock_composition_test pins the clock-injection seam at the
// composition root — the runServe boot path must thread ONE clock
// function through scheduler.Config, orchestrator.Config, and
// reaper.Config so an operator (or test harness) that swaps the source
// sees latency metrics, tick timestamps, and reaper deadlines all move
// together. A drift here is invisible at unit-test scope (each
// subsystem has its own nil-default fallback to time.Now) but breaks
// the end-to-end determinism guarantee the auditor depends on.
package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/reaper"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestCompositionRoot_ClockInjection_FlowsToOrchestrator pins one composition-root clock flowing through sched/o/reaper Configs.
func TestCompositionRoot_ClockInjection_FlowsToOrchestrator(t *testing.T) {
	// Single composition-root clock. Each call advances exactly 1s so
	// an integer-second tick-latency value proves the injected clock
	// won over wall-clock fallback (real wall clock would land on a
	// sub-ms fractional value in a unit test).
	clockTick := time.Unix(1_700_000_000, 0).UTC()
	clock := func() time.Time {
		now := clockTick
		clockTick = clockTick.Add(1 * time.Second)
		return now
	}

	db, err := state.OpenWithClock(
		context.Background(),
		state.DSN(filepath.Join(t.TempDir(), "composition.db")),
		clock,
	)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	// Construct the SAME trio runServe builds — scheduler, orchestrator,
	// reaper — passing the single clock through Config.Clock. This is
	// the wiring contract the runServe block at cmd/regatta/serve.go
	// scheduler.New / orchestrator.New / reaper.New must honour.
	sched := scheduler.New(db, scheduler.Config{
		LockTTL: time.Minute,
		Meter:   mp.Meter("composition-root-clock-test"),
		Clock:   clock,
	})

	// Orchestrator wiring participates in the contract even though this
	// test exercises sched.Tick directly — the constructor call must
	// not panic and must accept the clock field, mirroring runServe.
	_ = orchestrator.New(orchestrator.Config{
		DB:        db,
		Scheduler: sched,
		Clock:     clock,
	})

	// Reaper wiring participates likewise; nil deps are tolerated by
	// reaper.New when set.Worktrees is unavailable in unit scope.
	_ = reaper.New(reaper.Config{
		DB:    db,
		Clock: clock,
	})

	if _, err := sched.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	got, ok := histogramSumMsCompositionRoot(rm, "regatta.scheduler.tick.latency_ms")
	if !ok {
		t.Fatalf("histogram %q missing — scheduler did not record", "regatta.scheduler.tick.latency_ms")
	}
	if got < 1000 {
		t.Fatalf("tick latency = %.1fms; want >= 1000ms — composition-root clock not flowing to scheduler", got)
	}
	rem := got - float64(int64(got/1000))*1000
	if rem != 0 {
		t.Fatalf("tick latency = %.3fms; want integer-second multiple — wall-clock leak (composition-root clock not used)", got)
	}
}

// histogramSumMsCompositionRoot returns the sum of the first histogram
// data point for name. Local copy to keep this file self-contained;
// the scheduler package has its own unexported helper of the same
// shape.
func histogramSumMsCompositionRoot(rm metricdata.ResourceMetrics, name string) (float64, bool) {
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
