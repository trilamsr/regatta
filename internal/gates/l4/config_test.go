package l4

import "testing"

// ResolveModel honours yaml > env > default precedence.
func TestL4_ModelResolutionOrder(t *testing.T) {
	const def = "claude-sonnet-4-6"
	cases := []struct {
		name    string
		yamlVal string
		envVal  string
		want    string
	}{
		{"yaml-wins-over-env-and-default", "claude-opus-4-7", "claude-haiku-4-5", "claude-opus-4-7"},
		{"env-wins-when-yaml-empty", "", "claude-haiku-4-5", "claude-haiku-4-5"},
		{"default-when-both-empty", "", "", def},
		{"yaml-wins-when-env-empty", "claude-opus-4-7", "", "claude-opus-4-7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(EnvModel, c.envVal)
			got := ResolveModel(c.yamlVal)
			if got != c.want {
				t.Fatalf("ResolveModel(%q) with %s=%q: got %q, want %q",
					c.yamlVal, EnvModel, c.envVal, got, c.want)
			}
		})
	}
}

// DefaultModel matches the spec-binding decision.
func TestL4_DefaultModelIsSonnet46(t *testing.T) {
	if DefaultModel != "claude-sonnet-4-6" {
		t.Fatalf("default model drift: got %q, spec mandates claude-sonnet-4-6", DefaultModel)
	}
}
