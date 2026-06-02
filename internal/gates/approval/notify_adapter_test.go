package approval

import (
	"reflect"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Spec §5.8 + issue #133: state.Approval → notify.Request round-trips with all fields populated.
func TestNewNotifyRequest_RoundTrip(t *testing.T) {
	deadline := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	tokens := map[string]string{"alice": "tok-a", "bob": "tok-b", "carol": "tok-c"}
	a := state.Approval{
		ID:         "appr-01",
		WorkItemID: "wi-42",
		GateName:   "deploy-approval",
		ReviewerSetSnapshot: state.ReviewerSet{
			Reviewers: []string{"alice", "bob", "carol"},
			Quorum:    2,
		},
	}
	wi := state.WorkItem{ID: "wi-42"}

	got := newNotifyRequest(a, wi, deadline, tokens)

	if got.ApprovalID != a.ID {
		t.Errorf("ApprovalID=%q; want %q", got.ApprovalID, a.ID)
	}
	if got.WorkItemID != wi.ID {
		t.Errorf("WorkItemID=%q; want %q", got.WorkItemID, wi.ID)
	}
	if got.GateName != a.GateName {
		t.Errorf("GateName=%q; want %q", got.GateName, a.GateName)
	}
	if !reflect.DeepEqual(got.Reviewers, a.ReviewerSetSnapshot.Reviewers) {
		t.Errorf("Reviewers=%v; want %v (audit byte-equality requires snapshot source)", got.Reviewers, a.ReviewerSetSnapshot.Reviewers)
	}
	if !got.DecisionDeadline.Equal(deadline) {
		t.Errorf("DecisionDeadline=%v; want %v", got.DecisionDeadline, deadline)
	}
	if !reflect.DeepEqual(got.Tokens, tokens) {
		t.Errorf("Tokens=%v; want %v", got.Tokens, tokens)
	}
}

// Per Acceptance: reviewer_count audit attr MUST equal len(a.ReviewerSetSnapshot.Reviewers).
func TestNewNotifyRequest_ReviewerCountMatchesSnapshot(t *testing.T) {
	a := state.Approval{
		ID:         "appr-02",
		WorkItemID: "wi-7",
		GateName:   "prod-release",
		ReviewerSetSnapshot: state.ReviewerSet{
			Reviewers: []string{"r1", "r2", "r3", "r4", "r5"},
			Quorum:    3,
		},
	}
	got := newNotifyRequest(a, state.WorkItem{ID: "wi-7"}, time.Now(), nil)
	if len(got.Reviewers) != len(a.ReviewerSetSnapshot.Reviewers) {
		t.Errorf("len(Reviewers)=%d; want %d (matches a.ReviewerSetSnapshot.Reviewers per spec §7 audit byte-equality)", len(got.Reviewers), len(a.ReviewerSetSnapshot.Reviewers))
	}
}

// Defensive copy: caller mutation of the returned Reviewers slice MUST NOT mutate the snapshot.
func TestNewNotifyRequest_ReviewersDefensiveCopy(t *testing.T) {
	a := state.Approval{
		ReviewerSetSnapshot: state.ReviewerSet{Reviewers: []string{"alice", "bob"}},
	}
	got := newNotifyRequest(a, state.WorkItem{}, time.Time{}, nil)
	got.Reviewers[0] = "MUTATED"
	if a.ReviewerSetSnapshot.Reviewers[0] != "alice" {
		t.Errorf("snapshot aliased: a.ReviewerSetSnapshot.Reviewers[0]=%q; adapter must copy", a.ReviewerSetSnapshot.Reviewers[0])
	}
}
