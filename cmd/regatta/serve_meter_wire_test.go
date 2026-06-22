package main

import (
	"os"
	"strings"
	"testing"
)

// TestServe_CallsSetupMeterOnce asserts cmd/regatta/serve.go wires the
// global OTel MeterProvider before subsystems run. Without the call
// every meter.Int64Counter resolves to the SDK noop provider and
// metrics drop silently regardless of OTLP env config. The serve.go
// surface is too wide for a runtime integration test; a source-grep
// catches regressions cheaply.
func TestServe_CallsSetupMeterOnce(t *testing.T) {
	body, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatalf("read serve.go: %v", err)
	}
	src := string(body)
	if !strings.Contains(src, "otelpkg.SetupMeter(") {
		t.Fatalf("serve.go missing otelpkg.SetupMeter(ctx, ...) wiring; global MeterProvider stays noop and every regatta.* metric silently drops. Re-add the SetupMeter call near boot (post-secrets, pre-orchestrator).")
	}
}
