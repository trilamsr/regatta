// L4 adversarial-gate integration with Scheduler.Tick — spec
// 2026-06-02-s2-t2-adversarial-l4-gate §3.2 step 0.7. Verifies the
// pass between cost-governor (step 0.6) and the reservation loop:
// Blocking GateResults stay planned, advisory and pass verdicts let
// the wi reach reservation, and the hook is a no-op when L4 is unset.
package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/gates/l4"
	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
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
// PRSHA derives from wi.ID so the gate_rejected payload assertion can
// pin a deterministic value without bloating each test fixture.
func l4ResolverByID(ids map[string]struct{}) L4GateResolver {
	return func(wi state.WorkItem) (l4.Config, l4.Input, bool) {
		if _, ok := ids[wi.ID]; !ok {
			return l4.Config{}, l4.Input{}, false
		}
		return l4.Config{GateID: "l4_adversarial"}, l4.Input{
			RunID: wi.ID,
			PRSHA: "sha-" + wi.ID,
		}, true
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

// TestSchedulerTick_Step07_BlockingEmitsGateRejectedEvent pins the contract issue #479 wires: an L4 deny writes a `gate_rejected` events row whose payload carries pr_sha + reason so the RejectionRouter can drain it.
func TestSchedulerTick_Step07_BlockingEmitsGateRejectedEvent(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-L4-EMIT", "prod")

	fg := &fakeL4Gate{results: map[string]schemas.GateResult{
		"WI-L4-EMIT": {Verdict: schemas.VerdictFail, Blocking: true, Findings: []schemas.Finding{{
			ID: "L4-RACE-1", Severity: schemas.FindingCritical, Claim: "race",
		}}},
	}}
	sch := New(db, Config{
		L4Gate:         fg,
		L4GateResolver: l4ResolverByID(map[string]struct{}{"WI-L4-EMIT": {}}),
	})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	events, err := db.ListEventsByKindSince(ctx, rejectionrouter.EventKindGateRejected, 0, 10)
	if err != nil {
		t.Fatalf("ListEventsByKindSince: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events=%d; want 1 gate_rejected row", len(events))
	}
	var got struct {
		PRSHA  string `json:"pr_sha"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(events[0].PayloadJSON), &got); err != nil {
		t.Fatalf("payload unmarshal: %v (raw=%s)", err, events[0].PayloadJSON)
	}
	if got.PRSHA != "sha-WI-L4-EMIT" {
		t.Fatalf("payload.pr_sha=%q; want sha-WI-L4-EMIT", got.PRSHA)
	}
	if got.Reason != "L4-RACE-1" {
		t.Fatalf("payload.reason=%q; want L4-RACE-1 (first finding ID)", got.Reason)
	}
	// agent_id is NULL before a wi reaches reservation — audit-only row.
	if events[0].AgentID.Valid {
		t.Fatalf("agent_id=%d valid; want NULL pre-reservation", events[0].AgentID.Int64)
	}
}

// TestSchedulerTick_Step07_AdvisoryDoesNotEmit pins the dual to the blocking case: a non-blocking verdict must not write a gate_rejected row, else the router would wake an agent for a finding that did not actually block.
func TestSchedulerTick_Step07_AdvisoryDoesNotEmit(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-L4-ADV-EMIT", "prod")

	fg := &fakeL4Gate{results: map[string]schemas.GateResult{
		"WI-L4-ADV-EMIT": {Verdict: schemas.VerdictAdvisory, Blocking: false},
	}}
	sch := New(db, Config{
		L4Gate:         fg,
		L4GateResolver: l4ResolverByID(map[string]struct{}{"WI-L4-ADV-EMIT": {}}),
	})
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	events, _ := db.ListEventsByKindSince(ctx, rejectionrouter.EventKindGateRejected, 0, 10)
	if len(events) != 0 {
		t.Fatalf("events=%d; want 0 (advisory verdict is not a rejection)", len(events))
	}
}

// TestSchedulerTick_Step07_EmitDrainsThroughRejectionRouter is the producer→consumer smoke that pins the contract issue #479 closes: a scheduler-side L4 deny becomes a RejectionRouter input row, which then drives the agent's rejection counter. Both sides reference the same event kind + payload shape; this test fails if either drifts.
func TestSchedulerTick_Step07_EmitDrainsThroughRejectionRouter(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-L4-E2E", "prod")
	// Pre-create an agent in gates_running with a matching sha so the
	// router can apply the rejection (router-side guards: state must be
	// gates_running, payload.pr_sha must equal agent.pr_sha). The L4
	// deny path here re-uses the spec's emit hook even though in
	// production the gate fires pre-reservation; the smoke pins the
	// JSON shape only — full lifecycle stays in rejectionrouter_test.
	a, err := db.UpsertPending(ctx, "WI-L4-E2E", "prod")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	prSHA := "sha-WI-L4-E2E"
	mustWalkAgentToGatesRunning(t, db, a.ID, prSHA)

	// Scheduler emits directly via the helper (Tick path excludes wis
	// with existing agent rows; the helper is the load-bearing seam
	// regardless of which gate runner triggers the emit).
	sch := newWithDB(db, Config{})
	sch.emitGateRejected(ctx, "WI-L4-E2E", prSHA, "L4-DRAIN-OK")

	r := rejectionrouter.New(rejectionrouter.Config{DB: db})
	if err := r.Tick(ctx); err != nil {
		t.Fatalf("rejectionrouter.Tick: %v", err)
	}
	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.RejectionCount != 1 {
		t.Fatalf("rejection_count=%d; want 1 (router consumed the emit)", got.RejectionCount)
	}
	if got.State != state.AgentGatesFailed {
		t.Fatalf("state=%q; want %q", got.State, state.AgentGatesFailed)
	}
}

// mustWalkAgentToGatesRunning drives the agent through the legal
// transitions to gates_running with pr_sha set, so the rejectionrouter
// smoke can apply its pr_sha-guarded counter bump.
func mustWalkAgentToGatesRunning(t *testing.T, db *state.DB, id int64, prSHA string) {
	t.Helper()
	ctx := context.Background()
	pid := 1
	sess := "sess"
	if _, err := db.TransitionAgent(ctx, id, state.AgentSpawning, state.AgentMutation{PID: &pid, SessionID: &sess}); err != nil {
		t.Fatalf("spawning: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, id, state.AgentRunning, state.AgentMutation{}); err != nil {
		t.Fatalf("running: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, id, state.AgentPROpen, state.AgentMutation{PRSHA: &prSHA}); err != nil {
		t.Fatalf("pr_open: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, id, state.AgentGatesRunning, state.AgentMutation{}); err != nil {
		t.Fatalf("gates_running: %v", err)
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
