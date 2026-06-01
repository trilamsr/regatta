package spawner

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestConfig_TracerNilFallsBackToGlobal — spec §6 T5 invariant: nil
// Tracer resolves to otel.Tracer("spawner") without panic.
func TestConfig_TracerNilFallsBackToGlobal(t *testing.T) {
	s := New(Config{})
	if s.tracer == nil {
		t.Fatalf("nil-fallback failed: tracer is nil after New")
	}
}

// TestSpawner_Spawn_OpensOperatorInvocationSpan — spec §6 T5: Spawn
// emits one `operator_invocation` span; closed when the subprocess
// returns. T4 will open `llm_call` under this span via stream-json.
func TestSpawner_Spawn_OpensOperatorInvocationSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	s := New(Config{Tracer: tp.Tracer("spawner")})
	if _, err := s.Spawn(context.Background(), Request{AgentID: 1, WorkItemID: "WI-1", Lane: "server"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	var found int
	for _, sp := range sr.Ended() {
		if sp.Name() == "operator_invocation" {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("expected 1 operator_invocation span, got %d", found)
	}
}
