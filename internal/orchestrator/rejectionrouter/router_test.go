package rejectionrouter_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestTick_FirstRejection_GatesRunningToGatesFailed verifies a single gate_rejected event drives the gates_running -> gates_failed transition with rejection_count=1.
func TestTick_FirstRejection_GatesRunningToGatesFailed(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	id := newAgentInGatesRunning(t, db, "wi-1", "deadbeef")

	// Synthetic rejecting GateResult tied to the agent's current sha.
	if err := db.RecordEvent(ctx, id, "gate_rejected",
		`{"pr_sha":"deadbeef","gate_id":"l4_judge","verdict":"fail"}`); err != nil {
		t.Fatalf("record event: %v", err)
	}

	r := rejectionrouter.New(rejectionrouter.Config{DB: db})
	if err := r.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got := mustGetAgent(t, db, id)
	if got.State != state.AgentGatesFailed {
		t.Errorf("state=%q; want %q", got.State, state.AgentGatesFailed)
	}
	if got.RejectionCount != 1 {
		t.Errorf("rejection_count=%d; want 1", got.RejectionCount)
	}
}

// TestTick_StaleRejection_IgnoredOnShaMismatch verifies that a gate_rejected for a pr_sha other than the agent's current sha does not increment the counter.
func TestTick_StaleRejection_IgnoredOnShaMismatch(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	id := newAgentInGatesRunning(t, db, "wi-stale", "newsha")

	if err := db.RecordEvent(ctx, id, "gate_rejected",
		`{"pr_sha":"oldsha","gate_id":"l4_judge","verdict":"fail"}`); err != nil {
		t.Fatalf("record event: %v", err)
	}

	r := rejectionrouter.New(rejectionrouter.Config{DB: db})
	if err := r.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got := mustGetAgent(t, db, id)
	if got.RejectionCount != 0 {
		t.Errorf("rejection_count=%d; want 0 (stale sha)", got.RejectionCount)
	}
	if got.State != state.AgentGatesRunning {
		t.Errorf("state=%q; want %q (no transition on stale)", got.State, state.AgentGatesRunning)
	}
}

// TestTick_EachEventProcessedOnce verifies the cursor advances so a second Tick over the same events is a no-op.
func TestTick_EachEventProcessedOnce(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	id := newAgentInGatesRunning(t, db, "wi-once", "sha1")

	if err := db.RecordEvent(ctx, id, "gate_rejected",
		`{"pr_sha":"sha1","gate_id":"l4_judge","verdict":"fail"}`); err != nil {
		t.Fatalf("record event: %v", err)
	}

	r := rejectionrouter.New(rejectionrouter.Config{DB: db})
	if err := r.Tick(ctx); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}
	if err := r.Tick(ctx); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}

	got := mustGetAgent(t, db, id)
	if got.RejectionCount != 1 {
		t.Errorf("rejection_count=%d; want 1 (each event processed once)", got.RejectionCount)
	}
}

// TestTick_KEscalation_LabelsPRAndMarksEscalated verifies that the K-th rejection transitions gates_failed -> escalated, labels the PR needs-human, and releases locks.
func TestTick_KEscalation_LabelsPRAndMarksEscalated(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	id := newAgentInGatesRunning(t, db, "wi-esc", "sha1")
	mustAcquireLock(t, db, id, "wi-esc")

	labeler := &fakeLabeler{}
	r := rejectionrouter.New(rejectionrouter.Config{DB: db, K: 3, Labeler: labeler})

	// 1st rejection: gates_running -> gates_failed, count=1.
	mustRecordRejection(t, db, id, "sha1")
	if err := r.Tick(ctx); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}
	// Operator-side "wake": gates_failed -> running -> pr_open -> gates_running so the next sha can be rejected.
	cycleBackToGatesRunning(t, db, id, "sha2")

	mustRecordRejection(t, db, id, "sha2")
	if err := r.Tick(ctx); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}
	cycleBackToGatesRunning(t, db, id, "sha3")

	mustRecordRejection(t, db, id, "sha3")
	if err := r.Tick(ctx); err != nil {
		t.Fatalf("Tick 3: %v", err)
	}

	got := mustGetAgent(t, db, id)
	if got.State != state.AgentEscalated {
		t.Errorf("state=%q; want %q (K=3 escalation)", got.State, state.AgentEscalated)
	}
	if got.RejectionCount != 3 {
		t.Errorf("rejection_count=%d; want 3", got.RejectionCount)
	}

	labeler.mu.Lock()
	calls := append([]labelCall(nil), labeler.calls...)
	labeler.mu.Unlock()
	if len(calls) != 1 || calls[0].agentID != id || calls[0].label != "needs-human" {
		t.Errorf("labeler calls=%+v; want one call with agentID=%d label=needs-human", calls, id)
	}

	// Heartbeat-stop invariant: locks released so Orchestrator.Heartbeat
	// (which lists by non-terminal states) cannot keep refreshing leases
	// for an escalated agent.
	if n := countLocks(t, db, id); n != 0 {
		t.Errorf("locks held=%d; want 0 (escalated agents must not heartbeat)", n)
	}
}

// TestTick_LabelerFailure_RetriedNextTick verifies a labeler error leaves the cursor at the escalating event so the next Tick retries the label.
func TestTick_LabelerFailure_RetriedNextTick(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	id := newAgentInGatesRunning(t, db, "wi-retry", "sha1")

	flaky := &fakeLabeler{failFirst: true}
	r := rejectionrouter.New(rejectionrouter.Config{DB: db, K: 1, Labeler: flaky})

	mustRecordRejection(t, db, id, "sha1")
	if err := r.Tick(ctx); err == nil {
		t.Fatalf("Tick: want error from failing labeler, got nil")
	}

	// Counter incremented + transition applied even if labeling failed —
	// the gate verdict is durable; the label is the side-effect we retry.
	got := mustGetAgent(t, db, id)
	if got.State != state.AgentEscalated {
		t.Errorf("state=%q; want %q (transition before labeler call)", got.State, state.AgentEscalated)
	}

	// Second Tick: labeler now succeeds.
	if err := r.Tick(ctx); err != nil {
		t.Fatalf("Tick retry: %v", err)
	}
	flaky.mu.Lock()
	defer flaky.mu.Unlock()
	if flaky.successCount != 1 {
		t.Errorf("successful label calls=%d; want 1 (retried after first failure)", flaky.successCount)
	}
}


func openDB(t *testing.T) *state.DB {
	t.Helper()
	dsn := state.DSN(filepath.Join(t.TempDir(), "rr.db"))
	db, err := state.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newAgentInGatesRunning(t *testing.T, db *state.DB, workItemID, sha string) int64 {
	t.Helper()
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, workItemID, "lane-a")
	if err != nil {
		t.Fatalf("upsert pending: %v", err)
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

func cycleBackToGatesRunning(t *testing.T, db *state.DB, id int64, nextSHA string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.TransitionAgent(ctx, id, state.AgentRunning, state.AgentMutation{}); err != nil {
		t.Fatalf("gates_failed -> running: %v", err)
	}
	s := nextSHA
	if _, err := db.TransitionAgent(ctx, id, state.AgentPROpen, state.AgentMutation{PRSHA: &s}); err != nil {
		t.Fatalf("running -> pr_open: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, id, state.AgentGatesRunning, state.AgentMutation{}); err != nil {
		t.Fatalf("pr_open -> gates_running: %v", err)
	}
}

func mustGetAgent(t *testing.T, db *state.DB, id int64) *state.Agent {
	t.Helper()
	a, err := db.GetAgent(context.Background(), id)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	return a
}

func mustRecordRejection(t *testing.T, db *state.DB, agentID int64, sha string) {
	t.Helper()
	payload := `{"pr_sha":"` + sha + `","gate_id":"l4_judge","verdict":"fail"}`
	if err := db.RecordEvent(context.Background(), agentID, "gate_rejected", payload); err != nil {
		t.Fatalf("record rejection: %v", err)
	}
}

func mustAcquireLock(t *testing.T, db *state.DB, agentID int64, lockName string) {
	t.Helper()
	if err := db.TryAcquireLock(context.Background(), lockName, agentID, time.Minute); err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
}

func countLocks(t *testing.T, db *state.DB, agentID int64) int {
	t.Helper()
	locks, err := db.ListLocks(context.Background())
	if err != nil {
		t.Fatalf("ListLocks: %v", err)
	}
	n := 0
	for _, l := range locks {
		if l.AgentID == agentID {
			n++
		}
	}
	return n
}

type labelCall struct {
	agentID int64
	label   string
}

type fakeLabeler struct {
	mu           sync.Mutex
	calls        []labelCall
	failFirst    bool
	failed       bool
	successCount int
}

func (f *fakeLabeler) AddLabel(_ context.Context, agentID int64, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failFirst && !f.failed {
		f.failed = true
		return errFakeLabeler
	}
	f.calls = append(f.calls, labelCall{agentID: agentID, label: label})
	f.successCount++
	return nil
}

var errFakeLabeler = &fakeErr{msg: "fake labeler failure"}

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }
