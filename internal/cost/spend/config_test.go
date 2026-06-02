package spend_test

import (
	"testing"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/trilamsr/regatta/internal/cost/spend"
)

// TestConfig_MeterDefault_NoOp pins the nil-meter fallback contract.
func TestConfig_MeterDefault_NoOp(t *testing.T) {
	cfg := spend.Config{}
	m := cfg.ResolveMeter()
	if m == nil {
		t.Fatal("ResolveMeter() returned nil; want fallback meter")
	}
	// A noop meter constructs zero-cost instruments; the round trip
	// asserts the fallback wires without panicking.
	if _, err := m.Int64Counter("probe"); err != nil {
		t.Errorf("Int64Counter on fallback meter err = %v; want nil", err)
	}
}

// TestConfig_MeterExplicit_RoundTrips pins that the caller-supplied meter wins.
func TestConfig_MeterExplicit_RoundTrips(t *testing.T) {
	want := noop.NewMeterProvider().Meter("explicit-test")
	cfg := spend.Config{Meter: want}
	got := cfg.ResolveMeter()
	if got != want {
		t.Errorf("ResolveMeter() = %v; want caller-supplied meter %v", got, want)
	}
}
