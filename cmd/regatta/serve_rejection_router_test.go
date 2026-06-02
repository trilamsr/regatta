// serve_rejection_router_test pins the wiring from serve boot to the
// RejectionRouter (issue #475 / PR #469 followup). buildRejectionRouter
// is the helper serve.go calls between orchestrator.New and o.Run; the
// test exercises that helper end-to-end against a real *state.DB so a
// regression that silently nils the router shows up here, not at runtime
// after an AI-gate rejection.
package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestBuildRejectionRouter_NonNilWithDefaults pins that the helper
// returns a router with K=3 + label="needs-human" defaults so an
// operator who never edits regatta.yaml still gets the escalation path.
func TestBuildRejectionRouter_NonNilWithDefaults(t *testing.T) {
	db := openRejectionRouterTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	router := buildRejectionRouter(db, nil, logger)
	if router == nil {
		t.Fatal("buildRejectionRouter returned nil; want a non-nil Router")
	}
}

// TestServe_RejectionRouterRoutesGateRejectedThroughOrchestrator
// drives the full wiring: helper -> SetRejectionRouter -> RouteRejections.
// A pre-seeded gate_rejected event against an agent in gates_running must
// trigger the gates_running -> gates_failed transition with the K=3
// default counter, otherwise the router is wired but the orchestrator
// call is not — exactly the dead-code regression #475 guards against.
func TestServe_RejectionRouterRoutesGateRejectedThroughOrchestrator(t *testing.T) {
	ctx := context.Background()
	db := openRejectionRouterTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	agentID := seedAgentInGatesRunning(t, db, "wi-475", "sha475")
	if err := db.RecordEvent(ctx, agentID, rejectionrouter.EventKindGateRejected,
		`{"pr_sha":"sha475","gate_id":"l4_judge","verdict":"fail"}`); err != nil {
		t.Fatalf("record event: %v", err)
	}

	labeler := &capturingLabeler{}
	router := buildRejectionRouter(db, labeler, logger)
	if router == nil {
		t.Fatal("buildRejectionRouter returned nil; want non-nil")
	}

	o := orchestrator.New(orchestrator.Config{DB: db, Logger: logger})
	o.SetRejectionRouter(router)

	if err := o.RouteRejections(ctx); err != nil {
		t.Fatalf("RouteRejections: %v", err)
	}

	got, err := db.GetAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.State != state.AgentGatesFailed {
		t.Errorf("state=%q; want %q (first rejection should drive gates_failed)",
			got.State, state.AgentGatesFailed)
	}
	if got.RejectionCount != 1 {
		t.Errorf("rejection_count=%d; want 1 (one event processed)", got.RejectionCount)
	}
}

func openRejectionRouterTestDB(t *testing.T) *state.DB {
	t.Helper()
	dsn := state.DSN(filepath.Join(t.TempDir(), "rr.db"))
	db, err := state.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedAgentInGatesRunning(t *testing.T, db *state.DB, workItemID, sha string) int64 {
	t.Helper()
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, workItemID, "lane-a")
	if err != nil {
		t.Fatalf("UpsertPending: %v", err)
	}
	for _, next := range []state.AgentState{
		state.AgentSpawning, state.AgentRunning, state.AgentPROpen, state.AgentGatesRunning,
	} {
		var mut state.AgentMutation
		if next == state.AgentPROpen {
			s := sha
			mut.PRSHA = &s
		}
		if _, err := db.TransitionAgent(ctx, a.ID, next, mut); err != nil {
			t.Fatalf("transition %s -> %s: %v", a.State, next, err)
		}
	}
	return a.ID
}

type capturingLabeler struct {
	mu    sync.Mutex
	calls []string
}

func (c *capturingLabeler) AddLabel(_ context.Context, workItemID, label string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, workItemID+":"+label)
	return nil
}
