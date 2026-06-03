// Replay-skew detection (#549). Replay assumes the deterministic
// engine code that was live at record time. When the engine drifts
// (a tweaked routing rule, a new CEL helper) the silent divergence
// turns "what happened" into "what current code would have done" —
// which for a compliance auditor is misleading green.
//
// CheckEngineSkew is the boundary that turns that silent rot into a
// loud signal. WARN mode (default) tags the digest with the from→to
// SHA pair; STRICT mode (--strict at the replay CLI) returns
// ErrEngineSkew so the audit caller fails closed.
//
// The replay engine itself (substrate event re-execution) is a
// Phase X stub (`internal/history`); this helper exists NOW so the
// brief stamp + skew-detection contract is settled before the
// re-execution path lands. Without that, future Phase X work would
// likely re-litigate the contract under release pressure.

package program

import (
	"errors"
	"fmt"
)

// ErrEngineSkew surfaces from strict-mode CheckEngineSkew when the
// record-time engine identity differs from the replay-time identity.
// errors.Is friendly so audit callers can branch without string-match.
var ErrEngineSkew = errors.New("program: engine-version skew between record and replay")

// SkewResult is the verdict from CheckEngineSkew. Skewed=true with
// Tag/Warning non-empty signals a divergence; the strict-mode caller
// gets ErrEngineSkew in addition. Tag is the canonical short form
// ("engine-skew-replay-from=X to=Y") that digests + dashboards key on.
type SkewResult struct {
	Skewed  bool
	Tag     string
	Warning string
}

// CheckEngineSkew compares a brief's record-time engine identity
// against the current binary's identity. Returns Skewed=true when:
//
//  - brief.EngineVersion != current.Version (SHA mismatch), OR
//  - brief.EngineVersion is "" or "unknown" (no record-time SHA), OR
//  - brief.EngineBuildDirty || current.Dirty (either build was
//    non-reproducible even on SHA match).
//
// Strict mode returns ErrEngineSkew on skew; WARN mode returns nil
// error and lets the caller surface res.Warning to the operator.
//
// The "unknown" / "" branches are deliberately rejected even when both
// sides agree — that's the silent-rot bug #549 closes. A buildvcs=false
// brief replayed by a buildvcs=false binary IS reproducible-by-luck
// today and a hidden time bomb tomorrow; refuse-by-default is the only
// honest answer.
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
		// SHA-equal but at least one build is dirty: the SHA alone
		// does not pin the source tree. Surface as skew with a tag
		// that makes the dirty side legible in the digest.
		res = SkewResult{
			Skewed:  true,
			Tag:     fmt.Sprintf("engine-skew-dirty-record=%t replay=%t", briefDirty, curDirty),
			Warning: fmt.Sprintf("engine SHAs match (%s) but at least one build is dirty (record=%t replay=%t); SHA does not prove source-tree equivalence", from, briefDirty, curDirty),
		}
	default:
		// SHA match, both clean — the only honest "reproducible" answer.
		return SkewResult{}, nil
	}

	if strict {
		return res, fmt.Errorf("%w: %s", ErrEngineSkew, res.Tag)
	}
	return res, nil
}

// emptyAsLiteral renders empty strings as a visible token so the
// digest tag is not ambiguous ("from= to=v2" looks like a parse error;
// "from=<empty> to=v2" makes the operator's read clear).
func emptyAsLiteral(s string) string {
	if s == "" {
		return "<empty>"
	}
	return s
}
