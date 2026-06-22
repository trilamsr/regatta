package main

import (
	"os"
	"strings"
	"testing"
)

// TestServe_CallsObservabilityWireOnce asserts serve.go wires both OTel providers (meter + tracer) before subsystems start (else metrics + spans drop silently).
func TestServe_CallsObservabilityWireOnce(t *testing.T) {
	serveSrc, err := os.ReadFile("serve.go")
	if err != nil {
		t.Fatalf("read serve.go: %v", err)
	}
	if !strings.Contains(string(serveSrc), "wireObservability(") {
		t.Fatalf("serve.go missing wireObservability(ctx, ...) call; global Meter+Tracer providers stay noop and every regatta.* metric/span drops. Re-add the call near boot (post-secrets, pre-orchestrator).")
	}
	wireSrc, err := os.ReadFile("wire_obs.go")
	if err != nil {
		t.Fatalf("read wire_obs.go: %v", err)
	}
	for _, sym := range []string{"otelpkg.SetupMeter(", "otelpkg.Setup("} {
		if !strings.Contains(string(wireSrc), sym) {
			t.Errorf("wire_obs.go missing %s body; wireObservability must actually invoke the SDK setup.", sym)
		}
	}
}
