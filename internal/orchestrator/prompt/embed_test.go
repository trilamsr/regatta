package prompt

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestPromptResolver_TargetHasClaudeMd_UsesItVerbatim asserts target repo CLAUDE.md wins over bundled default (L1.2).
func TestPromptResolver_TargetHasClaudeMd_UsesItVerbatim(t *testing.T) {
	dir := t.TempDir()
	want := "# target-specific rules\n\ndo the target-specific thing\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(want), 0o644); err != nil {
		t.Fatalf("seed CLAUDE.md: %v", err)
	}
	got, source, err := ResolveClaudeMd(dir)
	if err != nil {
		t.Fatalf("ResolveClaudeMd: %v", err)
	}
	if got != want {
		t.Fatalf("expected verbatim target CLAUDE.md\nwant:\n%s\ngot:\n%s", want, got)
	}
	if source != SourceTarget {
		t.Fatalf("source = %q, want %q", source, SourceTarget)
	}
}

// TestPromptResolver_NoClaudeMd_UsesBundledDefault asserts bundled fallback when target has no CLAUDE.md (L1.1).
func TestPromptResolver_NoClaudeMd_UsesBundledDefault(t *testing.T) {
	dir := t.TempDir()
	got, source, err := ResolveClaudeMd(dir)
	if err != nil {
		t.Fatalf("ResolveClaudeMd: %v", err)
	}
	if source != SourceBundled {
		t.Fatalf("source = %q, want %q", source, SourceBundled)
	}
	if !strings.Contains(got, "bundled default") {
		t.Fatalf("bundled default missing identifier marker; got first 200 chars:\n%s", firstN(got, 200))
	}
	if len(got) < 200 {
		t.Fatalf("bundled default suspiciously short: %d bytes", len(got))
	}
}

// TestPromptEmbed_NoRepoSpecificSlugs asserts bundled assets carry no feedback_* slugs, scripts/check-*.sh refs, or regatta-specific paths (L1.3).
func TestPromptEmbed_NoRepoSpecificSlugs(t *testing.T) {
	feedbackRE := regexp.MustCompile(`feedback_[a-z][a-z0-9_]+`)
	scriptsRE := regexp.MustCompile(`scripts/check-[a-z0-9_-]+\.sh`)
	regattaPathREs := []*regexp.Regexp{
		regexp.MustCompile(`internal/orchestrator/spawner/claude\.go`),
		regexp.MustCompile(`docs/engineer/specs/`),
		regexp.MustCompile(`docs/engineer/dispatch-templates/`),
		regexp.MustCompile(`docs/engineer/briefs/`),
		regexp.MustCompile(`Makefile\.d/`),
		regexp.MustCompile(`\.regatta/items/`),
	}

	assets := AllBundledAssets()
	if len(assets) == 0 {
		t.Fatalf("AllBundledAssets returned 0 entries; expected CLAUDE.md.default + 4 dispatch templates")
	}
	for name, body := range assets {
		if hits := feedbackRE.FindAllString(body, -1); len(hits) > 0 {
			t.Errorf("%s carries feedback_* slugs: %v", name, hits)
		}
		if hits := scriptsRE.FindAllString(body, -1); len(hits) > 0 {
			t.Errorf("%s carries scripts/check-*.sh refs: %v", name, hits)
		}
		for _, re := range regattaPathREs {
			if hits := re.FindAllString(body, -1); len(hits) > 0 {
				t.Errorf("%s carries regatta-specific path %q: %v", name, re.String(), hits)
			}
		}
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
