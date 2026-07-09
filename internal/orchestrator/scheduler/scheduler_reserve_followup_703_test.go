package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// alwaysDenyCostCap halts the tick at the global cost-cap gate.
func alwaysDenyCostCap(_ context.Context) bool { return false }

// TestReserveOrphans_RechecksCostCap_R1 asserts a saturated daily cap also halts the orphan reservation pass (#703 R1).
func TestReserveOrphans_RechecksCostCap_R1(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "wi-cap-orphan", "prod")

	t1 := New(db, Config{LaneCaps: map[string]int{"prod": 0}})
	if _, err := t1.Tick(ctx); err != nil {
		t.Fatalf("T1 Tick: %v", err)
	}
	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 1 || pending[0].WorkItemID != "wi-cap-orphan" {
		t.Fatalf("after T1 pending=%+v; want one pending agent", pending)
	}

	t2 := New(db, Config{CostCap: alwaysDenyCostCap})
	ids, err := t2.Tick(ctx)
	if err != nil {
		t.Fatalf("T2 Tick: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("T2 reserved=%v; want 0 — cost cap saturation must halt orphan reservation", ids)
	}
	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 0 {
		t.Fatalf("spawning=%+v; want 0 (cost cap saturated)", spawning)
	}
}

// TestReserveOrphans_RechecksCostGate_R1 asserts a wi denied by the cost governor between materialise and reservation is dropped (#703 R1).
func TestReserveOrphans_RechecksCostGate_R1(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "wi-cost-orphan", "prod")

	t1 := New(db, Config{LaneCaps: map[string]int{"prod": 0}})
	if _, err := t1.Tick(ctx); err != nil {
		t.Fatalf("T1 Tick: %v", err)
	}

	fg := &fakeCostGate{verdicts: map[string]gate.Verdict{
		"wi-cost-orphan": {Allow: false, Reason: "cap_exceeded:dag:DAG-A"},
	}}
	t2 := New(db, Config{
		CostGate:         fg,
		CostGateResolver: costResolverFor(map[string]gate.WorkItemScope{"wi-cost-orphan": {WorkItemID: "wi-cost-orphan"}}),
	})
	ids, err := t2.Tick(ctx)
	if err != nil {
		t.Fatalf("T2 Tick: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("T2 reserved=%v; want 0 — cost gate must re-check on orphan reservation", ids)
	}
	if len(fg.calls) == 0 {
		t.Fatalf("cost gate not consulted at orphan reservation — re-check missing")
	}
	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 0 {
		t.Fatalf("spawning=%+v; want 0", spawning)
	}
}

// noWorkItemGetterDB wraps state.DB to NOT expose GetWorkItem (interface-stripped).
type noWorkItemGetterDB struct {
	listSpawnable      func(ctx context.Context) ([]state.WorkItem, error)
	upsertPending      func(ctx context.Context, workItemID, lane string) (*state.Agent, error)
	upsertPendingTx    func(ctx context.Context, tx *sql.Tx, workItemID, lane string) (*state.Agent, error)
	listAgentsByState  func(ctx context.Context, states ...state.AgentState) ([]state.Agent, error)
	countAgentsByLane  func(ctx context.Context, states ...state.AgentState) (map[string]int, error)
	transitionAgent    func(ctx context.Context, id int64, next state.AgentState, mut state.AgentMutation) (*state.Agent, error)
	transitionAgentTx  func(ctx context.Context, tx *sql.Tx, id int64, next state.AgentState, mut state.AgentMutation) (*state.Agent, error)
	tryAcquireLocks    func(ctx context.Context, names []string, agentID int64, ttl time.Duration) error
	tryAcquireLocksTx  func(ctx context.Context, tx *sql.Tx, names []string, agentID int64, ttl time.Duration) error
	releaseAgentLocks  func(ctx context.Context, agentID int64) (int64, error)
	expireStaleLocks   func(ctx context.Context, ttl time.Duration) (int64, error)
	listPendingEdges   func(ctx context.Context) ([]state.EdgeRow, error)
	listEdgesFrom      func(ctx context.Context, fromID string) ([]state.EdgeRow, error)
	countNonDefault    func(ctx context.Context, fromID string) (state.EdgeFromAggregate, error)
	markEdgeFired      func(ctx context.Context, edgeID int64, fired, contentSHA string) error
	getLatestOutput    func(ctx context.Context, workItemID string) (state.OutputJournalEntry, error)
	withTx             func(ctx context.Context, fn func(*sql.Tx) error) error
	transitionWorkItem func(ctx context.Context, id string, from, to state.WorkItemStatus) error
	getAgentByWI       func(ctx context.Context, workItemID string) (*state.Agent, error)
	recordEvent        func(ctx context.Context, agentID int64, kind, payloadJSON string) error
}

func (d *noWorkItemGetterDB) ListSpawnable(ctx context.Context) ([]state.WorkItem, error) {
	return d.listSpawnable(ctx)
}
func (d *noWorkItemGetterDB) UpsertPending(ctx context.Context, workItemID, lane string) (*state.Agent, error) {
	return d.upsertPending(ctx, workItemID, lane)
}
func (d *noWorkItemGetterDB) UpsertPendingTx(ctx context.Context, tx *sql.Tx, workItemID, lane string) (*state.Agent, error) {
	return d.upsertPendingTx(ctx, tx, workItemID, lane)
}
func (d *noWorkItemGetterDB) ListAgentsByState(ctx context.Context, states ...state.AgentState) ([]state.Agent, error) {
	return d.listAgentsByState(ctx, states...)
}
func (d *noWorkItemGetterDB) CountAgentsByLane(ctx context.Context, states ...state.AgentState) (map[string]int, error) {
	return d.countAgentsByLane(ctx, states...)
}
func (d *noWorkItemGetterDB) TransitionAgent(ctx context.Context, id int64, next state.AgentState, mut state.AgentMutation) (*state.Agent, error) {
	return d.transitionAgent(ctx, id, next, mut)
}
func (d *noWorkItemGetterDB) TransitionAgentTx(ctx context.Context, tx *sql.Tx, id int64, next state.AgentState, mut state.AgentMutation) (*state.Agent, error) {
	return d.transitionAgentTx(ctx, tx, id, next, mut)
}
func (d *noWorkItemGetterDB) TryAcquireLocks(ctx context.Context, names []string, agentID int64, ttl time.Duration) error {
	return d.tryAcquireLocks(ctx, names, agentID, ttl)
}
func (d *noWorkItemGetterDB) TryAcquireLocksTx(ctx context.Context, tx *sql.Tx, names []string, agentID int64, ttl time.Duration) error {
	return d.tryAcquireLocksTx(ctx, tx, names, agentID, ttl)
}
func (d *noWorkItemGetterDB) ReleaseAgentLocks(ctx context.Context, agentID int64) (int64, error) {
	return d.releaseAgentLocks(ctx, agentID)
}
func (d *noWorkItemGetterDB) ExpireStaleLocks(ctx context.Context, ttl time.Duration) (int64, error) {
	return d.expireStaleLocks(ctx, ttl)
}
func (d *noWorkItemGetterDB) ListPendingEdgesFromMerged(ctx context.Context) ([]state.EdgeRow, error) {
	return d.listPendingEdges(ctx)
}
func (d *noWorkItemGetterDB) ListEdgesFrom(ctx context.Context, fromID string) ([]state.EdgeRow, error) {
	return d.listEdgesFrom(ctx, fromID)
}
func (d *noWorkItemGetterDB) CountNonDefaultEdgeStates(ctx context.Context, fromID string) (state.EdgeFromAggregate, error) {
	return d.countNonDefault(ctx, fromID)
}
func (d *noWorkItemGetterDB) MarkEdgeFired(ctx context.Context, edgeID int64, fired, contentSHA string) error {
	return d.markEdgeFired(ctx, edgeID, fired, contentSHA)
}
func (d *noWorkItemGetterDB) GetLatestOutput(ctx context.Context, workItemID string) (state.OutputJournalEntry, error) {
	return d.getLatestOutput(ctx, workItemID)
}
func (d *noWorkItemGetterDB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return d.withTx(ctx, fn)
}
func (d *noWorkItemGetterDB) TransitionWorkItem(ctx context.Context, id string, from, to state.WorkItemStatus) error {
	return d.transitionWorkItem(ctx, id, from, to)
}
func (d *noWorkItemGetterDB) GetAgentByWorkItemID(ctx context.Context, workItemID string) (*state.Agent, error) {
	return d.getAgentByWI(ctx, workItemID)
}
func (d *noWorkItemGetterDB) RecordEvent(ctx context.Context, agentID int64, kind, payloadJSON string) error {
	return d.recordEvent(ctx, agentID, kind, payloadJSON)
}

// wrapNoGetter strips GetWorkItem from a *state.DB by routing every
// schedulerDB method through closures over the real DB.
func wrapNoGetter(db *state.DB) *noWorkItemGetterDB {
	return &noWorkItemGetterDB{
		listSpawnable:      db.ListSpawnable,
		upsertPending:      db.UpsertPending,
		upsertPendingTx:    db.UpsertPendingTx,
		listAgentsByState:  db.ListAgentsByState,
		countAgentsByLane:  db.CountAgentsByLane,
		transitionAgent:    db.TransitionAgent,
		transitionAgentTx:  db.TransitionAgentTx,
		tryAcquireLocks:    db.TryAcquireLocks,
		tryAcquireLocksTx:  db.TryAcquireLocksTx,
		releaseAgentLocks:  db.ReleaseAgentLocks,
		expireStaleLocks:   db.ExpireStaleLocks,
		listPendingEdges:   db.ListPendingEdgesFromMerged,
		listEdgesFrom:      db.ListEdgesFrom,
		countNonDefault:    db.CountNonDefaultEdgeStates,
		markEdgeFired:      db.MarkEdgeFired,
		getLatestOutput:    db.GetLatestOutput,
		withTx:             db.WithTx,
		transitionWorkItem: db.TransitionWorkItem,
		getAgentByWI:       db.GetAgentByWorkItemID,
		recordEvent:        db.RecordEvent,
	}
}

// TestReserveOrphans_LogsWhenGetterMissing_R2 asserts a missing workItemGetter logs a WARN AND fails-closed by leaving the orphan pending instead of spawning it blind (#703 R2, #776).
func TestReserveOrphans_LogsWhenGetterMissing_R2(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "wi-getter", "prod")

	t1 := newWithDB(wrapNoGetter(db), Config{LaneCaps: map[string]int{"prod": 0}})
	if _, err := t1.Tick(ctx); err != nil {
		t.Fatalf("T1 Tick: %v", err)
	}

	h := obstest.New()
	gate := &fakeGate{verdicts: map[string]approval.Result{"wi-getter": approval.ResultPause}}
	cfg := Config{
		Gate:         gate,
		GateResolver: gateResolverByID(map[string]approval.Config{"wi-getter": gateCfgFor("prod-gate")}),
		Logger:       slog.New(h),
	}
	t2 := newWithDB(wrapNoGetter(db), cfg)
	if _, err := t2.Tick(ctx); err != nil {
		t.Fatalf("T2 Tick: %v", err)
	}

	var sawWarn bool
	for _, r := range h.Records() {
		if !strings.Contains(r.Message, "gate_recheck_unavailable") {
			continue
		}
		sawWarn = true
	}
	if !sawWarn {
		t.Fatalf("missing gate_recheck_unavailable warn — operator cannot diagnose silently-disabled re-check")
	}

	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 0 {
		t.Fatalf("spawning=%+v; want 0 — getter-missing must fail-closed, not spawn blind (#776)", spawning)
	}
	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 1 || pending[0].WorkItemID != "wi-getter" {
		t.Fatalf("pending=%+v; want one preserved orphan for wi-getter (#776)", pending)
	}
}

// failingRejectDB wraps state.DB and fails TransitionWorkItem for a
// specific work_item_id so the markWorkItemRejected path in
// recheckApprovalGate returns an error.
type failingRejectDB struct {
	*state.DB
	failID string
	err    error
}

func (f *failingRejectDB) TransitionWorkItem(ctx context.Context, id string, from, to state.WorkItemStatus) error {
	if id == f.failID && to == state.WorkStatusRejected {
		return f.err
	}
	return f.DB.TransitionWorkItem(ctx, id, from, to)
}

// TestReserveOrphans_PreservesReservedOnRejectError_R4 asserts that an error from markWorkItemRejected does not discard work_items reserved earlier in the same orphan pass (#703 R4).
func TestReserveOrphans_PreservesReservedOnRejectError_R4(t *testing.T) {
	ctx := context.Background()
	realDB := statetest.OpenDB(t)

	// Two orphans seeded in id-ascending order so the orphan loop hits
	// wi-A (no gate config) first and reserves it BEFORE wi-Z reaches
	// the reject CAS — only then does the partial-progress preservation
	// claim of R4 become observable. Pre-fix the caller drops wi-A's
	// agent id from the returned reserved slice when wi-Z halts the tick.
	seedPlanned(t, realDB, "wi-A-ok", "prod")
	seedPlanned(t, realDB, "wi-Z-fail", "prod")

	t1 := New(realDB, Config{LaneCaps: map[string]int{"prod": 0}})
	if _, err := t1.Tick(ctx); err != nil {
		t.Fatalf("T1 Tick: %v", err)
	}
	pending, _ := realDB.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 2 {
		t.Fatalf("after T1 pending=%d; want 2", len(pending))
	}

	wantErr := errors.New("transition wi failed")
	fdb := &failingRejectDB{DB: realDB, failID: "wi-Z-fail", err: wantErr}

	gate := &fakeGate{verdicts: map[string]approval.Result{
		"wi-Z-fail": approval.ResultReject,
		// wi-A-ok absent from verdicts → gate paused, so we instead omit
		// from resolver and rely on no-gate-config to let it through.
	}}
	cfg := Config{
		Gate: gate,
		// Only wi-Z-fail is gated; wi-A-ok proceeds without consulting the
		// gate so the resolver returns gated=false for it.
		GateResolver: gateResolverByID(map[string]approval.Config{
			"wi-Z-fail": gateCfgFor("prod-gate"),
		}),
		LockTTL: time.Minute,
	}
	sch := newWithDB(fdb, cfg)

	ids, err := sch.Tick(ctx)
	if err == nil {
		t.Fatalf("Tick err=nil; want non-nil — reject CAS failure must halt the tick")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Tick err=%v; want chain containing %v", err, wantErr)
	}

	// wi-A-ok was reserved successfully BEFORE wi-Z-fail's reject failed.
	// Pre-fix this id is dropped because scheduler.go discards the
	// returned slice when err != nil. Verify via state: spawning rows
	// reflect committed reservations independent of the returned slice
	// — if the tx committed but the id was dropped from `ids`, the
	// orchestrator loses its handle on a live agent.
	spawning, _ := realDB.ListAgentsByState(ctx, state.AgentSpawning)
	var okAgentID int64
	for _, a := range spawning {
		if a.WorkItemID == "wi-A-ok" {
			okAgentID = a.ID
		}
	}
	if okAgentID == 0 {
		t.Fatalf("wi-A-ok was not transitioned to spawning; reject-CAS halt cancelled prior work: spawning=%+v", spawning)
	}
	var foundInIDs bool
	for _, id := range ids {
		if id == okAgentID {
			foundInIDs = true
		}
	}
	if !foundInIDs {
		t.Fatalf("Tick returned ids=%v but wi-A-ok agent %d is spawning — caller lost the handle (#703 R4)", ids, okAgentID)
	}
}
