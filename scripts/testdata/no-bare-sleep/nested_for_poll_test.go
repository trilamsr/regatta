package fixture

import (
	"testing"
	"time"
)

// TestNestedForPoll asserts O on I (#G3).
func TestNestedForPoll(t *testing.T) {
	results := make([]int, 0)
	for outer := 0; outer < 3; outer++ {
		for inner := 0; inner < 10; inner++ {
			if len(results) == outer*10+inner {
				results = append(results, inner)
			}
			time.Sleep(1 * time.Millisecond)
		}
	}
	if len(results) != 30 {
		t.Fatalf("got %d", len(results))
	}
}
