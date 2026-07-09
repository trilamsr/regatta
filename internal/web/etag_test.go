package web

import "testing"

// TestEtagHash_StableForSameInput asserts identical inputs produce identical digests (R-MEGA-2 P2).
func TestEtagHash_StableForSameInput(t *testing.T) {
	a := map[string]any{"rows": []int{1, 2, 3}, "n": 3}
	b := map[string]any{"rows": []int{1, 2, 3}, "n": 3}
	if etagHash(a) != etagHash(b) {
		t.Fatalf("etagHash inconsistent for equal inputs")
	}
}

// TestEtagHash_DiffersForChangedInput asserts a value change flips the digest.
func TestEtagHash_DiffersForChangedInput(t *testing.T) {
	a := map[string]any{"rows": []int{1, 2, 3}}
	b := map[string]any{"rows": []int{1, 2, 4}}
	if etagHash(a) == etagHash(b) {
		t.Fatalf("etagHash collapsed differing inputs")
	}
}

// TestEtagHash_NilReturnsEmpty asserts etagHash(nil) returns empty (R-MEGA-2 P2).
func TestEtagHash_NilReturnsEmpty(t *testing.T) {
	if got := etagHash(nil); got != "" {
		t.Fatalf("etagHash(nil)=%q want empty", got)
	}
}

// TestEtagHash_UnmarshallableReturnsEmpty asserts unmarshallable input returns empty (R-MEGA-2 P2).
func TestEtagHash_UnmarshallableReturnsEmpty(t *testing.T) {
	ch := make(chan int)
	if got := etagHash(ch); got != "" {
		t.Fatalf("etagHash(chan)=%q want empty", got)
	}
}
