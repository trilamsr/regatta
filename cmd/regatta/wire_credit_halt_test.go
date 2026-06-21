package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// TestCascade_CreditExhausted_EmitsEventAndHalts asserts a credit-exhausted exit records credit_exhausted + invokes the halt hook so dispatch stops (MAY-78).
func TestCascade_CreditExhausted_EmitsEventAndHalts(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()

	a, err := db.UpsertPending(ctx, "WORK-CREDIT", "server")
	if err != nil {
		t.Fatalf("upsert pending: %v", err)
	}
	pid := 4242
	sess := "session-credit"
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentSpawning, state.AgentMutation{}); err != nil {
		t.Fatalf("→spawning: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentRunning, state.AgentMutation{PID: &pid, SessionID: &sess}); err != nil {
		t.Fatalf("→running: %v", err)
	}

	halted := false
	cascade := newAgentExitedCascade(db, slog.Default(), func() { halted = true })
	cascade(a.ID, "WORK-CREDIT", 1, spawner.ExitReasonProviderCreditExhausted, 900)

	if !halted {
		t.Fatalf("halt hook not invoked on credit-exhausted exit; dispatch keeps burning quota")
	}
	events, err := db.ListEventsByKindSince(ctx, string(obs.EventCreditExhausted), 0, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("no %q event recorded; credit exhaustion was silent", obs.EventCreditExhausted)
	}
}

// TestCascade_NonCreditExit_DoesNotHalt asserts a non-credit exit leaves dispatch running — only the terminal credit fault halts (MAY-78 guard).
func TestCascade_NonCreditExit_DoesNotHalt(t *testing.T) {
	db := statetest.OpenDB(t)
	ctx := context.Background()

	a, err := db.UpsertPending(ctx, "WORK-OK", "server")
	if err != nil {
		t.Fatalf("upsert pending: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentSpawning, state.AgentMutation{}); err != nil {
		t.Fatalf("→spawning: %v", err)
	}
	pid := 1
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentRunning, state.AgentMutation{PID: &pid}); err != nil {
		t.Fatalf("→running: %v", err)
	}

	halted := false
	cascade := newAgentExitedCascade(db, slog.Default(), func() { halted = true })
	cascade(a.ID, "WORK-OK", 1, spawner.ExitReasonUnknown, 100)

	if halted {
		t.Fatalf("halt hook fired on non-credit exit; only credit exhaustion must halt")
	}
}
