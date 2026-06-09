package spawner

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
)

// trueBinForTest resolves a no-op exit-0 binary path that works on macOS and Linux test hosts; skips the test if neither /usr/bin/true nor /bin/true is on the box.
func trueBinForTest(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("true"); err == nil {
		return p
	}
	t.Skip("no `true` binary on PATH; skipping execStarter integration test")
	return ""
}

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
	if out := scrubChildEnv(nil); len(out) != 0 {
		t.Fatalf("nil parent should pass through, got %v", out)
	}
	in := []string{"BAREKEY", "=VALUEONLY", "PATH=/bin"}
	out := scrubChildEnv(in)
	if len(out) != 3 {
		t.Fatalf("malformed entries should survive: got=%v", out)
	}
}

// TestExecStarter_AppliesScrubbedEnvWhenStripEnabled: execStarter (the production ProcessStarter) sets cmd.Env to the scrubbed parent so the spawned claude child actually loses ANTHROPIC_API_KEY when strip is on (#1099 c7 — integration).
func TestExecStarter_AppliesScrubbedEnvWhenStripEnabled(t *testing.T) {
	t.Setenv(envStripAPIKey, "1")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-leaked")
	resetStripLogOnceForTest()

	cmd, err := execStarter(context.Background(), trueBinForTest(t), nil, nil, io.Discard, t.TempDir())
	if err != nil {
		t.Fatalf("execStarter: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })

	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			t.Fatalf("execStarter leaked ANTHROPIC_API_KEY into child cmd.Env: %v", cmd.Env)
		}
	}
}

// TestExecStarter_DefaultPassesAPIKeyThrough: with strip flag unset, execStarter preserves the parent env so macOS Docker operators (no file-backed subscription) keep working (#1099 c8 — integration).
func TestExecStarter_DefaultPassesAPIKeyThrough(t *testing.T) {
	t.Setenv(envStripAPIKey, "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-passthrough")
	resetStripLogOnceForTest()

	cmd, err := execStarter(context.Background(), trueBinForTest(t), nil, nil, io.Discard, t.TempDir())
	if err != nil {
		t.Fatalf("execStarter: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })

	found := false
	for _, kv := range cmd.Env {
		if kv == "ANTHROPIC_API_KEY=sk-ant-passthrough" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("execStarter default mode dropped ANTHROPIC_API_KEY; got Env=%v", cmd.Env)
	}
}

// TestScrubChildEnv_EmitsStartupLogOnce: when strip is enabled, the spawner logs one operator-visible breadcrumb so a later child-auth failure has a paired explanation (#1099 c9).
func TestScrubChildEnv_EmitsStartupLogOnce(t *testing.T) {
	t.Setenv(envStripAPIKey, "1")
	resetStripLogOnceForTest()

	var buf bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prior) })

	_ = scrubChildEnv([]string{"ANTHROPIC_API_KEY=x"})
	_ = scrubChildEnv([]string{"ANTHROPIC_API_KEY=x"})

	hits := strings.Count(buf.String(), "spawner.api_key_stripped_from_children")
	if hits != 1 {
		t.Fatalf("expected exactly 1 startup log, got %d: %q", hits, buf.String())
	}
}
