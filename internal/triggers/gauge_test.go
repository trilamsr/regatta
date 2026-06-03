package triggers

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestProvider_GaugeEmitsDayCount confirms the registered callback observes the live State on scrape.
func TestProvider_GaugeEmitsDayCount(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	p := NewProvider()
	if err := p.Register(MeterConfig{Meter: mp.Meter("test")}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	p.Update("30_day_green", State{DayCount: 21, DaysRemaining: 9})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	found := map[string]float64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			g, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				continue
			}
			for _, dp := range g.DataPoints {
				found[m.Name] = dp.Value
			}
		}
	}
	if got := found["regatta.green_clock.day_count"]; got != 21 {
		t.Errorf("day_count gauge = %v; want 21", got)
	}
	if got := found["regatta.trigger.days_remaining"]; got != 9 {
		t.Errorf("days_remaining gauge = %v; want 9", got)
	}
}

// TestProvider_NilMeterFallback pins the nil-meter fallback contract.
func TestProvider_NilMeterFallback(t *testing.T) {
	cfg := MeterConfig{}
	if cfg.ResolveMeter() == nil {
		t.Fatal("ResolveMeter returned nil")
	}
}
