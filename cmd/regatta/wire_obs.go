package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	otelpkg "github.com/trilamsr/regatta/internal/obs/otel"
)

// wireMeterProvider invokes otelpkg.SetupMeter so every meter.Int64Counter
// call resolves to the OTLP / Prom exporter the operator env wired. Without
// the call the global MeterProvider stays noop and every regatta.* metric
// drops silently. Returns a shutdownFn the caller must defer; the
// shutdown derives a NEW bounded context (not a child of the
// SIGTERM-canceled parent) so the final flush still has 5s to drain.
func wireMeterProvider(_ context.Context, slogger *slog.Logger) (func(), error) {
	shutdown, err := otelpkg.SetupMeter(context.Background(), otelpkg.Config{ServiceName: "regatta"}) //nolint:contextcheck // Setup uses Background so the SIGTERM-canceled serve ctx does not poison the exporter wiring.
	if err != nil {
		return nil, err
	}
	//nolint:contextcheck // parent ctx is already SIGTERM-canceled when shutdown fires; derive fresh background-bounded ctx so the flush window survives.
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			slogger.Warn("meter.shutdown_failed", "err", err)
		}
	}, nil
}

// wireObservability launches both OTel providers (meter + tracer/log)
// at boot and returns a combined shutdown the caller defers exactly
// once. Bundles the two SetupX calls so serve.go stays under the
// file-size ceiling.
func wireObservability(ctx context.Context, slogger *slog.Logger) (func(), error) {
	meterShutdown, err := wireMeterProvider(ctx, slogger)
	if err != nil {
		return nil, fmt.Errorf("setup meter: %w", err)
	}
	tracerShutdown, err := wireTracerProvider(ctx, slogger)
	if err != nil {
		meterShutdown()
		return nil, fmt.Errorf("setup tracer: %w", err)
	}
	return func() {
		tracerShutdown()
		meterShutdown()
	}, nil
}

// wireTracerProvider invokes otelpkg.Setup so spans + logs flow
// through the OTLP exporter the operator env wired. Without the call
// the global TracerProvider stays noop and every span / log batch
// drops silently — same root cause class as the meter wiring (R2-Bug-11)
// but for the trace+log pair. Returns a shutdown closure with the
// same SIGTERM-safe child-of-background ctx as the meter shutdown.
func wireTracerProvider(_ context.Context, slogger *slog.Logger) (func(), error) {
	shutdown, err := otelpkg.Setup(context.Background(), otelpkg.Config{ServiceName: "regatta"}) //nolint:contextcheck // Setup uses Background so the SIGTERM-canceled serve ctx does not poison the exporter wiring.
	if err != nil {
		return nil, err
	}
	//nolint:contextcheck // parent ctx is already SIGTERM-canceled when shutdown fires; derive fresh background-bounded ctx so the flush window survives.
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			slogger.Warn("tracer.shutdown_failed", "err", err)
		}
	}, nil
}
