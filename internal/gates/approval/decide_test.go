package approval

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
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

func (h *decideTxHarness) payload(reviewer string) approvaltoken.TokenPayload {
	return approvaltoken.TokenPayload{
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

// TestApprovalDecideTx_TerminalRaceLeavesEventLogIntact pins #206: terminal event between GetApproval and BEGIN no longer silently overwrites status.
func TestApprovalDecideTx_TerminalRaceLeavesEventLogIntact(t *testing.T) {
	h := newDecideTxHarness(t, "system", []string{"alice", "bob"}, 2, false)
	ctx := context.Background()

	// Worst-case: reaper's terminal event lands BEFORE DecideTx is even called.
	// Pre-fix DecideTx appended `decided` + `approved` and flipped the denorm
	// to approved, producing the split-status bug in #206.
	if err := h.db.AppendApprovalEvent(ctx, state.ApprovalEvent{
		ApprovalID: h.approvalID,
		Ts:         h.now,
		Kind:       EventKindTimedOut,
		Actor:      "system",
		Payload:    []byte(`{"policy":"fail"}`),
	}); err != nil {
		t.Fatalf("seed timed_out: %v", err)
	}
	if err := h.db.MarkApprovalDecided(ctx, h.approvalID, state.ApprovalStatusTimedOut, []string{}, h.now); err != nil {
		t.Fatalf("seed denorm: %v", err)
	}

	priorEvents, err := h.db.ListApprovalEvents(ctx, h.approvalID)
	if err != nil {
		t.Fatalf("ListApprovalEvents (pre): %v", err)
	}
	priorCount := len(priorEvents)

	_, _, err = DecideTx(ctx, h.db, h.payload("alice"), "alice", "allow", "", h.clock)
	if err == nil {
		t.Fatalf("DecideTx: got nil err; want terminal-race sentinel")
	}
	if !errors.Is(err, state.ErrTokenReplay) {
		t.Fatalf("DecideTx err=%v; want state.ErrTokenReplay (terminal-race sentinel)", err)
	}

	got, err := h.db.GetApproval(ctx, h.approvalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusTimedOut {
		t.Fatalf("denorm status=%q; want timed_out (must not be overwritten by losing decide tx)", got.Status)
	}

	postEvents, err := h.db.ListApprovalEvents(ctx, h.approvalID)
	if err != nil {
		t.Fatalf("ListApprovalEvents (post): %v", err)
	}
	if len(postEvents) != priorCount {
		kinds := make([]string, len(postEvents))
		for i, e := range postEvents {
			kinds[i] = e.Kind
		}
		t.Fatalf("events grew from %d to %d (kinds=%v); decide tx must not append after terminal", priorCount, len(postEvents), kinds)
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

// TestMarkApprovalDecidedTx_RowsAffectedZeroReturnsNotFound pins the UPDATE-zero-rows branch (decide.go rows==0).
func TestMarkApprovalDecidedTx_RowsAffectedZeroReturnsNotFound(t *testing.T) {
	h := newDecideTxHarness(t, "system", []string{"alice"}, 1, false)
	ctx := context.Background()
	const missingID = "a-does-not-exist"
	err := h.db.WithTx(ctx, func(tx *sql.Tx) error {
		return markApprovalDecidedTx(ctx, tx, missingID, state.ApprovalStatusApproved, []string{"alice"}, h.now)
	})
	if err == nil {
		t.Fatalf("markApprovalDecidedTx(missing id): got nil err; want not-found")
	}
	if !strings.Contains(err.Error(), missingID) {
		t.Fatalf("markApprovalDecidedTx err=%q; want to cite id %q", err.Error(), missingID)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("markApprovalDecidedTx err=%q; want 'not found' substring", err.Error())
	}
}

// TestMarkApprovalDecidedTx_HappyPathReturnsNilAndFlipsStatus is the positive twin isolating the not-found test to the UPDATE-miss branch.
func TestMarkApprovalDecidedTx_HappyPathReturnsNilAndFlipsStatus(t *testing.T) {
	h := newDecideTxHarness(t, "system", []string{"alice"}, 1, false)
	ctx := context.Background()
	if err := h.db.WithTx(ctx, func(tx *sql.Tx) error {
		return markApprovalDecidedTx(ctx, tx, h.approvalID, state.ApprovalStatusApproved, []string{"alice"}, h.now)
	}); err != nil {
		t.Fatalf("markApprovalDecidedTx(existing id): %v", err)
	}
	got, err := h.db.GetApproval(ctx, h.approvalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusApproved {
		t.Fatalf("denorm status=%q; want approved", got.Status)
	}
}

// TestApprovalDecideTx_ConcurrentDecideRelyOnTerminalRaceGuard proves rows==0 stays unreachable because the pre-tx isTerminal guard (#206) fires first.
func TestApprovalDecideTx_ConcurrentDecideRelyOnTerminalRaceGuard(t *testing.T) {
	h := newDecideTxHarness(t, "system", []string{"alice", "bob"}, 1, false)
	ctx := context.Background()

	var wg sync.WaitGroup
	results := make([]error, 2)
	statuses := make([]string, 2)
	starts := make(chan struct{})
	for i, reviewer := range []string{"alice", "bob"} {
		i, reviewer := i, reviewer
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-starts
			_, status, err := DecideTx(ctx, h.db, h.payload(reviewer), reviewer, "allow", "", h.clock)
			results[i] = err
			statuses[i] = status
		}()
	}
	close(starts)
	wg.Wait()

	var wins, losses int
	for i, err := range results {
		switch {
		case err == nil && statuses[i] == state.ApprovalStatusApproved:
			wins++
		case errors.Is(err, state.ErrTokenReplay):
			losses++
		default:
			t.Fatalf("goroutine %d: err=%v status=%q; want (nil,approved) OR ErrTokenReplay", i, err, statuses[i])
		}
	}
	if wins != 1 || losses != 1 {
		t.Fatalf("wins=%d losses=%d; want exactly one winner + one ErrTokenReplay loser (results=%v)", wins, losses, results)
	}

	// Denorm must reflect exactly one terminal flip — proof the loser did NOT
	// reach markApprovalDecidedTx and hit the rows==0 branch. If the pre-tx
	// terminal guard regressed, we'd see the loser blow up with the
	// not-found error surfaced by the direct-call test above.
	got, err := h.db.GetApproval(ctx, h.approvalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusApproved {
		t.Fatalf("denorm status=%q; want approved", got.Status)
	}
}
