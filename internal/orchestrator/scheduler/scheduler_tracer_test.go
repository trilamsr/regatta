package scheduler

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestConfig_TracerNilFallsBackToGlobal — spec §6 T5 invariant: nil
// Tracer resolves to otel.Tracer("scheduler") without panic.
func TestConfig_TracerNilFallsBackToGlobal(t *testing.T) {
	s := New(statetest.OpenDB(t), Config{})
	if s.tracer == nil {
		t.Fatalf("nil-fallback failed: tracer is nil after New")
	}
}

// TestScheduler_Tick_WorkItemSpansChildOfTick — spec §6 T5: every
// `work_item` span observed in a tick has the tick span as parent.
// The orchestrator owns the `tick` span (see orchestrator.ScheduleOnce);
// here we open it manually to test the scheduler-side parenting.
func TestScheduler_Tick_WorkItemSpansChildOfTick(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	db := statetest.OpenDB(t)
	seedPlanned(t, db, "F-1", "server")
	seedPlanned(t, db, "F-2", "server")

	tracer := tp.Tracer("scheduler-test")
	s := New(db, Config{Tracer: tracer})

	// Simulate the orchestrator's tick-span wrapping.
	ctx, tickSpan := tracer.Start(context.Background(), "tick")
	if _, err := s.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	tickSpan.End()

	var tickSC []byte
	for _, sp := range sr.Ended() {
		if sp.Name() == "tick" {
			sid := sp.SpanContext().SpanID()
			tickSC = sid[:]
		}
	}
	if tickSC == nil {
		t.Fatalf("tick span not recorded")
	}
	var wiCount int
	for _, sp := range sr.Ended() {
		if sp.Name() != "work_item" {
			continue
		}
		wiCount++
		parent := sp.Parent().SpanID()
		if string(parent[:]) != string(tickSC) {
			t.Errorf("work_item span has parent %x, want tick %x", parent, tickSC)
		}
	}
	if wiCount == 0 {
		t.Fatalf("expected ≥1 work_item span, got 0")
	}
}

// TestScheduler_Tick_WorkItemAttrsPresent — pins spec §4.1 attribute
// set on work_item spans (work_item_id, lane, regatta.kind).
func TestScheduler_Tick_WorkItemAttrsPresent(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	db := statetest.OpenDB(t)
	seedPlanned(t, db, "F-1", "server")

	s := New(db, Config{Tracer: tp.Tracer("scheduler-test")})
	if _, err := s.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	for _, sp := range sr.Ended() {
		if sp.Name() != "work_item" {
			continue
		}
		got := map[string]string{}
		for _, a := range sp.Attributes() {
			got[string(a.Key)] = a.Value.AsString()
		}
		if got["work_item_id"] != "F-1" {
			t.Errorf("work_item_id=%q, want F-1", got["work_item_id"])
		}
		if got["lane"] != "server" {
			t.Errorf("lane=%q, want server", got["lane"])
		}
		if got["regatta.kind"] != string(state.KindFeature) {
			t.Errorf("regatta.kind=%q, want %q", got["regatta.kind"], state.KindFeature)
		}
		return
	}
	t.Fatalf("no work_item span observed")
}
