package orchestrator_test

import (
	"testing"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

// TestConfig_MeterDefault_NoOp pins the nil-meter fallback contract on orchestrator Config.
func TestConfig_MeterDefault_NoOp(t *testing.T) {
	cfg := orchestrator.Config{}
	m := cfg.ResolveMeter()
	if m == nil {
		t.Fatal("ResolveMeter() returned nil; want fallback meter")
	}
	if _, err := m.Int64Counter("probe"); err != nil {
		t.Errorf("Int64Counter on fallback meter err = %v; want nil", err)
	}
}

// TestConfig_MeterExplicit_RoundTrips pins the caller-supplied meter wiring.
func TestConfig_MeterExplicit_RoundTrips(t *testing.T) {
	want := noop.NewMeterProvider().Meter("explicit-test")
	cfg := orchestrator.Config{Meter: want}
	got := cfg.ResolveMeter()
	if got != want {
		t.Errorf("ResolveMeter() = %v; want caller-supplied meter", got)
	}
}
