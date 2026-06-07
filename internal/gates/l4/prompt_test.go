package l4

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// RenderPrompt inlines every Input section verbatim into the embedded
// template. Covers both the adapter dry-run path (#373) and the
// hot-reload active-slot path (#387) — same surface, same expectations.
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
	if !strings.HasPrefix(sha, "sha256:") || len(sha) != len("sha256:")+64 {
		t.Fatalf("unexpected sha shape: %q", sha)
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

// PromptSHA is stable across identical renders (audit-replay pin) and
// matches the package-level PromptSHA() accessor used by gate telemetry.
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
	if PromptSHA() != sha1 {
		t.Fatalf("PromptSHA() = %q want %q", PromptSHA(), sha1)
	}
}

// TestRenderPromptSplit_HotReloadRace asserts the returned (static, sha) pair always agrees on the slot rendered against (#886).
func TestRenderPromptSplit_HotReloadRace(t *testing.T) {
	prev := active.Load()
	t.Cleanup(func() { active.Store(prev) })

	bodyA := "STATIC-A reviewer preamble {{ \"\" }}" + cacheBreakpointMarker + "\nDYN-A {{ .PRSHA }}"
	bodyB := "STATIC-B different preamble {{ \"\" }} extra line\n" + cacheBreakpointMarker + "\nDYN-B {{ .PRSHA }}"
	slotA, err := parseSlot(bodyA)
	if err != nil {
		t.Fatalf("parseSlot A: %v", err)
	}
	slotB, err := parseSlot(bodyB)
	if err != nil {
		t.Fatalf("parseSlot B: %v", err)
	}
	if slotA.sha == slotB.sha {
		t.Fatalf("test setup: slots must hash differently")
	}
	wantStatic := map[string]string{
		slotA.sha: slotA.static,
		slotB.sha: slotB.static,
	}

	active.Store(slotA)

	var (
		stop      atomic.Bool
		swapWG    sync.WaitGroup
		renderWG  sync.WaitGroup
		mismatch  atomic.Int64
		iterCount atomic.Int64
	)

	swapWG.Add(1)
	go func() {
		defer swapWG.Done()
		toggle := false
		for !stop.Load() {
			if toggle {
				active.Store(slotA)
			} else {
				active.Store(slotB)
			}
			toggle = !toggle
		}
	}()

	const renderers = 4
	for i := 0; i < renderers; i++ {
		renderWG.Add(1)
		go func() {
			defer renderWG.Done()
			in := Input{PRSHA: "abc"}
			for j := 0; j < 5000; j++ {
				static, _, sha, err := RenderPromptSplit(in, 100)
				if err != nil {
					t.Errorf("RenderPromptSplit: %v", err)
					return
				}
				iterCount.Add(1)
				if want, ok := wantStatic[sha]; ok {
					if static != want {
						mismatch.Add(1)
					}
				} else {
					t.Errorf("unknown sha returned: %q", sha)
					return
				}
			}
		}()
	}

	renderWG.Wait()
	stop.Store(true)
	swapWG.Wait()

	if mismatch.Load() > 0 {
		t.Fatalf("static/sha disagreement on %d/%d renders — hot-reload race (#886)",
			mismatch.Load(), iterCount.Load())
	}
}

// Oversize diff clips to maxChars before substitution so the model never
// sees the unclipped blob even when the caller forgot to apply
// MaxDiffChars upstream.
func TestRenderPrompt_ClipsOversizeDiff(t *testing.T) {
	big := strings.Repeat("x", 1000)
	in := Input{Diff: big}
	out, _, err := RenderPrompt(in, 50)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Template body itself contains a few literal 'x' characters
	// (instruction prose), so allow a small slack above maxChars.
	if strings.Count(out, "x") > 60 {
		t.Fatalf("diff not clipped: got %d 'x' bytes, want <= 60", strings.Count(out, "x"))
	}
}
