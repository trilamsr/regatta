package costcap_test

import (
	"context"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestEnforcer_TwoSchedulersAgreeUnderRace_NoDuplicate pins the #652
// storage-layer guard: two RecordEvent writes for the same UTC day +
// kind=cost_cap_throttled collapse to one durable row via partial
// UNIQUE index.
func TestEnforcer_TwoSchedulersAgreeUnderRace_NoDuplicate(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()
	// Scheduler-A writes the canonical throttled row (agent_id=0 ⇒ NULL).
	if err := db.RecordEvent(ctx, 0, "cost_cap_throttled", `{"cap":40}`); err != nil {
		t.Fatalf("first throttled append: %v", err)
	}
	// Scheduler-B races the same transition within the same UTC day.
	// The partial UNIQUE index on (kind, created_at/86400) must reject
	// the second row.
	err := db.RecordEvent(ctx, 0, "cost_cap_throttled", `{"cap":40}`)
	if err == nil {
		t.Fatalf("second throttled append succeeded; want UNIQUE violation (#652)")
	}
	if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Fatalf("unexpected error shape: %v", err)
	}
	// Resumed events MUST remain unconstrained — the index is throttle-
	// scoped on purpose (operator can override + re-throttle in one day).
	if err := db.RecordEvent(ctx, 0, "cost_cap_resumed", `{"actor":"tree"}`); err != nil {
		t.Fatalf("resumed append rejected by index leak: %v", err)
	}
	if err := db.RecordEvent(ctx, 0, "cost_cap_resumed", `{"actor":"tree"}`); err != nil {
		t.Fatalf("second resumed append rejected; want unconstrained: %v", err)
	}
}
