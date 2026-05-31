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
