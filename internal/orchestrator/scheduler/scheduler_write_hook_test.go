package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

var errWriteHookSim = errors.New("write-hook-sim")

// TestWriteHook_NilDefault_TickProceeds pins the production path: WriteHook=nil means Tick runs unmodified.
func TestWriteHook_NilDefault_TickProceeds(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	seedPlanned(t, db, "WORK-1", "server")

	s := New(db, Config{LockTTL: time.Minute})
	if s.WriteHook != nil {
		t.Fatalf("WriteHook default not nil — production wiring leaked a hook")
	}

	ids, err := s.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("reserved=%d want 1", len(ids))
	}
}

// TestWriteHook_FailsFirstWrite_TickReturnsError pins the seam: a hook that errors on writeIndex=0 aborts Tick.
func TestWriteHook_FailsFirstWrite_TickReturnsError(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	seedPlanned(t, db, "WORK-1", "server")

	s := New(db, Config{LockTTL: time.Minute})
	var calls []int
	var mu sync.Mutex
	s.WriteHook = func(writeIndex int) error {
		mu.Lock()
		calls = append(calls, writeIndex)
		mu.Unlock()
		return errWriteHookSim
	}

	_, err := s.Tick(ctx)
	if !errors.Is(err, errWriteHookSim) {
		t.Fatalf("Tick err=%v want errWriteHookSim", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) == 0 {
		t.Fatalf("WriteHook called=%d want ≥1", len(calls))
	}
	if calls[0] != 0 {
		t.Fatalf("first writeIndex=%d want 0", calls[0])
	}

	// No spawning agents — first-write crash leaves no transitions.
	spawning, _ := db.ListAgentsByState(ctx, state.AgentSpawning)
	if len(spawning) != 0 {
		t.Fatalf("spawning=%d want 0 — first-write crash transitioned agents", len(spawning))
	}
}

// TestWriteHook_FailsAfterN pins writeIndex monotonicity: hook lets N writes through, then errors.
func TestWriteHook_FailsAfterN(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	seedPlanned(t, db, "WORK-1", "server")
	seedPlanned(t, db, "WORK-2", "server")

	s := New(db, Config{LockTTL: time.Minute})
	const failAt = 2
	var seen []int
	var mu sync.Mutex
	s.WriteHook = func(writeIndex int) error {
		mu.Lock()
		seen = append(seen, writeIndex)
		mu.Unlock()
		if writeIndex >= failAt {
			return errWriteHookSim
		}
		return nil
	}

	_, err := s.Tick(ctx)
	if err != nil && !errors.Is(err, errWriteHookSim) {
		t.Fatalf("Tick err=%v want nil or errWriteHookSim", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for i, idx := range seen {
		if idx != i {
			t.Fatalf("writeIndex[%d]=%d want %d — hook counter not monotonic", i, idx, i)
		}
	}
}
