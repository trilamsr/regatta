package fixture

import (
	"testing"
	"time"
)

// TestAllowlisted asserts O on I (#G3).
func TestAllowlisted(t *testing.T) {
	ready := false
	for i := 0; i < 100; i++ {
		if ready {
			break
		}
		time.Sleep(5 * time.Millisecond) // allow-sleep: external scheduler advances tick on wall clock; no signal channel exposed
	}
	if !ready {
		t.Fatal("never ready")
	}
}
