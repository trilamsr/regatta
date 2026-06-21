//go:build unix

package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestRecover_StubGhostReapedUnderClaudeBackend asserts a stub-session agent recovered under the claude backend is withdrawn + lane freed + spawner_backend_changed fires (MAY-79).
func TestRecover_StubGhostReapedUnderClaudeBackend(t *testing.T) {
	ctx := context.Background()

	// Boot 1 (stub): reserve + spawn one agent so the row carries a
	// "stub-" session_id and the stub's negative PID.
	o, _, db, _ := newHarness(t, 1)
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	running, _ := db.ListAgentsByState(ctx, state.AgentRunning)
	if len(running) != 1 {
		t.Fatalf("running after spawn=%d; want 1", len(running))
	}

	// Boot 2 (claude): same state.db, claude backend now active.
	o2 := New(Config{
		DB:             db,
		Scheduler:      scheduler.New(db, scheduler.Config{LockTTL: time.Minute}),
		Spawner:        spawner.New(spawner.Config{}),
		SpawnerBackend: "claude",
		LockTTL:        time.Minute,
	})
	if err := o2.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}

	withdrawn, _ := db.ListAgentsByState(ctx, state.AgentWithdrawn)
	if len(withdrawn) != 1 {
		t.Fatalf("withdrawn after recover=%d; want 1 (stub ghost must be reaped, not requeued)", len(withdrawn))
	}
	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 0 {
		t.Fatalf("pending after recover=%d; want 0 (ghost must not requeue)", len(pending))
	}
	locks, _ := db.ListLocks(ctx)
	if len(locks) != 0 {
		t.Fatalf("locks not released after reap: %+v", locks)
	}

	events, _ := db.ListEvents(ctx, 50)
	saw := false
	for _, e := range events {
		if e.Kind == string(obs.EventSpawnerBackendChanged) {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("spawner_backend_changed event not recorded")
	}
}

// TestRecover_StubAgentRequeuesUnderStubBackend asserts a stub-session agent under the stub backend still requeues — backend-mismatch reap must not fire in a stub-only deployment (MAY-79 guard).
func TestRecover_StubAgentRequeuesUnderStubBackend(t *testing.T) {
	ctx := context.Background()
	o, _, db, _ := newHarness(t, 1)
	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if err := o.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	pending, _ := db.ListAgentsByState(ctx, state.AgentPending)
	if len(pending) != 1 {
		t.Fatalf("pending after recover=%d; want 1 (stub backend must requeue, not reap)", len(pending))
	}
	withdrawn, _ := db.ListAgentsByState(ctx, state.AgentWithdrawn)
	if len(withdrawn) != 0 {
		t.Fatalf("withdrawn after recover=%d; want 0", len(withdrawn))
	}
}
