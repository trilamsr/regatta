// Cost-gate integration with Scheduler.Tick — spec §3.2 step 0.6.
// Verifies the cost-gate-pass between approval gate and reservation:
// gated work_items denied for over-cap stay planned, soft-cap downgrade
// surfaces ModelOverride, and the hook is a no-op when cost is unset.
package scheduler

import (
	"context"
	"testing"

	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// fakeCostGate records the wis it evaluated and returns a per-wi verdict.
type fakeCostGate struct {
	verdicts map[string]gate.Verdict
	calls    []string
}

func (f *fakeCostGate) Evaluate(_ context.Context, scope gate.WorkItemScope) (gate.Verdict, error) {
	f.calls = append(f.calls, scope.WorkItemID)
	v, ok := f.verdicts[scope.WorkItemID]
	if !ok {
		return gate.Verdict{Allow: true}, nil
	}
	return v, nil
}

func costResolverFor(m map[string]gate.WorkItemScope) CostGateResolver {
	return func(wi state.WorkItem) (gate.WorkItemScope, bool) {
		s, ok := m[wi.ID]
		return s, ok
	}
}

func TestSchedulerTick_Step06_RunsBeforeReserve(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-COST-1", "prod")

	fg := &fakeCostGate{verdicts: map[string]gate.Verdict{"WI-COST-1": {Allow: true}}}
	sch := New(db, Config{
		CostGate:         fg,
		CostGateResolver: costResolverFor(map[string]gate.WorkItemScope{"WI-COST-1": {WorkItemID: "WI-COST-1"}}),
	})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(fg.calls) != 1 || fg.calls[0] != "WI-COST-1" {
		t.Fatalf("cost gate calls=%v; want one call for WI-COST-1", fg.calls)
	}
	// Allow=true must let the wi reach the reservation loop.
	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 1 {
		t.Fatalf("spawning=%d; want 1 (cost gate allowed)", len(spawning))
	}
}

func TestSchedulerTick_DeniedWorkItemStaysPlanned(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-DENY", "prod")

	fg := &fakeCostGate{verdicts: map[string]gate.Verdict{
		"WI-DENY": {Allow: false, Reason: "cap_exceeded:dag:DAG-A"},
	}}
	sch := New(db, Config{
		CostGate:         fg,
		CostGateResolver: costResolverFor(map[string]gate.WorkItemScope{"WI-DENY": {WorkItemID: "WI-DENY"}}),
	})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("reserved=%v; want 0 (cost denied)", ids)
	}
	wi, err := db.GetWorkItem(ctx, "WI-DENY")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if wi.Status != state.WorkStatusPlanned {
		t.Fatalf("status=%q; want planned (next tick re-evaluates)", wi.Status)
	}
	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 0 {
		t.Fatalf("spawning=%d; want 0", len(spawning))
	}
}

func TestSchedulerTick_HookIsNoopWhenCostUnset(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-NO-COST", "prod")

	// CostGate=nil and CostGateResolver=nil -> identity short-circuit.
	sch := New(db, Config{})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("reserved=%v; want 1 (cost hook must be no-op when unset)", ids)
	}
	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 1 {
		t.Fatalf("spawning=%d; want 1", len(spawning))
	}
}

func TestSchedulerTick_SoftCapDowngrade_PassesModelOverride(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-DOWN", "prod")

	fg := &fakeCostGate{verdicts: map[string]gate.Verdict{
		"WI-DOWN": {Allow: true, SoftCapBreached: true, DowngradeTo: "claude-haiku-4-5"},
	}}
	var captured string
	sch := New(db, Config{
		CostGate: fg,
		CostGateResolver: costResolverFor(map[string]gate.WorkItemScope{
			"WI-DOWN": {WorkItemID: "WI-DOWN", AllowDowngrade: true},
		}),
		OnDowngrade: func(workItemID, model string) { captured = model },
	})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if captured != "claude-haiku-4-5" {
		t.Fatalf("OnDowngrade captured=%q; want claude-haiku-4-5", captured)
	}
}
