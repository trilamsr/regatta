package main

import (
	"context"
	"log/slog"
	"time"

	otelpkg "github.com/trilamsr/regatta/internal/obs/otel"
)

// wireMeterProvider invokes otelpkg.SetupMeter so every meter.Int64Counter
// call resolves to the OTLP / Prom exporter the operator env wired. Without
// the call the global MeterProvider stays noop and every regatta.* metric
// drops silently. Returns a shutdownFn the caller must defer; the
// shutdown derives a bounded child context from the parent so SIGTERM
// still bounds the flush window.
func wireMeterProvider(ctx context.Context, slogger *slog.Logger) (func(), error) {
	shutdown, err := otelpkg.SetupMeter(ctx, otelpkg.Config{ServiceName: "regatta"})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			slogger.Warn("meter.shutdown_failed", "err", err)
		}
	}, nil
}
