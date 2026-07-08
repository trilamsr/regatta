package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestLaneCapSnapshotHoldsAcrossTick pins that mid-tick LaneCaps deletion cannot oversubscribe the lane (R31-I5, #1362).
func TestLaneCapSnapshotHoldsAcrossTick(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	seedPlanned(t, db, "wi-1", "prod")
	seedPlanned(t, db, "wi-2", "prod")
	seedPlanned(t, db, "wi-3", "prod")
	seedPlanned(t, db, "wi-4", "prod")

	sch := New(db, Config{
		LockTTL:  time.Minute,
		LaneCaps: map[string]int{"prod": 2},
	})

	// WriteHook fires 3× per successful reservation (upsert, lock,
	// transition). After the first reservation completes (idx==2 is the
	// last hook of wi-1's tx), delete the "prod" cap entry — simulating
	// operator config-reload dropping the gate mid-tick.
	sch.WriteHook = func(idx int) error {
		if idx == 2 {
			delete(sch.cfg.LaneCaps, "prod")
		}
		return nil
	}

	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	// Snapshot cap at tick start was 2 → tick MUST NOT reserve more than 2.
	// Under the pre-fix code (per-call read of s.cfg.LaneCaps), the delete
	// removes the cap gate for wi-2/wi-3/wi-4 → all 4 reserved → 4 > 2.
	if len(ids) > 2 {
		t.Fatalf("reserved %d agents; want ≤ 2 (snapshot cap held) — mid-tick LaneCaps deletion oversubscribed lane", len(ids))
	}
}
