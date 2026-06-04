package fixture

import (
	"testing"
	"time"
)

// TestStandaloneSleep asserts O on I (#G3).
//
// time.Sleep here is a one-shot observation window (no polling loop) — the
// for-loop below the Sleep is a result-collection iteration, not the
// surrounding poll structure that the gate forbids.
func TestStandaloneSleep(t *testing.T) {
	results := map[string]int{"a": 1, "b": 2}
	// One-shot settle, no surrounding for.
	time.Sleep(10 * time.Millisecond)
	for k, v := range results {
		if v == 0 {
			t.Fatalf("zero value for %s", k)
		}
	}
}
