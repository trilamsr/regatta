package approval

import (
	"encoding/json"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state"

	"pgregory.net/rapid"
)

// Spec §7 — fold is the canonical truth for approval status, and the
// approvals.status denorm column is a cache. The properties below pin:
//
//   - determinism: re-folding the same events yields the same status.
//   - witness validity: every DecidedBy entry is a reviewer (or a
//     synthetic system actor when a terminal event fired).
//   - prevent_self_review: a requester's own decided rows cannot satisfy
//     quorum on their own.
//
// The full all-event-kinds property (TestApprovalStatus_FoldEquivalent
// ToStateMachine below) compares fold against an independent reference
// state-machine over the entire ev-kind set called out by spec §6:
// requested / notified / decided / approved / rejected / timed_out /
// escalated / token_consumed / token_revoked.

func TestApprovalStatus_FoldEquivalence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		reviewers := rapid.SliceOfN(rapid.SampledFrom([]string{"a", "b", "c", "d", "e"}), 1, 5).
			Filter(func(r []string) bool { return len(distinct(r)) == len(r) }).
			Draw(t, "reviewers")
		quorum := rapid.IntRange(1, len(reviewers)).Draw(t, "quorum")
		preventSelf := rapid.Bool().Draw(t, "preventSelf")
		requestedBy := ""
		if preventSelf && len(reviewers) > 0 {
			requestedBy = rapid.SampledFrom(reviewers).Draw(t, "requestedBy")
		}
		events := rapid.SliceOfN(genEvent(reviewers), 0, 12).Draw(t, "events")

		cfg := FoldConfig{
			ReviewerSet: state.ReviewerSet{
				Reviewers:         reviewers,
				Quorum:            quorum,
				PreventSelfReview: preventSelf,
			},
			RequestedBy: requestedBy,
		}
		first := Fold(events, cfg)
		second := Fold(events, cfg)
		if first.Status != second.Status {
			t.Fatalf("non-deterministic fold: %v vs %v", first.Status, second.Status)
		}

		hasTerminal := false
		for _, ev := range events {
			if ev.Kind == EventKindApproved || ev.Kind == EventKindRejected || ev.Kind == EventKindTimedOut {
				hasTerminal = true
				break
			}
		}
		if !hasTerminal {
			allowed := reviewerLookup(reviewers)
			for _, who := range first.DecidedBy {
				if !allowed[who] {
					t.Fatalf("DecidedBy contains non-reviewer %q; set=%v", who, reviewers)
				}
			}
		}

		if preventSelf && !hasTerminal {
			onlySelf := true
			anyDecide := false
			for _, ev := range events {
				if ev.Kind != EventKindDecided {
					continue
				}
				anyDecide = true
				if ev.Actor != requestedBy {
					onlySelf = false
					break
				}
			}
			if anyDecide && onlySelf && first.Status == StatusApproved {
				t.Fatalf("self-only vote yielded approved despite prevent_self_review; requester=%q", requestedBy)
			}
		}
	})
}

// TestApprovalStatus_FoldEquivalentToStateMachine — spec §6 + #182 W5: Fold ≡ reference state-machine across random event sequences.
func TestApprovalStatus_FoldEquivalentToStateMachine(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		reviewers := rapid.SliceOfN(rapid.SampledFrom([]string{"a", "b", "c", "d", "e"}), 1, 5).
			Filter(func(r []string) bool { return len(distinct(r)) == len(r) }).
			Draw(t, "reviewers")
		quorum := rapid.IntRange(1, len(reviewers)).Draw(t, "quorum")
		preventSelf := rapid.Bool().Draw(t, "preventSelf")
		requestedBy := ""
		if preventSelf && len(reviewers) > 0 {
			requestedBy = rapid.SampledFrom(reviewers).Draw(t, "requestedBy")
		}
		events := rapid.SliceOfN(genFullEventKind(reviewers), 0, 20).Draw(t, "events")

		cfg := FoldConfig{
			ReviewerSet: state.ReviewerSet{
				Reviewers:         reviewers,
				Quorum:            quorum,
				PreventSelfReview: preventSelf,
			},
			RequestedBy: requestedBy,
		}
		foldStatus := Fold(events, cfg).Status
		smStatus := referenceFold(events, cfg)
		if foldStatus != smStatus {
			t.Fatalf("fold/state-machine disagreement\n  events=%s\n  cfg=%+v\n  fold=%v sm=%v",
				eventsDebugString(events), cfg, foldStatus, smStatus)
		}
	})
}

// referenceFold is the spec §4.1 oracle: an independent transcription
// of the canonical state-machine. Kept literal so a fold-refactor cannot
// inherit the same bug in both implementations.
func referenceFold(events []state.ApprovalEvent, cfg FoldConfig) FoldStatus {
	allowed := map[string]bool{}
	for _, r := range cfg.ReviewerSet.Reviewers {
		allowed[r] = true
	}
	quorum := cfg.ReviewerSet.Quorum
	if quorum < 1 {
		return StatusPending
	}
	preventSelf := cfg.ReviewerSet.PreventSelfReview && cfg.RequestedBy != ""

	allowVotes := map[string]bool{}
	denyVotes := map[string]bool{}
	for _, ev := range events {
		switch ev.Kind {
		case EventKindApproved:
			return StatusApproved
		case EventKindRejected:
			return StatusRejected
		case EventKindTimedOut:
			return StatusTimedOut
		case EventKindDecided:
			if !allowed[ev.Actor] {
				continue
			}
			if preventSelf && ev.Actor == cfg.RequestedBy {
				continue
			}
			var p struct {
				Decision string `json:"decision"`
			}
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			switch p.Decision {
			case DecisionAllow:
				if !denyVotes[ev.Actor] && !allowVotes[ev.Actor] {
					allowVotes[ev.Actor] = true
				}
			case DecisionDeny:
				if !denyVotes[ev.Actor] && !allowVotes[ev.Actor] {
					denyVotes[ev.Actor] = true
				}
			}
		default:
			// requested / notified / escalated / token_consumed /
			// token_revoked / token_minted — non-terminal, no fold
			// transition. Fall through.
		}
	}
	if len(allowVotes) >= quorum {
		return StatusApproved
	}
	eligible := len(allowed)
	if preventSelf && allowed[cfg.RequestedBy] {
		eligible--
	}
	if eligible-len(denyVotes) < quorum {
		return StatusRejected
	}
	return StatusPending
}

func genEvent(reviewers []string) *rapid.Generator[state.ApprovalEvent] {
	return rapid.Custom(func(t *rapid.T) state.ApprovalEvent {
		kind := rapid.SampledFrom([]string{
			EventKindDecided, EventKindDecided, EventKindDecided,
			EventKindApproved, EventKindRejected, EventKindTimedOut,
		}).Draw(t, "kind")
		switch kind {
		case EventKindApproved, EventKindRejected, EventKindTimedOut:
			return state.ApprovalEvent{Kind: kind, Actor: "system"}
		}
		actor := rapid.SampledFrom(append(append([]string{}, reviewers...), "mallory")).Draw(t, "actor")
		decision := rapid.SampledFrom([]string{DecisionAllow, DecisionDeny}).Draw(t, "decision")
		payload, _ := json.Marshal(map[string]string{"decision": decision})
		return state.ApprovalEvent{Kind: EventKindDecided, Actor: actor, Payload: payload}
	})
}

// genFullEventKind covers every kind the spec §6 W5 fold-property task
// calls out. token_revoked has no first-class kind in the production
// schema — the reaper expresses revocation as a token_consumed event
// with payload.reason='escalated'. We model both as same-kind here so
// the reference state-machine and the production fold see the same
// canonical bytes.
func genFullEventKind(reviewers []string) *rapid.Generator[state.ApprovalEvent] {
	return rapid.Custom(func(t *rapid.T) state.ApprovalEvent {
		kind := rapid.SampledFrom([]string{
			EventKindRequested,
			EventKindNotified,
			EventKindDecided, EventKindDecided, EventKindDecided,
			EventKindApproved, EventKindRejected, EventKindTimedOut,
			kindEscalated,
			kindTokenConsumed, // covers both consume + revoked-as-consumed.
		}).Draw(t, "kind")
		actor := rapid.SampledFrom(append(append([]string{}, reviewers...), "system", "mallory")).Draw(t, "actor")
		switch kind {
		case EventKindApproved, EventKindRejected, EventKindTimedOut, kindEscalated:
			// Terminal & escalated events: actor is whoever the reaper or
			// gate stamped — system-prefixed in production but the fold
			// does not inspect actor for these branches.
			return state.ApprovalEvent{Kind: kind, Actor: actor}
		case EventKindRequested, EventKindNotified:
			return state.ApprovalEvent{Kind: kind, Actor: ActorOrchestrator}
		case kindTokenConsumed:
			reason := rapid.SampledFrom([]string{"", "escalated", "consumed"}).Draw(t, "reason")
			var payload []byte
			if reason != "" {
				payload, _ = json.Marshal(map[string]string{"reason": reason})
			}
			jti := rapid.SampledFrom([]string{"jti-1", "jti-2", "jti-3"}).Draw(t, "jti")
			return state.ApprovalEvent{Kind: kind, Actor: actor, Payload: payload, TokenJTI: jti}
		}
		decision := rapid.SampledFrom([]string{DecisionAllow, DecisionDeny, "abstain"}).Draw(t, "decision")
		payload, _ := json.Marshal(map[string]string{"decision": decision})
		return state.ApprovalEvent{Kind: EventKindDecided, Actor: actor, Payload: payload}
	})
}

func distinct(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// eventsDebugString prints a compact failure-mode dump so a rapid
// counter-example is readable in the test log instead of a binary blob.
func eventsDebugString(events []state.ApprovalEvent) string {
	type tinyEv struct {
		Kind    string `json:"kind"`
		Actor   string `json:"actor,omitempty"`
		Payload string `json:"payload,omitempty"`
		JTI     string `json:"jti,omitempty"`
	}
	out := make([]tinyEv, 0, len(events))
	for _, e := range events {
		out = append(out, tinyEv{Kind: e.Kind, Actor: e.Actor, Payload: string(e.Payload), JTI: e.TokenJTI})
	}
	b, _ := json.Marshal(out)
	return string(b)
}
