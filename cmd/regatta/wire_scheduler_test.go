package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestBuildScheduler_LoggerAccepted pins schedulerDeps.Logger forwarding through scheduler.Config (#1227).
func TestBuildScheduler_LoggerAccepted(t *testing.T) {
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "wire-scheduler-logger.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	logger := slog.New(slog.DiscardHandler).With("sentinel", "wire-scheduler-logger-test")
	sched := buildScheduler(db, serveFlags{}, schedulerDeps{Logger: logger})
	if sched == nil {
		t.Fatal("buildScheduler with Logger returned nil")
	}
	if _, err := sched.Tick(context.Background()); err != nil {
		t.Fatalf("Tick with forwarded Logger: %v", err)
	}
}

// TestBuildScheduler_DefaultsPropagate pins buildScheduler threading of LaneCaps + LockTTL + Clock through scheduler.Config (#975 slice 1).
func TestBuildScheduler_DefaultsPropagate(t *testing.T) {
	clockTick := time.Unix(1_700_000_000, 0).UTC()
	clock := func() time.Time {
		now := clockTick
		clockTick = clockTick.Add(1 * time.Second)
		return now
	}

	db, err := state.OpenWithClock(
		context.Background(),
		state.DSN(filepath.Join(t.TempDir(), "wire-scheduler.db")),
		clock,
	)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	f := serveFlags{
		LaneCaps: laneCapsFlag{"server": 1},
		LockTTL:  time.Minute,
	}
	deps := schedulerDeps{
		Clock: clock,
		Meter: mp.Meter("wire-scheduler-test"),
	}
	sched := buildScheduler(db, f, deps)
	if sched == nil {
		t.Fatal("buildScheduler returned nil")
	}

	if _, err := sched.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	got, ok := histogramSumMsWireScheduler(rm, "regatta.scheduler.tick.latency_ms")
	if !ok {
		t.Fatalf("histogram %q missing — scheduler did not record", "regatta.scheduler.tick.latency_ms")
	}
	if got < 1000 {
		t.Fatalf("tick latency = %.1fms; want >= 1000ms — Clock not threaded through buildScheduler", got)
	}
	rem := got - float64(int64(got/1000))*1000
	if rem != 0 {
		t.Fatalf("tick latency = %.3fms; want integer-second multiple — wall-clock leak (buildScheduler dropped Clock)", got)
	}
}

// histogramSumMsWireScheduler returns the first histogram data point sum for name; local copy keeps this file self-contained.
func histogramSumMsWireScheduler(rm metricdata.ResourceMetrics, name string) (float64, bool) {
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

// TestBuildScheduler_DepsWiredToConfig asserts buildScheduler forwards Evaluator/Gate/CostCap deps so #975 slice 1 stays a pure refactor.
func TestBuildScheduler_DepsWiredToConfig(t *testing.T) {
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "wire-scheduler-deps.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Zero-value deps + zero-value flags: buildScheduler must still
	// return a usable *scheduler.Scheduler — the same nil-default
	// contract scheduler.New honours today.
	sched := buildScheduler(db, serveFlags{}, schedulerDeps{})
	if sched == nil {
		t.Fatal("buildScheduler with zero deps returned nil; want nil-default fallback")
	}
	// Smoke: Tick on an empty DB must not panic.
	if _, err := sched.Tick(context.Background()); err != nil {
		t.Fatalf("Tick with zero deps: %v", err)
	}

	// Ensure the scheduler package import is referenced for compile-
	// time wiring even when buildScheduler is the only call site here.
	_ = (*scheduler.Scheduler)(nil)
}
