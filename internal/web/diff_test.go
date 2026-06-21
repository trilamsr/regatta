package web

import (
	"strings"
	"testing"
)

// TestClampDiff_TruncatesAt8KiB asserts a >8 KiB input clamps to ≤MaxDiffBytes with overflowed=true (MAY-116).
func TestClampDiff_TruncatesAt8KiB(t *testing.T) {
	in := []byte(strings.Repeat("a", 20*1024))
	clamped, overflowed := ClampDiff(in)
	if len(clamped) > MaxDiffBytes {
		t.Fatalf("ClampDiff len = %d want <= %d", len(clamped), MaxDiffBytes)
	}
	if !overflowed {
		t.Fatalf("ClampDiff overflowed = false want true for %d-byte input", len(in))
	}
}

// TestClampDiff_PassesUnderCap asserts an input <=MaxDiffBytes is returned verbatim with overflowed=false (MAY-116).
func TestClampDiff_PassesUnderCap(t *testing.T) {
	in := []byte(strings.Repeat("b", MaxDiffBytes))
	clamped, overflowed := ClampDiff(in)
	if overflowed {
		t.Fatalf("ClampDiff overflowed = true want false for exactly-cap input")
	}
	if len(clamped) != len(in) {
		t.Fatalf("ClampDiff len = %d want %d", len(clamped), len(in))
	}
}
