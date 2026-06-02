package l4

import (
	"strings"
	"testing"
)

// Prompt template inlines every Input section verbatim.
func TestRenderPrompt_InlinesAllSections(t *testing.T) {
	in := Input{
		PRSHA:     "abc1234",
		BaseSHA:   "base5678",
		RepoRoot:  "/repo/root",
		Diff:      "diff --git a/foo b/foo",
		Spec:      "## Spec body",
		Scorecard: "- B: implemented",
	}
	out, sha, err := RenderPrompt(in, DefaultMaxDiffChars)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if sha == "" {
		t.Fatalf("prompt sha must not be empty")
	}
	for _, want := range []string{
		"abc1234",
		"base5678",
		"/repo/root",
		"diff --git a/foo b/foo",
		"Spec body",
		"B: implemented",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt missing %q\nout:\n%s", want, out)
		}
	}
}

// PromptSHA is stable across identical renders (audit-replay pin).
func TestRenderPrompt_SHAStable(t *testing.T) {
	in := Input{PRSHA: "a", BaseSHA: "b", Diff: "d", Spec: "s", Scorecard: "c"}
	_, sha1, err := RenderPrompt(in, 1000)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_, sha2, err := RenderPrompt(in, 1000)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if sha1 != sha2 {
		t.Fatalf("sha drift across identical renders: %s vs %s", sha1, sha2)
	}
}

// Oversize diff clips to MaxDiffChars before substitution.
func TestRenderPrompt_ClipsOversizeDiff(t *testing.T) {
	big := strings.Repeat("x", 1000)
	in := Input{Diff: big}
	out, _, err := RenderPrompt(in, 50)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Count(out, "x") > 60 {
		t.Fatalf("diff not clipped: got %d 'x' bytes, want <= 60", strings.Count(out, "x"))
	}
}
