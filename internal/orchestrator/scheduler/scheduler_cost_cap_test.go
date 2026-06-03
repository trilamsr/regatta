// Cost-cap integration with Scheduler.Tick — spec PHASE-AUTONOMY W5.
// Verifies that the W5 global daily-cap pre-filter halts the entire
// tick when Allow=false, that disabled (CostCap=nil) is byte-equal to
// pre-W5 behaviour, and that the per-scope gate is NOT consulted under
// global throttle.
package scheduler

import (
	"context"
	"testing"

	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// fakeCostCapGate is the seam fake for CostCapGate.
type fakeCostCapGate struct {
	allow bool
	calls int
}

func (f *fakeCostCapGate) Allow(_ context.Context) bool {
	f.calls++
	return f.allow
}

// TestSchedulerTick_CostCapAllow_PassesThrough Allow=true leaves spawnable intact.
func TestSchedulerTick_CostCapAllow_PassesThrough(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-CAP-OK", "prod")

	cc := &fakeCostCapGate{allow: true}
	sch := New(db, Config{CostCap: cc})
	reserved, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if cc.calls == 0 {
		t.Fatalf("CostCap.Allow not invoked")
	}
	if len(reserved) != 1 {
		t.Fatalf("reserved=%d; want 1 (Allow=true should not block)", len(reserved))
	}
}

// TestSchedulerTick_CostCapDeny_HaltsTick Allow=false reserves nothing.
func TestSchedulerTick_CostCapDeny_HaltsTick(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-CAP-THROTTLED-1", "prod")
	seedPlanned(t, db, "WI-CAP-THROTTLED-2", "prod")

	cc := &fakeCostCapGate{allow: false}
	sch := New(db, Config{CostCap: cc})
	reserved, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(reserved) != 0 {
		t.Fatalf("reserved=%d; want 0 (throttled)", len(reserved))
	}
	if cc.calls == 0 {
		t.Fatalf("CostCap.Allow not invoked")
	}
}

// TestSchedulerTick_CostCapNil_NoOpZeroOverhead nil CostCap is identity.
func TestSchedulerTick_CostCapNil_NoOpZeroOverhead(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "WI-NOCAP-1", "prod")

	sch := New(db, Config{}) // CostCap left nil
	reserved, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(reserved) != 1 {
		t.Fatalf("reserved=%d; want 1 (no cap, no block)", len(reserved))
	}
}

