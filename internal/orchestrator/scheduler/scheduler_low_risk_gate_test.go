// scheduler_low_risk_gate_test pins the MAY-86 low-risk filter on the
// gates_pass → auto-merge seam: when a LowRiskGate is wired and HOLDS a
// PR, OnGatesPass must NOT PrepareMerge/Enqueue (the agent stays in
// GatesRunning, no intent on file). When no gate is wired, OnGatesPass
// stays byte-for-byte the pre-MAY-86 behavior.
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

// holdAllGate models the conservative default — auto-merge wired on but
// the low-risk gate disabled, so EVERYTHING is held for operator glance.
type holdAllGate struct{}

func (holdAllGate) Eligible(_ context.Context, _ int, _ string) (bool, string) {
	return false, "low_risk_disabled"
}

// passAllGate models a positive eligibility decision so the wired-gate
// path still reaches PrepareMerge+Enqueue.
type passAllGate struct{}

func (passAllGate) Eligible(_ context.Context, _ int, _ string) (bool, string) {
	return true, "eligible"
}

// TestSchedulerLowRiskGate_ConservativeDefaultHoldsEverything asserts a hold-all gate keeps the agent in GatesRunning with no intent even when auto-merge is wired (MAY-86).
func TestSchedulerLowRiskGate_ConservativeDefaultHoldsEverything(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WI-LRG-1", "shaHold")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	coord, err := merge.New(merge.Config{DB: db, Prober: &fakeMergeProber{}, Logger: logger})
	if err != nil {
		t.Fatalf("merge.New: %v", err)
	}
	w := merge.NewWorker(coord, 4, logger)

	sch := New(db, Config{
		MergeCoordinator: coord,
		MergeWorker:      w,
		LowRiskGate:      holdAllGate{},
		Logger:           logger,
	})

	if err := sch.OnGatesPass(ctx, a.ID, 7, "shaHold"); err != nil {
		t.Fatalf("OnGatesPass: %v", err)
	}

	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.State != state.AgentGatesRunning {
		t.Fatalf("state=%q; want %q (held, not auto-merged)", got.State, state.AgentGatesRunning)
	}
	if _, err := merge.LatestIntent(ctx, db, a.ID); err == nil {
		t.Fatalf("merge intent written for a held PR; want none")
	}
}

// TestSchedulerLowRiskGate_EligiblePRProceeds asserts an eligible-gate PR transitions to AwaitingMerge via PrepareMerge+Enqueue (MAY-86).
func TestSchedulerLowRiskGate_EligiblePRProceeds(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WI-LRG-2", "shaPass")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	coord, err := merge.New(merge.Config{DB: db, Prober: &fakeMergeProber{}, Logger: logger})
	if err != nil {
		t.Fatalf("merge.New: %v", err)
	}
	w := merge.NewWorker(coord, 4, logger)

	sch := New(db, Config{
		MergeCoordinator: coord,
		MergeWorker:      w,
		LowRiskGate:      passAllGate{},
		Logger:           logger,
	})

	if err := sch.OnGatesPass(ctx, a.ID, 8, "shaPass"); err != nil {
		t.Fatalf("OnGatesPass: %v", err)
	}

	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.State != state.AgentAwaitingMerge {
		t.Fatalf("state=%q; want %q (eligible → proceeds)", got.State, state.AgentAwaitingMerge)
	}
}

// TestSchedulerLowRiskGate_NilGateByteEquivalent asserts a nil gate keeps the pre-MAY-86 path: PrepareMerge+Enqueue fire unconditionally (MAY-86).
func TestSchedulerLowRiskGate_NilGateByteEquivalent(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WI-LRG-3", "shaNil")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	coord, err := merge.New(merge.Config{DB: db, Prober: &fakeMergeProber{}, Logger: logger})
	if err != nil {
		t.Fatalf("merge.New: %v", err)
	}
	w := merge.NewWorker(coord, 4, logger)

	sch := New(db, Config{
		MergeCoordinator: coord,
		MergeWorker:      w,
		// LowRiskGate intentionally nil.
		Logger: logger,
	})

	if err := sch.OnGatesPass(ctx, a.ID, 9, "shaNil"); err != nil {
		t.Fatalf("OnGatesPass: %v", err)
	}

	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.State != state.AgentAwaitingMerge {
		t.Fatalf("state=%q; want %q (nil gate = pre-MAY-86 path)", got.State, state.AgentAwaitingMerge)
	}
}
