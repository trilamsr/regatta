package program

import (
	"testing"
)

// withCompileOverrides swaps the package-level ldflags vars for the
// duration of a test and restores them on cleanup. Tests cannot rely
// on the prod ldflags being unset (go test sets buildvcs=false but
// future toolchains might inject something), so the swap is explicit.
func withCompileOverrides(t *testing.T, version, dirty string) {
	t.Helper()
	prevV, prevD := compileEngineVersion, compileEngineDirty
	compileEngineVersion = version
	compileEngineDirty = dirty
	t.Cleanup(func() {
		compileEngineVersion = prevV
		compileEngineDirty = prevD
	})
}

func TestEngineInfo_LdflagsOverrideWins(t *testing.T) {
	withCompileOverrides(t, "deadbeefcafe", "true")
	got := EngineInfo()
	if got.Version != "deadbeefcafe" {
		t.Fatalf("version: got %q want %q", got.Version, "deadbeefcafe")
	}
	if !got.Dirty {
		t.Fatalf("dirty: got false want true")
	}
}

func TestEngineInfo_LdflagsCleanBuild(t *testing.T) {
	withCompileOverrides(t, "0123456789ab", "false")
	got := EngineInfo()
	if got.Dirty {
		t.Fatalf("dirty: got true want false")
	}
}

func TestEngineInfo_EmptyLdflags_FallsThrough(t *testing.T) {
	// With both overrides empty, EngineInfo must either return a real
	// VCS SHA (when `go test` retained buildvcs) or the literal
	// "unknown" sentinel. Crucially it must NEVER return "" — that's
	// the bug #549 closes ("empty engine_version silently treated as
	// match").
	withCompileOverrides(t, "", "")
	got := EngineInfo()
	if got.Version == "" {
		t.Fatalf("EngineInfo returned empty Version; must be SHA or 'unknown'")
	}
}

func TestParseDirtyFlag(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
		{"True", true},
		{"garbage", false}, // typo never silently flips clean -> dirty
	}
	for _, c := range cases {
		if got := parseDirtyFlag(c.in); got != c.want {
			t.Errorf("parseDirtyFlag(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
