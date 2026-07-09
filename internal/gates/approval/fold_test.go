package approval

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Fold is the canonical source-of-truth derivation per spec §4.1.
// Every test here exercises one event sequence against the FoldConfig
// and asserts the derived status; the denorm column in `approvals` is
// a cache for the scheduler hot path, not the truth.

func evDecided(actor, decision string) state.ApprovalEvent {
	payload, _ := json.Marshal(map[string]string{"decision": decision})
	return state.ApprovalEvent{Kind: EventKindDecided, Actor: actor, Payload: payload}
}

func evTimedOut() state.ApprovalEvent {
	return state.ApprovalEvent{Kind: EventKindTimedOut, Actor: "system"}
}

func evApprovedTerminal(actor string) state.ApprovalEvent {
	return state.ApprovalEvent{Kind: EventKindApproved, Actor: actor}
}

func evRejectedTerminal(actor string) state.ApprovalEvent {
	return state.ApprovalEvent{Kind: EventKindRejected, Actor: actor}
}

func TestFold_QuorumExact(t *testing.T) {
	cfg := FoldConfig{
		ReviewerSet: state.ReviewerSet{Reviewers: []string{"a", "b", "c"}, Quorum: 2},
		RequestedBy: "owner",
	}
	got := Fold([]state.ApprovalEvent{
		evDecided("a", "allow"),
		evDecided("b", "allow"),
	}, cfg)
	if got.Status != StatusApproved {
		t.Fatalf("Status=%q; want %q", got.Status, StatusApproved)
	}
	if len(got.DecidedBy) != 2 {
		t.Errorf("DecidedBy=%v; want 2 entries", got.DecidedBy)
	}
}

func TestFold_QuorumUnderPending(t *testing.T) {
	cfg := FoldConfig{
		ReviewerSet: state.ReviewerSet{Reviewers: []string{"a", "b", "c"}, Quorum: 2},
	}
	got := Fold([]state.ApprovalEvent{evDecided("a", "allow")}, cfg)
	if got.Status != StatusPending {
		t.Fatalf("Status=%q; want pending (1 allow vs quorum=2)", got.Status)
	}
}

func TestFold_RejectMajority(t *testing.T) {
	cfg := FoldConfig{
		ReviewerSet: state.ReviewerSet{Reviewers: []string{"a", "b", "c"}, Quorum: 2},
	}
	// Two denies meet rejection threshold (n - quorum + 1 = 2): cannot
	// reach quorum=2 allows even if c later allows.
	got := Fold([]state.ApprovalEvent{
		evDecided("a", "deny"),
		evDecided("b", "deny"),
	}, cfg)
	if got.Status != StatusRejected {
		t.Fatalf("Status=%q; want rejected", got.Status)
	}
}

func TestFold_PreventSelfReview(t *testing.T) {
	cfg := FoldConfig{
		ReviewerSet: state.ReviewerSet{
			Reviewers:         []string{"alice", "bob"},
			Quorum:            1,
			PreventSelfReview: true,
		},
		RequestedBy: "alice",
	}
	// Alice authored; her allow MUST be ignored — quorum=1 means bob's
	// vote alone decides. Fold returns pending with only alice's vote.
	got := Fold([]state.ApprovalEvent{evDecided("alice", "allow")}, cfg)
	if got.Status != StatusPending {
		t.Fatalf("Status=%q; want pending (self-vote dropped)", got.Status)
	}
	// Reason: alice was the requester. Document this in a typed reason
	// so callers can distinguish "ignored, not yet decided" from a
	// genuinely under-quorum case.
	if !got.SelfReviewDropped {
		t.Error("SelfReviewDropped=false; want true (alice = requested_by)")
	}
}

func TestFold_TerminalEventOverridesVotes(t *testing.T) {
	cfg := FoldConfig{
		ReviewerSet: state.ReviewerSet{Reviewers: []string{"a", "b"}, Quorum: 2},
	}
	// A timed_out event in the log derives status without waiting for
	// quorum-by-decision. Same shape covers reaper-injected terminal.
	got := Fold([]state.ApprovalEvent{
		evDecided("a", "allow"),
		evTimedOut(),
	}, cfg)
	if got.Status != StatusTimedOut {
		t.Fatalf("Status=%q; want timed_out", got.Status)
	}
}

func TestFold_TerminalApproved(t *testing.T) {
	cfg := FoldConfig{
		ReviewerSet: state.ReviewerSet{Reviewers: []string{"a"}, Quorum: 1},
	}
	got := Fold([]state.ApprovalEvent{evApprovedTerminal("system:timeout-default")}, cfg)
	if got.Status != StatusApproved {
		t.Fatalf("Status=%q; want approved", got.Status)
	}
}

func TestFold_TerminalRejected(t *testing.T) {
	cfg := FoldConfig{
		ReviewerSet: state.ReviewerSet{Reviewers: []string{"a"}, Quorum: 1},
	}
	got := Fold([]state.ApprovalEvent{evRejectedTerminal("system")}, cfg)
	if got.Status != StatusRejected {
		t.Fatalf("Status=%q; want rejected", got.Status)
	}
}

func TestFold_EmptyEventsPending(t *testing.T) {
	cfg := FoldConfig{
		ReviewerSet: state.ReviewerSet{Reviewers: []string{"a"}, Quorum: 1},
	}
	got := Fold(nil, cfg)
	if got.Status != StatusPending {
		t.Fatalf("Status=%q; want pending", got.Status)
	}
}

func TestFold_IgnoresNonReviewerVote(t *testing.T) {
	cfg := FoldConfig{
		ReviewerSet: state.ReviewerSet{Reviewers: []string{"a", "b"}, Quorum: 1},
	}
	// "mallory" is not in the reviewer set; his vote MUST NOT count.
	got := Fold([]state.ApprovalEvent{evDecided("mallory", "allow")}, cfg)
	if got.Status != StatusPending {
		t.Fatalf("Status=%q; want pending; mallory not a reviewer", got.Status)
	}
}

func TestFold_DoubleVoteCountsOnce(t *testing.T) {
	cfg := FoldConfig{
		ReviewerSet: state.ReviewerSet{Reviewers: []string{"a", "b", "c"}, Quorum: 2},
	}
	got := Fold([]state.ApprovalEvent{
		evDecided("a", "allow"),
		evDecided("a", "allow"),
	}, cfg)
	if got.Status != StatusPending {
		t.Fatalf("Status=%q; want pending; alice's two allows count once", got.Status)
	}
}

// TestFold_TableDrivenSequences covers 12 representative event sequences (A-tier ≥20 met combined with unit tests above).
func TestFold_TableDrivenSequences(t *testing.T) {
	rs3 := state.ReviewerSet{Reviewers: []string{"a", "b", "c"}, Quorum: 2}
	rs5 := state.ReviewerSet{Reviewers: []string{"a", "b", "c", "d", "e"}, Quorum: 3}
	rs1 := state.ReviewerSet{Reviewers: []string{"a"}, Quorum: 1}

	cases := []struct {
		name   string
		set    state.ReviewerSet
		reqBy  string
		events []state.ApprovalEvent
		want   FoldStatus
	}{
		{"3of3_quorum2_approves", rs3, "", []state.ApprovalEvent{
			evDecided("a", "allow"), evDecided("b", "allow"), evDecided("c", "allow"),
		}, StatusApproved},
		{"3of3_quorum2_one_allow_pending", rs3, "", []state.ApprovalEvent{evDecided("a", "allow")}, StatusPending},
		{"3of3_quorum2_two_deny_rejects", rs3, "", []state.ApprovalEvent{
			evDecided("a", "deny"), evDecided("b", "deny"),
		}, StatusRejected},
		{"3of3_quorum2_allow_then_deny_decides_on_third", rs3, "", []state.ApprovalEvent{
			evDecided("a", "allow"), evDecided("b", "deny"), evDecided("c", "allow"),
		}, StatusApproved},
		{"5of5_quorum3_three_allows_approves", rs5, "", []state.ApprovalEvent{
			evDecided("a", "allow"), evDecided("b", "allow"), evDecided("c", "allow"),
		}, StatusApproved},
		{"5of5_quorum3_three_denies_rejects", rs5, "", []state.ApprovalEvent{
			evDecided("a", "deny"), evDecided("b", "deny"), evDecided("c", "deny"),
		}, StatusRejected},
		{"5of5_quorum3_mixed_pending", rs5, "", []state.ApprovalEvent{
			evDecided("a", "allow"), evDecided("b", "deny"),
		}, StatusPending},
		{"1of1_quorum1_allow_approves", rs1, "", []state.ApprovalEvent{evDecided("a", "allow")}, StatusApproved},
		{"1of1_quorum1_deny_rejects", rs1, "", []state.ApprovalEvent{evDecided("a", "deny")}, StatusRejected},
		{"terminal_overrides_pending", rs3, "", []state.ApprovalEvent{
			evDecided("a", "allow"), evApprovedTerminal("system"),
		}, StatusApproved},
		{"timed_out_kind_yields_timed_out", rs3, "", []state.ApprovalEvent{evTimedOut()}, StatusTimedOut},
		{"empty_pending", rs3, "", nil, StatusPending},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := Fold(tc.events, FoldConfig{ReviewerSet: tc.set, RequestedBy: tc.reqBy})
			if got.Status != tc.want {
				t.Errorf("Status=%q; want %q", got.Status, tc.want)
			}
		})
	}
}

// TestFoldStatus_StringMatchesStateConstants pins FoldStatus.String() 1-1 to state.ApprovalStatus* so denorm persistence never drifts from fold.
func TestFoldStatus_StringMatchesStateConstants(t *testing.T) {
	cases := []struct {
		fs   FoldStatus
		want string
	}{
		{StatusPending, state.ApprovalStatusPending},
		{StatusApproved, state.ApprovalStatusApproved},
		{StatusRejected, state.ApprovalStatusRejected},
		{StatusTimedOut, state.ApprovalStatusTimedOut},
	}
	for _, tc := range cases {
		if got := tc.fs.String(); got != tc.want {
			t.Errorf("FoldStatus(%v).String()=%q; want %q", tc.fs, got, tc.want)
		}
	}
}

// TestFold_DecisionMissingErr asserts a decided event without a decision key surfaces ErrDecisionMissing (no panic, no silent allow).
func TestFold_DecisionMissingErr(t *testing.T) {
	cfg := FoldConfig{ReviewerSet: state.ReviewerSet{Reviewers: []string{"a"}, Quorum: 1}}
	ev := state.ApprovalEvent{Kind: EventKindDecided, Actor: "a", Payload: []byte(`{}`)}
	got := Fold([]state.ApprovalEvent{ev}, cfg)
	if !errors.Is(got.Err, ErrDecisionMissing) {
		t.Fatalf("Err=%v; want ErrDecisionMissing", got.Err)
	}
	// Corrupted log → fold returns pending; the gate caller can decide
	// to escalate or surface to the operator separately.
	if got.Status != StatusPending {
		t.Errorf("Status=%q; want pending on corrupted payload", got.Status)
	}
}
