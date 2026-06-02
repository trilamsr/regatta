// L4 adversarial-gate integration with Scheduler.Tick — spec
// 2026-06-02-s2-t2-adversarial-l4-gate §3.2 step 0.7. Verifies the
// pass between cost-governor (step 0.6) and the reservation loop:
// Blocking GateResults stay planned, advisory and pass verdicts let
// the wi reach reservation, and the hook is a no-op when L4 is unset.
package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/gates/l4"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// fakeL4Gate returns canned GateResults keyed by work_item id so tests
// can pin per-wi verdicts. Captures calls so step-ordering tests can
// assert L4 fan-out happened exactly when expected.
type fakeL4Gate struct {
	results map[string]schemas.GateResult
	errs    map[string]error
	calls   []string
}

func (f *fakeL4Gate) Evaluate(_ context.Context, _ l4.Config, in l4.Input) (schemas.GateResult, error) {
	f.calls = append(f.calls, in.RunID)
	if err := f.errs[in.RunID]; err != nil {
		return schemas.GateResult{Verdict: schemas.VerdictFail, Blocking: true}, err
	}
	if gr, ok := f.results[in.RunID]; ok {
		return gr, nil
	}
	return schemas.GateResult{Verdict: schemas.VerdictPass}, nil
}

// l4ResolverByID maps wi.ID -> (Config, Input) so each test pins which
// work_items are L4-gated and threads the wi id through Input.RunID.
func l4ResolverByID(ids map[string]struct{}) L4GateResolver {
	return func(wi state.WorkItem) (l4.Config, l4.Input, bool) {
		if _, ok := ids[wi.ID]; !ok {
			return l4.Config{}, l4.Input{}, false
		}
		return l4.Config{GateID: "l4_adversarial"}, l4.Input{RunID: wi.ID}, true
	}
}

func TestSchedulerTick_Step07_BlocksOnCriticalFinding(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-L4-CRIT", "prod")

	fg := &fakeL4Gate{results: map[string]schemas.GateResult{
		"WI-L4-CRIT": {Verdict: schemas.VerdictFail, Blocking: true, Findings: []schemas.Finding{{
			ID: "L4-CORR-RACE", Severity: schemas.FindingCritical, Claim: "race in reservation tx",
		}}},
	}}
	sch := New(db, Config{
		L4Gate:         fg,
		L4GateResolver: l4ResolverByID(map[string]struct{}{"WI-L4-CRIT": {}}),
	})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("reserved=%v; want 0 (L4 blocked)", ids)
	}
	wi, err := db.GetWorkItem(ctx, "WI-L4-CRIT")
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

func TestSchedulerTick_Step07_CleanReviewProceeds(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-L4-OK", "prod")

	fg := &fakeL4Gate{results: map[string]schemas.GateResult{
		"WI-L4-OK": {Verdict: schemas.VerdictPass},
	}}
	sch := New(db, Config{
		L4Gate:         fg,
		L4GateResolver: l4ResolverByID(map[string]struct{}{"WI-L4-OK": {}}),
	})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(fg.calls) != 1 || fg.calls[0] != "WI-L4-OK" {
		t.Fatalf("L4 calls=%v; want one call for WI-L4-OK", fg.calls)
	}
	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 1 {
		t.Fatalf("spawning=%d; want 1 (L4 passed)", len(spawning))
	}
}

func TestSchedulerTick_Step07_AdvisoryDoesNotBlock(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-L4-ADV", "prod")

	// Advisory verdict (Blocking=false) — wave-1 rollout posture per
	// spec §4. Must reach the reservation loop.
	fg := &fakeL4Gate{results: map[string]schemas.GateResult{
		"WI-L4-ADV": {Verdict: schemas.VerdictAdvisory, Blocking: false},
	}}
	sch := New(db, Config{
		L4Gate:         fg,
		L4GateResolver: l4ResolverByID(map[string]struct{}{"WI-L4-ADV": {}}),
	})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 1 {
		t.Fatalf("spawning=%d; want 1 (advisory does not block)", len(spawning))
	}
}

func TestSchedulerTick_Step07_HookIsNoopWhenL4Unset(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-NO-L4", "prod")

	// L4Gate=nil and L4GateResolver=nil -> identity short-circuit.
	sch := New(db, Config{})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("reserved=%v; want 1 (L4 hook must be no-op when unset)", ids)
	}
}

func TestSchedulerTick_Step07_EvaluateErrorFailsClosed(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-L4-ERR", "prod")

	fg := &fakeL4Gate{errs: map[string]error{"WI-L4-ERR": errors.New("model adapter exploded")}}
	sch := New(db, Config{
		L4Gate:         fg,
		L4GateResolver: l4ResolverByID(map[string]struct{}{"WI-L4-ERR": {}}),
	})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("reserved=%v; want 0 (fail-closed on Evaluate error)", ids)
	}
	wi, _ := db.GetWorkItem(ctx, "WI-L4-ERR")
	if wi.Status != state.WorkStatusPlanned {
		t.Fatalf("status=%q; want planned (next tick retries)", wi.Status)
	}
}

func TestSchedulerTick_Step07_RunsAfterCostGovernor(t *testing.T) {
	// Step ordering: cost-deny short-circuits BEFORE L4 fires. The L4
	// gate is the expensive model call; spec §3.2 step 0.6 → 0.7 puts
	// cheap deterministic deny first so we never pay model tokens for
	// a wi the cheap gate already rejected.
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-COST-DENY", "prod")

	costFG := &fakeCostGate{verdicts: map[string]gate.Verdict{
		"WI-COST-DENY": {Allow: false, Reason: "over-cap"},
	}}
	l4FG := &fakeL4Gate{}
	sch := New(db, Config{
		CostGate:         costFG,
		CostGateResolver: costResolverFor(map[string]gate.WorkItemScope{"WI-COST-DENY": {WorkItemID: "WI-COST-DENY"}}),
		L4Gate:           l4FG,
		L4GateResolver:   l4ResolverByID(map[string]struct{}{"WI-COST-DENY": {}}),
	})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(l4FG.calls) != 0 {
		t.Fatalf("L4 calls=%v; want 0 (cost gate must short-circuit before L4)", l4FG.calls)
	}
}
