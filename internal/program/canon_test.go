package program

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestCanonicalise_SortsObjectKeys(t *testing.T) {
	in := []byte(`{"b":2,"a":1,"c":{"y":4,"x":3}}`)
	out, err := canonicaliseJSON(in)
	if err != nil {
		t.Fatalf("canonicaliseJSON: %v", err)
	}
	want := `{"a":1,"b":2,"c":{"x":3,"y":4}}`
	if string(out) != want {
		t.Fatalf("canonicaliseJSON got %q want %q", string(out), want)
	}
}

func TestCanonicalise_NumericRoundTrip(t *testing.T) {
	in := []byte(`{"n":3.14,"i":42,"big":1234567890}`)
	out, err := canonicaliseJSON(in)
	if err != nil {
		t.Fatalf("canonicaliseJSON: %v", err)
	}
	// Re-canonicalise; must be idempotent.
	out2, err := canonicaliseJSON(out)
	if err != nil {
		t.Fatalf("canonicaliseJSON pass 2: %v", err)
	}
	if string(out) != string(out2) {
		t.Fatalf("not idempotent: %q -> %q", string(out), string(out2))
	}
}

func TestCanonicalise_DeterministicSHA(t *testing.T) {
	// Same payload, different surface forms => same sha256.
	inputs := [][]byte{
		[]byte(`{"a":1,"b":2}`),
		[]byte(`{"b":2,  "a":1}`),
		[]byte("{\n  \"a\": 1,\n  \"b\": 2\n}"),
	}
	var first string
	for i, in := range inputs {
		out, err := canonicaliseJSON(in)
		if err != nil {
			t.Fatalf("input %d: %v", i, err)
		}
		h := sha256.Sum256(out)
		sha := hex.EncodeToString(h[:])
		if i == 0 {
			first = sha
			continue
		}
		if sha != first {
			t.Fatalf("input %d sha %s != %s (canonical %q)", i, sha, first, string(out))
		}
	}
}

func TestCanonicalise_InvalidJSON(t *testing.T) {
	if _, err := canonicaliseJSON([]byte(`{not json`)); err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

// TestCanonicalise_RejectsTrailingGarbage pins the streaming-decoder
// contract: a payload that parses to a valid JSON value but carries
// extra non-whitespace bytes after the terminator MUST reject. Without
// this guard the canonical form silently drops the trailing data and
// downstream content-SHA hashes diverge from operator expectation.
func TestCanonicalise_RejectsTrailingGarbage(t *testing.T) {
	in := []byte(`{"a":1} extra`)
	_, err := canonicaliseJSON(in)
	if err == nil {
		t.Fatalf("expected error for trailing garbage after JSON value")
	}
}

// TestCanonicalise_RejectsDuplicateKeys pins the duplicate-key
// contract: stdlib json.Decoder silently picks "last wins" when an
// object body declares the same key twice, masking content-SHA drift
// across re-canonicalisation. The canonical form must reject so
// producers see the bug at ingest time, not after replay diverges.
func TestCanonicalise_RejectsDuplicateKeys(t *testing.T) {
	in := []byte(`{"a":1,"a":2}`)
	_, err := canonicaliseJSON(in)
	if err == nil {
		t.Fatalf("expected error for duplicate object key")
	}
}

// TestCanonicalise_RejectsNestedDuplicateKeys covers duplicates that
// appear inside an inner object, exercising the recursive walk.
func TestCanonicalise_RejectsNestedDuplicateKeys(t *testing.T) {
	in := []byte(`{"outer":{"k":1,"k":2}}`)
	_, err := canonicaliseJSON(in)
	if err == nil {
		t.Fatalf("expected error for nested duplicate object key")
	}
}
