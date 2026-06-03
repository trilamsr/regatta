//go:build unix

// orchestrator_awaiting_merge_test exercises the awaiting_merge
// crash-recovery wiring PHASE AUTONOMY §11 W2 c0 added on top of
// Recover() + Heartbeat(). The merge package's own tests cover the
// Coordinator's reducer (status → transition); these tests cover the
// orchestrator-level seam:
//
//  1. Recover() calls into the Coordinator when one is wired.
//  2. Recover() does NOT touch awaiting_merge agents (transition them
//     to crashed) when no Coordinator is wired — the pid-bound
//     recovery loop must stay decoupled.
//  3. Heartbeat() refreshes locks for awaiting_merge agents so a long
//     external merge call does not lose its hotspot lock.
package orchestrator

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// stagedAwaitingMerge drives a fresh agent through the canonical path
// (pending → ... → awaiting_merge) and writes a merge_intent. Returns
// the agent so callers can assert on its state post-Recover.
func stagedAwaitingMerge(t *testing.T, db *state.DB, workItemID string, prNumber int, headSHA string) *state.Agent {
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
	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		return merge.WriteIntent(ctx, tx, db, a.ID, prNumber, headSHA)
	}); err != nil {
		t.Fatalf("write intent: %v", err)
	}
	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	return got
}

// fakeProber is the orchestrator-test mirror of the merge package's
// fakeProber. Kept private so the public PRProber interface stays the
// only seam external packages depend on.
type fakeProber struct {
	Result merge.ProbeResult
	Err    error
}

func (f *fakeProber) Probe(_ context.Context, _ int, _ string) (merge.ProbeResult, error) {
	return f.Result, f.Err
}

// TestRecover_WithMergeCoordinator_ReconcilesAwaitingMerge asserts SetMergeCoordinator wires Reconcile into Recover so stranded awaiting_merge agents reach a terminal state.
func TestRecover_WithMergeCoordinator_ReconcilesAwaitingMerge(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 0)

	a := stagedAwaitingMerge(t, db, "WORK-MERGE-1", 42, "abc")

	coord, err := merge.New(merge.Config{
		DB:     db,
		Prober: &fakeProber{Result: merge.ProbeResult{Status: merge.PRStatusMerged, MergeSHA: "merged-sha"}},
	})
	if err != nil {
		t.Fatalf("merge.New: %v", err)
	}
	o.SetMergeCoordinator(coord)

	if err := o.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.State != state.AgentDone {
		t.Fatalf("state after recover = %s, want done", got.State)
	}
}

// TestRecover_WithoutMergeCoordinator_LeavesAwaitingMergeAlone asserts a pre-W2 build (no Coordinator) leaves a healthy awaiting_merge agent in place.
func TestRecover_WithoutMergeCoordinator_LeavesAwaitingMergeAlone(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 0)

	a := stagedAwaitingMerge(t, db, "WORK-MERGE-2", 99, "sha99")
	// No SetMergeCoordinator call.

	if err := o.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.State != state.AgentAwaitingMerge {
		t.Fatalf("state after recover = %s, want awaiting_merge (no coordinator, no transition)", got.State)
	}
}

// TestHeartbeat_RefreshesAwaitingMergeLocks asserts an awaiting_merge agent's locks keep refreshing during long external merge calls.
func TestHeartbeat_RefreshesAwaitingMergeLocks(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 0)

	a := stagedAwaitingMerge(t, db, "WORK-MERGE-3", 7, "sha7")

	// Acquire a lock for the agent, then advance time and Heartbeat.
	// The heartbeat_at column must advance — proving the AwaitingMerge
	// state was enumerated.
	if err := db.TryAcquireLock(ctx, "hotspot/x", a.ID, time.Minute); err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	locksBefore, _ := db.ListLocks(ctx)
	if len(locksBefore) != 1 {
		t.Fatalf("locks before=%d, want 1", len(locksBefore))
	}
	hbBefore := locksBefore[0].HeartbeatAt

	// Wait one second so the unix-second heartbeat_at column advances
	// on Heartbeat() — sub-second writes would tie the timestamp.
	time.Sleep(1100 * time.Millisecond)

	if err := o.Heartbeat(ctx); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	locksAfter, _ := db.ListLocks(ctx)
	if len(locksAfter) != 1 {
		t.Fatalf("locks after=%d, want 1", len(locksAfter))
	}
	if !locksAfter[0].HeartbeatAt.After(hbBefore) {
		t.Fatalf("heartbeat_at did not advance for awaiting_merge agent: before=%s after=%s",
			hbBefore, locksAfter[0].HeartbeatAt)
	}
}
