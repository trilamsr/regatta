// Package missions holds the mission-layer decision logic. See
// docs/design.md §Missions for the design contract.
//
// The package is deliberately tiny: mission-progression past each
// child is a deterministic Go function over signed gate_results.
// There is no LLM agent here. P1 (deterministic gate before AI gate
// on destructive ops) is applied one layer up: advancing a milestone
// IS the destructive op.
package missions

import (
	"errors"
	"fmt"
	"time"
)

// Verdict is one gate's signed result over a child WorkItem.
// Subset of schemas/gate_result.schema.json sufficient for routing.
type Verdict struct {
	GateID           string
	PRSHA            string
	Result           string // "pass" | "fail" | "advisory"
	Blocking         bool
	Severity         string
	Findings         []Finding
	OutOfScope       []string // claims this gate explicitly deferred to another
	Responsibility   []string // scope this gate explicitly owned
	StartedAt        *time.Time
	FinishedAt       *time.Time
	Signature        Signature
}

type Finding struct {
	ID          string
	Severity    string
	Claim       string
	TrapPattern string
}

type Signature struct {
	Alg   string
	KeyID string
	MAC   string
}

// VerifyFunc verifies one Verdict's HMAC against the keyring.
// Implementation lives in schemas/gate_result.go (or wherever the
// canonical HMAC verifier lives). Pass it in to avoid an import
// cycle and to make this package trivially testable with a stub.
type VerifyFunc func(v Verdict) error

// DepthCap is the typed iteration bound, enforced in the Go spawner.
// Per-axis caps prevent one failure class from consuming another's
// budget. See docs/design.md §Missions §Depth caps.
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
func (d Depth) Over(cap DepthCap) (string, bool) {
	switch {
	case d.Functional > cap.Functional:
		return "functional", true
	case d.Security > cap.Security:
		return "security", true
	case d.UserTesting > cap.UserTesting:
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

const (
	Advance           Action = "advance"
	Iterate           Action = "iterate"
	HaltHuman         Action = "halt_human"
	RerouteValidator  Action = "reroute_validator"
)

// Decision is the typed output of RouteVerdicts.
type Decision struct {
	Action       Action
	Reason       string         // operator-facing English
	FixFeatures  []FixFeature   // populated when Action == Iterate
	RerouteGate  string         // populated when Action == RerouteValidator
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
// mission's progression past each child. It takes no LLM input,
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
func RouteVerdicts(child Child, verify VerifyFunc, cap DepthCap) Decision {
	if verify == nil {
		return Decision{Action: HaltHuman, Reason: "no verifier configured"}
	}
	if len(child.Verdicts) == 0 {
		return Decision{Action: HaltHuman, Reason: "no verdicts to route"}
	}

	now := time.Now()
	for _, v := range child.Verdicts {
		if err := verify(v); err != nil {
			return Decision{
				Action: HaltHuman,
				Reason: fmt.Sprintf("unverifiable verdict %s: %v", v.GateID, err),
			}
		}
		if v.StartedAt == nil || v.FinishedAt == nil {
			return Decision{
				Action: HaltHuman,
				Reason: fmt.Sprintf("missing heartbeat anchor on %s", v.GateID),
			}
		}
		if now.Sub(*v.StartedAt) > HeartbeatStale && v.FinishedAt == nil {
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

	if axis, over := child.Depth.Over(cap); over {
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
		if v.Blocking && v.Result == "fail" {
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
		if !v.Blocking || v.Result != "fail" {
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
