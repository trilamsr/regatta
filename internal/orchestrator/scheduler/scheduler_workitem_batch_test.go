package scheduler

import (
	"context"
	"testing"

	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// countingGetterDB embeds *state.DB and counts GetWorkItem invocations
// so the per-tick N+1 assertion is observable regardless of orphan count.
type countingGetterDB struct {
	*state.DB
	getCalls int
}

func (d *countingGetterDB) GetWorkItem(ctx context.Context, id string) (state.WorkItem, error) {
	d.getCalls++
	return d.DB.GetWorkItem(ctx, id)
}

// TestScheduler_OrphanRecheck_BatchesGetWorkItem_1359 asserts tick-scoped snapshot serves N orphan re-checks with 0 per-id GetWorkItem calls (#1359).
func TestScheduler_OrphanRecheck_BatchesGetWorkItem_1359(t *testing.T) {
	ctx := context.Background()
	realDB := statetest.OpenDB(t)

	const N = 5
	ids := []string{"wi-orph-1", "wi-orph-2", "wi-orph-3", "wi-orph-4", "wi-orph-5"}
	for _, id := range ids {
		seedPlanned(t, realDB, id, "prod")
	}

	// T1: park all N as pending orphans (lane saturated at 0).
	t1 := New(realDB, Config{LaneCaps: map[string]int{"prod": 0}})
	if _, err := t1.Tick(ctx); err != nil {
		t.Fatalf("T1 Tick: %v", err)
	}
	pending, _ := realDB.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != N {
		t.Fatalf("after T1 pending=%d; want %d", len(pending), N)
	}

	// T2: wire a proceed-verdict gate so recheckGates fires on every orphan.
	// Pre-fix the orphan pass calls GetWorkItem once per orphan (N+1 shape);
	// post-fix the tick-scoped snapshot serves the map — zero per-orphan
	// GetWorkItem calls.
	cdb := &countingGetterDB{DB: realDB}
	verdicts := map[string]approval.Result{}
	gateCfg := map[string]approval.Config{}
	for _, id := range ids {
		verdicts[id] = approval.ResultProceed
		gateCfg[id] = gateCfgFor("prod-gate")
	}
	gate := &fakeGate{verdicts: verdicts}
	cfg := Config{
		Gate:         gate,
		GateResolver: gateResolverByID(gateCfg),
	}
	sch := newWithDB(cdb, cfg)
	if _, err := sch.Tick(ctx); err != nil {
		t.Fatalf("T2 Tick: %v", err)
	}

	if cdb.getCalls != 0 {
		t.Fatalf("orphan pass GetWorkItem calls=%d; want 0 — batch snapshot must serve the re-check without per-orphan fetch (#1359)", cdb.getCalls)
	}
}
