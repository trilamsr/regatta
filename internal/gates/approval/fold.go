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

// isTerminal returns true when the event log already contains an
// `approved`, `rejected`, or `timed_out` row. Drives two race-defence
// gates: reaper.sweepOne short-circuits on a parallel-winner's terminal
// marker (§3.2.1 idempotency), and decide.go aborts a vote that arrives
// after the reaper already committed a terminal event (§4.1 fold-
// equivalence — denorm status MUST mirror first-terminal-wins).
func isTerminal(events []state.ApprovalEvent) bool {
	for _, e := range events {
		switch e.Kind {
		case EventKindApproved, EventKindRejected, EventKindTimedOut:
			return true
		}
	}
	return false
}

// nextChainTier returns the next tier in the escalation chain plus
// the prior / new indices. priorIdx is the count of `escalated`
// events already in the log (initial chain position 0 = original
// snapshot; each escalation event bumps the pointer one step). Lives
// alongside Fold because the escalated count is itself a fold over
// the event stream — same input shape, same id-ASC ordering contract.
func nextChainTier(a state.Approval, events []state.ApprovalEvent) (priorIdx, newIdx int, tier state.TierConfig, ok bool) {
	priorIdx = 0
	for _, e := range events {
		if e.Kind == EventKindEscalated {
			priorIdx++
		}
	}
	newIdx = priorIdx + 1
	// The original snapshot occupies chain slot 0 conceptually; the
	// EscalationChain slice holds the next-tier configs starting at
	// index 0 = tier 1. So tier n requires chain[n-1].
	if newIdx-1 < 0 || newIdx-1 >= len(a.EscalationChain) {
		return 0, 0, state.TierConfig{}, false
	}
	return priorIdx, newIdx, a.EscalationChain[newIdx-1], true
}

// replayedVote is the per-vote audit row embedded in the escalated
// event's payload. Exported as JSON via field tags only — the type
// itself stays package-private to keep the payload shape governed by
// the spec rather than by an external caller's import graph.
type replayedVote struct {
	Actor     string `json:"actor"`
	Vote      string `json:"vote"`
	Discarded bool   `json:"discarded"`
}

// replayVotes walks prior `decided` events and decides whether each
// reviewer's vote carries forward into the new tier. A vote carries
// forward iff the reviewer is also a member of the new tier; the
// payload still records discarded votes so the audit trail tells the
// full story (§3.3.1.1).
func replayVotes(events []state.ApprovalEvent, newReviewers []string) []replayedVote {
	in := make(map[string]struct{}, len(newReviewers))
	for _, r := range newReviewers {
		in[r] = struct{}{}
	}
	out := make([]replayedVote, 0)
	for _, e := range events {
		if e.Kind != EventKindDecided {
			continue
		}
		var p struct {
			Vote string `json:"vote"`
		}
		_ = json.Unmarshal(e.Payload, &p)
		_, present := in[e.Actor]
		out = append(out, replayedVote{
			Actor:     e.Actor,
			Vote:      p.Vote,
			Discarded: !present,
		})
	}
	return out
}

// tallyReplay aggregates non-discarded votes by side, returning the
// allow / deny counts and the deciders list (for MarkApprovalDecided
// when the new tier is already satisfied by replays alone, §3.3.1.2).
func tallyReplay(replayed []replayedVote) (allow, deny int, deciders []string) {
	deciders = make([]string, 0)
	for _, v := range replayed {
		if v.Discarded {
			continue
		}
		switch v.Vote {
		case DecisionAllow:
			allow++
			deciders = append(deciders, v.Actor)
		case DecisionDeny:
			deny++
			deciders = append(deciders, v.Actor)
		}
	}
	return allow, deny, deciders
}

// outstandingJTIs returns every token JTI that was minted (kind=
// 'token_minted', or any non-empty token_jti on a non-consumed event)
// and has not already been consumed. Reaper inserts one
// token_consumed-with-reason=escalated row per JTI to revoke the
// outstanding token under the prior tier (§3.3.1.3).
func outstandingJTIs(events []state.ApprovalEvent) []string {
	minted := make([]string, 0)
	consumed := make(map[string]struct{})
	for _, e := range events {
		if e.TokenJTI == "" {
			continue
		}
		if e.Kind == EventKindTokenConsumed {
			consumed[e.TokenJTI] = struct{}{}
			continue
		}
		// First occurrence of a JTI on any non-consumed event is the
		// mint marker. Duplicate sightings (e.g. notified + decided
		// carrying the same JTI) collapse into one outstanding entry.
		exists := false
		for _, j := range minted {
			if j == e.TokenJTI {
				exists = true
				break
			}
		}
		if !exists {
			minted = append(minted, e.TokenJTI)
		}
	}
	out := make([]string, 0, len(minted))
	for _, j := range minted {
		if _, c := consumed[j]; !c {
			out = append(out, j)
		}
	}
	return out
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
