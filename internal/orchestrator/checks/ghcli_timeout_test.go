package checks

import (
	"testing"
	"time"
)

// TestDefaultGHTimeout_NonZeroAndSane asserts the per-call gh-shell timeout is set and within an operator-sane range (R31-I4: hung gh PR-checks stalled orchestrator sweep loop indefinitely before this).
func TestDefaultGHTimeout_NonZeroAndSane(t *testing.T) {
	if defaultGHTimeout == 0 {
		t.Fatalf("defaultGHTimeout=0; the gate exists to prevent the indefinite-stall class (R31-I4)")
	}
	if defaultGHTimeout < 1*time.Second || defaultGHTimeout > 60*time.Second {
		t.Fatalf("defaultGHTimeout=%s outside sane operator range [1s,60s]", defaultGHTimeout)
	}
}
