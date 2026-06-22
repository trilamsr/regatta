package main

import (
	"os"
	"strings"
	"testing"
)

// TestServe_CallsSetupMeterOnce asserts serve.go wires the global OTel MeterProvider before subsystems start (else every counter drops silently).
func TestServe_CallsSetupMeterOnce(t *testing.T) {
	serveSrc, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatalf("read serve.go: %v", err)
	}
	if !strings.Contains(string(serveSrc), "wireMeterProvider(") {
		t.Fatalf("serve.go missing wireMeterProvider(ctx, ...) call; global MeterProvider stays noop and every regatta.* metric silently drops. Re-add the call near boot (post-secrets, pre-orchestrator).")
	}
	wireSrc, err := os.ReadFile("wire_obs.go")
	if err != nil {
		t.Fatalf("read wire_obs.go: %v", err)
	}
	if !strings.Contains(string(wireSrc), "otelpkg.SetupMeter(") {
		t.Fatal("wire_obs.go missing otelpkg.SetupMeter() body; wireMeterProvider must actually invoke the SDK setup.")
	}
}
