package adapter

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestConfig_TracerNilFallsBackToGlobal — spec §6 T5 nil-tracer invariant.
func TestConfig_TracerNilFallsBackToGlobal(t *testing.T) {
	dir := t.TempDir()
	writeItem(t, dir, "good.md", sampleItem)
	a, err := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir})
	if err != nil {
		t.Fatalf("NewMarkdownCatalog: %v", err)
	}
	if _, err := a.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
}

// TestMarkdownAdapter_OpensListSpan — spec §8 T5 entry-point span invariant.
func TestMarkdownAdapter_OpensListSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	dir := t.TempDir()
	writeItem(t, dir, "good.md", sampleItem)
	a, err := NewMarkdownCatalog(MarkdownCatalogConfig{Root: dir, Tracer: tp.Tracer("adapter-test")})
	if err != nil {
		t.Fatalf("NewMarkdownCatalog: %v", err)
	}
	if _, err := a.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	var count int
	for _, sp := range sr.Ended() {
		if sp.Name() == "adapter.markdown.list" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 adapter.markdown.list span, got %d", count)
	}
}
