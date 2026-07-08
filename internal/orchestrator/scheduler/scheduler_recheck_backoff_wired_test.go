package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// countingFailGetterDB embeds *state.DB and counts GetWorkItem calls,
// returning a forced error so fetchWorkItemForRecheck's getter.GetWorkItem
// path always fails. GetWorkItemsBatch also fails so snapshotWorkItems
// leaves tc.workItems nil and the per-orphan fallback exercises the
// backoff (#1359 preserves the pre-batch failure mode).
type countingFailGetterDB struct {
	*state.DB
	calls int
	err   error
}

func (d *countingFailGetterDB) GetWorkItem(ctx context.Context, id string) (state.WorkItem, error) {
	d.calls++
	return state.WorkItem{}, d.err
}

func (d *countingFailGetterDB) GetWorkItemsBatch(ctx context.Context, ids []string) (map[string]state.WorkItem, error) {
	return nil, d.err
}

// TestScheduler_OrphanRecheck_BackoffKicksIn asserts the wired recheckBackoff suppresses GetWorkItem after K=3 consecutive fetch failures for the same orphan id (#793).
func TestScheduler_OrphanRecheck_BackoffKicksIn(t *testing.T) {
	ctx := context.Background()
	realDB := statetest.OpenDB(t)
	seedPlanned(t, realDB, "wi-backoff", "prod")

	// T1: park the wi as a pending orphan (lane saturated).
	t1 := New(realDB, Config{LaneCaps: map[string]int{"prod": 0}})
	if _, err := t1.Tick(ctx); err != nil {
		t.Fatalf("T1 Tick: %v", err)
	}
	pending, _ := realDB.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 1 || pending[0].WorkItemID != "wi-backoff" {
		t.Fatalf("after T1 pending=%+v; want one pending agent", pending)
	}

	// Drive subsequent ticks against the failing-getter wrapper. A
	// (Gate, GateResolver) pair wires the gate-recheck branch so
	// fetchWorkItemForRecheck consults GetWorkItem each orphan pass.
	fdb := &countingFailGetterDB{DB: realDB, err: errors.New("forced getworkitem failure")}
	gate := &fakeGate{verdicts: map[string]approval.Result{"wi-backoff": approval.ResultProceed}}
	cfg := Config{
		Gate:         gate,
		GateResolver: gateResolverByID(map[string]approval.Config{"wi-backoff": gateCfgFor("prod-gate")}),
	}
	sch := newWithDB(fdb, cfg)

	// K=3 ticks: GetWorkItem called each tick, all fail; orphan stays
	// pending each time (fail-closed). Tick 4+ admit must be suppressed
	// so GetWorkItem call count stops climbing.
	for i := 0; i < recheckBackoffDefaultK; i++ {
		if _, err := sch.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	if fdb.calls != recheckBackoffDefaultK {
		t.Fatalf("after K=%d failure ticks GetWorkItem calls=%d; want %d", recheckBackoffDefaultK, fdb.calls, recheckBackoffDefaultK)
	}

	// Next tick: backoff window engaged, Admit=false, GetWorkItem MUST
	// NOT be invoked.
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("post-K tick: %v", err)
	}
	if fdb.calls != recheckBackoffDefaultK {
		t.Fatalf("post-backoff tick GetWorkItem calls=%d; want %d (backoff must suppress fetch)", fdb.calls, recheckBackoffDefaultK)
	}
}
