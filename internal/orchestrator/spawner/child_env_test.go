package spawner

import (
	"strings"
	"testing"
)

// TestScrubChildEnv_StripsAnthropicKey: with strip enabled, ANTHROPIC_API_KEY removed so child claude CLI falls back to subscription auth (#1099 c1).
func TestScrubChildEnv_StripsAnthropicKey(t *testing.T) {
	t.Setenv(envStripAPIKey, "1")
	in := []string{
		"PATH=/usr/local/bin:/usr/bin",
		"ANTHROPIC_API_KEY=sk-ant-xxx",
		"HOME=/home/op",
	}
	out := scrubChildEnv(in)
	for _, kv := range out {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			t.Fatalf("scrubChildEnv left ANTHROPIC_API_KEY in child env: %v", out)
		}
	}
}

// TestScrubChildEnv_StripsAuthToken: with strip enabled, ANTHROPIC_AUTH_TOKEN alias also dropped (#1099 c2).
func TestScrubChildEnv_StripsAuthToken(t *testing.T) {
	t.Setenv(envStripAPIKey, "1")
	in := []string{
		"ANTHROPIC_AUTH_TOKEN=secret",
		"PATH=/bin",
	}
	out := scrubChildEnv(in)
	for _, kv := range out {
		if strings.HasPrefix(kv, "ANTHROPIC_AUTH_TOKEN=") {
			t.Fatalf("scrubChildEnv left ANTHROPIC_AUTH_TOKEN in child env: %v", out)
		}
	}
}

// TestScrubChildEnv_KeepsOtherVars: PATH/HOME/GH_TOKEN survive; child needs them for git push, gh CLI (#1099 c3).
func TestScrubChildEnv_KeepsOtherVars(t *testing.T) {
	t.Setenv(envStripAPIKey, "1")
	in := []string{
		"PATH=/usr/bin",
		"HOME=/home/op",
		"GH_TOKEN=ghp_xxx",
		"ANTHROPIC_API_KEY=sk-ant-xxx",
	}
	out := scrubChildEnv(in)
	want := map[string]bool{"PATH=/usr/bin": true, "HOME=/home/op": true, "GH_TOKEN=ghp_xxx": true}
	for _, kv := range out {
		if strings.HasPrefix(kv, "ANTHROPIC_") {
			t.Fatalf("anthropic key leaked: %s", kv)
		}
		delete(want, kv)
	}
	if len(want) > 0 {
		t.Fatalf("scrubChildEnv dropped required vars: %v", want)
	}
}

// TestScrubChildEnv_DoesNotMutateParent: parent slice and its strings are preserved (#1099 c4).
func TestScrubChildEnv_DoesNotMutateParent(t *testing.T) {
	t.Setenv(envStripAPIKey, "1")
	in := []string{"ANTHROPIC_API_KEY=sk-ant-xxx", "PATH=/bin"}
	cp := []string{in[0], in[1]}
	_ = scrubChildEnv(in)
	for i := range in {
		if in[i] != cp[i] {
			t.Fatalf("scrubChildEnv mutated parent at %d: got=%q want=%q", i, in[i], cp[i])
		}
	}
}

// TestScrubChildEnv_DefaultPassesThrough: without REGATTA_SPAWNER_STRIP_API_KEY, parent env passes through unchanged so macOS Docker keychain-less operators keep working (#1099 c5).
func TestScrubChildEnv_DefaultPassesThrough(t *testing.T) {
	t.Setenv(envStripAPIKey, "")
	in := []string{"ANTHROPIC_API_KEY=sk-ant-xxx", "PATH=/bin"}
	out := scrubChildEnv(in)
	if len(out) != 2 || out[0] != in[0] || out[1] != in[1] {
		t.Fatalf("default mode should pass-through; got=%v", out)
	}
}

// TestScrubChildEnv_EmptyAndMalformed: bare key (no =) survives; nil/empty no-ops (#1099 c6).
func TestScrubChildEnv_EmptyAndMalformed(t *testing.T) {
	t.Setenv(envStripAPIKey, "1")
	if out := scrubChildEnv(nil); out != nil && len(out) != 0 {
		t.Fatalf("nil parent should pass through, got %v", out)
	}
	in := []string{"BAREKEY", "=VALUEONLY", "PATH=/bin"}
	out := scrubChildEnv(in)
	if len(out) != 3 {
		t.Fatalf("malformed entries should survive: got=%v", out)
	}
}
