package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestRecover_CrashedStateIdempotent asserts second Recover drains a stuck-Crashed agent (R-MEGA-2 C3).
func TestRecover_CrashedStateIdempotent(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 1)
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	running, _ := db.ListAgentsByState(ctx, state.AgentRunning)
	if len(running) != 1 {
		t.Fatalf("running=%d; want 1", len(running))
	}
	a := running[0]

	// Seed a held lock so the test can prove Recover releases it on the
	// idempotent path (the stub spawner skips lane locks by design).
	if err := db.TryAcquireLock(ctx, "test.lock", a.ID, time.Minute); err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentCrashed, state.AgentMutation{}); err != nil {
		t.Fatalf("force crashed: %v", err)
	}

	locks, _ := db.ListLocks(ctx)
	if len(locks) == 0 {
		t.Fatalf("precondition: locks empty before Recover")
	}

	if err := o.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}

	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 1 {
		t.Fatalf("pending after recover=%d; want 1 (Crashed must be drained to Pending)", len(pending))
	}
	postLocks, _ := db.ListLocks(ctx)
	for _, l := range postLocks {
		if l.AgentID == a.ID {
			t.Fatalf("agent %d still holds lock %q after Recover", a.ID, l.Name)
		}
	}
}
