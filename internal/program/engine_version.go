// Package program stamps the engine identity (git-SHA + dirty flag) of
// the binary that produced each ProgramBrief so replay months later
// can refuse on engine-version skew rather than silently produce a
// divergent verdict (#549).
//
// Sources (priority order):
//  1. compileEngineVersion — `-ldflags "-X .../compileEngineVersion=$SHA"` at build time.
//  2. runtime/debug.ReadBuildInfo (vcs.revision + vcs.modified) — `go build -buildvcs=true` (default) embeds these.
//  3. Fallback: "unknown" + dirty=false — surfaced verbatim so audit
//     replay can refuse rather than treat "unknown" as a match.
package program

import (
	"runtime/debug"
	"strconv"
)

// compileEngineVersion is overridden via `-ldflags "-X ..."` at build
// time. Dev default "" forces the runtime/debug fallback.
var compileEngineVersion = ""

// compileEngineDirty is overridden via `-ldflags "-X ..."` at build
// time. String-typed because ldflags can only set string vars; empty
// falls through to the VCS dirty flag.
var compileEngineDirty = ""

// EngineRef captures the engine identity that produced (or is replaying)
// a ProgramBrief. The replay-skew check compares brief.EngineRef()
// against EngineInfo() and surfaces any mismatch.
type EngineRef struct {
	// Version is a 40-hex git-SHA when ldflags or VCS supply it, or "unknown" when neither is wired.
	Version string
	// Dirty reports uncommitted changes at build time — when true, the SHA alone cannot reproduce the build.
	Dirty bool
}

// EngineInfo returns the current binary's engine identity. Priority:
// ldflags > runtime/debug VCS > unknown. Resolved at call time (no
// init() side effects) so tests that monkey-patch the package-level
// overrides see the fresh value.
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

// parseDirtyFlag tolerates common truthy strings in build scripts.
// Anything unparseable falls back to false so a typo never silently
// flips a clean release to dirty.
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
