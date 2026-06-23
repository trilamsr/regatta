package etag

import "testing"

// TestHash_StableForSameInput asserts identical inputs produce identical digests (R-MEGA-2 P2).
func TestHash_StableForSameInput(t *testing.T) {
	a := map[string]any{"rows": []int{1, 2, 3}, "n": 3}
	b := map[string]any{"rows": []int{1, 2, 3}, "n": 3}
	if Hash(a) != Hash(b) {
		t.Fatalf("Hash inconsistent for equal inputs")
	}
}

// TestHash_DiffersForChangedInput asserts a value change flips the digest.
func TestHash_DiffersForChangedInput(t *testing.T) {
	a := map[string]any{"rows": []int{1, 2, 3}}
	b := map[string]any{"rows": []int{1, 2, 4}}
	if Hash(a) == Hash(b) {
		t.Fatalf("Hash collapsed differing inputs")
	}
}

// TestHash_NilReturnsEmpty asserts Hash(nil) returns empty (R-MEGA-2 P2).
func TestHash_NilReturnsEmpty(t *testing.T) {
	if got := Hash(nil); got != "" {
		t.Fatalf("Hash(nil)=%q want empty", got)
	}
}

// TestHash_UnmarshallableReturnsEmpty asserts unmarshallable input returns empty (R-MEGA-2 P2).
func TestHash_UnmarshallableReturnsEmpty(t *testing.T) {
	ch := make(chan int)
	if got := Hash(ch); got != "" {
		t.Fatalf("Hash(chan)=%q want empty", got)
	}
}
