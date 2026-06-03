// prepare_merge_test pins Coordinator.PrepareMerge added by PR #558
// adversarial-review Bug-1. WriteIntent existed but had no production
// call site — PrepareMerge is the seam c2's auto-merge policy engine
// calls before the external `gh pr merge`, atomically writing the
// merge_intent row + GatesRunning → AwaitingMerge transition.
package merge_test

import (
	"context"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// driveToGatesRunning stages an agent at the state PrepareMerge must
// transition from. Shared across the PrepareMerge tests; the broader
// driveToAwaitingMerge in coordinator_test.go goes one state further.
func driveToGatesRunning(t *testing.T, db *state.DB, workItemID string) state.Agent {
	t.Helper()
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, workItemID, "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	for _, s := range []state.AgentState{
		state.AgentSpawning, state.AgentRunning, state.AgentPROpen,
		state.AgentGatesRunning,
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

// TestPrepareMerge_AtomicallyWritesIntentAndTransitions asserts intent-write + state-transition commit as one unit.
func TestPrepareMerge_AtomicallyWritesIntentAndTransitions(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	c := newCoordinator(t, db, &fakeProber{})

	if err := c.PrepareMerge(ctx, a.ID, 42, "abc123"); err != nil {
		t.Fatalf("prepare merge: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentAwaitingMerge {
		t.Fatalf("state=%s, want awaiting_merge", got.State)
	}
	intent, err := merge.LatestIntent(ctx, db, a.ID)
	if err != nil {
		t.Fatalf("latest intent: %v", err)
	}
	if intent.PRNumber != 42 || intent.HeadSHA != "abc123" {
		t.Fatalf("intent=%+v, want pr=42 sha=abc123", intent)
	}
}

// TestPrepareMerge_FailsOnExistingTerminalState asserts a Done agent is rejected without writing an intent.
func TestPrepareMerge_FailsOnExistingTerminalState(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	// Drive past gates_running into awaiting_merge then done so the
	// PrepareMerge call attempts a forbidden gates_running → ... edge.
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentAwaitingMerge, state.AgentMutation{}); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentDone, state.AgentMutation{}); err != nil {
		t.Fatalf("transition done: %v", err)
	}

	c := newCoordinator(t, db, &fakeProber{})
	err := c.PrepareMerge(ctx, a.ID, 42, "sha")
	if err == nil {
		t.Fatalf("PrepareMerge from done succeeded, want invalid-transition error")
	}
	// And critically: no intent on file (rollback held).
	if _, err := merge.LatestIntent(ctx, db, a.ID); err == nil {
		t.Fatalf("intent written despite failed transition; tx did not roll back")
	}
}

// TestPrepareMerge_RollsBackIntentOnTransitionFailure asserts a bad-input transition path leaves no orphan intent row.
func TestPrepareMerge_RollsBackIntentOnTransitionFailure(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a, err := db.UpsertPending(ctx, "WORK-1", "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Agent is in pending — pending → awaiting_merge is not a valid
	// edge. PrepareMerge must error AND leave no intent row.
	c := newCoordinator(t, db, &fakeProber{})
	if err := c.PrepareMerge(ctx, a.ID, 7, "sha7"); err == nil {
		t.Fatalf("PrepareMerge from pending succeeded, want invalid-transition error")
	}
	if _, err := merge.LatestIntent(ctx, db, a.ID); err == nil {
		t.Fatalf("intent persisted across rolled-back tx")
	}
}

// TestPrepareMerge_ValidatesInputs asserts pr_number > 0 and head_sha non-empty are enforced before any tx work.
func TestPrepareMerge_ValidatesInputs(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	c := newCoordinator(t, db, &fakeProber{})

	if err := c.PrepareMerge(ctx, a.ID, 0, "sha"); err == nil {
		t.Fatalf("PrepareMerge(pr=0) succeeded, want validation error")
	}
	if err := c.PrepareMerge(ctx, a.ID, 42, ""); err == nil {
		t.Fatalf("PrepareMerge(sha=\"\") succeeded, want validation error")
	}
	// Agent must still be in gates_running (no partial transition).
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentGatesRunning {
		t.Fatalf("state=%s, want gates_running (validation must run pre-tx)", got.State)
	}
}
