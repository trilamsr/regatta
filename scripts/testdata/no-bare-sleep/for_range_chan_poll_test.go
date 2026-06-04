package fixture

import (
	"testing"
	"time"
)

// TestForRangeChanPoll asserts O on I (#G3).
func TestForRangeChanPoll(t *testing.T) {
	ch := make(chan int, 1)
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case v := <-ch:
			if v == 42 {
				return
			}
		default:
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("never received 42")
}
