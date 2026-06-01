package approval

import (
	"encoding/json"
	"errors"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Fold-derivation sentinels.
var (
	// ErrDecisionMissing fires when a `decided` event's payload omits
	// the "decision" key. A corrupted log MUST NOT silently count as
	// allow; the gate caller surfaces this to the operator instead.
	ErrDecisionMissing = errors.New("approval: decided event payload missing 'decision'")
)

// FoldStatus is the canonical derivation result. Spec §4.1: truth is
// fold(events); approvals.status is a denorm cache. String() maps to
// the state.ApprovalStatus* constants so the gate's denorm write
// cannot drift.
type FoldStatus int

// Fold status values.
const (
	StatusPending FoldStatus = iota
	StatusApproved
	StatusRejected
	StatusTimedOut
)

// String maps to the canonical state.ApprovalStatus* strings used in
// the approvals.status denorm column. Drift here means the gate would
// persist a value the DB CHECK constraint rejects.
func (s FoldStatus) String() string {
	switch s {
	case StatusPending:
		return state.ApprovalStatusPending
	case StatusApproved:
		return state.ApprovalStatusApproved
	case StatusRejected:
		return state.ApprovalStatusRejected
	case StatusTimedOut:
		return state.ApprovalStatusTimedOut
	}
	return state.ApprovalStatusPending
}

// FoldConfig carries the constants Fold needs to derive a verdict from
// an event log. Snapshot-driven (state.ReviewerSet + RequestedBy) so
// Fold stays a pure function: no DB, no clock, no time dependence.
type FoldConfig struct {
	ReviewerSet state.ReviewerSet
	RequestedBy string
}

// FoldResult carries the derived verdict plus the diagnostic flags a
// caller needs to record audit / surface to the operator. Err is set
// when the event log is corrupted but the fold can still derive a
// safe-default (pending) — Spec §3.2 fail-closed contract.
type FoldResult struct {
	Status            FoldStatus
	DecidedBy         []string
	SelfReviewDropped bool
	Err               error
}

// Fold derives the canonical verdict from an event log against a
// reviewer-set snapshot. Pure function — no DB, no clock, no I/O.
//
// Ordering: callers MUST pass events in id-ASC order (the order
// state.ListApprovalEvents returns). A terminal event (`approved`,
// `rejected`, `timed_out`) takes precedence over later decided rows
// because once the row's status is denormalised the event log is
// frozen for that branch (spec §4.1).
func Fold(events []state.ApprovalEvent, cfg FoldConfig) FoldResult {
	allowed := reviewerLookup(cfg.ReviewerSet.Reviewers)
	quorum := cfg.ReviewerSet.Quorum
	preventSelfReview := cfg.ReviewerSet.PreventSelfReview && cfg.RequestedBy != ""

	var allowsBy, deniesBy []string
	allowSeen := map[string]bool{}
	denySeen := map[string]bool{}
	var selfDropped bool
	var foldErr error

	for _, ev := range events {
		switch ev.Kind {
		case EventKindApproved:
			return FoldResult{Status: StatusApproved, DecidedBy: append([]string(nil), ev.Actor), Err: foldErr}
		case EventKindRejected:
			return FoldResult{Status: StatusRejected, DecidedBy: append([]string(nil), ev.Actor), Err: foldErr}
		case EventKindTimedOut:
			return FoldResult{Status: StatusTimedOut, Err: foldErr}
		case EventKindDecided:
			if !allowed[ev.Actor] {
				continue
			}
			if preventSelfReview && ev.Actor == cfg.RequestedBy {
				selfDropped = true
				continue
			}
			dec, err := extractDecision(ev.Payload)
			if err != nil {
				// Don't return early — keep folding so the caller sees
				// every defect in one pass. Status stays pending until
				// quorum is reached by other (valid) votes.
				foldErr = err
				continue
			}
			switch dec {
			case DecisionAllow:
				if !allowSeen[ev.Actor] && !denySeen[ev.Actor] {
					allowsBy = append(allowsBy, ev.Actor)
					allowSeen[ev.Actor] = true
				}
			case DecisionDeny:
				if !allowSeen[ev.Actor] && !denySeen[ev.Actor] {
					deniesBy = append(deniesBy, ev.Actor)
					denySeen[ev.Actor] = true
				}
			}
		}
	}

	if quorum < 1 {
		// Defensive: should be caught by Config.Validate, but a
		// corrupted snapshot must not loop or panic. Fail-closed.
		return FoldResult{Status: StatusPending, SelfReviewDropped: selfDropped, Err: foldErr}
	}

	if len(allowsBy) >= quorum {
		return FoldResult{Status: StatusApproved, DecidedBy: allowsBy, SelfReviewDropped: selfDropped, Err: foldErr}
	}
	// Rejection threshold: once enough denies arrive that the remaining
	// not-yet-voted reviewers cannot reach quorum, the approval is
	// terminal-rejected. Concretely: n - denies < quorum → reject.
	eligible := len(allowed)
	if preventSelfReview && allowed[cfg.RequestedBy] {
		eligible--
	}
	if eligible-len(deniesBy) < quorum {
		return FoldResult{Status: StatusRejected, DecidedBy: deniesBy, SelfReviewDropped: selfDropped, Err: foldErr}
	}
	return FoldResult{Status: StatusPending, SelfReviewDropped: selfDropped, Err: foldErr}
}

func reviewerLookup(rs []string) map[string]bool {
	m := make(map[string]bool, len(rs))
	for _, r := range rs {
		m[r] = true
	}
	return m
}

// extractDecision pulls the "decision" string from a decided-event's
// payload without surfacing JSON-shape detail to callers — the fold
// returns ErrDecisionMissing whenever the payload is corrupted.
func extractDecision(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", ErrDecisionMissing
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", ErrDecisionMissing
	}
	v, ok := m["decision"]
	if !ok {
		return "", ErrDecisionMissing
	}
	s, ok := v.(string)
	if !ok {
		return "", ErrDecisionMissing
	}
	return s, nil
}
