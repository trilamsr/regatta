package otel_test

import (
	"log/slog"
	"testing"

	otellog "go.opentelemetry.io/otel/log"

	obsotel "github.com/trilamsr/regatta/internal/obs/otel"
)

// TestNewBridgeHandler_AcceptsLoggerProviderDirectly pins the post-Wave-E
// signature: lp is a direct arg, not a functional option (#deletion-default).
func TestNewBridgeHandler_AcceptsLoggerProviderDirectly(t *testing.T) {
	t.Helper()
	var lp otellog.LoggerProvider
	h := obsotel.NewBridgeHandler(slog.Default().Handler(), "regatta-sig-test", lp)
	if h == nil {
		t.Fatal("NewBridgeHandler returned nil")
	}
}
