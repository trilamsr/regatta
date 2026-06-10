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

// TestScheduler_TickHonorsParallelCap_WhenSpawnableLargerThanCap asserts ParallelCap truncates spawnable (#1169).
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
		records := h.Records()
		msgs := make([]string, 0, len(records))
		for _, r := range records {
			msgs = append(msgs, r.Message)
		}
		t.Fatalf("expected scheduler.parallel_cap_* log, got %v", msgs)
	}
}

// TestScheduler_TickWithParallelCapZero_PreservesLaneCapBehavior asserts cap=0 disables global cap (#1169 back-compat).
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

// TestScheduler_TickRespectsCapAcrossTicks_WhenAgentsStillActive asserts running agents count against ParallelCap (#1169).
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
