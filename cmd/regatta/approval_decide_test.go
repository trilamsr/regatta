package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// approvalDecideHarness is the per-test wiring: a DB, a fixed clock,
// a fresh keyring, and a seeded approval. Keeps each table-row terse.
type approvalDecideHarness struct {
	db          *state.DB
	dsn         string
	clock       func() time.Time
	now         time.Time
	keyring     canon.MapKeyring
	keyID       string
	keyEnvName  string
	approvalID  string
	workItemID  string
	requestedBy string
}

func newApprovalDecideHarness(t *testing.T, requestedBy string, reviewers []string, quorum int, preventSelf bool) *approvalDecideHarness {
	t.Helper()
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return t0 }
	dbPath := filepath.Join(t.TempDir(), "decide.db")
	db, err := state.OpenWithClock(context.Background(), state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	seedApprovalWorkItemForCLI(t, db, "F-1", t0)

	key := bytes.Repeat([]byte{0x42}, 32)
	kr := canon.MapKeyring{"kdecide": key}

	approvalID := "a-cli00000001"
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

	// Stage the keyring + DB path into env so the subcommand's runtime
	// loader picks it up. Tests use t.Setenv so values restore.
	keyEnvName := "REGATTA_APPROVAL_TOKEN_KEY_TEST_" + t.Name()
	// '/' from sub-test names is illegal in posix env keys on some shells;
	// strip to '_' for safety even though Go's t.Setenv tolerates it.
	keyEnvName = strings.ReplaceAll(keyEnvName, "/", "_")
	t.Setenv(keyEnvName, string(key))
	t.Setenv("REGATTA_APPROVAL_TOKEN_KEY_ENV", keyEnvName)
	t.Setenv("REGATTA_APPROVAL_TOKEN_KEY_ID", "kdecide")

	return &approvalDecideHarness{
		db:          db,
		dsn:         state.DSN(dbPath),
		clock:       clock,
		now:         t0,
		keyring:     kr,
		keyID:       "kdecide",
		keyEnvName:  keyEnvName,
		approvalID:  approvalID,
		workItemID:  "F-1",
		requestedBy: requestedBy,
	}
}

func seedApprovalWorkItemForCLI(t *testing.T, db *state.DB, id string, at time.Time) {
	t.Helper()
	if err := db.UpsertWorkItem(context.Background(), state.WorkItem{
		ID: id, Kind: state.KindFeature, Title: id, Lane: "server", Status: state.WorkStatusPlanned,
	}, state.SourceBrief, at); err != nil {
		t.Fatalf("seed work item: %v", err)
	}
}

func mintHarnessToken(t *testing.T, h *approvalDecideHarness, reviewer string, window time.Time) string {
	t.Helper()
	wire, _, err := canon.MintToken(h.keyring, h.keyID, canon.TokenPayload{
		WI:       h.workItemID,
		AID:      h.approvalID,
		Reviewer: reviewer,
		Window:   window.Unix(),
	}, rand.Reader)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	return wire
}

func runDecide(t *testing.T, h *approvalDecideHarness, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runApprovalDecideWith(approvalDecideDeps{
		Stdout: &stdout, Stderr: &stderr, Clock: h.clock, DSN: h.dsn,
	}, args)
	return code, stdout.String(), stderr.String()
}

func TestApprovalDecide_HappyAllowExit0(t *testing.T) {
	h := newApprovalDecideHarness(t, "system", []string{"alice"}, 1, false)
	wire := mintHarnessToken(t, h, "alice", h.now.Add(time.Hour))
	code, _, stderr := runDecide(t, h, "--token", wire, "--decision", "allow", "--reviewer-id", "alice", "--reason", "ok")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	got, err := h.db.GetApproval(context.Background(), h.approvalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusApproved {
		t.Fatalf("status=%q want approved", got.Status)
	}
	if len(got.DecidedBy) != 1 || got.DecidedBy[0] != "alice" {
		t.Fatalf("decided_by=%v want [alice]", got.DecidedBy)
	}
}

func TestApprovalDecide_ReplayReturnsExit4(t *testing.T) {
	h := newApprovalDecideHarness(t, "system", []string{"alice", "bob"}, 2, false)
	wire := mintHarnessToken(t, h, "alice", h.now.Add(time.Hour))
	code, _, stderr := runDecide(t, h, "--token", wire, "--decision", "allow", "--reviewer-id", "alice")
	if code != 0 {
		t.Fatalf("first decide: exit=%d stderr=%q", code, stderr)
	}
	code2, _, stderr2 := runDecide(t, h, "--token", wire, "--decision", "allow", "--reviewer-id", "alice")
	if code2 != exitTokenReplay {
		t.Fatalf("replay exit=%d want %d stderr=%q", code2, exitTokenReplay, stderr2)
	}
}

func TestApprovalDecide_TamperedExit2(t *testing.T) {
	h := newApprovalDecideHarness(t, "system", []string{"alice"}, 1, false)
	wire := mintHarnessToken(t, h, "alice", h.now.Add(time.Hour))
	// Flip a byte in the payload half (after the dot) so HMAC mismatches.
	dot := strings.IndexByte(wire, '.')
	if dot < 0 {
		t.Fatal("no dot in wire")
	}
	// Mutate the last char (within the payload b64) to a different valid b64 char.
	last := len(wire) - 1
	repl := byte('A')
	if wire[last] == 'A' {
		repl = 'B'
	}
	tampered := wire[:last] + string(repl)
	code, _, stderr := runDecide(t, h, "--token", tampered, "--decision", "allow", "--reviewer-id", "alice")
	if code != exitUnverifiable && code != exitTokenInvalid {
		// b64 round-trip may turn a single-char mutation into either
		// framing-malformed or HMAC-mismatch depending on the trailing
		// bits; both map to the spec's "this token is not trusted" bucket.
		t.Fatalf("tampered exit=%d want %d or %d stderr=%q", code, exitUnverifiable, exitTokenInvalid, stderr)
	}
}

func TestApprovalDecide_ExpiredExit3(t *testing.T) {
	h := newApprovalDecideHarness(t, "system", []string{"alice"}, 1, false)
	// Window already in the past relative to the harness clock.
	wire := mintHarnessToken(t, h, "alice", h.now.Add(-time.Minute))
	code, _, stderr := runDecide(t, h, "--token", wire, "--decision", "allow", "--reviewer-id", "alice")
	if code != exitTokenExpired {
		t.Fatalf("expired exit=%d want %d stderr=%q", code, exitTokenExpired, stderr)
	}
}

func TestApprovalDecide_NotReviewerExit6(t *testing.T) {
	h := newApprovalDecideHarness(t, "system", []string{"alice"}, 1, false)
	// Mint a token for "carol" who is NOT in the snapshot. VerifyToken
	// would catch this only if the CLI passed --reviewer-id=carol; the
	// snapshot check is the second wall.
	wire := mintHarnessToken(t, h, "carol", h.now.Add(time.Hour))
	code, _, stderr := runDecide(t, h, "--token", wire, "--decision", "allow", "--reviewer-id", "carol")
	if code != exitNotReviewer {
		t.Fatalf("not-reviewer exit=%d want %d stderr=%q", code, exitNotReviewer, stderr)
	}
}

func TestApprovalDecide_SelfReviewBlockedExit7(t *testing.T) {
	// requested_by = alice; reviewer = alice; prevent_self_review = true.
	h := newApprovalDecideHarness(t, "alice", []string{"alice", "bob"}, 1, true)
	wire := mintHarnessToken(t, h, "alice", h.now.Add(time.Hour))
	code, _, stderr := runDecide(t, h, "--token", wire, "--decision", "allow", "--reviewer-id", "alice")
	if code != exitSelfReview {
		t.Fatalf("self-review exit=%d want %d stderr=%q", code, exitSelfReview, stderr)
	}
}

func TestApprovalDecide_PendingAfterOneOfTwo(t *testing.T) {
	h := newApprovalDecideHarness(t, "system", []string{"alice", "bob"}, 2, false)
	wire := mintHarnessToken(t, h, "alice", h.now.Add(time.Hour))
	code, _, stderr := runDecide(t, h, "--token", wire, "--decision", "allow", "--reviewer-id", "alice")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	got, err := h.db.GetApproval(context.Background(), h.approvalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusPending {
		t.Fatalf("status=%q want pending", got.Status)
	}
}

func TestApprovalDecide_DenyTerminalRejected(t *testing.T) {
	h := newApprovalDecideHarness(t, "system", []string{"alice"}, 1, false)
	wire := mintHarnessToken(t, h, "alice", h.now.Add(time.Hour))
	code, _, stderr := runDecide(t, h, "--token", wire, "--decision", "deny", "--reviewer-id", "alice", "--reason", "audit fail")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	got, err := h.db.GetApproval(context.Background(), h.approvalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusRejected {
		t.Fatalf("status=%q want rejected", got.Status)
	}
}

func TestApprovalDecide_AtomicityZeroEventsOnReplay(t *testing.T) {
	// Mid-tx failure path: after the first decide consumes the token,
	// the second invocation's token_consumed INSERT collides on the
	// UNIQUE index and the transaction rolls back — no 'decided' row
	// should be appended for the failed attempt.
	h := newApprovalDecideHarness(t, "system", []string{"alice", "bob", "carol"}, 2, false)
	wire := mintHarnessToken(t, h, "alice", h.now.Add(time.Hour))
	if code, _, stderr := runDecide(t, h, "--token", wire, "--decision", "allow", "--reviewer-id", "alice"); code != 0 {
		t.Fatalf("first: exit=%d stderr=%q", code, stderr)
	}
	before, err := h.db.ListApprovalEvents(context.Background(), h.approvalID)
	if err != nil {
		t.Fatalf("ListApprovalEvents: %v", err)
	}
	code, _, _ := runDecide(t, h, "--token", wire, "--decision", "allow", "--reviewer-id", "alice")
	if code != exitTokenReplay {
		t.Fatalf("replay exit=%d want %d", code, exitTokenReplay)
	}
	after, err := h.db.ListApprovalEvents(context.Background(), h.approvalID)
	if err != nil {
		t.Fatalf("ListApprovalEvents post: %v", err)
	}
	if len(before) != len(after) {
		t.Fatalf("replay leaked events: before=%d after=%d", len(before), len(after))
	}
}

func TestApprovalDecide_ExitCodeMappingTable(t *testing.T) {
	// Table-driven assertion that every typed sentinel in the spec §5.6
	// list has a defined exit code. Drift in either direction (missing
	// mapping or duplicate exit code) fails this guard.
	want := map[error]int{
		canon.ErrTokenInvalid:    exitTokenInvalid,
		canon.ErrUnverifiable:    exitUnverifiable,
		canon.ErrTokenExpired:    exitTokenExpired,
		state.ErrTokenReplay:     exitTokenReplay,
		canon.ErrUnknownKeyID:    exitUnknownKeyID,
		errApprovalNotReviewer:   exitNotReviewer,
		errApprovalSelfReview:    exitSelfReview,
	}
	seen := map[int]error{}
	for err, code := range want {
		if code <= 0 {
			t.Errorf("%v: exit code <= 0", err)
		}
		if prev, dup := seen[code]; dup {
			t.Errorf("exit %d duplicated by %v and %v", code, prev, err)
		}
		seen[code] = err
		// Round-trip via exitCodeFor: every typed sentinel must resolve.
		if got := exitCodeFor(err); got != code {
			t.Errorf("exitCodeFor(%v)=%d want %d", err, got, code)
		}
		// errors.Is wrapping must still resolve through exitCodeFor.
		wrapped := errors.Join(errors.New("wrap"), err)
		if got := exitCodeFor(wrapped); got != code {
			t.Errorf("wrapped exitCodeFor(%v)=%d want %d", err, got, code)
		}
	}
}
