package state

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// PersistTraceIDFromContext is the single seam every state insert path
// uses to populate the trace_id column from the active OTel span.
// Spec §3.5 + §8 seam contracts bullet 3: T4 + T5 (and any future
// insert path) call this helper rather than reaching into
// trace.SpanContextFromContext directly, so the empty-default rule
// and the 32-hex lowercase contract live in one place.
//
// No active span (no OTel SDK initialized, or insert called outside
// any tracer.Start scope) sets *dest to "" — pinned by
// TestNoActiveSpan_PersistsEmptyTraceID.
func PersistTraceIDFromContext(ctx context.Context, dest *string) {
	if dest == nil {
		return
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		*dest = ""
		return
	}
	*dest = sc.TraceID().String()
}
