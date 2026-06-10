package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestScheduler_TickHonorsParallelCap_WhenSpawnableLargerThanCap asserts
// that with N spawnable items > ParallelCap, exactly ParallelCap reservations
// land in one Tick (#1169 §A). Pre-impl: scheduler walks the entire spawnable
// slice (43 spawns in <5s observed live). Post-impl: spawnable truncated to
// ParallelCap before reserveFromSpawnable runs.
func TestScheduler_TickHonorsParallelCap_WhenSpawnableLargerThanCap(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()

	// 12 spawnable items across 3 lanes, no per-lane cap; global cap = 4.
	lanes := []string{"server", "customer", "self-host"}
	for i := 0; i < 12; i++ {
		seedPlanned(t, db, fmt.Sprintf("WORK-%d", i), lanes[i%len(lanes)])
	}

	h := obstest.New()
	sch := New(db, Config{
		LockTTL:     time.Minute,
		ParallelCap: 4,
		Logger:      slog.New(h),
	})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got, want := len(ids), 4; got != want {
		t.Fatalf("reserved=%d, want %d (ParallelCap must truncate)", got, want)
	}

	// One INFO/structured log line confirming cap was applied.
	gotCapLog := false
	for _, r := range h.Records() {
		if strings.Contains(r.Message, "parallel_cap") {
			gotCapLog = true
		}
	}
	if !gotCapLog {
		msgs := []string{}
		for _, r := range h.Records() {
			msgs = append(msgs, r.Message)
		}
		t.Fatalf("expected scheduler.parallel_cap_* log, got %v", msgs)
	}
}

// TestScheduler_TickWithParallelCapZero_PreservesLaneCapBehavior locks the
// backward-compat path: ParallelCap == 0 disables the global cap; only
// lane-cap semantics apply. With 6 spawnable, no lane caps, ParallelCap=0,
// all 6 reserve in one tick (pre-#1169 behavior).
func TestScheduler_TickWithParallelCapZero_PreservesLaneCapBehavior(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()

	for i := 0; i < 6; i++ {
		seedPlanned(t, db, fmt.Sprintf("WORK-%d", i), "")
	}

	sch := New(db, Config{LockTTL: time.Minute, ParallelCap: 0})
	ids, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got, want := len(ids), 6; got != want {
		t.Fatalf("reserved=%d, want %d (ParallelCap=0 must not truncate)", got, want)
	}
}

// TestScheduler_TickRespectsCapAcrossTicks_WhenAgentsStillActive asserts that
// running agents from a prior tick count against ParallelCap. Tick 1 reserves
// 4; tick 2 with 4 agents still in spawning state and 4 more spawnable must
// reserve 0 (cap saturated).
func TestScheduler_TickRespectsCapAcrossTicks_WhenAgentsStillActive(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()

	for i := 0; i < 8; i++ {
		seedPlanned(t, db, fmt.Sprintf("WORK-%d", i), "server")
	}

	sch := New(db, Config{LockTTL: time.Minute, ParallelCap: 4})
	ids1, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick1: %v", err)
	}
	if got, want := len(ids1), 4; got != want {
		t.Fatalf("tick1 reserved=%d, want %d", got, want)
	}

	// 4 agents still in active states; cap saturated → 0 new reservations.
	ids2, err := sch.Tick(ctx)
	if err != nil {
		t.Fatalf("tick2: %v", err)
	}
	if got, want := len(ids2), 0; got != want {
		t.Fatalf("tick2 reserved=%d, want %d (cap saturated)", got, want)
	}
}
