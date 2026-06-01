package adaptersync_test

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
)

// TestConfig_TracerNilFallsBackToGlobal — spec §6 T5 nil-tracer invariant.
func TestConfig_TracerNilFallsBackToGlobal(t *testing.T) {
	s := mustNew(t, adaptersync.Config{Adapter: &stubAdapter{}, DB: newSyncTestDB(t)})
	// No panic on Sync → tracer resolved at construct-time.
	if err := s.Sync(context.Background(), time.Now()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

// TestAdapterSync_OpensSyncSpan — spec §8 T5 entry-point span invariant.
func TestAdapterSync_OpensSyncSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	stub := &stubAdapter{items: []schemas.WorkItem{
		{ID: "ITEM-1", Kind: schemas.KindFeature, Status: schemas.StatusPlanned, Lane: "server"},
	}}
	s := mustNew(t, adaptersync.Config{Adapter: stub, DB: newSyncTestDB(t), Tracer: tp.Tracer("adaptersync-test")})
	if err := s.Sync(context.Background(), time.Now()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	var count int
	for _, sp := range sr.Ended() {
		if sp.Name() == "adaptersync.sync" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 adaptersync.sync span, got %d", count)
	}
}
