// Package program: replay-skew detection (#549). When the deterministic
// engine drifts between record and replay, the silent divergence turns
// "what happened" into "what current code would have done" — misleading
// green for a compliance auditor. CheckEngineSkew is the boundary that
// turns the rot into a loud signal; STRICT mode fails closed.
//
// The replay engine itself is a Phase X stub (`internal/history`); this
// helper exists now so the contract is settled before re-execution
// lands.

package program

import (
	"errors"
	"fmt"
)

// ErrEngineSkew surfaces from strict-mode CheckEngineSkew when the
// record-time engine identity differs from the replay-time identity.
var ErrEngineSkew = errors.New("program: engine-version skew between record and replay")

// SkewResult is the verdict from CheckEngineSkew. Tag is the canonical
// short form ("engine-skew-replay-from=X to=Y") that digests +
// dashboards key on.
type SkewResult struct {
	Skewed  bool
	Tag     string
	Warning string
}

// CheckEngineSkew compares a brief's record-time engine identity
// against the current binary's identity. Skewed=true when SHAs differ,
// either side is empty/"unknown", or either build was dirty (SHA alone
// does not pin source tree). Strict mode wraps ErrEngineSkew.
//
// The empty/"unknown" branches are deliberately rejected even on agreement
// — that's the silent-rot bug #549 closes: buildvcs=false on both sides
// is reproducible-by-luck today and a time bomb tomorrow.
func CheckEngineSkew(brief *ProgramBrief, current EngineRef, strict bool) (SkewResult, error) {
	if brief == nil {
		return SkewResult{}, errors.New("program: nil brief")
	}
	from := brief.EngineVersion
	to := current.Version
	briefDirty := brief.EngineBuildDirty
	curDirty := current.Dirty

	var res SkewResult
	switch {
	case from == "" || from == "unknown":
		res = SkewResult{
			Skewed:  true,
			Tag:     fmt.Sprintf("engine-skew-replay-from=%s to=%s", emptyAsLiteral(from), to),
			Warning: fmt.Sprintf("brief has no record-time engine_version (got %q); replay cannot prove reproducibility", from),
		}
	case to == "" || to == "unknown":
		res = SkewResult{
			Skewed:  true,
			Tag:     fmt.Sprintf("engine-skew-replay-from=%s to=%s", from, emptyAsLiteral(to)),
			Warning: fmt.Sprintf("current binary has no engine_version (got %q); rebuild with -buildvcs=true or -X compileEngineVersion", to),
		}
	case from != to:
		res = SkewResult{
			Skewed:  true,
			Tag:     fmt.Sprintf("engine-skew-replay-from=%s to=%s", from, to),
			Warning: fmt.Sprintf("engine-skew-replay-from=%s to=%s; replay verdict reflects CURRENT engine, not record-time engine", from, to),
		}
	case briefDirty || curDirty:
		res = SkewResult{
			Skewed:  true,
			Tag:     fmt.Sprintf("engine-skew-dirty-record=%t replay=%t", briefDirty, curDirty),
			Warning: fmt.Sprintf("engine SHAs match (%s) but at least one build is dirty (record=%t replay=%t); SHA does not prove source-tree equivalence", from, briefDirty, curDirty),
		}
	default:
		return SkewResult{}, nil
	}

	if strict {
		return res, fmt.Errorf("%w: %s", ErrEngineSkew, res.Tag)
	}
	return res, nil
}

// emptyAsLiteral renders "" as "<empty>" so the digest tag is not
// ambiguous ("from= to=v2" looks like a parse error).
func emptyAsLiteral(s string) string {
	if s == "" {
		return "<empty>"
	}
	return s
}
