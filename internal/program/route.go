// Package program holds the program-layer decision logic. See
// docs/design.md §Programs for the design contract.
//
// The package is deliberately tiny: program-progression past each
// child is a deterministic Go function over signed gate_results.
// There is no LLM agent here. P1 (deterministic gate before AI gate
// on destructive ops) is applied one layer up: advancing a milestone
// IS the destructive op.
package program

import (
	"errors"
	"fmt"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// Verdict is the routing-time alias for schemas.GateResult plus the
// orchestrator-owned scope-coverage hints. Keeping these two fields
// here (OutOfScope, Responsibility) until they earn a schema spot
// in MVP-2's handoff verification path.
type Verdict struct {
	schemas.GateResult
	OutOfScope     []string // claims this gate explicitly deferred to another
	Responsibility []string // scope this gate explicitly owned
}

// VerifyFunc verifies one Verdict's HMAC against the keyring.
// Implementation lives in schemas/sign.go. Pass it in to avoid an
// import cycle in tests with a stub.
type VerifyFunc func(v Verdict) error

// DepthCap is the typed iteration bound, enforced in the Go spawner.
// Per-axis caps prevent one failure class from consuming another's
// budget. See docs/design.md §Programs §Depth caps.
type DepthCap struct {
	Functional  int // default 2
	Security    int // default 5
	UserTesting int // default 3
}

// DefaultDepthCap returns the documented defaults.
func DefaultDepthCap() DepthCap {
	return DepthCap{Functional: 2, Security: 5, UserTesting: 3}
}

// Depth is the current per-axis iteration count for the child.
type Depth struct {
	Functional  int
	Security    int
	UserTesting int
}

// Over reports whether any axis exceeds its cap.
func (d Depth) Over(capCfg DepthCap) (string, bool) {
	switch {
	case d.Functional > capCfg.Functional:
		return "functional", true
	case d.Security > capCfg.Security:
		return "security", true
	case d.UserTesting > capCfg.UserTesting:
		return "user_testing", true
	}
	return "", false
}

// Child is the orchestrator's view of a child WorkItem and its
// accumulated verdicts at the current PR head.
type Child struct {
	WorkItemID string
	PRSHA      string
	Verdicts   []Verdict
	Depth      Depth
}

// Action is the deterministic decision the orchestrator must take.
type Action string

// Action values: the closed enum of decisions RouteVerdicts may emit.
// Reroute-on-indeterminate is intentionally NOT in the enum until a
// production indeterminate verdict surfaces; the rule per PRINCIPLES
// #2 is no abstraction before the second concrete use. Wiring path
// tracked at issue #48.
const (
	Advance   Action = "advance"
	Iterate   Action = "iterate"
	HaltHuman Action = "halt_human"
)

// Decision is the typed output of RouteVerdicts.
type Decision struct {
	Action      Action
	Reason      string       // operator-facing English
	FixFeatures []FixFeature // populated when Action == Iterate
}

// FixFeature is a new child WorkItem the orchestrator should inject
// in response to a failing verdict.
type FixFeature struct {
	Title              string
	AddressesFinding   string // Finding.ID
	FulfillsCriteria   []string
	DependsOnFeatures  []string
}

// HeartbeatStale is how long a "started but never finished" gate run
// can sit before the orchestrator treats it as a silent bypass.
// Pairs with `regatta audit --reconcile` (docs/design.md §Failure modes).
const HeartbeatStale = 2 * 30 * time.Minute // 30 min cap × 2

// RouteVerdicts is THE deterministic decision function. It owns the
// program's progression past each child. It takes no LLM input,
// emits no LLM output, and is the implementation of P1 applied one
// layer up the stack.
//
// Order of checks matters:
//
//  1. Every verdict MUST verify. Unverifiable = halt.
//  2. Every verdict's heartbeat anchors MUST be present. Missing
//     gate_finished is a silent-bypass attempt (the textbook
//     audit-log-bypass-by-writing-nothing attack).
//  3. Any blocking fail with actionable findings → iterate.
//  4. Any blocking fail without actionable findings → halt.
//  5. Any cross-gate out-of-scope claim not reciprocated by another
//     gate's responsibility → halt. (Coverage union check.)
//  6. Depth axis over cap → halt.
//  7. Otherwise → advance.
//
// The function is intentionally branchy and verbose; the design
// thesis is that this code path is auditable by a human reading the
// switch statement, not by reading a prompt.
//
// Wall clock is sourced from time.Now; callers that need to pin
// the "now" axis (heartbeat staleness, deterministic replay) use
// RouteVerdictsAt instead.
func RouteVerdicts(child Child, verify VerifyFunc, capCfg DepthCap) Decision {
	return RouteVerdictsAt(child, verify, capCfg, time.Now())
}

// RouteVerdictsAt is RouteVerdicts with an explicit "now" — the
// heartbeat-staleness pivot. Lets tests + replay assertions fix the
// clock without coupling RouteVerdicts to a Clock injection seam its
// callers don't otherwise carry.
func RouteVerdictsAt(child Child, verify VerifyFunc, capCfg DepthCap, now time.Time) Decision {
	if verify == nil {
		return Decision{Action: HaltHuman, Reason: "no verifier configured"}
	}
	if len(child.Verdicts) == 0 {
		return Decision{Action: HaltHuman, Reason: "no verdicts to route"}
	}

	for _, v := range child.Verdicts {
		if err := verify(v); err != nil {
			return Decision{
				Action: HaltHuman,
				Reason: fmt.Sprintf("unverifiable verdict %s: %v", v.GateID, err),
			}
		}
		if v.Heartbeat.StartedAt.IsZero() || v.Heartbeat.FinishedAt.IsZero() {
			return Decision{
				Action: HaltHuman,
				Reason: fmt.Sprintf("missing heartbeat anchor on %s", v.GateID),
			}
		}
		if now.Sub(v.Heartbeat.StartedAt) > HeartbeatStale {
			return Decision{
				Action: HaltHuman,
				Reason: fmt.Sprintf("stale heartbeat on %s", v.GateID),
			}
		}
	}

	fixes := collectFixFeatures(child)
	if len(fixes) > 0 {
		return Decision{
			Action:      Iterate,
			Reason:      fmt.Sprintf("%d blocking finding(s) require fix-features", len(fixes)),
			FixFeatures: fixes,
		}
	}
	if anyBlockingFail(child) {
		return Decision{
			Action: HaltHuman,
			Reason: "blocking fail without actionable findings",
		}
	}

	if uncovered := uncoveredOutOfScope(child); len(uncovered) > 0 {
		return Decision{
			Action: HaltHuman,
			Reason: fmt.Sprintf("uncovered out-of-scope: %v", uncovered),
		}
	}

	if axis, over := child.Depth.Over(capCfg); over {
		return Decision{
			Action: HaltHuman,
			Reason: fmt.Sprintf("depth cap exceeded on axis=%s", axis),
		}
	}

	return Decision{Action: Advance, Reason: "all gates pass; heartbeats verified"}
}

// anyBlockingFail reports whether any verdict is blocking + fail.
func anyBlockingFail(child Child) bool {
	for _, v := range child.Verdicts {
		if v.Blocking && v.Verdict == schemas.VerdictFail {
			return true
		}
	}
	return false
}

// collectFixFeatures pulls fix-feature suggestions out of blocking
// fails. A blocking fail with NO actionable findings is NOT
// converted to a fix-feature (caller halts to human instead) -- that
// case is the symptom of a gate that knows something's wrong but
// can't say what, which is exactly the moment to involve a human.
func collectFixFeatures(child Child) []FixFeature {
	var out []FixFeature
	for _, v := range child.Verdicts {
		if !v.Blocking || v.Verdict != schemas.VerdictFail {
			continue
		}
		for _, f := range v.Findings {
			out = append(out, FixFeature{
				Title:            fmt.Sprintf("fix: %s", f.Claim),
				AddressesFinding: f.ID,
			})
		}
	}
	return out
}

// uncoveredOutOfScope returns out-of-scope claims that no other
// verdict reciprocally claims responsibility for. The coverage
// invariant: every (source, sink) pair has an owning gate. If
// gate A defers to gate B but B never claims it, the bug falls
// between them.
func uncoveredOutOfScope(child Child) []string {
	covered := map[string]bool{}
	for _, v := range child.Verdicts {
		for _, r := range v.Responsibility {
			covered[r] = true
		}
	}
	var uncovered []string
	seen := map[string]bool{}
	for _, v := range child.Verdicts {
		for _, oos := range v.OutOfScope {
			if seen[oos] {
				continue
			}
			seen[oos] = true
			if !covered[oos] {
				uncovered = append(uncovered, oos)
			}
		}
	}
	return uncovered
}

// ErrUnverifiable is returned by a verify function when the signed
// payload cannot be verified. Exported so callers (and the audit
// reconciler) can match against it.
var ErrUnverifiable = errors.New("verdict signature unverifiable")
