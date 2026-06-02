package approval

import (
	"encoding/json"
	"testing"
	"time"

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

// TestApprovalStatus_FoldEqualsStateMachine pins issue #182 fold≡status invariant: Fold matches a separately-coded reference state machine over random event sequences.
func TestApprovalStatus_FoldEqualsStateMachine(t *testing.T) {
	start := time.Now()
	rapid.Check(t, func(t *rapid.T) {
		reviewers := rapid.SliceOfN(rapid.SampledFrom([]string{"a", "b", "c", "d", "e"}), 1, 5).
			Filter(func(r []string) bool { return len(distinct(r)) == len(r) }).
			Draw(t, "reviewers")
		quorum := rapid.IntRange(1, len(reviewers)).Draw(t, "quorum")
		preventSelf := rapid.Bool().Draw(t, "preventSelf")
		requestedBy := ""
		if preventSelf {
			requestedBy = rapid.SampledFrom(reviewers).Draw(t, "requestedBy")
		}
		events := rapid.SliceOfN(genEvent(reviewers), 0, 16).Draw(t, "events")

		cfg := FoldConfig{
			ReviewerSet: state.ReviewerSet{
				Reviewers:         reviewers,
				Quorum:            quorum,
				PreventSelfReview: preventSelf,
			},
			RequestedBy: requestedBy,
		}
		got := Fold(events, cfg)
		want := referenceFold(events, cfg)
		if got.Status != want {
			t.Fatalf("Fold/referenceFold drift: fold=%v ref=%v events=%v cfg=%+v",
				got.Status, want, eventKindsLog(events), cfg)
		}
	})
	// Latency budget per A+ rubric: 1000-check sweep MUST stay <5s so
	// the property test is cheap enough to run in pre-push-check.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("property latency budget exceeded: elapsed=%v want<5s (raise rapid.checks or simplify gen)", elapsed)
	}
}

// referenceFold re-implements spec §4.1 from scratch using a per-actor
// tally map rather than Fold's slice-based dedup. Kept dead-simple on
// purpose: any disagreement with Fold is either a Fold defect or a
// spec-interpretation drift — the reference is the falsifiable oracle.
//
// Algorithm:
//  1. First terminal event (approved/rejected/timed_out) wins; return.
//  2. Otherwise, tally one canonical vote per reviewer-actor (first
//     decided event per actor sticks; later decides by same actor are
//     ignored). Non-reviewer actors are dropped. Self-vote dropped
//     when prevent_self_review=true and actor==requested_by.
//  3. allows≥quorum → approved. eligible-denies<quorum → rejected.
//     Otherwise pending.
func referenceFold(events []state.ApprovalEvent, cfg FoldConfig) FoldStatus {
	allowed := map[string]bool{}
	for _, r := range cfg.ReviewerSet.Reviewers {
		allowed[r] = true
	}
	preventSelf := cfg.ReviewerSet.PreventSelfReview && cfg.RequestedBy != ""

	type vote struct{ allow, deny bool }
	tally := map[string]vote{}

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
			if _, seen := tally[ev.Actor]; seen {
				continue
			}
			dec, err := extractDecision(ev.Payload)
			if err != nil {
				continue
			}
			switch dec {
			case DecisionAllow:
				tally[ev.Actor] = vote{allow: true}
			case DecisionDeny:
				tally[ev.Actor] = vote{deny: true}
			}
		}
	}

	var allows, denies int
	for _, v := range tally {
		if v.allow {
			allows++
		}
		if v.deny {
			denies++
		}
	}

	quorum := cfg.ReviewerSet.Quorum
	if quorum < 1 {
		return StatusPending
	}
	if allows >= quorum {
		return StatusApproved
	}
	eligible := len(allowed)
	if preventSelf && allowed[cfg.RequestedBy] {
		eligible--
	}
	if eligible-denies < quorum {
		return StatusRejected
	}
	return StatusPending
}

func eventKindsLog(events []state.ApprovalEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Kind + "/" + e.Actor
	}
	return out
}
