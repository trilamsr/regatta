package main

import (
	"context"
	"errors"
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
	shutdown, err := otelpkg.SetupMeter(context.Background(), otelpkg.Config{ServiceName: "regatta"})
	if err != nil {
		return nil, err
	}
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			slogger.Warn("meter.shutdown_failed", "err", err)
		}
	}, nil
}
