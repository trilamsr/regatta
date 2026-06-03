// executor_test pins Coordinator.ExecuteMerge — the c2 seam wiring
// PrepareMerge → gh-shellout → completion-event between the scheduler's
// gate-pass decision and the autonomous merge.
package merge_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// fakeExecutor is a deterministic merge.Executor stub. Tests set
// Result + Err per call; ProbeFunc lets concurrent-race tests inject
// per-invocation behavior.
type fakeExecutor struct {
	mu     sync.Mutex
	Result merge.ExecutorResult
	Err    error
	Calls  []fakeExecutorCall
	Func   func(ctx context.Context, prNumber int, headSHA string) (merge.ExecutorResult, error)
}

type fakeExecutorCall struct {
	PRNumber int
	HeadSHA  string
}

func (f *fakeExecutor) Merge(ctx context.Context, prNumber int, headSHA string) (merge.ExecutorResult, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, fakeExecutorCall{PRNumber: prNumber, HeadSHA: headSHA})
	fn := f.Func
	res, err := f.Result, f.Err
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, prNumber, headSHA)
	}
	return res, err
}

// newCoordinatorWithExecutor wires a Coordinator with both a prober +
// executor — most ExecuteMerge tests don't exercise the prober but a
// non-nil PRProber is required by merge.New.
func newCoordinatorWithExecutor(t *testing.T, db *state.DB, exec *fakeExecutor) *merge.Coordinator {
	t.Helper()
	c := newCoordinator(t, db, &fakeProber{})
	c.SetExecutor(exec)
	return c
}

// TestExecuteMerge_HappyPath_WritesCompletion asserts a successful gh merge writes merge_completed + transitions Done.
func TestExecuteMerge_HappyPath_WritesCompletion(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	exec := &fakeExecutor{Result: merge.ExecutorResult{
		Outcome:    merge.OutcomeMerged,
		MergeSHA:   "deadbeef",
		ExitCode:   0,
		DurationMs: 487,
	}}
	c := newCoordinatorWithExecutor(t, db, exec)

	if err := c.ExecuteMerge(ctx, a.ID, 42, "abc123"); err != nil {
		t.Fatalf("execute merge: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentDone {
		t.Fatalf("state=%s, want done", got.State)
	}
	assertEventExists(t, db, a.ID, merge.EventKindMergeCompleted)
	assertEventExists(t, db, a.ID, merge.EventKindMergeExecuted)
	raw := loadEvent(t, db, a.ID, merge.EventKindMergeCompleted)
	var p merge.CompletedPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("decode completed: %v", err)
	}
	if p.Source != "merge_call" {
		t.Fatalf("source=%q, want merge_call (distinguishes normal vs recovery path)", p.Source)
	}
	if p.MergeSHA != "deadbeef" {
		t.Fatalf("merge_sha=%q, want deadbeef", p.MergeSHA)
	}
}

// TestExecuteMerge_AlreadyMerged_TreatsAsSuccess asserts gh's already-merged stderr drives the success path idempotently.
func TestExecuteMerge_AlreadyMerged_TreatsAsSuccess(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	exec := &fakeExecutor{Result: merge.ExecutorResult{Outcome: merge.OutcomeAlreadyMerged}}
	c := newCoordinatorWithExecutor(t, db, exec)

	if err := c.ExecuteMerge(ctx, a.ID, 7, "sha7"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentDone {
		t.Fatalf("state=%s, want done (already_merged is success)", got.State)
	}
	assertEventExists(t, db, a.ID, merge.EventKindMergeCompleted)
}

// TestExecuteMerge_BranchProtection_WritesFailed asserts branch-protection rejection writes merge_failed terminal.
func TestExecuteMerge_BranchProtection_WritesFailed(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	exec := &fakeExecutor{
		Result: merge.ExecutorResult{Outcome: merge.OutcomeBranchProtection},
		Err:    errors.New("branch protection rule violated"),
	}
	c := newCoordinatorWithExecutor(t, db, exec)

	if err := c.ExecuteMerge(ctx, a.ID, 9, "sha9"); err != nil {
		t.Fatalf("execute (terminal failures are not propagated as errors): %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentCrashed {
		t.Fatalf("state=%s, want crashed (terminal branch_protection)", got.State)
	}
	failed := loadEvent(t, db, a.ID, merge.EventKindMergeFailed)
	if !strings.Contains(failed, "branch_protection") {
		t.Fatalf("failed payload missing reason: %s", failed)
	}
}

// TestExecuteMerge_Conflict_WritesFailed asserts a merge conflict writes merge_failed terminal.
func TestExecuteMerge_Conflict_WritesFailed(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	exec := &fakeExecutor{
		Result: merge.ExecutorResult{Outcome: merge.OutcomeConflict},
		Err:    errors.New("merge conflict"),
	}
	c := newCoordinatorWithExecutor(t, db, exec)

	if err := c.ExecuteMerge(ctx, a.ID, 11, "sha11"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentCrashed {
		t.Fatalf("state=%s, want crashed (conflict terminal)", got.State)
	}
	failed := loadEvent(t, db, a.ID, merge.EventKindMergeFailed)
	if !strings.Contains(failed, "conflict") {
		t.Fatalf("failed payload missing reason: %s", failed)
	}
}

// TestExecuteMerge_HeadSHADrift_RefusesViaFlag asserts --match-head-commit rejection writes merge_failed terminal.
func TestExecuteMerge_HeadSHADrift_RefusesViaFlag(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	exec := &fakeExecutor{
		Result: merge.ExecutorResult{Outcome: merge.OutcomeSHADiverged},
		Err:    errors.New("head commit does not match"),
	}
	c := newCoordinatorWithExecutor(t, db, exec)

	if err := c.ExecuteMerge(ctx, a.ID, 13, "oldsha"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentCrashed {
		t.Fatalf("state=%s, want crashed (sha_diverged terminal)", got.State)
	}
}

// TestExecuteMerge_RateLimit_Retransient asserts rate-limit returns transient error, leaves agent in awaiting_merge.
func TestExecuteMerge_RateLimit_Retransient(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	exec := &fakeExecutor{
		Result: merge.ExecutorResult{Outcome: merge.OutcomeRateLimit},
		Err:    errors.New("API rate limit exceeded"),
	}
	c := newCoordinatorWithExecutor(t, db, exec)

	err := c.ExecuteMerge(ctx, a.ID, 15, "sha15")
	if err == nil {
		t.Fatalf("execute returned nil, want transient error for rate_limit")
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentAwaitingMerge {
		t.Fatalf("state=%s, want awaiting_merge (transient leaves agent for next sweep)", got.State)
	}
	if eventExists(t, db, a.ID, merge.EventKindMergeCompleted) {
		t.Fatalf("merge_completed written on transient")
	}
	if eventExists(t, db, a.ID, merge.EventKindMergeFailed) {
		t.Fatalf("merge_failed written on transient")
	}
}

// TestExecuteMerge_NoExecutor_ReturnsErr asserts ExecuteMerge without SetExecutor surfaces wiring bug at call time.
func TestExecuteMerge_NoExecutor_ReturnsErr(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	c := newCoordinator(t, db, &fakeProber{})

	err := c.ExecuteMerge(ctx, a.ID, 1, "sha")
	if !errors.Is(err, merge.ErrNoExecutor) {
		t.Fatalf("err=%v, want ErrNoExecutor", err)
	}
}

// TestExecuteMerge_RaceLoss_IsNoOp asserts a second ExecuteMerge against an agent already in awaiting_merge returns nil without calling the executor.
func TestExecuteMerge_RaceLoss_IsNoOp(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	// Move agent past gates_running so PrepareMerge fails with
	// ErrInvalidTransition (the race-loss shape).
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentAwaitingMerge, state.AgentMutation{}); err != nil {
		t.Fatalf("transition: %v", err)
	}
	exec := &fakeExecutor{Result: merge.ExecutorResult{Outcome: merge.OutcomeMerged}}
	c := newCoordinatorWithExecutor(t, db, exec)

	if err := c.ExecuteMerge(ctx, a.ID, 1, "sha"); err != nil {
		t.Fatalf("execute on race-loss: %v", err)
	}
	if len(exec.Calls) != 0 {
		t.Fatalf("executor called %d times on race-loss, want 0", len(exec.Calls))
	}
}

// TestExecuteMerge_AutoQueued_LeavesInAwaitingMerge asserts --auto queue-accepted writes merge_executed only, no FSM transition.
func TestExecuteMerge_AutoQueued_LeavesInAwaitingMerge(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	exec := &fakeExecutor{Result: merge.ExecutorResult{Outcome: merge.OutcomeAutoQueued}}
	c := newCoordinatorWithExecutor(t, db, exec)

	if err := c.ExecuteMerge(ctx, a.ID, 21, "sha21"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentAwaitingMerge {
		t.Fatalf("state=%s, want awaiting_merge (auto_queued defers to Reconcile)", got.State)
	}
	assertEventExists(t, db, a.ID, merge.EventKindMergeExecuted)
	if eventExists(t, db, a.ID, merge.EventKindMergeCompleted) {
		t.Fatalf("merge_completed written on auto_queued path")
	}
}

// TestExecuteMerge_ConcurrentInstances_NoDuplicateCompletion asserts two parallel ExecuteMerge calls land exactly one merge_completed row.
func TestExecuteMerge_ConcurrentInstances_NoDuplicateCompletion(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	exec := &fakeExecutor{Result: merge.ExecutorResult{
		Outcome: merge.OutcomeMerged, MergeSHA: "x",
	}}
	c1 := newCoordinatorWithExecutor(t, db, exec)
	c2 := newCoordinatorWithExecutor(t, db, exec)

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make([]error, 2)
	go func() { defer wg.Done(); errs[0] = c1.ExecuteMerge(ctx, a.ID, 99, "sha99") }()
	go func() { defer wg.Done(); errs[1] = c2.ExecuteMerge(ctx, a.ID, 99, "sha99") }()
	wg.Wait()

	for _, e := range errs {
		if e != nil {
			t.Fatalf("execute err: %v", e)
		}
	}
	if n := countEvents(t, db, a.ID, merge.EventKindMergeCompleted); n != 1 {
		t.Fatalf("merge_completed count=%d, want 1 (UNIQUE index must suppress)", n)
	}
}

// TestExecuteMerge_CrashAfterShell_BeforeCompletion_ReconcileRecovers asserts a crash post-gh-call pre-completion-event is recovered by Reconcile.
func TestExecuteMerge_CrashAfterShell_BeforeCompletion_ReconcileRecovers(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)
	a := driveToGatesRunning(t, db, "WORK-1")
	// Simulate "crash mid-execute" by calling PrepareMerge directly +
	// stopping. The intent + state transition committed, but no
	// merge_executed or merge_completed event landed.
	c := newCoordinator(t, db, &fakeProber{Map: map[int]merge.ProbeResult{
		77: {Status: merge.PRStatusMerged, MergeSHA: "recovered"},
	}})
	if err := c.PrepareMerge(ctx, a.ID, 77, "sha77"); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// Reconcile probes the fake prober (which reports merged), writes
	// merge_completed (Source=recovery), transitions to Done.
	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, _ := db.GetAgent(ctx, a.ID)
	if got.State != state.AgentDone {
		t.Fatalf("state=%s, want done (Reconcile recovered the dangling intent)", got.State)
	}
	raw := loadEvent(t, db, a.ID, merge.EventKindMergeCompleted)
	var p merge.CompletedPayload
	_ = json.Unmarshal([]byte(raw), &p)
	if p.Source != "recovery" {
		t.Fatalf("source=%q, want recovery", p.Source)
	}
}

// TestClassifyStderr_OutcomeMap asserts the stderr→Outcome classifier maps gh's stable strings to the spec §4.3 enum.
func TestClassifyStderr_OutcomeMap(t *testing.T) {
	cases := []struct {
		stderr string
		want   merge.Outcome
	}{
		{"pull request was already merged", merge.OutcomeAlreadyMerged},
		{"head commit does not match", merge.OutcomeSHADiverged},
		{"API rate limit exceeded", merge.OutcomeRateLimit},
		{"authentication required", merge.OutcomeAuthExpired},
		{"branch protection rule violated", merge.OutcomeBranchProtection},
		{"merge conflict between branches", merge.OutcomeConflict},
		{"pull request is closed", merge.OutcomePRClosed},
		{"random unknown gh output", merge.OutcomeUnknown},
	}
	for _, tc := range cases {
		got := merge.ClassifyStderrForTest(tc.stderr)
		if got != tc.want {
			t.Errorf("classify(%q)=%s, want %s", tc.stderr, got, tc.want)
		}
	}
}

// TestOutcomeTerminality asserts spec §4.3 terminal-vs-recoverable split.
func TestOutcomeTerminality(t *testing.T) {
	terminal := []merge.Outcome{
		merge.OutcomeSHADiverged, merge.OutcomeBranchProtection,
		merge.OutcomeConflict, merge.OutcomePRClosed,
	}
	for _, o := range terminal {
		if !o.IsTerminal() {
			t.Errorf("%s should be terminal", o)
		}
	}
	recoverable := []merge.Outcome{
		merge.OutcomeRateLimit, merge.OutcomeAuthExpired,
		merge.OutcomeTimeout, merge.OutcomeUnknown,
	}
	for _, o := range recoverable {
		if o.IsTerminal() {
			t.Errorf("%s should not be terminal", o)
		}
	}
	if !merge.OutcomeMerged.IsSuccess() || !merge.OutcomeAlreadyMerged.IsSuccess() {
		t.Errorf("success outcomes IsSuccess mismatch")
	}
}
