package program

import (
	"errors"
	"strings"
	"testing"
)

// TestReplay_EngineVersionSkew_EmitsWarning asserts WARN-mode tags skew without erroring so audit-loop stays unblocked.
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

// TestReplay_StrictMode_EngineVersionSkew_Refuses asserts strict=true returns ErrEngineSkew so audit fails loud.
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

// TestReplay_EngineVersionMatch_NoWarning asserts same-SHA clean-both emits no tag (happy path).
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

// TestReplay_BriefDirty_AlwaysSkewedEvenOnMatch asserts dirty record-time build skews despite SHA match (non-reproducible).
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

// TestReplay_CurrentDirty_FlaggedAsSkew asserts dirty current-binary skews despite SHA match (replay-side mirror).
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

// TestReplay_UnknownEngineVersion_SkewsEvenInWarn asserts "unknown"=="unknown" never counts as match (silent-rot guard).
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

// TestReplay_EmptyEngineVersion_SkewsEvenInWarn asserts pre-#549 unstamped brief surfaces as skew, not silent-match.
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
