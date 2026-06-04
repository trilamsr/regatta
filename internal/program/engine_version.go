// Package program stamps the engine identity (git-SHA + dirty flag) of
// the binary that produced each ProgramBrief so replay months later
// refuses on engine-version skew rather than silently producing a
// divergent verdict (#549).
//
// Sources (priority order): ldflags `-X
// compileEngineVersion=$SHA` > runtime/debug.ReadBuildInfo
// (vcs.revision + vcs.modified) > "unknown" + dirty=false. The
// fallback surfaces verbatim so replay refuses rather than treats
// "unknown" as a match.
package program

import (
	"runtime/debug"
	"strconv"
)

// compileEngineVersion / compileEngineDirty are ldflags-injected at
// build time; empty defaults force the runtime/debug fallback. Dirty
// is string-typed because ldflags only sets string vars.
var (
	compileEngineVersion = ""
	compileEngineDirty   = ""
)

// EngineRef captures the engine identity that produced (or is
// replaying) a ProgramBrief. Version is a 40-hex git-SHA when wired,
// else "unknown". Dirty=true means the SHA alone cannot reproduce.
type EngineRef struct {
	Version string
	Dirty   bool
}

// EngineInfo returns the current binary's engine identity (priority:
// ldflags > runtime/debug VCS > unknown). Resolved at call time so
// tests monkey-patching the package-level overrides see fresh values.
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

// parseDirtyFlag tolerates common truthy strings. Anything unparseable
// falls back to false so a typo never silently flips a clean release.
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
