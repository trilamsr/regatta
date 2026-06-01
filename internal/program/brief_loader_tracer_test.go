package program

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestConfig_TracerNilFallsBackToGlobal — spec §6 T5 nil-tracer invariant.
func TestConfig_TracerNilFallsBackToGlobal(t *testing.T) {
	l := mustNewLoader(t, BriefLoaderConfig{
		FS:      fstest.MapFS{},
		DB:      newBriefTestDB(t),
		Keyring: map[string][]byte{},
	})
	// Sync over empty FS must not panic — nil-fallback resolved.
	if err := l.Sync(context.Background(), time.Now()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

// TestBriefLoader_OpensSyncSpan — spec §8 T5 entry-point span invariant.
func TestBriefLoader_OpensSyncSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	l := mustNewLoader(t, BriefLoaderConfig{
		FS:      fstest.MapFS{},
		DB:      newBriefTestDB(t),
		Keyring: map[string][]byte{},
		Tracer:  tp.Tracer("program-test"),
	})
	if err := l.Sync(context.Background(), time.Now()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	var count int
	for _, sp := range sr.Ended() {
		if sp.Name() == "brief_loader.sync" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 brief_loader.sync span, got %d", count)
	}
}
