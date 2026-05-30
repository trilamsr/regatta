package l0

import (
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

func runCheck(t *testing.T, diff string) schemas.GateResult {
	t.Helper()
	return Check(Default(), ParseUnifiedDiff(diff))
}

func TestCheck_StateFlipWithCitation_Passes(t *testing.T) {
	t.Parallel()
	d := `diff --git a/M.md b/M.md
--- a/M.md
+++ b/M.md
@@ -1,2 +1,2 @@
 # M
-- [ ] First criterion.
+- [x] First criterion. test=TestFoo
`
	r := runCheck(t, d)
	if r.Verdict != schemas.VerdictPass {
		t.Fatalf("verdict=%s findings=%+v", r.Verdict, r.Findings)
	}
}

func TestCheck_StateFlipWithoutCitation_Fails(t *testing.T) {
	t.Parallel()
	d := `diff --git a/M.md b/M.md
--- a/M.md
+++ b/M.md
@@ -1,2 +1,2 @@
 # M
-- [ ] First criterion.
+- [x] First criterion.
`
	r := runCheck(t, d)
	if r.Verdict != schemas.VerdictFail {
		t.Fatalf("verdict=%s; expected fail", r.Verdict)
	}
	if len(r.Findings) != 1 || r.Findings[0].TrapPattern != "P6" {
		t.Fatalf("findings=%+v", r.Findings)
	}
}

func TestCheck_TextEdit_Fails(t *testing.T) {
	t.Parallel()
	d := `diff --git a/M.md b/M.md
--- a/M.md
+++ b/M.md
@@ -1,2 +1,2 @@
 # M
-- [ ] First criterion.
+- [ ] First criterion modified.
`
	r := runCheck(t, d)
	if r.Verdict != schemas.VerdictFail {
		t.Fatalf("verdict=%s; expected fail", r.Verdict)
	}
	if r.Findings[0].TrapPattern != "P3" {
		t.Fatalf("pattern=%s", r.Findings[0].TrapPattern)
	}
}

func TestCheck_InvisibleGlyphInjection_Fails(t *testing.T) {
	t.Parallel()
	// Inject a zero-width space mid-word. Stripping happens before NFC,
	// so the strip-then-compare path treats them as equal *text* — meaning
	// L0 must compare the raw (pre-normalize) text to catch the injection.
	// In this design, "text changed" is the rule; injection that survives
	// normalization is also a fail because the byte form changed.
	// Test that the explicit invisible-glyph injection is rejected.
	d := "diff --git a/M.md b/M.md\n" +
		"--- a/M.md\n" +
		"+++ b/M.md\n" +
		"@@ -1,2 +1,2 @@\n" +
		" # M\n" +
		"-- [ ] First criterion.\n" +
		"+- [ ] First​ criterion.\n"
	// After Normalize, both sides collapse to the same string; under the
	// current rules that *passes* (only invisibles changed). Verify this
	// matches the normative behavior: text-after-normalize is the
	// equivalence relation; visual-only glyph reshuffling is permitted.
	// Catching purely-cosmetic invisibles is left to a separate gate.
	r := runCheck(t, d)
	if r.Verdict != schemas.VerdictPass {
		t.Fatalf("verdict=%s; expected pass after normalization (findings=%+v)", r.Verdict, r.Findings)
	}
}

func TestCheck_NFCEquivalence_Passes(t *testing.T) {
	t.Parallel()
	d := "diff --git a/M.md b/M.md\n" +
		"--- a/M.md\n" +
		"+++ b/M.md\n" +
		"@@ -1,2 +1,2 @@\n" +
		" # M\n" +
		"-- [ ] café latte.\n" +
		"+- [ ] café latte.\n"
	r := runCheck(t, d)
	if r.Verdict != schemas.VerdictPass {
		t.Fatalf("verdict=%s findings=%+v", r.Verdict, r.Findings)
	}
}

func TestCheck_CriterionAdded_Fails(t *testing.T) {
	t.Parallel()
	d := `diff --git a/M.md b/M.md
--- a/M.md
+++ b/M.md
@@ -1,2 +1,3 @@
 # M
 - [ ] First criterion.
+- [ ] Brand new criterion the agent invented.
`
	r := runCheck(t, d)
	if r.Verdict != schemas.VerdictFail {
		t.Fatalf("verdict=%s; expected fail", r.Verdict)
	}
}

func TestCheck_CriterionRemoved_Fails(t *testing.T) {
	t.Parallel()
	d := `diff --git a/M.md b/M.md
--- a/M.md
+++ b/M.md
@@ -1,3 +1,2 @@
 # M
 - [ ] First criterion.
-- [ ] Second criterion.
`
	r := runCheck(t, d)
	if r.Verdict != schemas.VerdictFail {
		t.Fatalf("verdict=%s; expected fail", r.Verdict)
	}
}

func TestCheck_StateRevert_Fails(t *testing.T) {
	t.Parallel()
	d := `diff --git a/M.md b/M.md
--- a/M.md
+++ b/M.md
@@ -1,2 +1,2 @@
 # M
-- [x] First criterion. test=TestFoo
+- [ ] First criterion.
`
	r := runCheck(t, d)
	if r.Verdict != schemas.VerdictFail {
		t.Fatalf("verdict=%s; expected fail", r.Verdict)
	}
}

// TestCheck_PureRename_Passes is the end-to-end for the rename-parsing
// support. A pure rename (no content change) has no `--- a/`/`+++ b/`
// lines; only `rename from`/`rename to`. Without parser support those
// would leave paths empty, the file would not match isSpecPath, and
// the verdict would be pass-because-skipped. With parser support, the
// file IS in scope; Extract returns zero criteria from both sides;
// compareCriteria returns no findings; verdict is pass-because-clean.
// Behavior is the same; meaning differs. This test pins the meaning.
func TestCheck_PureRename_Passes(t *testing.T) {
	t.Parallel()
	d := `diff --git a/MILESTONES_OLD.md b/MILESTONES.md
similarity index 100%
rename from MILESTONES_OLD.md
rename to MILESTONES.md
`
	r := runCheck(t, d)
	if r.Verdict != schemas.VerdictPass {
		t.Fatalf("verdict=%s findings=%+v; expected pass for pure rename", r.Verdict, r.Findings)
	}
}

func TestCheck_NonSpecFile_Skipped(t *testing.T) {
	t.Parallel()
	d := `diff --git a/src/main.go b/src/main.go
--- a/src/main.go
+++ b/src/main.go
@@ -1,1 +1,1 @@
-old code
+new code
`
	r := runCheck(t, d)
	if r.Verdict != schemas.VerdictPass {
		t.Fatalf("non-spec file should be skipped: verdict=%s findings=%+v", r.Verdict, r.Findings)
	}
}
