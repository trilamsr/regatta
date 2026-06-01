package otel_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"

	otelpkg "github.com/trilamsr/regatta/internal/obs/otel"
)

// captureHandler records every error the OTel SDK dispatches to the
// global error handler. The BSP routes export failures here rather than
// surfacing them through provider.Shutdown, so this is the seam the
// backend-down assertion hooks into.
type captureHandler struct {
	mu   sync.Mutex
	errs []error
}

func (c *captureHandler) Handle(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errs = append(c.errs, err)
}

func (c *captureHandler) snapshot() []error {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]error, len(c.errs))
	copy(out, c.errs)
	return out
}

// noopErrorHandler swallows errors so test cleanup can detach the
// capture handler without leaking exporter errors to stderr in later
// tests that share the global OTel error handler.
type noopErrorHandler struct{}

func (noopErrorHandler) Handle(error) {}

// TestSetup_OTLPEndpoint_BackendDown_StartupSucceeds pins spec §9 R2: an unreachable OTLP backend must not block app boot.
func TestSetup_OTLPEndpoint_BackendDown_StartupSucceeds(t *testing.T) {
	// Port 1 (tcpmux) is IANA-reserved and bound by nothing on a clean
	// macOS/Linux/CI host, so connect(2) returns ECONNREFUSED on the
	// first SYN — fail-fast, not slow-fail. A bogus DNS name would
	// instead spin on resolver retries and miss the < 2s budget.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")
	// Cap per-export RPC at 200ms and BSP's outer drain at 500ms so
	// shutdown surfaces the export failure inside the test budget; the
	// SDK defaults (10s + 30s) would blow it.
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "200")
	t.Setenv("OTEL_BSP_EXPORT_TIMEOUT", "500")
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "50")

	// Install the capture handler before Setup so the very first export
	// attempt (kicked off when the BSP drains during shutdown) routes
	// its connection-refused error into the test rather than stderr.
	handler := &captureHandler{}
	otel.SetErrorHandler(handler)
	t.Cleanup(func() { otel.SetErrorHandler(noopErrorHandler{}) })

	setupCtx, setupCancel := context.WithTimeout(context.Background(), time.Second)
	defer setupCancel()

	start := time.Now()
	shutdown, err := otelpkg.Setup(setupCtx, otelpkg.Config{
		ServiceName:    "regatta",
		ServiceVersion: "v0.0.0-test",
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Setup err = %v; want nil (spec §9 R2: backend-down must not block boot)", err)
	}
	if shutdown == nil {
		t.Fatal("Setup returned nil shutdown; want closure")
	}
	// 250ms is well under OTEL_EXPORTER_OTLP_TIMEOUT (200ms) + dial; a
	// breach here means Setup is dialing synchronously and the §9 R2
	// invariant has regressed.
	if elapsed > 250*time.Millisecond {
		t.Errorf("Setup blocked %v on unreachable endpoint; want <250ms (§9 R2 non-blocking)", elapsed)
	}

	// Open a span so the BSP has work to flush; the failed export on
	// shutdown is the signal that surfaces through the error handler.
	_, span := otel.Tracer("backend-down-probe").Start(context.Background(), "probe")
	span.End()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()

	shutdownStart := time.Now()
	// Shutdown must not hang on the unreachable backend; setup.go joins
	// the per-provider Shutdown errors via errors.Join so a returned
	// non-nil error is acceptable, but the load-bearing assertion is
	// that the export failure reached the global error handler.
	_ = shutdown(shutdownCtx)
	shutdownElapsed := time.Since(shutdownStart)
	if shutdownElapsed > 900*time.Millisecond {
		t.Errorf("shutdown took %v; want <900ms inside 1s ctx budget", shutdownElapsed)
	}

	got := handler.snapshot()
	if len(got) == 0 {
		t.Fatal("OTel error handler got 0 errors; want >=1 export failure surfaced from unreachable backend")
	}
}
