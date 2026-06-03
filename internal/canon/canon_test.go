package canon

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// TestCanon_Marshal_Deterministic_UnstableMapOrder asserts canon.Marshal returns byte-identical output across runs for the same logical map.
func TestCanon_Marshal_Deterministic_UnstableMapOrder(t *testing.T) {
	t.Parallel()
	// Build a map with many keys so Go's randomized iteration order
	// surfaces drift if Marshal does not sort. Nest objects + arrays so
	// recursion is exercised. Mix numeric types so the encoder must
	// agree across runs.
	keys := []string{
		"zebra", "alpha", "mango", "beta", "yankee", "kilo", "foxtrot",
		"gamma", "delta", "epsilon", "omega", "sigma", "tango", "uniform",
		"victor", "whiskey", "x-ray", "hotel", "india", "juliet",
	}
	build := func() map[string]any {
		m := map[string]any{}
		for i, k := range keys {
			m[k] = map[string]any{
				"int":   int64(i),
				"float": float64(i) + 0.5,
				"neg":   math.Copysign(0, -1),
				"big":   int64(1) << 53,
				"items": []any{int64(3), int64(1), int64(2)},
				"sub": map[string]any{
					"nested-z": "z",
					"nested-a": "a",
				},
			}
		}
		return m
	}
	// 32 marshalings must all be byte-equal — guards against any
	// reliance on map iteration order in the encoder.
	var prev []byte
	for i := 0; i < 32; i++ {
		got, err := Marshal(build())
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if i > 0 && !bytes.Equal(prev, got) {
			t.Fatalf("Marshal non-deterministic at i=%d:\nprev=%s\n got=%s", i, prev, got)
		}
		prev = got
	}
	// Sanity: top-level keys appear in lex order in the output bytes.
	out := string(prev)
	last := ""
	for _, k := range []string{"alpha", "beta", "delta", "epsilon", "foxtrot", "gamma", "hotel", "india", "juliet", "kilo", "mango", "omega", "sigma", "tango", "uniform", "victor", "whiskey", "x-ray", "yankee", "zebra"} {
		idx := strings.Index(out, `"`+k+`":`)
		if idx < 0 {
			t.Fatalf("key %q missing from output: %s", k, out)
		}
		if last != "" && strings.Index(out, `"`+last+`":`) > idx {
			t.Fatalf("keys out of order: %q before %q", k, last)
		}
		last = k
	}
}

// TestCanon_Marshal_NumericEdgeCases asserts Marshal handles integer + float forms identically to CanonicaliseJSON.
func TestCanon_Marshal_NumericEdgeCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   any
	}{
		{"int64-max", int64(math.MaxInt64)},
		{"int64-min", int64(math.MinInt64)},
		{"float-0.5", float64(0.5)},
		{"float-neg-zero", math.Copysign(0, -1)},
		{"large-int", int64(1) << 53},
		{"negative-int", int64(-42)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, err := Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			// Round-trip through CanonicaliseJSON for the same input via
			// raw bytes — must produce identical output.
			raw, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			b, err := CanonicaliseJSON(raw)
			if err != nil {
				t.Fatalf("CanonicaliseJSON: %v", err)
			}
			if !bytes.Equal(a, b) {
				t.Fatalf("Marshal vs CanonicaliseJSON diverge:\nMarshal=%s\ncanon  =%s", a, b)
			}
		})
	}
}

// TestCanon_CrossImpl_ByteEquality asserts the unified canon path yields one MAC for payloads the 4 forks previously disagreed on.
func TestCanon_CrossImpl_ByteEquality(t *testing.T) {
	t.Parallel()
	// Fixture: a payload built two different ways that previously
	// canonicalised to different bytes across the schemas / tick /
	// audit forks (number normalization + key ordering drift). After
	// unify, both routes through canon.Marshal must produce identical
	// bytes.
	payloadA := map[string]any{
		"zebra": float64(1),
		"alpha": int64(42),
		"items": []any{
			map[string]any{"y": 1, "x": 2},
			map[string]any{"b": "two", "a": "one"},
		},
	}
	payloadB := map[string]any{
		"alpha": int64(42),
		"items": []any{
			map[string]any{"x": 2, "y": 1},
			map[string]any{"a": "one", "b": "two"},
		},
		"zebra": float64(1),
	}
	a, err := Marshal(payloadA)
	if err != nil {
		t.Fatalf("Marshal A: %v", err)
	}
	b, err := Marshal(payloadB)
	if err != nil {
		t.Fatalf("Marshal B: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("logically-equal payloads canonicalise differently:\nA=%s\nB=%s", a, b)
	}
	want := `{"alpha":42,"items":[{"x":2,"y":1},{"a":"one","b":"two"}],"zebra":1}`
	if string(a) != want {
		t.Fatalf("canonical form drift:\n got=%s\nwant=%s", a, want)
	}
}

// TestCanon_CanonicaliseJSON_RejectsDuplicateKeys re-asserts the dup-key defense that the schemas/tick forks silently lost.
func TestCanon_CanonicaliseJSON_RejectsDuplicateKeys(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"a":1,"a":2}`)
	_, err := CanonicaliseJSON(raw)
	if err == nil {
		t.Fatalf("expected error on duplicate keys, got nil")
	}
}

// TestCanon_Marshal_PreservesLargeIntPrecision proves the unified path keeps int64 precision past 2^53 where the old schemas fork lost it.
func TestCanon_Marshal_PreservesLargeIntPrecision(t *testing.T) {
	t.Parallel()
	// int64 values above 2^53 cannot round-trip through float64
	// without loss. The old schemas.CanonicalJSON did exactly that
	// round-trip (json.Marshal → json.Unmarshal into any → re-marshal);
	// the value would silently truncate. canon.Marshal uses UseNumber
	// so the original digit string survives.
	big := int64(1<<60) + 1
	out, err := Marshal(map[string]any{"v": big})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"v":1152921504606846977}`
	if string(out) != want {
		t.Fatalf("large-int precision lost:\n got=%s\nwant=%s", out, want)
	}
}
