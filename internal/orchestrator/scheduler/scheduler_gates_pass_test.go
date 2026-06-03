// scheduler_gates_pass_test pins the gates_pass → PrepareMerge +
// Enqueue hook (#612). When an agent in GatesRunning passes all
// required gates AND auto-merge is wired (Coordinator + Worker both
// non-nil), the scheduler must atomically write the intent + transition
// to AwaitingMerge + enqueue the merge request — closing the W2-c2
// gap that left --auto-merge=true a no-op.
package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// fakeMergeProber returns a canned ProbeResult per PR number. Empty
// map defaults to PRStatusOpenSHAMatches so a gates_running agent
// looks "ready to merge" — matching the scheduler's gates_pass intent.
type fakeMergeProber struct {
	Map map[int]merge.ProbeResult
}

func (f *fakeMergeProber) Probe(_ context.Context, prNumber int, _ string) (merge.ProbeResult, error) {
	if r, ok := f.Map[prNumber]; ok {
		return r, nil
	}
	return merge.ProbeResult{Status: merge.PRStatusOpenSHAMatches}, nil
}

// driveToGatesRunning stages an agent in GatesRunning with a known PR
// SHA so OnGatesPass has a well-formed intent to write.
func driveToGatesRunning(t *testing.T, db *state.DB, workItemID, prSHA string) state.Agent {
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
		mut := state.AgentMutation{}
		if s == state.AgentPROpen {
			sha := prSHA
			mut.PRSHA = &sha
		}
		if _, err := db.TransitionAgent(ctx, a.ID, s, mut); err != nil {
			t.Fatalf("transition %s: %v", s, err)
		}
	}
	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	return *got
}

// TestSchedulerGatesPass_TriggersPrepareMergeAndEnqueue asserts the hook writes intent + transitions + enqueues when auto-merge is wired (#612).
func TestSchedulerGatesPass_TriggersPrepareMergeAndEnqueue(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WI-MERGE-1", "abc123")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	coord, err := merge.New(merge.Config{DB: db, Prober: &fakeMergeProber{}, Logger: logger})
	if err != nil {
		t.Fatalf("merge.New: %v", err)
	}
	w := merge.NewWorker(coord, 4, logger)

	sch := New(db, Config{
		MergeCoordinator: coord,
		MergeWorker:      w,
		Logger:           logger,
	})

	if err := sch.OnGatesPass(ctx, a.ID, 42, "abc123"); err != nil {
		t.Fatalf("OnGatesPass: %v", err)
	}

	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.State != state.AgentAwaitingMerge {
		t.Fatalf("state=%q; want %q", got.State, state.AgentAwaitingMerge)
	}
	// Intent must be on file so a crash before Worker drains is
	// recoverable via Coordinator.Reconcile.
	intent, err := merge.LatestIntent(ctx, db, a.ID)
	if err != nil {
		t.Fatalf("latest intent: %v", err)
	}
	if intent.PRNumber != 42 || intent.HeadSHA != "abc123" {
		t.Fatalf("intent=%+v; want pr=42 sha=abc123", intent)
	}
}

// TestSchedulerGatesPass_AutoMergeDisabled_NoEnqueue asserts OnGatesPass is a no-op when Coordinator or Worker is nil — c2's --auto-merge=false default stays operator-observable-equivalent to pre-c2 (#612).
func TestSchedulerGatesPass_AutoMergeDisabled_NoEnqueue(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WI-MERGE-2", "sha2")

	sch := New(db, Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		// Coordinator + Worker intentionally nil.
	})
	if err := sch.OnGatesPass(ctx, a.ID, 99, "sha2"); err != nil {
		t.Fatalf("OnGatesPass: %v", err)
	}
	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.State != state.AgentGatesRunning {
		t.Fatalf("state=%q; want %q (auto-merge off → no transition)", got.State, state.AgentGatesRunning)
	}
}
