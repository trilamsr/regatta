package approval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// decideTxHarness mirrors cmd/regatta/approval_decide_test.go::approvalDecideHarness
// so the lift's correctness invariant is provable independently of CLI plumbing.
type decideTxHarness struct {
	db          *state.DB
	clock       func() time.Time
	now         time.Time
	approvalID  string
	workItemID  string
	requestedBy string
	reviewers   []string
	quorum      int
}

func newDecideTxHarness(t *testing.T, requestedBy string, reviewers []string, quorum int, preventSelf bool) *decideTxHarness {
	t.Helper()
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return t0 }
	dbPath := filepath.Join(t.TempDir(), "decide.db")
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.UpsertWorkItem(context.Background(), state.WorkItem{
		ID: "F-1", Kind: state.KindFeature, Title: "F-1", Lane: "server", Status: state.WorkStatusPlanned,
	}, state.SourceBrief, t0); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}

	approvalID := "a-dec00000001"
	a := state.Approval{
		ID:          approvalID,
		WorkItemID:  "F-1",
		GateName:    "ship-gate",
		RequestedAt: t0,
		RequestedBy: requestedBy,
		ReviewerSetSnapshot: state.ReviewerSet{
			Reviewers:         reviewers,
			Quorum:            quorum,
			PreventSelfReview: preventSelf,
		},
		Quorum:    quorum,
		Status:    state.ApprovalStatusPending,
		TimeoutAt: t0.Add(time.Hour),
		OnTimeout: "fail",
	}
	if err := db.CreateApproval(context.Background(), a); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	return &decideTxHarness{
		db:          db,
		clock:       clock,
		now:         t0,
		approvalID:  approvalID,
		workItemID:  "F-1",
		requestedBy: requestedBy,
		reviewers:   reviewers,
		quorum:      quorum,
	}
}

func (h *decideTxHarness) payload(reviewer string) canon.TokenPayload {
	return canon.TokenPayload{
		WI:       h.workItemID,
		AID:      h.approvalID,
		Reviewer: reviewer,
		Window:   h.now.Add(time.Hour).Unix(),
		JTI:      "jti-" + reviewer + "-" + h.approvalID,
	}
}

func TestApprovalDecideTx_HappyAllow(t *testing.T) {
	h := newDecideTxHarness(t, "system", []string{"alice"}, 1, false)
	res, status, err := DecideTx(context.Background(), h.db, h.payload("alice"), "alice", "allow", "ok", h.clock)
	if err != nil {
		t.Fatalf("DecideTx: %v", err)
	}
	if status != state.ApprovalStatusApproved {
		t.Fatalf("status=%q want approved", status)
	}
	if !res.Terminal || !res.TerminalAllow {
		t.Fatalf("result.Terminal=%v TerminalAllow=%v want true,true", res.Terminal, res.TerminalAllow)
	}
	if len(res.DecidedBy) != 1 || res.DecidedBy[0] != "alice" {
		t.Fatalf("DecidedBy=%v want [alice]", res.DecidedBy)
	}
	got, err := h.db.GetApproval(context.Background(), h.approvalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusApproved {
		t.Fatalf("denorm status=%q want approved", got.Status)
	}
}

func TestApprovalDecideTx_HappyDeny(t *testing.T) {
	h := newDecideTxHarness(t, "system", []string{"alice"}, 1, false)
	res, status, err := DecideTx(context.Background(), h.db, h.payload("alice"), "alice", "deny", "audit fail", h.clock)
	if err != nil {
		t.Fatalf("DecideTx: %v", err)
	}
	if status != state.ApprovalStatusRejected {
		t.Fatalf("status=%q want rejected", status)
	}
	if !res.Terminal || res.TerminalAllow {
		t.Fatalf("result.Terminal=%v TerminalAllow=%v want true,false", res.Terminal, res.TerminalAllow)
	}
	got, err := h.db.GetApproval(context.Background(), h.approvalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusRejected {
		t.Fatalf("denorm status=%q want rejected", got.Status)
	}
}

func TestApprovalDecideTx_ReplayReturnsErrTokenReplay(t *testing.T) {
	h := newDecideTxHarness(t, "system", []string{"alice", "bob"}, 2, false)
	p := h.payload("alice")
	if _, _, err := DecideTx(context.Background(), h.db, p, "alice", "allow", "", h.clock); err != nil {
		t.Fatalf("first DecideTx: %v", err)
	}
	_, _, err := DecideTx(context.Background(), h.db, p, "alice", "allow", "", h.clock)
	if err == nil {
		t.Fatalf("second DecideTx: got nil err want ErrTokenReplay")
	}
	if !errors.Is(err, state.ErrTokenReplay) {
		t.Fatalf("second DecideTx err=%v want state.ErrTokenReplay", err)
	}
}

func TestApprovalDecideTx_SelfReviewRejected(t *testing.T) {
	h := newDecideTxHarness(t, "alice", []string{"alice", "bob"}, 1, true)
	_, _, err := DecideTx(context.Background(), h.db, h.payload("alice"), "alice", "allow", "", h.clock)
	if !errors.Is(err, ErrSelfReview) {
		t.Fatalf("DecideTx err=%v want ErrSelfReview", err)
	}
}

func TestApprovalDecideTx_RefactorPreservesCLIBehavior(t *testing.T) {
	// Quorum-of-2: first reviewer leaves the approval pending with one
	// decided_by entry; second reviewer flips it to approved. Mirrors the
	// CLI's TestApprovalDecide_PendingAfterOneOfTwo + TestApprovalDecide_HappyAllowExit0
	// fixtures so byte-identical FoldResult invariants surface in both
	// directions (pending and terminal).
	h := newDecideTxHarness(t, "system", []string{"alice", "bob"}, 2, false)
	res1, status1, err := DecideTx(context.Background(), h.db, h.payload("alice"), "alice", "allow", "", h.clock)
	if err != nil {
		t.Fatalf("DecideTx alice: %v", err)
	}
	if status1 != state.ApprovalStatusPending {
		t.Fatalf("after one of two: status=%q want pending", status1)
	}
	if res1.Terminal {
		t.Fatalf("after one of two: Terminal=true want false")
	}
	if len(res1.DecidedBy) != 1 || res1.DecidedBy[0] != "alice" {
		t.Fatalf("after one of two: DecidedBy=%v want [alice]", res1.DecidedBy)
	}
	if res1.AllowCount != 1 || res1.DenyCount != 0 {
		t.Fatalf("after one of two: Allow=%d Deny=%d want 1,0", res1.AllowCount, res1.DenyCount)
	}

	res2, status2, err := DecideTx(context.Background(), h.db, h.payload("bob"), "bob", "allow", "", h.clock)
	if err != nil {
		t.Fatalf("DecideTx bob: %v", err)
	}
	if status2 != state.ApprovalStatusApproved {
		t.Fatalf("after two of two: status=%q want approved", status2)
	}
	if !res2.Terminal || !res2.TerminalAllow {
		t.Fatalf("after two of two: Terminal=%v TerminalAllow=%v want true,true", res2.Terminal, res2.TerminalAllow)
	}
	if res2.AllowCount != 2 {
		t.Fatalf("after two of two: AllowCount=%d want 2", res2.AllowCount)
	}
	if len(res2.DecidedBy) != 2 || res2.DecidedBy[0] != "alice" || res2.DecidedBy[1] != "bob" {
		t.Fatalf("after two of two: DecidedBy=%v want [alice bob]", res2.DecidedBy)
	}
}

func TestApprovalDecideTx_NoUpdateDeleteOutsideApprovalEvents(t *testing.T) {
	// Append-only invariant: decide.go writes UPDATE only against the
	// approvals denorm row (the same path the CLI used pre-lift). No
	// DELETE anywhere; no UPDATE outside approvals. Static check on the
	// lifted file source.
	body, err := readDecideGoSource()
	if err != nil {
		t.Fatalf("read decide.go: %v", err)
	}
	if regexp.MustCompile(`(?m)\bDELETE\b`).FindString(body) != "" {
		t.Errorf("decide.go contains DELETE — append-only invariant violated")
	}
	// UPDATE allowed ONLY against approvals (denorm cache); never against
	// approval_events (event log is strictly append-only).
	for _, m := range regexp.MustCompile(`UPDATE\s+(\w+)`).FindAllStringSubmatch(body, -1) {
		if m[1] != "approvals" {
			t.Errorf("decide.go contains UPDATE %s — only approvals denorm UPDATE permitted", m[1])
		}
	}
}

func readDecideGoSource() (string, error) {
	b, err := os.ReadFile("decide.go")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
