// serve_rejection_router_test pins the wiring from serve boot to the
// RejectionRouter. buildRejectionRouter is the helper serve.go calls
// between orchestrator.New and o.Run; the helper test exercises it
// end-to-end against a real *state.DB. The --tick-once boot test guards
// against the dead-code regression class: it runs runServe in-process
// and asserts a seeded gate_rejected row drives the state transition
// end-to-end, so silently dropping the SetRejectionRouter call from
// runServe fails CI.
package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestBuildRejectionRouter_NonNilWithDefaults pins the helper returns a non-nil Router under K=3 + label=needs-human defaults.
func TestBuildRejectionRouter_NonNilWithDefaults(t *testing.T) {
	db := openRejectionRouterTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	router := buildRejectionRouter(db, nil, logger)
	if router == nil {
		t.Fatal("buildRejectionRouter returned nil; want a non-nil Router")
	}
}

// TestServe_RejectionRouterRoutesGateRejectedThroughOrchestrator pins helper -> SetRejectionRouter -> RouteRejections drives gates_running -> gates_failed.
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

// TestRunServe_TickOnce_RoutesGateRejected boots runServe --tick-once and asserts a seeded gate_rejected event drives gates_failed end-to-end.
func TestRunServe_TickOnce_RoutesGateRejected(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	repoRoot := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".regatta", "items"), 0o755); err != nil {
		t.Fatalf("mkdir items: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, ".regatta", "programs"), 0o755); err != nil {
		t.Fatalf("mkdir programs: %v", err)
	}

	dbPath := filepath.Join(dir, "state.db")
	// Open the DB so we can seed an agent + rejection BEFORE runServe.
	// runServe will reopen the same path; sqlite supports a serial
	// reopen with no file lock contention here (tick-once is one-shot).
	seedDB, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	// Use the test-process PID so runServe.Recover sees the agent as
	// live (pidAlive == true) and skips the crashed-requeue branch.
	// Without this the synthetic PID=0 is treated as dead, Recover
	// cycles the agent back to pending, ScheduleOnce respawns it, and
	// the seeded rejection drops because the agent is no longer in
	// gates_running by the time RouteRejections fires.
	agentID := seedAgentInGatesRunningWithPID(t, seedDB, "wi-tick-once", "sha-tick", os.Getpid())
	if err := seedDB.RecordEvent(ctx, agentID, rejectionrouter.EventKindGateRejected,
		`{"pr_sha":"sha-tick","gate_id":"l4_judge","verdict":"fail"}`); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := seedDB.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	rc := runServe([]string{
		"--db", dbPath,
		"--repo", repoRoot,
		"--items-root", repoRoot,
		// --spawner defaults to stub; omit to avoid duplicating
		// the literal triggered by the goconst lint gate.
		"--ui=false",
		"--tick-once",
	})
	if rc != 0 {
		t.Fatalf("runServe rc=%d; want 0", rc)
	}

	// Reopen the DB to read the post-tick state.
	verifyDB, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("verify open: %v", err)
	}
	defer func() { _ = verifyDB.Close() }()
	got, err := verifyDB.GetAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.State != state.AgentGatesFailed {
		t.Errorf("state=%q; want %q — runServe did not invoke RouteRejections on the live agent",
			got.State, state.AgentGatesFailed)
	}
	if got.RejectionCount != 1 {
		t.Errorf("rejection_count=%d; want 1 — gate_rejected event was not processed by the wired router",
			got.RejectionCount)
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
	return seedAgentInGatesRunningWithPID(t, db, workItemID, sha, 0)
}

// seedAgentInGatesRunningWithPID drives the agent through the lifecycle
// to gates_running and optionally pins a PID on the spawning step.
// pid=0 leaves the column at its default (synthetic) value; tests that
// must survive Recover should pass os.Getpid().
func seedAgentInGatesRunningWithPID(t *testing.T, db *state.DB, workItemID, sha string, pid int) int64 {
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
		if next == state.AgentSpawning && pid != 0 {
			p := pid
			mut.PID = &p
		}
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
