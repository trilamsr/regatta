package fixture

import (
	"testing"
)

// TestClean asserts O on I (#G3).
func TestClean(t *testing.T) {
	if 1+1 != 2 {
		t.Fatal("math broken")
	}
}
