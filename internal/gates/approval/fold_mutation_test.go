package approval

import (
	"encoding/json"
	"strconv"
	"testing"

	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Mutation-survival coverage for the reaper's tier-comparison helpers
// (`nextChainTier`, `replayVotes`, `tallyReplay`) extracted into
// `fold.go` consolidated by issues #145+#146. Spec §7 A+ rubric (issue
// #147) requires that commenting out any single guard in those helpers
// flips at least one assertion red. The triad below is structured so
// each helper is exercised twice: once with a deterministic table that
// pins the exact mutation evidence cited in the PR body, and once with
// a `rapid` property that scans the input space for survivors.
//
// Documented mutation evidence (operator can reproduce):
//
//	# 1. priorIdx advance: zero-out the escalated-event counter.
//	sed -i.bak 's|priorIdx++|// priorIdx++|' internal/gates/approval/fold.go
//	go test ./internal/gates/approval/ -run TestReaperMutation -count=1
//	# expect: TestReaperMutation_NextChainTier/advances_after_one_escalation FAILS
//	mv internal/gates/approval/fold.go.bak internal/gates/approval/fold.go
//
//	# 2. Discarded flag: invert membership test.
//	sed -i.bak 's|Discarded: !present|Discarded: present|' internal/gates/approval/fold.go
//	go test ./internal/gates/approval/ -run TestReaperMutation -count=1
//	# expect: TestReaperMutation_ReplayVotes_DiscardedFlag FAILS
//	mv internal/gates/approval/fold.go.bak internal/gates/approval/fold.go
//
//	# 3. Replay tally: skip discarded gate so non-members count.
//	sed -i.bak 's|if v.Discarded {|if false \&\& v.Discarded {|' internal/gates/approval/fold.go
//	go test ./internal/gates/approval/ -run TestReaperMutation -count=1
//	# expect: TestReaperMutation_TallyReplay_QuorumRecount FAILS
//	mv internal/gates/approval/fold.go.bak internal/gates/approval/fold.go

// chainTierMutationCase pins each subtest's expected (priorIdx, newIdx,
// reviewers, ok) so a single-condition mutation flips at least one row.
type chainTierMutationCase struct {
	name      string
	chain     []state.TierConfig
	priorEsc  int
	priorIdx  int
	newIdx    int
	reviewers []string
	ok        bool
}

// TestReaperMutation_NextChainTier pins the tier-index advance: every escalation event MUST bump newIdx by one and surface the matching chain rung.
func TestReaperMutation_NextChainTier(t *testing.T) {
	t.Parallel()
	chain := []state.TierConfig{
		{Reviewers: []string{"sec-l1"}, Quorum: 1},
		{Reviewers: []string{"sec-l2", "sec-l2b"}, Quorum: 2},
		{Reviewers: []string{"cto"}, Quorum: 1},
	}
	cases := []chainTierMutationCase{
		{
			name:      "first_escalation_picks_chain_0",
			chain:     chain,
			priorEsc:  0,
			priorIdx:  0,
			newIdx:    1,
			reviewers: []string{"sec-l1"},
			ok:        true,
		},
		{
			name:      "advances_after_one_escalation",
			chain:     chain,
			priorEsc:  1,
			priorIdx:  1,
			newIdx:    2,
			reviewers: []string{"sec-l2", "sec-l2b"},
			ok:        true,
		},
		{
			name:      "advances_after_two_escalations",
			chain:     chain,
			priorEsc:  2,
			priorIdx:  2,
			newIdx:    3,
			reviewers: []string{"cto"},
			ok:        true,
		},
		{
			name:      "exhausted_chain_returns_not_ok",
			chain:     chain,
			priorEsc:  3,
			priorIdx:  3,
			newIdx:    4,
			reviewers: nil,
			ok:        false,
		},
		{
			name:      "empty_chain_returns_not_ok",
			chain:     nil,
			priorEsc:  0,
			priorIdx:  0,
			newIdx:    1,
			reviewers: nil,
			ok:        false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			a := state.Approval{EscalationChain: tc.chain}
			events := make([]state.ApprovalEvent, 0, tc.priorEsc)
			for i := 0; i < tc.priorEsc; i++ {
				events = append(events, state.ApprovalEvent{Kind: EventKindEscalated, Actor: "system"})
			}
			// Interleave unrelated events to prove the counter only
			// reacts to escalated rows. A mutation that drops the
			// `if e.Kind == EventKindEscalated` guard would over-count
			// these and trip the priorIdx assertion below.
			events = append(events,
				state.ApprovalEvent{Kind: EventKindDecided, Actor: "noise"},
				state.ApprovalEvent{Kind: EventKindTokenMinted, Actor: "system"},
			)

			gotPrior, gotNew, gotTier, gotOK := nextChainTier(a, events)
			if gotOK != tc.ok {
				t.Fatalf("ok=%v; want %v", gotOK, tc.ok)
			}
			if !tc.ok {
				return
			}
			if gotPrior != tc.priorIdx {
				t.Errorf("priorIdx=%d; want %d (mutation suspect: priorIdx++ removed or escalated-kind guard dropped)", gotPrior, tc.priorIdx)
			}
			if gotNew != tc.newIdx {
				t.Errorf("newIdx=%d; want %d (mutation suspect: newIdx = priorIdx + 1 dropped)", gotNew, tc.newIdx)
			}
			if got, want := gotTier.Reviewers, tc.reviewers; !equalStringSlice(got, want) {
				t.Errorf("tier.Reviewers=%v; want %v (mutation suspect: chain[newIdx-1] off-by-one)", got, want)
			}
		})
	}
}

// evReplayDecided builds a decided event using the canonical "decision" payload key — matches decide.go's emit (issue #508 fix).
func evReplayDecided(actor, decision string) state.ApprovalEvent {
	payload, _ := json.Marshal(map[string]string{"decision": decision})
	return state.ApprovalEvent{Kind: EventKindDecided, Actor: actor, Payload: payload}
}

// TestReaperMutation_ReplayVotes_DiscardedFlag pins discarded=!member: a vote carries forward iff the actor is in the new tier.
func TestReaperMutation_ReplayVotes_DiscardedFlag(t *testing.T) {
	t.Parallel()
	prior := []state.ApprovalEvent{
		evReplayDecided("alice", DecisionAllow),
		evReplayDecided("bob", DecisionDeny),
		evReplayDecided("carol", DecisionAllow),
		// non-decided rows MUST be ignored by replay.
		{Kind: EventKindEscalated, Actor: "system"},
		{Kind: EventKindTokenMinted, Actor: "system"},
	}
	newTier := []string{"alice", "carol", "dave"}

	replayed := replayVotes(prior, newTier)
	if got := len(replayed); got != 3 {
		t.Fatalf("len(replayed)=%d; want 3 (mutation suspect: non-decided kinds leaking in)", got)
	}

	// alice and carol carry forward; bob is dropped from the new tier.
	want := []replayedVote{
		{Actor: "alice", Vote: DecisionAllow, Discarded: false},
		{Actor: "bob", Vote: DecisionDeny, Discarded: true},
		{Actor: "carol", Vote: DecisionAllow, Discarded: false},
	}
	for i, w := range want {
		got := replayed[i]
		if got.Actor != w.Actor || got.Vote != w.Vote {
			t.Errorf("replayed[%d]=%+v; want %+v", i, got, w)
		}
		if got.Discarded != w.Discarded {
			t.Errorf("replayed[%d].Discarded=%v; want %v actor=%q (mutation suspect: Discarded sense inverted)",
				i, got.Discarded, w.Discarded, got.Actor)
		}
	}
}

// TestReaperMutation_TallyReplay_QuorumRecount pins tally semantics: discarded votes MUST NOT count toward the new-tier quorum.
func TestReaperMutation_TallyReplay_QuorumRecount(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		input     []replayedVote
		wantAllow int
		wantDeny  int
		wantDec   []string
	}{
		{
			name: "discarded_vote_excluded_from_quorum",
			input: []replayedVote{
				{Actor: "alice", Vote: DecisionAllow, Discarded: false},
				{Actor: "bob", Vote: DecisionAllow, Discarded: true},
				{Actor: "carol", Vote: DecisionDeny, Discarded: false},
			},
			wantAllow: 1,
			wantDeny:  1,
			wantDec:   []string{"alice", "carol"},
		},
		{
			name: "all_discarded_yields_zero",
			input: []replayedVote{
				{Actor: "alice", Vote: DecisionAllow, Discarded: true},
				{Actor: "bob", Vote: DecisionDeny, Discarded: true},
			},
			wantAllow: 0,
			wantDeny:  0,
			wantDec:   []string{},
		},
		{
			name: "carry_forward_quorum_satisfied",
			input: []replayedVote{
				{Actor: "alice", Vote: DecisionAllow, Discarded: false},
				{Actor: "carol", Vote: DecisionAllow, Discarded: false},
			},
			wantAllow: 2,
			wantDeny:  0,
			wantDec:   []string{"alice", "carol"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			allow, deny, deciders := tallyReplay(tc.input)
			if allow != tc.wantAllow {
				t.Errorf("allow=%d; want %d (mutation suspect: discarded gate skipped, allow++ branch flipped)", allow, tc.wantAllow)
			}
			if deny != tc.wantDeny {
				t.Errorf("deny=%d; want %d (mutation suspect: discarded gate skipped, deny++ branch flipped)", deny, tc.wantDeny)
			}
			if !equalStringSlice(deciders, tc.wantDec) {
				t.Errorf("deciders=%v; want %v (mutation suspect: deciders append moved out of allow/deny branch)", deciders, tc.wantDec)
			}
		})
	}
}

// TestReaperMutation_PipelineProperty wires nextChainTier → replayVotes → tallyReplay end-to-end and asserts the invariants any single-line mutation would violate.
func TestReaperMutation_PipelineProperty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		// Build a chain of 1..4 tiers, each drawing reviewers from a
		// shared pool so cross-tier membership is realistic (some
		// votes carry forward, some get discarded).
		pool := []string{"a", "b", "c", "d", "e"}
		chainLen := rapid.IntRange(1, 4).Draw(rt, "chain_len")
		chain := make([]state.TierConfig, chainLen)
		for i := 0; i < chainLen; i++ {
			n := rapid.IntRange(1, len(pool)).Draw(rt, "tier_size_"+strconv.Itoa(i))
			revs := rapid.SliceOfN(rapid.SampledFrom(pool), n, n).
				Filter(func(rs []string) bool { return len(distinct(rs)) == len(rs) }).
				Draw(rt, "tier_revs_"+strconv.Itoa(i))
			chain[i] = state.TierConfig{Reviewers: revs, Quorum: rapid.IntRange(1, len(revs)).Draw(rt, "tier_q_"+strconv.Itoa(i))}
		}
		a := state.Approval{EscalationChain: chain}

		// Draw prior escalation count in [0, chainLen+1] so we cover
		// the chain-exhausted edge too.
		priorEsc := rapid.IntRange(0, chainLen+1).Draw(rt, "prior_esc")
		events := make([]state.ApprovalEvent, 0, priorEsc+4)
		for i := 0; i < priorEsc; i++ {
			events = append(events, state.ApprovalEvent{Kind: EventKindEscalated, Actor: "system"})
		}

		// Add 0..6 decided events from the pool (incl. an outsider
		// "mallory" the replay MUST still record as discarded since
		// replayVotes doesn't know the reviewer set — it only knows
		// the NEW tier's membership).
		nVotes := rapid.IntRange(0, 6).Draw(rt, "n_votes")
		votersPool := append(append([]string{}, pool...), "mallory")
		for i := 0; i < nVotes; i++ {
			actor := rapid.SampledFrom(votersPool).Draw(rt, "vote_actor_"+strconv.Itoa(i))
			dec := rapid.SampledFrom([]string{DecisionAllow, DecisionDeny}).Draw(rt, "vote_dec_"+strconv.Itoa(i))
			// Canonical "decision" key — matches decide.go's emit (issue #508).
			payload, _ := json.Marshal(map[string]string{"decision": dec})
			events = append(events, state.ApprovalEvent{Kind: EventKindDecided, Actor: actor, Payload: payload})
		}

		priorIdx, newIdx, tier, ok := nextChainTier(a, events)

		// Reference count: priorIdx (when ok) MUST equal the number
		// of EventKindEscalated rows. When ok=false the function
		// returns zero-values, so the reference comparison only fires
		// on the happy path.
		var refEsc int
		for _, e := range events {
			if e.Kind == EventKindEscalated {
				refEsc++
			}
		}

		// Invariant 1 — ok iff (refEsc + 1) is a valid chain slot.
		wantOK := refEsc+1-1 >= 0 && refEsc+1-1 < len(chain) && len(chain) > 0
		if ok != wantOK {
			rt.Fatalf("ok=%v; want %v (escalations=%d chainLen=%d)", ok, wantOK, refEsc, len(chain))
		}
		if !ok {
			return
		}

		// Invariant 2 — priorIdx counts ONLY escalated rows.
		if priorIdx != refEsc {
			rt.Fatalf("priorIdx=%d; want %d (escalated-event count) — mutation suspect: priorIdx++ guard altered",
				priorIdx, refEsc)
		}

		// Invariant 3 — newIdx = priorIdx + 1, always.
		if newIdx != priorIdx+1 {
			rt.Fatalf("newIdx=%d; want priorIdx+1=%d — mutation suspect: newIdx assignment changed",
				newIdx, priorIdx+1)
		}

		// Invariant 4 — tier returned matches chain[newIdx-1].
		if !equalStringSlice(tier.Reviewers, chain[newIdx-1].Reviewers) {
			rt.Fatalf("tier.Reviewers=%v; want %v (off-by-one on chain index)",
				tier.Reviewers, chain[newIdx-1].Reviewers)
		}

		// Invariant 5 — replayVotes records every decided event,
		// discarded ↔ actor NOT in new tier.
		replayed := replayVotes(events, tier.Reviewers)
		var refDecided int
		for _, e := range events {
			if e.Kind == EventKindDecided {
				refDecided++
			}
		}
		if len(replayed) != refDecided {
			rt.Fatalf("len(replayed)=%d; want %d (decided-event count)", len(replayed), refDecided)
		}
		newMembers := map[string]struct{}{}
		for _, r := range tier.Reviewers {
			newMembers[r] = struct{}{}
		}
		for i, rv := range replayed {
			_, member := newMembers[rv.Actor]
			if rv.Discarded == member {
				rt.Fatalf("replayed[%d].Discarded=%v but member=%v (actor=%q tier=%v)",
					i, rv.Discarded, member, rv.Actor, tier.Reviewers)
			}
		}

		// Invariant 6 — tallyReplay sums ONLY non-discarded votes and
		// the deciders slice exactly matches the carry-forward actors
		// whose vote was a recognised allow/deny.
		allow, deny, deciders := tallyReplay(replayed)
		var refAllow, refDeny int
		refDec := make([]string, 0)
		for _, rv := range replayed {
			if rv.Discarded {
				continue
			}
			switch rv.Vote {
			case DecisionAllow:
				refAllow++
				refDec = append(refDec, rv.Actor)
			case DecisionDeny:
				refDeny++
				refDec = append(refDec, rv.Actor)
			}
		}
		if allow != refAllow {
			rt.Fatalf("allow=%d; want %d (discarded gate or DecisionAllow branch mutated)", allow, refAllow)
		}
		if deny != refDeny {
			rt.Fatalf("deny=%d; want %d (discarded gate or DecisionDeny branch mutated)", deny, refDeny)
		}
		if !equalStringSlice(deciders, refDec) {
			rt.Fatalf("deciders=%v; want %v", deciders, refDec)
		}
	})
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

