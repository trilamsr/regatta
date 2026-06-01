package approval

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestConfig_TracerNilFallsBackToGlobal — spec §6 T5 invariant: a nil
// Gate.Tracer must not panic; Evaluate resolves to
// otel.Tracer("gates/approval") at call time. Gate.Tracer is an
// exported field rather than a positional constructor arg to keep the
// existing NewGate signature stable across 13 call sites.
func TestConfig_TracerNilFallsBackToGlobal(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := newGateTestDB(t, func() time.Time { return now })
	g := NewGate(db, &captureNotifier{}, testKeyring(), "k1",
		func() time.Time { return now },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if g.Tracer != nil {
		t.Fatalf("expected Tracer=nil before set; got %v", g.Tracer)
	}
	cfg := testCfg()
	wi := testWorkItem()
	seedWorkItem(t, db, wi, now)
	// Must not panic with nil tracer (resolves to global at call time).
	if _, err := g.Evaluate(context.Background(), wi, cfg); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
}

// TestGateApproval_EvaluateOpensGateSpan — spec §6 T5: gate evaluation
// opens a `gate.evaluate` span as a child of the active work_item
// span; verdict attribute matches the decision.
func TestGateApproval_EvaluateOpensGateSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := newGateTestDB(t, func() time.Time { return now })
	g := NewGate(db, &captureNotifier{}, testKeyring(), "k1",
		func() time.Time { return now },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	g.Tracer = tp.Tracer("gates/approval-test")

	cfg := testCfg()
	wi := testWorkItem()
	seedWorkItem(t, db, wi, now)

	// Simulate the scheduler-side work_item span wrapping.
	tracer := tp.Tracer("scheduler-test")
	ctx, parentSpan := tracer.Start(context.Background(), "work_item")
	if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	parentSpan.End()

	var parentSC []byte
	for _, sp := range sr.Ended() {
		if sp.Name() == "work_item" {
			sid := sp.SpanContext().SpanID()
			parentSC = sid[:]
		}
	}
	if parentSC == nil {
		t.Fatalf("work_item parent span not recorded")
	}
	var gateCount int
	for _, sp := range sr.Ended() {
		if sp.Name() != "gate.evaluate" {
			continue
		}
		gateCount++
		parent := sp.Parent().SpanID()
		if string(parent[:]) != string(parentSC) {
			t.Errorf("gate.evaluate parent=%x, want work_item %x", parent, parentSC)
		}
		var verdict string
		var gateID string
		for _, a := range sp.Attributes() {
			if string(a.Key) == "verdict" {
				verdict = a.Value.AsString()
			}
			if string(a.Key) == "gate_id" {
				gateID = a.Value.AsString()
			}
		}
		if verdict != ResultPause.String() {
			t.Errorf("verdict=%q, want pause", verdict)
		}
		if gateID != cfg.Name {
			t.Errorf("gate_id=%q, want %q", gateID, cfg.Name)
		}
	}
	if gateCount != 1 {
		t.Fatalf("expected 1 gate.evaluate span, got %d", gateCount)
	}
}
