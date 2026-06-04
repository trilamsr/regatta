package fixture

import (
	"testing"
	"time"
)

// TestPollingViolation asserts O on I (#G3).
func TestPollingViolation(t *testing.T) {
	ready := false
	for i := 0; i < 100; i++ {
		if ready {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !ready {
		t.Fatal("never ready")
	}
}
