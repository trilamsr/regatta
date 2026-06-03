package rejectionrouter_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestEventKindLabeled_MatchesStateSQLFilter pins the contract that state.ListEscalatedUnlabeled's hardcoded "labeled" string equals rejectionrouter.EventKindLabeled — a rename without updating the SQL silently re-introduces #478.
func TestEventKindLabeled_MatchesStateSQLFilter(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	id := newAgentInEscalated(t, db, "wi-pin")
	if err := db.RecordEvent(ctx, id, rejectionrouter.EventKindLabeled, "{}"); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, err := db.ListEscalatedUnlabeled(ctx, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("filter did not see the labeled event recorded with EventKindLabeled=%q; got %d rows — likely the SQL literal drifted from the constant", rejectionrouter.EventKindLabeled, len(got))
	}
}

// TestSweep_AlreadyLabeledBacklogDoesNotInvokeLabeler pins issue #478: per-tick work must not scale with the size of the terminal escalated+labeled set.
func TestSweep_AlreadyLabeledBacklogDoesNotInvokeLabeler(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)

	// 50 escalated agents that ALREADY have a labeled audit row.
	// These are the "terminal forever" rows #478 says will accrue.
	const backlog = 50
	for i := 0; i < backlog; i++ {
		id := newAgentInEscalated(t, db, fmt.Sprintf("wi-old-%d", i))
		if err := db.RecordEvent(ctx, id, rejectionrouter.EventKindLabeled,
			`{"label":"needs-human"}`); err != nil {
			t.Fatalf("seed labeled event: %v", err)
		}
	}

	counter := &countingLabeler{}
	r := rejectionrouter.New(rejectionrouter.Config{DB: db, Labeler: counter})

	// Drive several ticks. The labeler must NEVER be called for an
	// already-labeled backlog row — the SQL filter, not in-Go skip,
	// owns the bound.
	for i := 0; i < 5; i++ {
		if err := r.Tick(ctx); err != nil {
			t.Fatalf("Tick %d: %v", i, err)
		}
	}

	counter.mu.Lock()
	defer counter.mu.Unlock()
	if counter.calls != 0 {
		t.Fatalf("labeler invoked %d times on already-labeled backlog; want 0", counter.calls)
	}
}

// TestSweep_PerTickWorkBoundedByBatchLimit asserts per-tick labeler calls saturate at BatchLimit regardless of how many unlabeled escalations are queued.
func TestSweep_PerTickWorkBoundedByBatchLimit(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)

	// 30 escalated agents with NO labeled row — every one is sweep
	// material. With BatchLimit=10 the sweep must cap at 10.
	const queued = 30
	const batchLimit = 10
	for i := 0; i < queued; i++ {
		newAgentInEscalated(t, db, fmt.Sprintf("wi-pending-%d", i))
	}

	counter := &countingLabeler{}
	r := rejectionrouter.New(rejectionrouter.Config{DB: db, BatchLimit: batchLimit, Labeler: counter})

	if err := r.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	counter.mu.Lock()
	got := counter.calls
	counter.mu.Unlock()
	if got != batchLimit {
		t.Fatalf("labeler calls=%d; want %d (capped at BatchLimit)", got, batchLimit)
	}
}

// newAgentInEscalated promotes a fresh agent all the way to escalated state for sweep tests.
func newAgentInEscalated(t *testing.T, db *state.DB, workItemID string) int64 {
	t.Helper()
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, workItemID, "lane-a")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	sha := "deadbeef"
	for _, next := range []state.AgentState{
		state.AgentSpawning, state.AgentRunning, state.AgentPROpen,
		state.AgentGatesRunning, state.AgentGatesFailed, state.AgentEscalated,
	} {
		mut := state.AgentMutation{}
		if next == state.AgentPROpen {
			mut.PRSHA = &sha
		}
		if _, err := db.TransitionAgent(ctx, a.ID, next, mut); err != nil {
			t.Fatalf("transition -> %s: %v", next, err)
		}
	}
	return a.ID
}

type countingLabeler struct {
	mu    sync.Mutex
	calls int
}

func (c *countingLabeler) AddLabel(_ context.Context, _ int64, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return nil
}
