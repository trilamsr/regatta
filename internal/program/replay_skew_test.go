package program

import (
	"errors"
	"strings"
	"testing"
)

// TestReplay_EngineVersionSkew_EmitsWarning closes the WARN-mode branch
// (#549 default). Brief produced under "v1"; replay running "v2" must
// surface a SkewResult tagged "engine-skew-replay-from=v1 to=v2" that
// callers (digest, program show) render verbatim. The non-strict path
// MUST NOT return an error — the audit-loop stays unblocked.
func TestReplay_EngineVersionSkew_EmitsWarning(t *testing.T) {
	brief := &ProgramBrief{
		ProgramID:     "m-aaaaaaaaaaaa",
		EngineVersion: "v1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	current := EngineRef{Version: "v2bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	res, err := CheckEngineSkew(brief, current, false)
	if err != nil {
		t.Fatalf("WARN mode must not error: %v", err)
	}
	if !res.Skewed {
		t.Fatalf("expected Skewed=true")
	}
	want := "engine-skew-replay-from=v1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa to=v2bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if res.Tag != want {
		t.Fatalf("tag: got %q want %q", res.Tag, want)
	}
	if !strings.Contains(res.Warning, want) {
		t.Fatalf("warning must embed tag: %q", res.Warning)
	}
}

// TestReplay_StrictMode_EngineVersionSkew_Refuses closes the STRICT
// branch (#549 audit). Same skew, strict=true must return ErrEngineSkew
// so the audit caller fails loud rather than tagging+continuing.
func TestReplay_StrictMode_EngineVersionSkew_Refuses(t *testing.T) {
	brief := &ProgramBrief{
		ProgramID:     "m-aaaaaaaaaaaa",
		EngineVersion: "v1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	current := EngineRef{Version: "v2bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	res, err := CheckEngineSkew(brief, current, true)
	if !errors.Is(err, ErrEngineSkew) {
		t.Fatalf("strict mode: expected ErrEngineSkew, got %v", err)
	}
	if !res.Skewed {
		t.Fatalf("strict mode must still surface Skewed=true for inspection")
	}
}

// TestReplay_EngineVersionMatch_NoWarning closes the happy path: same
// SHA, both clean, no tag emitted.
func TestReplay_EngineVersionMatch_NoWarning(t *testing.T) {
	sha := "abc123def456abc123def456abc123def456abc1"
	brief := &ProgramBrief{ProgramID: "m-aaaaaaaaaaaa", EngineVersion: sha}
	current := EngineRef{Version: sha}
	res, err := CheckEngineSkew(brief, current, false)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if res.Skewed {
		t.Fatalf("match must not skew: %+v", res)
	}
	if res.Tag != "" || res.Warning != "" {
		t.Fatalf("match must be silent: %+v", res)
	}
}

// TestReplay_BriefDirty_AlwaysSkewedEvenOnMatch closes the dirty-flag
// branch. A dirty record-time build is inherently non-reproducible: the
// SHA alone does not pin the source tree. Match-by-SHA-but-dirty must
// surface as skew so the auditor knows the SHA proves nothing.
func TestReplay_BriefDirty_AlwaysSkewedEvenOnMatch(t *testing.T) {
	sha := "abc123def456abc123def456abc123def456abc1"
	brief := &ProgramBrief{
		ProgramID:        "m-aaaaaaaaaaaa",
		EngineVersion:    sha,
		EngineBuildDirty: true,
	}
	current := EngineRef{Version: sha, Dirty: false}
	res, err := CheckEngineSkew(brief, current, false)
	if err != nil {
		t.Fatalf("dirty WARN: %v", err)
	}
	if !res.Skewed {
		t.Fatalf("dirty record must surface skew")
	}
	if !strings.Contains(res.Warning, "dirty") {
		t.Fatalf("warning must mention dirty: %q", res.Warning)
	}
}

// TestReplay_CurrentDirty_FlaggedAsSkew mirrors the prior case from the
// replay side. Even a SHA match cannot prove reproducibility when the
// CURRENT binary is dirty.
func TestReplay_CurrentDirty_FlaggedAsSkew(t *testing.T) {
	sha := "abc123def456abc123def456abc123def456abc1"
	brief := &ProgramBrief{ProgramID: "m-aaaaaaaaaaaa", EngineVersion: sha}
	current := EngineRef{Version: sha, Dirty: true}
	res, err := CheckEngineSkew(brief, current, false)
	if err != nil {
		t.Fatalf("current-dirty WARN: %v", err)
	}
	if !res.Skewed {
		t.Fatalf("current-dirty must surface skew")
	}
}

// TestReplay_UnknownEngineVersion_SkewsEvenInWarn covers the audit
// false-green: a brief produced by a buildvcs=false binary stamps
// "unknown". The replay path must NEVER treat "unknown" as match —
// that's exactly the silent-rot bug #549 closes.
func TestReplay_UnknownEngineVersion_SkewsEvenInWarn(t *testing.T) {
	brief := &ProgramBrief{ProgramID: "m-aaaaaaaaaaaa", EngineVersion: "unknown"}
	current := EngineRef{Version: "unknown"}
	res, err := CheckEngineSkew(brief, current, false)
	if err != nil {
		t.Fatalf("unknown WARN: %v", err)
	}
	if !res.Skewed {
		t.Fatalf("unknown==unknown must NOT count as match")
	}
}

// TestReplay_EmptyEngineVersion_SkewsEvenInWarn closes the migration
// branch: a pre-#549 brief (no engine_version stamp) must surface as
// skew, not silently treated as match. Otherwise the audit pipeline
// would call it green.
func TestReplay_EmptyEngineVersion_SkewsEvenInWarn(t *testing.T) {
	brief := &ProgramBrief{ProgramID: "m-aaaaaaaaaaaa", EngineVersion: ""}
	current := EngineRef{Version: "abc123def456abc123def456abc123def456abc1"}
	res, err := CheckEngineSkew(brief, current, false)
	if err != nil {
		t.Fatalf("empty WARN: %v", err)
	}
	if !res.Skewed {
		t.Fatalf("empty brief.EngineVersion must surface skew")
	}
}
