package main

import (
	"strings"
	"testing"
)

// TestStateDSNFromArgs_RoutesThroughStateDSN pins R24: resume path picks up every state.DSN pragma so concurrent reader/writer scaling matches the orchestrator's main DB.
func TestStateDSNFromArgs_RoutesThroughStateDSN(t *testing.T) {
	got := stateDSNFromArgs(nil)
	for _, want := range []string{
		"busy_timeout(5000)",
		"foreign_keys(1)",
		"journal_mode(WAL)",
		"synchronous(NORMAL)",
		"_txlock=immediate",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stateDSNFromArgs missing %q in %q", want, got)
		}
	}
}
