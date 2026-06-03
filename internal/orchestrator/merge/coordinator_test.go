package merge_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// fakeProber is a deterministic PRProber stub. Tests set the per-PR
// outcome via Map; callers can also override the entire Probe call
// via ProbeFunc to inject transient errors.
type fakeProber struct {
	Map       map[int]merge.ProbeResult
	ProbeFunc func(ctx context.Context, prNumber int, expectedSHA string) (merge.ProbeResult, error)
	Calls     []probeCall
}

type probeCall struct {
	PRNumber    int
	ExpectedSHA string
}

func (f *fakeProber) Probe(ctx context.Context, prNumber int, expectedSHA string) (merge.ProbeResult, error) {
	f.Calls = append(f.Calls, probeCall{PRNumber: prNumber, ExpectedSHA: expectedSHA})
	if f.ProbeFunc != nil {
		return f.ProbeFunc(ctx, prNumber, expectedSHA)
	}
	if r, ok := f.Map[prNumber]; ok {
		return r, nil
	}
	return merge.ProbeResult{Status: merge.PRStatusUnknown}, nil
}

// driveToAwaitingMerge drives a fresh agent through the canonical FSM
// path: pending → spawning → running → pr_open → gates_running →
// awaiting_merge. Used by the recovery tests so each case starts from
// the exact state Recover() handles.
func driveToAwaitingMerge(t *testing.T, db *state.DB, workItemID string) state.Agent {
	t.Helper()
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, workItemID, "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	for _, s := range []state.AgentState{
		state.AgentSpawning, state.AgentRunning, state.AgentPROpen,
		state.AgentGatesRunning, state.AgentAwaitingMerge,
	} {
		if _, err := db.TransitionAgent(ctx, a.ID, s, state.AgentMutation{}); err != nil {
			t.Fatalf("transition %s: %v", s, err)
		}
	}
	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	return *got
}

// mustWriteIntent calls merge.WriteIntent inside a real tx; the
// recovery tests stage their precondition by writing the intent the
// production path would have appended before the external merge call.
func mustWriteIntent(t *testing.T, db *state.DB, agentID int64, prNumber int, headSHA string) {
	t.Helper()
	err := db.WithTx(context.Background(), func(tx *sql.Tx) error {
		return merge.WriteIntent(context.Background(), tx, db, agentID, prNumber, headSHA)
	})
	if err != nil {
		t.Fatalf("write intent: %v", err)
	}
}

// newCoordinator wires a Coordinator with the supplied prober and a
// discard logger so test output stays clean.
func newCoordinator(t *testing.T, db *state.DB, prober merge.PRProber) *merge.Coordinator {
	t.Helper()
	c, err := merge.New(merge.Config{
		DB:     db,
		Prober: prober,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("merge.New: %v", err)
	}
	return c
}

// assertEventExists fails the test if no event with the given kind
// has been recorded against agentID.
func assertEventExists(t *testing.T, db *state.DB, agentID int64, kind string) {
	t.Helper()
	if !eventExists(t, db, agentID, kind) {
		t.Fatalf("no %s event for agent %d", kind, agentID)
	}
}

// eventExists is the boolean form of assertEventExists.
func eventExists(t *testing.T, db *state.DB, agentID int64, kind string) bool {
	t.Helper()
	var n int
	err := db.SQL().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE agent_id = ? AND kind = ?`,
		agentID, kind).Scan(&n)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n > 0
}

// countEvents returns the row count for (agent_id, kind) — used by
// the idempotency test to assert no duplicate completions.
func countEvents(t *testing.T, db *state.DB, agentID int64, kind string) int {
	t.Helper()
	var n int
	err := db.SQL().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE agent_id = ? AND kind = ?`,
		agentID, kind).Scan(&n)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

// loadEvent returns the payload_json of the latest event matching
// (agent_id, kind). Fails the test if no such event exists.
func loadEvent(t *testing.T, db *state.DB, agentID int64, kind string) string {
	t.Helper()
	var raw string
	err := db.SQL().QueryRowContext(context.Background(),
		`SELECT payload_json FROM events WHERE agent_id = ? AND kind = ?
		 ORDER BY id DESC LIMIT 1`,
		agentID, kind).Scan(&raw)
	if err != nil {
		t.Fatalf("load %s: %v", kind, err)
	}
	return raw
}

// TestAwaitingMerge_CrashBetweenCallAndCompletion_RecoversCorrectly asserts a crash post-merge-call pre-completion-event recovers to Done without re-issuing the merge.
func TestAwaitingMerge_CrashBetweenCallAndCompletion_RecoversCorrectly(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToAwaitingMerge(t, db, "WORK-1")
	mustWriteIntent(t, db, a.ID, 42, "abc123")

	prober := &fakeProber{Map: map[int]merge.ProbeResult{
		42: {Status: merge.PRStatusMerged, MergeSHA: "deadbeef"},
	}}
	c := newCoordinator(t, db, prober)

	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.State != state.AgentDone {
		t.Fatalf("state after recovery = %s, want done", got.State)
	}
	assertEventExists(t, db, a.ID, merge.EventKindMergeCompleted)
	assertEventExists(t, db, a.ID, merge.EventKindMergeRecovered)
	if len(prober.Calls) != 1 {
		t.Fatalf("probe calls=%d, want 1", len(prober.Calls))
	}
	if prober.Calls[0].PRNumber != 42 || prober.Calls[0].ExpectedSHA != "abc123" {
		t.Fatalf("probe call=%+v, want pr=42 sha=abc123", prober.Calls[0])
	}
}

// TestAwaitingMerge_RecoveryWhenBranchDeleted asserts the squash-merge-outside-regatta path completes the agent via the merged-prober verdict.
func TestAwaitingMerge_RecoveryWhenBranchDeleted(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToAwaitingMerge(t, db, "WORK-1")
	mustWriteIntent(t, db, a.ID, 99, "branchsha")

	prober := &fakeProber{Map: map[int]merge.ProbeResult{
		99: {Status: merge.PRStatusMerged, MergeSHA: "mergedsha"},
	}}
	c := newCoordinator(t, db, prober)

	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentDone {
		t.Fatalf("state=%s, want done (branch-deleted merge path)", got.State)
	}
	completed := loadEvent(t, db, a.ID, merge.EventKindMergeCompleted)
	var p merge.CompletedPayload
	if err := json.Unmarshal([]byte(completed), &p); err != nil {
		t.Fatalf("decode completed: %v", err)
	}
	if p.Source != "recovery" {
		t.Fatalf("completed.source=%q, want recovery", p.Source)
	}
	if p.MergeSHA != "mergedsha" {
		t.Fatalf("completed.merge_sha=%q, want mergedsha", p.MergeSHA)
	}
}

// TestAwaitingMerge_RecoveryWhenSHADiverged asserts a force-push between decision and recovery fails the agent rather than re-merging un-gated code.
func TestAwaitingMerge_RecoveryWhenSHADiverged(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToAwaitingMerge(t, db, "WORK-1")
	mustWriteIntent(t, db, a.ID, 7, "oldsha")

	prober := &fakeProber{Map: map[int]merge.ProbeResult{
		7: {Status: merge.PRStatusOpenSHADiverged},
	}}
	c := newCoordinator(t, db, prober)

	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentCrashed {
		t.Fatalf("state=%s, want crashed (sha-diverged failure)", got.State)
	}
	failed := loadEvent(t, db, a.ID, merge.EventKindMergeFailed)
	var p merge.FailedPayload
	if err := json.Unmarshal([]byte(failed), &p); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if p.Reason != "sha_diverged" {
		t.Fatalf("failed.reason=%q, want sha_diverged", p.Reason)
	}
}

// TestAwaitingMerge_RecoveryClosedUnmerged asserts a PR closed without merge writes merge_failed and crashes the agent for requeue.
func TestAwaitingMerge_RecoveryClosedUnmerged(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToAwaitingMerge(t, db, "WORK-1")
	mustWriteIntent(t, db, a.ID, 12, "sha12")

	prober := &fakeProber{Map: map[int]merge.ProbeResult{
		12: {Status: merge.PRStatusClosedUnmerged},
	}}
	c := newCoordinator(t, db, prober)

	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentCrashed {
		t.Fatalf("state=%s, want crashed (closed-unmerged)", got.State)
	}
	failed := loadEvent(t, db, a.ID, merge.EventKindMergeFailed)
	if !strings.Contains(failed, "closed_unmerged") {
		t.Fatalf("failed payload missing reason: %s", failed)
	}
}

// TestAwaitingMerge_RecoveryOpenSHAMatches_LeavesInPlace asserts a still-open same-SHA PR leaves the agent in awaiting_merge for the next normal-path retry.
func TestAwaitingMerge_RecoveryOpenSHAMatches_LeavesInPlace(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToAwaitingMerge(t, db, "WORK-1")
	mustWriteIntent(t, db, a.ID, 5, "samesha")

	prober := &fakeProber{Map: map[int]merge.ProbeResult{
		5: {Status: merge.PRStatusOpenSHAMatches},
	}}
	c := newCoordinator(t, db, prober)

	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentAwaitingMerge {
		t.Fatalf("state=%s, want awaiting_merge (safe-to-retry, no transition)", got.State)
	}
	if eventExists(t, db, a.ID, merge.EventKindMergeCompleted) {
		t.Fatalf("merge_completed written for retry path")
	}
	if eventExists(t, db, a.ID, merge.EventKindMergeFailed) {
		t.Fatalf("merge_failed written for retry path")
	}
}

// TestAwaitingMerge_RecoveryUnknownStatus_StaysForNextSweep asserts a transient prober Unknown verdict leaves the agent in awaiting_merge for the next sweep retry.
func TestAwaitingMerge_RecoveryUnknownStatus_StaysForNextSweep(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToAwaitingMerge(t, db, "WORK-1")
	mustWriteIntent(t, db, a.ID, 3, "sha3")

	prober := &fakeProber{Map: map[int]merge.ProbeResult{
		3: {Status: merge.PRStatusUnknown},
	}}
	c := newCoordinator(t, db, prober)

	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentAwaitingMerge {
		t.Fatalf("state=%s, want awaiting_merge (unknown probe status)", got.State)
	}
}

// TestAwaitingMerge_RecoveryNoIntent_TransitionsCrashed asserts an awaiting_merge agent with no intent on file crashes rather than stays stuck.
func TestAwaitingMerge_RecoveryNoIntent_TransitionsCrashed(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToAwaitingMerge(t, db, "WORK-1")
	// Deliberately skip the intent write.

	prober := &fakeProber{Map: map[int]merge.ProbeResult{}}
	c := newCoordinator(t, db, prober)

	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentCrashed {
		t.Fatalf("state=%s, want crashed (no_intent_on_file)", got.State)
	}
	failed := loadEvent(t, db, a.ID, merge.EventKindMergeFailed)
	if !strings.Contains(failed, "no_intent_on_file") {
		t.Fatalf("failed payload missing reason: %s", failed)
	}
	if len(prober.Calls) != 0 {
		t.Fatalf("prober called %d times, want 0", len(prober.Calls))
	}
}

// TestAwaitingMerge_RecoveryProberError_IsNonFatal asserts a per-agent prober failure does not strand the rest of the sweep.
func TestAwaitingMerge_RecoveryProberError_IsNonFatal(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a1 := driveToAwaitingMerge(t, db, "WORK-A")
	a2 := driveToAwaitingMerge(t, db, "WORK-B")
	mustWriteIntent(t, db, a1.ID, 1, "sha-a")
	mustWriteIntent(t, db, a2.ID, 2, "sha-b")

	prober := &fakeProber{
		ProbeFunc: func(_ context.Context, prNumber int, _ string) (merge.ProbeResult, error) {
			if prNumber == 1 {
				return merge.ProbeResult{}, errors.New("synthetic gh-api failure")
			}
			return merge.ProbeResult{Status: merge.PRStatusMerged, MergeSHA: "ok"}, nil
		},
	}
	c := newCoordinator(t, db, prober)

	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	g1, _ := db.GetAgent(ctx, a1.ID)
	g2, _ := db.GetAgent(ctx, a2.ID)
	if g1.State != state.AgentAwaitingMerge {
		t.Fatalf("agent#1 state=%s, want awaiting_merge (transient error retries)", g1.State)
	}
	if g2.State != state.AgentDone {
		t.Fatalf("agent#2 state=%s, want done (sweep continued past agent#1 error)", g2.State)
	}
}

// TestAwaitingMerge_RecoveryIsIdempotent asserts re-running Reconcile across already-completed agents does not double-write completion events.
func TestAwaitingMerge_RecoveryIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToAwaitingMerge(t, db, "WORK-1")
	mustWriteIntent(t, db, a.ID, 42, "abc")

	prober := &fakeProber{Map: map[int]merge.ProbeResult{
		42: {Status: merge.PRStatusMerged, MergeSHA: "x"},
	}}
	c := newCoordinator(t, db, prober)

	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if n := countEvents(t, db, a.ID, merge.EventKindMergeCompleted); n != 1 {
		t.Fatalf("merge_completed count=%d, want 1 (idempotency violated)", n)
	}
	if len(prober.Calls) != 1 {
		t.Fatalf("probe calls=%d, want 1 (recovery storm hit gh twice)", len(prober.Calls))
	}
}

// TestWriteIntent_ValidatesInputs asserts pr_number > 0 and head_sha non-empty are enforced to block unrecoverable intent rows.
func TestWriteIntent_ValidatesInputs(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToAwaitingMerge(t, db, "WORK-1")

	err := db.WithTx(ctx, func(tx *sql.Tx) error {
		return merge.WriteIntent(ctx, tx, db, a.ID, 0, "sha")
	})
	if err == nil {
		t.Fatalf("WriteIntent(pr=0) succeeded, want validation error")
	}

	err = db.WithTx(ctx, func(tx *sql.Tx) error {
		return merge.WriteIntent(ctx, tx, db, a.ID, 42, "")
	})
	if err == nil {
		t.Fatalf("WriteIntent(sha=\"\") succeeded, want validation error")
	}
}

// TestNonceCollision_RevertedBranchSafe asserts a revert + re-push to a prior SHA writes a fresh intent that LatestIntent picks over the first.
func TestNonceCollision_RevertedBranchSafe(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToAwaitingMerge(t, db, "WORK-1")

	mustWriteIntent(t, db, a.ID, 77, "oldsha")
	mustWriteIntent(t, db, a.ID, 77, "newsha")

	got, err := merge.LatestIntent(ctx, db, a.ID)
	if err != nil {
		t.Fatalf("latest intent: %v", err)
	}
	if got.HeadSHA != "newsha" {
		t.Fatalf("latest intent sha=%q, want newsha (most-recent wins)", got.HeadSHA)
	}
}

// TestLatestIntent_NoIntent_ReturnsErrNoIntent asserts the sentinel ErrNoIntent surfaces so callers can errors.Is rather than text-match.
func TestLatestIntent_NoIntent_ReturnsErrNoIntent(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToAwaitingMerge(t, db, "WORK-1")

	_, err := merge.LatestIntent(ctx, db, a.ID)
	if !errors.Is(err, merge.ErrNoIntent) {
		t.Fatalf("err=%v, want ErrNoIntent", err)
	}
}
