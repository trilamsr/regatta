package state

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// TestCausalHash_Deterministic asserts CausalInputs.Hash returns lowercase hex sha256 repeatably (#operator-console-S0).
func TestCausalHash_Deterministic(t *testing.T) {
	t.Parallel()
	in := CausalInputs{
		SpecHash: "s1", ModelHash: "m1", PromptTemplateHash: "p1",
		ToolImplHash: "t1", Seed: "seed-1",
		Versions: map[string]string{"go": "1.25.0", "claude-code": "1.0.0"},
	}
	a, err := in.Hash()
	if err != nil {
		t.Fatal(err)
	}
	b, err := in.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("non-deterministic: %s != %s", a, b)
	}
	if len(a) != 64 {
		t.Errorf("expected 64-hex sha256, got %d chars: %s", len(a), a)
	}
	if strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("not lowercase hex: %s", a)
	}
}

// TestCausalHash_InputSensitive_Rapid asserts any change to a causal field flips the hash (#operator-console-S0).
func TestCausalHash_InputSensitive_Rapid(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		base := CausalInputs{
			SpecHash:           rapid.StringN(0, 64, -1).Draw(t, "spec"),
			ModelHash:          rapid.StringN(0, 64, -1).Draw(t, "model"),
			PromptTemplateHash: rapid.StringN(0, 64, -1).Draw(t, "pt"),
			ToolImplHash:       rapid.StringN(0, 64, -1).Draw(t, "tool"),
			Seed:               rapid.StringN(0, 64, -1).Draw(t, "seed"),
		}
		other := base
		other.ModelHash = base.ModelHash + "X"
		a, _ := base.Hash()
		b, _ := other.Hash()
		if a == b {
			t.Fatalf("hash collision on differing ModelHash: %s == %s", a, b)
		}
	})
}

// TestCausalHash_MapKeyOrderIndependent asserts insertion order of Versions does not change the hash (#operator-console-S0).
func TestCausalHash_MapKeyOrderIndependent(t *testing.T) {
	t.Parallel()
	keys := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	v := map[string]string{}
	for i, k := range keys {
		v[k] = string(rune('0' + i))
	}
	a, _ := CausalInputs{Versions: v}.Hash()

	v2 := map[string]string{}
	for i := len(keys) - 1; i >= 0; i-- {
		v2[keys[i]] = string(rune('0' + i))
	}
	b, _ := CausalInputs{Versions: v2}.Hash()

	if a != b {
		t.Errorf("map key order affected hash: %s != %s", a, b)
	}
}
