package reaper

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestConfig_TracerNilFallsBackToGlobal — spec §6 T5 invariant: a nil
// Tracer must not panic; New resolves to otel.Tracer("reaper").
func TestConfig_TracerNilFallsBackToGlobal(t *testing.T) {
	r := New(Config{DB: statetest.OpenDB(t), WM: newWM(t)})
	if r.tracer == nil {
		t.Fatalf("nil-fallback failed: tracer is nil after New")
	}
}

// TestReaper_SweepOpensReapSpan — spec §6 T5: ReapAll opens a
// `reaper.sweep` span per call. Standalone (not nested under tick) per
// spec §3.5 architecture diagram.
func TestReaper_SweepOpensReapSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	r := New(Config{
		DB:     statetest.OpenDB(t),
		WM:     newWM(t),
		Tracer: tp.Tracer("reaper"),
	})
	if err := r.ReapAll(context.Background()); err != nil {
		t.Fatalf("ReapAll: %v", err)
	}
	spans := sr.Ended()
	var sweepCount int
	for _, s := range spans {
		if s.Name() == "reaper.sweep" {
			sweepCount++
			if s.Parent().IsValid() {
				t.Errorf("reaper.sweep span has parent (expected standalone per spec §3.5): %v", s.Parent())
			}
		}
	}
	if sweepCount != 1 {
		t.Fatalf("expected 1 reaper.sweep span, got %d (all spans: %d)", sweepCount, len(spans))
	}
}
