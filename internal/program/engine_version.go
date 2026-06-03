// Engine-version stamping for ProgramBrief. The deterministic engine
// (CELDecider + EdgeEvaluator + routing) is code that ships and
// changes; replay months later under today's engine silently rots
// when that code drifts. #549 closes the audit-claim gap by journaling
// the git-SHA + dirty-flag of the binary that produced each brief,
// so the replay path can refuse (or warn) on engine-version skew
// rather than silently producing a divergent verdict.
//
// Sources (in priority order):
//
//  1. compileEngineVersion — `-ldflags "-X .../internal/program.compileEngineVersion=$SHA"`
//     at `go build` time. Operator-controlled; the release-build path
//     pins this from `git rev-parse HEAD`. Same source feeds the
//     `regatta version` line.
//  2. runtime/debug.ReadBuildInfo VCS settings (`vcs.revision` +
//     `vcs.modified`). `go build -buildvcs=true` (default) embeds
//     these; only `go test`-driven binaries and `-buildvcs=false`
//     builds fall through.
//  3. Fallback: "unknown" + dirty=false. Surfaced verbatim so audit
//     replay can refuse rather than treat "unknown" as a match.
//
// "Dirty" semantics: the VCS-injected `vcs.modified` flag reports
// uncommitted changes at build time (both staged and unstaged — Go's
// build tooling does not distinguish). compileEngineVersion is treated
// as clean unless an explicit dirty-flag override is wired by the
// release build (operators producing release binaries from a clean
// checkout pass dirty=false; CI sets compileEngineDirty="true" on
// any non-tag build per #549 risk-tier guidance).

package program

import (
	"runtime/debug"
	"strconv"
)

// compileEngineVersion is overridden via `-ldflags "-X ..."` at build
// time. The dev default "" forces the runtime/debug fallback so a
// freshly-cloned developer build still stamps the brief with a SHA
// (assuming `-buildvcs=true`, the Go default).
var compileEngineVersion = ""

// compileEngineDirty is overridden via `-ldflags "-X ..."` at build
// time. Encoded as a string because ldflags can only set string-typed
// vars; parsed to bool at first use. Empty string => fall through to
// runtime/debug VCS dirty flag.
var compileEngineDirty = ""

// EngineRef captures the engine identity that produced (or is replaying)
// a ProgramBrief. Stored verbatim in the brief — the replay-skew check
// compares brief.EngineRef() against EngineInfo() and surfaces any
// mismatch.
type EngineRef struct {
	// Version is a git-SHA (40-hex) when set via ldflags or VCS, or
	// "unknown" when neither source is wired. Operators reading
	// `regatta program show` see this verbatim.
	Version string
	// Dirty reports whether the build had uncommitted changes. True
	// means the binary's source tree does not match the SHA, so the
	// SHA alone is not sufficient to reproduce the build.
	Dirty bool
}

// EngineInfo returns the current binary's engine identity. Priority:
// compile-time ldflags > runtime/debug VCS > unknown. The choice is
// resolved at call time (no init() side effects) so tests that
// monkey-patch the package-level overrides see the fresh value.
func EngineInfo() EngineRef {
	if compileEngineVersion != "" {
		return EngineRef{
			Version: compileEngineVersion,
			Dirty:   parseDirtyFlag(compileEngineDirty),
		}
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		var revision string
		var dirty bool
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if revision != "" {
			return EngineRef{Version: revision, Dirty: dirty}
		}
	}
	return EngineRef{Version: "unknown", Dirty: false}
}

// parseDirtyFlag tolerates the common truthy strings operators paste
// into a build script. Anything not parseable as bool falls back to
// false so a typo never silently flips a clean release to dirty.
func parseDirtyFlag(s string) bool {
	if s == "" {
		return false
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false
	}
	return b
}
