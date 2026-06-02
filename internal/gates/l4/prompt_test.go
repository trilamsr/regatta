package l4

import (
	"strings"
	"testing"
)

// RenderPrompt inlines all section values into the embedded template.
func TestRenderPrompt_InlinesSections(t *testing.T) {
	in := Input{
		PRSHA:     "abc123",
		BaseSHA:   "def456",
		RepoRoot:  "/repo",
		Diff:      "diff body",
		Spec:      "spec body",
		Scorecard: "scorecard body",
	}
	out, sha, err := RenderPrompt(in, DefaultMaxDiffChars)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"abc123", "def456", "/repo", "diff body", "spec body", "scorecard body"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in prompt:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(sha, "sha256:") || len(sha) != len("sha256:")+64 {
		t.Fatalf("unexpected sha shape: %q", sha)
	}
}

// PromptSHA is stable across identical renders (audit-replay pin).
func TestRenderPrompt_SHAStable(t *testing.T) {
	in := Input{PRSHA: "x", BaseSHA: "y", Diff: "d"}
	_, s1, err := RenderPrompt(in, 1000)
	if err != nil {
		t.Fatal(err)
	}
	_, s2, err := RenderPrompt(in, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if s1 != s2 {
		t.Fatalf("sha drift: %q vs %q", s1, s2)
	}
	if PromptSHA() != s1 {
		t.Fatalf("PromptSHA() = %q want %q", PromptSHA(), s1)
	}
}

// Oversize diffs clip to maxChars before substitution.
func TestRenderPrompt_ClipsOversizeDiff(t *testing.T) {
	in := Input{Diff: strings.Repeat("Z", 1000)}
	out, _, err := RenderPrompt(in, 50)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "Z") > 50 {
		t.Fatalf("diff not clipped, got %d Z's", strings.Count(out, "Z"))
	}
}
