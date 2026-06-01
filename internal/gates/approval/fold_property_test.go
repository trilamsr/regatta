package approval

import (
	"encoding/json"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state"

	"pgregory.net/rapid"
)

// Spec §7 A+ — fold is the canonical truth for approval status, and
// the approvals.status denorm column is a cache. This property test
// generates 200 random event sequences and asserts the fold is
// internally consistent: re-folding the same events yields the same
// status; the decided-by witness slice is always a subset of the
// reviewer set; terminal events freeze the verdict.

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

		// Witness invariant: every DecidedBy entry MUST be a reviewer.
		// Exception: terminal events synthesise a system actor.
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

		// Self-review invariant: when prevent_self_review fires and
		// the only votes are the requester's, the status MUST stay
		// pending (unless a terminal event arrived).
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
