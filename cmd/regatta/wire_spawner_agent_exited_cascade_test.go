package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestNewAgentExitedCascade_DrivesRunningToCrashed asserts the cascade transitions an active agent to crashed AND records the agent.exited audit row — closes the #1218 stall where the spawner emitted only a slog line and the agents table leaked phantoms.
func TestNewAgentExitedCascade_DrivesRunningToCrashed(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()

	a, err := db.UpsertPending(ctx, "WORK-CRASH", "server")
	if err != nil {
		t.Fatalf("upsert pending: %v", err)
	}
	pid := 12345
	sess := "session-x"
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentSpawning, state.AgentMutation{}); err != nil {
		t.Fatalf("→spawning: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentRunning, state.AgentMutation{
		PID:       &pid,
		SessionID: &sess,
	}); err != nil {
		t.Fatalf("→running: %v", err)
	}

	cascade := newAgentExitedCascade(db, slog.Default())
	cascade(a.ID, "WORK-CRASH", 7, spawner.ExitReasonUnknown, 1500)

	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got.State != state.AgentCrashed {
		t.Fatalf("agent.state=%q want %q (phantom-agent stall)", got.State, state.AgentCrashed)
	}

	events, err := db.ListEventsByKindSince(ctx, string(obs.EventAgentExited), 0, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("no %q events recorded; the cascade silently dropped the audit row", obs.EventAgentExited)
	}
	if !strings.Contains(events[0].PayloadJSON, `"exit_code":7`) {
		t.Fatalf("payload missing exit_code=7: %s", events[0].PayloadJSON)
	}
	if !strings.Contains(events[0].PayloadJSON, `"duration_ms":1500`) {
		t.Fatalf("payload missing duration_ms=1500: %s", events[0].PayloadJSON)
	}
}

// TestNewAgentExitedCascade_TerminalAgentIsNoOp asserts the cascade leaves an already-crashed agent alone so a race between reaper / rejection-router / cascade does not throw ErrInvalidTransition into operator logs.
func TestNewAgentExitedCascade_TerminalAgentIsNoOp(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()

	a, err := db.UpsertPending(ctx, "WORK-DONE", "server")
	if err != nil {
		t.Fatalf("upsert pending: %v", err)
	}
	// Walk to a terminal state via spawning→crashed (valid edge).
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentSpawning, state.AgentMutation{}); err != nil {
		t.Fatalf("→spawning: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentCrashed, state.AgentMutation{}); err != nil {
		t.Fatalf("→crashed: %v", err)
	}

	cascade := newAgentExitedCascade(db, slog.Default())
	cascade(a.ID, "WORK-DONE", 0, spawner.ExitReasonCompleted, 100)

	events, err := db.ListEventsByKindSince(ctx, string(obs.EventAgentExited), 0, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("terminal-state cascade recorded %d events; want 0", len(events))
	}
}
