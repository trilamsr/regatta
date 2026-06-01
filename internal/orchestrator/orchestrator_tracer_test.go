//go:build unix

package orchestrator

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
)

// TestConfig_TracerNilFallsBackToGlobal — spec §6 T5 nil-tracer invariant.
func TestConfig_TracerNilFallsBackToGlobal(t *testing.T) {
	o, _, _, _ := newHarness(t, 0)
	if o.tracer == nil {
		t.Fatalf("nil-fallback failed: tracer is nil after New")
	}
}

// TestScheduler_Tick_OpensTickSpan — spec §6 T5 tick-span-per-ScheduleOnce invariant.
func TestScheduler_Tick_OpensTickSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	o, _, db, _ := newHarness(t, 1)
	o.tracer = tp.Tracer("orchestrator-test")

	ctx := context.Background()
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	// Reset recorder so we only count tick spans from ScheduleOnce.
	sr.Reset()
	syncer, _ := adaptersync.New(adaptersync.Config{Adapter: nil, DB: db})
	_ = syncer

	stub := spawner.New(spawner.Config{Tracer: tp.Tracer("spawner-test")})
	o.spawner = stub
	o.sched = scheduler.New(db, scheduler.Config{Tracer: tp.Tracer("scheduler-test")})

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("ScheduleOnce: %v", err)
	}
	var tickCount int
	for _, sp := range sr.Ended() {
		if sp.Name() == "tick" {
			tickCount++
		}
	}
	if tickCount != 1 {
		t.Fatalf("expected 1 tick span, got %d", tickCount)
	}
}
