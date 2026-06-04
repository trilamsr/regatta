package otel_test

import (
	"log/slog"
	"testing"

	otellog "go.opentelemetry.io/otel/log"

	obsotel "github.com/trilamsr/regatta/internal/obs/otel"
)

// TestNewBridgeHandler_AcceptsLoggerProviderDirectly pins post-Wave-E NewBridgeHandler signature: lp as direct arg, not functional option.
func TestNewBridgeHandler_AcceptsLoggerProviderDirectly(t *testing.T) {
	t.Helper()
	var lp otellog.LoggerProvider
	h := obsotel.NewBridgeHandler(slog.Default().Handler(), "regatta-sig-test", lp)
	if h == nil {
		t.Fatal("NewBridgeHandler returned nil")
	}
}
