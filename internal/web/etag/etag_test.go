package etag

import "testing"

// TestHash_StableForSameInput asserts identical row-sets produce
// identical hex digests so the ETag header round-trips for htmx 304s.
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

// TestHash_NilReturnsEmpty asserts a nil view skips the ETag rather
// than emitting a misleading constant digest.
func TestHash_NilReturnsEmpty(t *testing.T) {
	if got := Hash(nil); got != "" {
		t.Fatalf("Hash(nil)=%q want empty", got)
	}
}

// TestHash_UnmarshallableReturnsEmpty asserts a value json.Marshal cannot
// encode (e.g. channel) returns empty rather than panicking.
func TestHash_UnmarshallableReturnsEmpty(t *testing.T) {
	ch := make(chan int)
	if got := Hash(ch); got != "" {
		t.Fatalf("Hash(chan)=%q want empty", got)
	}
}
