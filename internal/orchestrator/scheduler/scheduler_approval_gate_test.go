// Approval-gate integration with Scheduler.Tick — spec §3.1 step 0.5.
// Verifies the gate-pass between edge-eval and reservation: gated
// work_items are paused, rejected, or allowed to proceed by the same
// approval.Result enum the standalone Gate.Evaluate test in
// internal/gates/approval/gate_test.go already pins. The seam is the
// Config.Gate interface + Config.GateResolver func — concrete wiring
// lives in cmd/regatta/serve.go.
package scheduler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// fakeGate is the in-test ApprovalGate. Returns a fixed verdict per
// (work_item_id, gate_name) and records the calls so tests can assert
// the scheduler fanned out exactly once per gated wi per tick.
type fakeGate struct {
	verdicts map[string]approval.Result
	errs     map[string]error
	calls    []fakeGateCall
}

type fakeGateCall struct {
	wiID string
	gate string
}

func (f *fakeGate) Evaluate(_ context.Context, wi state.WorkItem, cfg approval.Config) (approval.Result, error) {
	f.calls = append(f.calls, fakeGateCall{wiID: wi.ID, gate: cfg.Name})
	if err := f.errs[wi.ID]; err != nil {
		return approval.ResultPause, err
	}
	v, ok := f.verdicts[wi.ID]
	if !ok {
		return approval.ResultPause, nil
	}
	return v, nil
}

func gateCfgFor(name string) approval.Config {
	return approval.Config{
		Name:           name,
		RiskClass:      approval.RiskLow,
		Reviewers:      []string{"alice", "bob"},
		Quorum:         1,
		Timeout:        time.Hour,
		DecisionWindow: 30 * time.Minute,
		OnTimeout:      approval.OnTimeoutFail,
	}
}

// gateResolverByID maps wi.ID -> gate config so each test pins exactly
// which work_items are gated.
func gateResolverByID(m map[string]approval.Config) GateResolver {
	return func(wi state.WorkItem) (approval.Config, bool) {
		c, ok := m[wi.ID]
		return c, ok
	}
}

func TestTick_GateFirstEvaluationPauses(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "GATED-1", "prod")

	gate := &fakeGate{verdicts: map[string]approval.Result{"GATED-1": approval.ResultPause}}
	sch := New(db, Config{
		Gate:         gate,
		GateResolver: gateResolverByID(map[string]approval.Config{"GATED-1": gateCfgFor("prod-gate")}),
	})

	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("reserved=%v; want 0 (gate paused the wi)", ids)
	}

	wi, err := db.GetWorkItem(ctx, "GATED-1")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if wi.Status != state.WorkStatusPlanned {
		t.Fatalf("status=%q; want planned (pause leaves wi unchanged)", wi.Status)
	}

	if len(gate.calls) != 1 || gate.calls[0].wiID != "GATED-1" || gate.calls[0].gate != "prod-gate" {
		t.Fatalf("gate.calls=%+v; want one call (GATED-1, prod-gate)", gate.calls)
	}

	spawning, err := db.ListAgentsByState(ctx, state.AgentSpawning)
	if err != nil {
		t.Fatalf("ListAgentsByState: %v", err)
	}
	if len(spawning) != 0 {
		t.Fatalf("spawning=%d; want 0 (gate paused the wi)", len(spawning))
	}
}

func TestTick_GateApprovedSpawnsNextTick(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "GATED-2", "prod")

	gate := &fakeGate{verdicts: map[string]approval.Result{"GATED-2": approval.ResultProceed}}
	sch := New(db, Config{
		Gate:         gate,
		GateResolver: gateResolverByID(map[string]approval.Config{"GATED-2": gateCfgFor("prod-gate")}),
	})

	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("reserved=%v; want 1 (gate proceeded)", ids)
	}

	wi, err := db.GetWorkItem(ctx, "GATED-2")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if wi.Status != state.WorkStatusPlanned {
		t.Fatalf("status=%q; want planned (proceed leaves wi for the reservation loop)", wi.Status)
	}

	spawning, err := db.ListAgentsByState(ctx, state.AgentSpawning)
	if err != nil {
		t.Fatalf("ListAgentsByState: %v", err)
	}
	if len(spawning) != 1 || spawning[0].WorkItemID != "GATED-2" {
		t.Fatalf("spawning=%+v; want one agent for GATED-2", spawning)
	}
}

func TestTick_GateRejectedTransitionsToRejected(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "GATED-3", "prod")

	gate := &fakeGate{verdicts: map[string]approval.Result{"GATED-3": approval.ResultReject}}
	sch := New(db, Config{
		Gate:         gate,
		GateResolver: gateResolverByID(map[string]approval.Config{"GATED-3": gateCfgFor("prod-gate")}),
	})

	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("reserved=%v; want 0 (gate rejected the wi)", ids)
	}

	wi, err := db.GetWorkItem(ctx, "GATED-3")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if string(wi.Status) != "rejected" {
		t.Fatalf("status=%q; want rejected", wi.Status)
	}

	spawning, err := db.ListAgentsByState(ctx, state.AgentSpawning)
	if err != nil {
		t.Fatalf("ListAgentsByState: %v", err)
	}
	if len(spawning) != 0 {
		t.Fatalf("spawning=%+v; want 0 (rejected wi must never reserve an agent)", spawning)
	}

	// Second tick must be idempotent: status stays rejected, gate is NOT
	// re-evaluated because ListSpawnable filters status='planned'.
	gate.calls = nil
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if len(gate.calls) != 0 {
		t.Fatalf("gate.calls=%+v after rejected; want 0 (status=rejected hides wi from ListSpawnable)", gate.calls)
	}
}

func TestTick_GatePendingSkipsWI(t *testing.T) {
	// Pending here = scheduler already has an approval row + the gate
	// re-evaluates and returns ResultPause. Distinct from the
	// FirstEvaluation case in that the gate row pre-exists; this covers
	// the spec §3.1 "subsequent tick" branch where fold(events)=pending.
	db := statetest.OpenDB(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	seedPlanned(t, db, "GATED-4", "prod")

	cfg := gateCfgFor("prod-gate")
	approvalID := "a-pending01"
	if err := db.CreateApproval(ctx, state.Approval{
		ID:                  approvalID,
		WorkItemID:          "GATED-4",
		GateName:            cfg.Name,
		RequestedAt:         now,
		ReviewerSetSnapshot: state.ReviewerSet{Reviewers: cfg.Reviewers, Quorum: cfg.Quorum},
		Quorum:              cfg.Quorum,
		Status:              state.ApprovalStatusPending,
		TimeoutAt:           now.Add(cfg.Timeout),
		OnTimeout:           cfg.OnTimeout,
	}); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	if err := db.AppendApprovalEvent(ctx, state.ApprovalEvent{
		ApprovalID: approvalID, Ts: now, Kind: approval.EventKindRequested, Actor: approval.ActorOrchestrator,
		Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("AppendApprovalEvent: %v", err)
	}

	gate := &fakeGate{verdicts: map[string]approval.Result{"GATED-4": approval.ResultPause}}
	sch := New(db, Config{
		Gate:         gate,
		GateResolver: gateResolverByID(map[string]approval.Config{"GATED-4": cfg}),
	})

	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("reserved=%v; want 0", ids)
	}

	wi, err := db.GetWorkItem(ctx, "GATED-4")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if wi.Status != state.WorkStatusPlanned {
		t.Fatalf("status=%q; want planned", wi.Status)
	}

	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(spawning) != 0 {
		t.Fatalf("spawning=%d; want 0", len(spawning))
	}
	if len(pending) != 0 {
		t.Fatalf("pending=%d; want 0 (paused wi must not materialize an agent row)", len(pending))
	}
}

func TestTick_NonGatedWorkItemUnaffected(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "PLAIN-1", "server")

	gate := &fakeGate{}
	sch := New(db, Config{
		Gate:         gate,
		GateResolver: gateResolverByID(map[string]approval.Config{}), // empty -> no wi gated
	})

	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("reserved=%v; want 1 (non-gated wi spawns)", ids)
	}
	if len(gate.calls) != 0 {
		t.Fatalf("gate.calls=%+v; want 0 (resolver returned !ok)", gate.calls)
	}

	spawning, err := db.ListAgentsByState(ctx, state.AgentSpawning)
	if err != nil {
		t.Fatalf("ListAgentsByState: %v", err)
	}
	if len(spawning) != 1 || spawning[0].WorkItemID != "PLAIN-1" {
		t.Fatalf("spawning=%+v; want one agent for PLAIN-1", spawning)
	}
}
