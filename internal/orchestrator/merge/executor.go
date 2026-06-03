package merge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Outcome classifies the result of a single `gh pr merge` invocation.
// Stable strings — dashboards filter on these, the W4 self-improvement
// detector matches on them.
type Outcome string

// Outcome enum — split between terminal (write merge_failed) and
// recoverable (leave in AwaitingMerge, Reconcile probes). See spec §4.3.
const (
	OutcomeMerged           Outcome = "merged"
	OutcomeAlreadyMerged    Outcome = "already_merged"
	OutcomeAutoQueued       Outcome = "auto_queued"
	OutcomeSHADiverged      Outcome = "sha_diverged"
	OutcomeBranchProtection Outcome = "branch_protection"
	OutcomeConflict         Outcome = "conflict"
	OutcomePRClosed         Outcome = "pr_closed"
	OutcomeRateLimit        Outcome = "rate_limit"
	OutcomeAuthExpired      Outcome = "auth_expired"
	OutcomeGhNotInstalled   Outcome = "gh_not_installed"
	OutcomeTimeout          Outcome = "timeout"
	// OutcomeBranchDeleted — gh refused because the head branch was
	// already deleted (race: an earlier sweep + --delete-branch landed,
	// then a duplicate execute call hits the empty ref). Spec §4.3
	// completion-event semantics: equivalent to AlreadyMerged — success
	// path writes merge_completed + transitions Done (#655).
	OutcomeBranchDeleted Outcome = "branch_deleted"
	OutcomeUnknown       Outcome = "unknown"
)

// IsTerminal — terminal outcomes write merge_failed and crash the
// agent for requeue; non-terminal outcomes leave the agent in
// AwaitingMerge so Reconcile + the next tick can re-probe.
// BranchDeleted is terminal-success (handled via IsSuccess) so it
// drives the merge_completed path, not merge_failed.
func (o Outcome) IsTerminal() bool {
	switch o {
	case OutcomeSHADiverged, OutcomeBranchProtection, OutcomeConflict, OutcomePRClosed:
		return true
	}
	return false
}

// IsSuccess — completion path writes merge_completed and transitions
// to Done. BranchDeleted joins AlreadyMerged as success-via-error: gh
// rejected the call but the underlying invariant (PR landed) holds.
func (o Outcome) IsSuccess() bool {
	return o == OutcomeMerged || o == OutcomeAlreadyMerged || o == OutcomeBranchDeleted
}

// ExecutorResult is the executor's structured response — pure data so
// the coordinator's reducer is deterministic.
type ExecutorResult struct {
	Outcome    Outcome
	MergeSHA   string
	ExitCode   int
	DurationMs int64
	Stderr     string
}

// Executor abstracts the gh-CLI shell-out so coordinator tests stay
// process-fork-free. Production wires the GhExecutor below; tests
// substitute a deterministic fake.
type Executor interface {
	Merge(ctx context.Context, prNumber int, headSHA string) (ExecutorResult, error)
}

// GhExecutor shells out to `gh pr merge` and classifies stderr into an
// Outcome. The 30s timeout caps wall-clock for the worst GitHub-API
// edge; gh itself completes in ~500 ms typical.
type GhExecutor struct {
	// Bin overrides the gh binary path for tests; empty defaults to "gh"
	// resolved via $PATH at exec time.
	Bin string
	// Timeout caps the gh call; zero defaults to 30s.
	Timeout time.Duration
}

// Merge invokes `gh pr merge <N> --squash --auto --delete-branch
// --match-head-commit <sha>`. The --match-head-commit guard refuses if
// the PR head moved between PrepareMerge and Merge (gh ≥ 2.40).
func (g GhExecutor) Merge(ctx context.Context, prNumber int, headSHA string) (ExecutorResult, error) {
	bin := g.Bin
	if bin == "" {
		bin = "gh"
	}
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := []string{"pr", "merge", strconv.Itoa(prNumber),
		"--squash", "--auto", "--delete-branch",
		"--match-head-commit", headSHA}
	cmd := exec.CommandContext(cctx, bin, args...) //nolint:gosec // G204: literal binary; prNumber int + headSHA from intent row
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start).Milliseconds()
	res := ExecutorResult{
		Stderr:     strings.TrimSpace(stderr.String()),
		DurationMs: dur,
	}
	// Timeout via context-cancel manifests as exec.ExitError with
	// non-zero exit, but cctx.Err() is the authoritative signal.
	if cctx.Err() == context.DeadlineExceeded {
		res.Outcome = OutcomeTimeout
		return res, fmt.Errorf("merge: gh pr merge %d: %w", prNumber, cctx.Err())
	}
	if err != nil {
		// gh missing on host: exec.LookPath-style error wraps
		// exec.ErrNotFound. Surfaced as terminal-at-boot upstream.
		if errors.Is(err, exec.ErrNotFound) {
			res.Outcome = OutcomeGhNotInstalled
			return res, fmt.Errorf("merge: gh not installed: %w", err)
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		}
		res.Outcome = classifyStderr(res.Stderr)
		// Success-via-error-exit: gh returned non-zero but the
		// underlying invariant (PR landed) holds. Surface no error
		// upstream so ExecuteMerge runs the completion path.
		if res.Outcome.IsSuccess() {
			return res, nil
		}
		return res, fmt.Errorf("merge: gh pr merge %d: %w (%s)", prNumber, err, res.Stderr)
	}
	// Exit 0: distinguish synchronous merge from auto-queue-accepted
	// (gh prints "Pull request ... merged" vs "auto-merge enabled").
	res.Outcome = classifyStdoutSuccess(strings.TrimSpace(stdout.String()), res.Stderr)
	res.MergeSHA = extractMergeSHA(stdout.String() + " " + stderr.String())
	return res, nil
}

// classifyStderr maps gh's stderr substrings to an Outcome. The set is
// stable for gh ≥ 2.40; new gh strings degrade to OutcomeUnknown which
// the coordinator treats as recoverable (next sweep retries).
//
// BranchDeleted ordering: tested BEFORE AlreadyMerged because the gh
// stderr for the delete-branch-after-merge race is
// "pull request was already merged; branch was already deleted" — both
// substrings match, and the spec §4.3 completion-event semantics are
// equivalent, but BranchDeleted carries the more specific signal for
// dashboards (#655). Both outcomes drive IsSuccess so the FSM
// transition is identical.
//
// PRClosed matches gh's two phrasings: pre-2.45 prints "pull request
// is closed"; 2.45+ prints "pull request <N> is not in the open
// state". Both yield OutcomePRClosed (#657).
//
// Anchors are tight to avoid over-match: "branch was" + "deleted"
// requires the gh-specific phrasing rather than any stray "deleted"
// substring; "not in open state" + "pull request" requires gh's full
// idiom.
func classifyStderr(s string) Outcome {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "branch was already deleted"), strings.Contains(l, "branch has been deleted"):
		return OutcomeBranchDeleted
	case strings.Contains(l, "already merged"), strings.Contains(l, "pull request was already merged"):
		return OutcomeAlreadyMerged
	case strings.Contains(l, "head commit does not match"), strings.Contains(l, "match-head-commit"):
		return OutcomeSHADiverged
	case strings.Contains(l, "rate limit"):
		return OutcomeRateLimit
	case strings.Contains(l, "authentication"), strings.Contains(l, "bad credentials"):
		return OutcomeAuthExpired
	case strings.Contains(l, "branch protection"), strings.Contains(l, "required status check"):
		return OutcomeBranchProtection
	case strings.Contains(l, "merge conflict"), strings.Contains(l, "conflicts must be resolved"):
		return OutcomeConflict
	case strings.Contains(l, "pull request is closed"),
		strings.Contains(l, "closed without"),
		strings.Contains(l, "not in the open state"),
		strings.Contains(l, "not in open state"):
		return OutcomePRClosed
	case strings.Contains(l, "command not found"), strings.Contains(l, "executable file not found"):
		return OutcomeGhNotInstalled
	case strings.Contains(l, "context deadline exceeded"), strings.Contains(l, "operation timed out"):
		return OutcomeTimeout
	}
	return OutcomeUnknown
}

// classifyStdoutSuccess distinguishes synchronous merge from queue-
// accepted. gh prints "Merged pull request" on the sync path and
// "Pull request ... will be automatically merged" on the --auto queue
// path.
func classifyStdoutSuccess(stdout, stderr string) Outcome {
	c := strings.ToLower(stdout + " " + stderr)
	if strings.Contains(c, "auto-merge") || strings.Contains(c, "will be automatically") ||
		strings.Contains(c, "auto merge enabled") || strings.Contains(c, "automatically merged when") {
		return OutcomeAutoQueued
	}
	return OutcomeMerged
}

// extractMergeSHA pulls the merge commit SHA from gh output when
// present. Not all gh versions surface it on stdout; absence is benign
// — Reconcile probes can pick it up later via `gh pr view`.
func extractMergeSHA(out string) string {
	// gh 2.40+ on success prints e.g. "Merged pull request #N (sha=abc...)"
	// pattern is loose; we return empty when nothing parses.
	for _, tok := range strings.Fields(out) {
		t := strings.TrimSpace(tok)
		if len(t) >= 7 && len(t) <= 64 && isHex(t) {
			return t
		}
	}
	return ""
}

func isHex(s string) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// EventKindMergeExecuted is the audit row written for every gh merge
// attempt — distinct from merge_completed (which is FSM-driving). One
// merge_executed row per attempt so dashboards can count gh-side
// latency + retries.
const EventKindMergeExecuted = "merge_executed"

// ExecutedPayload is the JSON shape of EventKindMergeExecuted. Outcome
// is the stable enum, NOT the gh stderr text — dashboards filter on it.
type ExecutedPayload struct {
	PRNumber   int    `json:"pr_number"`
	HeadSHA    string `json:"head_sha"`
	MergeSHA   string `json:"merge_sha,omitempty"`
	Outcome    string `json:"outcome"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
}

// ExecuteMerge wires PrepareMerge → Executor.Merge → completion-event
// in one call. Crash recovery is delegated to c0's Reconcile sweep: a
// crash anywhere after PrepareMerge commits leaves the agent in
// AwaitingMerge with an intent on file, and Reconcile re-probes
// GitHub on the next sweep.
//
// Outcome handling per spec §4.1:
//   - Success path (Merged/AlreadyMerged): write merge_completed +
//     merge_executed, transition to Done.
//   - AutoQueued: write merge_executed only; agent stays in
//     AwaitingMerge so Reconcile drives to Done when the queue drains.
//   - Terminal failure (Conflict/BranchProtection/PRClosed/
//     SHADiverged): write merge_failed + merge_executed, transition
//     to Crashed for requeue.
//   - Transient (RateLimit/AuthExpired/Timeout/Unknown): write
//     merge_executed only, return the error so the caller can back
//     off. Reconcile is the long-tail safety net.
//
// SetExecutor must have been called; without an Executor ExecuteMerge
// returns ErrNoExecutor so a wiring bug surfaces at the first call.
func (c *Coordinator) ExecuteMerge(ctx context.Context, agentID int64, prNumber int, headSHA string) error {
	if c.executor == nil {
		return ErrNoExecutor
	}
	if err := c.PrepareMerge(ctx, agentID, prNumber, headSHA); err != nil {
		// ErrInvalidTransition = another instance already moved the
		// agent; treat as race-loss no-op so the winner drives the
		// outcome.
		if errors.Is(err, state.ErrInvalidTransition) {
			c.log.Info("merge.execute_race_lost",
				"agent_id", agentID, "pr_number", prNumber)
			return nil
		}
		return fmt.Errorf("merge: execute prepare: %w", err)
	}
	res, execErr := c.executor.Merge(ctx, prNumber, headSHA)
	// merge_executed is audit-only — recorded for every attempt,
	// including failures. recordMergeEvent suppresses UNIQUE
	// violations, but merge_executed is NOT in the unique index, so
	// duplicates are expected across retries.
	if err := c.writeExecutedEvent(ctx, agentID, prNumber, headSHA, res); err != nil {
		c.log.Warn("merge.executed_event_write_failed",
			"agent_id", agentID, "err", err.Error())
	}
	if execErr == nil && res.Outcome.IsSuccess() {
		return c.markCompletedFromMergeCall(ctx, agentID, prNumber, headSHA, res.MergeSHA)
	}
	if execErr == nil && res.Outcome == OutcomeAutoQueued {
		c.log.Info("merge.auto_queued",
			"agent_id", agentID, "pr_number", prNumber, "head_sha", headSHA)
		return nil
	}
	if res.Outcome.IsTerminal() {
		return c.markFailedFromMergeCall(ctx, agentID, prNumber, headSHA, string(res.Outcome))
	}
	// Transient — return error so the caller (mergeWorker) can back
	// off. Agent stays in AwaitingMerge; Reconcile owns long-tail.
	c.log.Warn("merge.execute_transient",
		"agent_id", agentID, "pr_number", prNumber,
		"outcome", string(res.Outcome), "err", errStr(execErr))
	if execErr != nil {
		return execErr
	}
	return fmt.Errorf("merge: transient outcome %s", res.Outcome)
}

// writeExecutedEvent records the per-attempt audit row. Failures are
// non-fatal — the executed event is observability-only; FSM
// transitions ride on merge_completed/merge_failed.
func (c *Coordinator) writeExecutedEvent(ctx context.Context, agentID int64, prNumber int, headSHA string, res ExecutorResult) error {
	payload, err := json.Marshal(ExecutedPayload{
		PRNumber:   prNumber,
		HeadSHA:    headSHA,
		MergeSHA:   res.MergeSHA,
		Outcome:    string(res.Outcome),
		ExitCode:   res.ExitCode,
		DurationMs: res.DurationMs,
	})
	if err != nil {
		return fmt.Errorf("marshal executed: %w", err)
	}
	return c.db.RecordEvent(ctx, agentID, EventKindMergeExecuted, string(payload))
}

// markCompletedFromMergeCall mirrors markCompleted but stamps
// Source = "merge_call" so dashboards can split normal-path
// completions from crash-recovery completions.
func (c *Coordinator) markCompletedFromMergeCall(ctx context.Context, agentID int64, prNumber int, headSHA, mergeSHA string) error {
	a, err := c.db.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("merge: get agent: %w", err)
	}
	payload, err := json.Marshal(CompletedPayload{
		PRNumber: prNumber,
		HeadSHA:  headSHA,
		MergeSHA: mergeSHA,
		Source:   "merge_call",
	})
	if err != nil {
		return fmt.Errorf("merge: marshal completed: %w", err)
	}
	if err := c.recordMergeEvent(ctx, *a, EventKindMergeCompleted, string(payload)); err != nil {
		return fmt.Errorf("merge: record completed: %w", err)
	}
	if _, err := c.db.TransitionAgent(ctx, agentID, state.AgentDone, state.AgentMutation{}); err != nil {
		if !errors.Is(err, state.ErrInvalidTransition) {
			return fmt.Errorf("merge: transition done: %w", err)
		}
	}
	c.log.Info("merge.completed_normal_path",
		"agent_id", agentID, "pr_number", prNumber,
		"head_sha", headSHA, "merge_sha", mergeSHA)
	return nil
}

// markFailedFromMergeCall mirrors markFailed for the normal-path
// terminal outcome — gh told us this can't merge (closed, conflict,
// branch protection, SHA diverged).
func (c *Coordinator) markFailedFromMergeCall(ctx context.Context, agentID int64, prNumber int, headSHA, reason string) error {
	a, err := c.db.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("merge: get agent: %w", err)
	}
	payload, err := json.Marshal(FailedPayload{
		PRNumber: prNumber,
		HeadSHA:  headSHA,
		Reason:   reason,
		Source:   "merge_call",
	})
	if err != nil {
		return fmt.Errorf("merge: marshal failed: %w", err)
	}
	if err := c.recordMergeEvent(ctx, *a, EventKindMergeFailed, string(payload)); err != nil {
		return fmt.Errorf("merge: record failed: %w", err)
	}
	if _, err := c.db.TransitionAgent(ctx, agentID, state.AgentCrashed, state.AgentMutation{}); err != nil {
		if !errors.Is(err, state.ErrInvalidTransition) {
			return fmt.Errorf("merge: transition crashed: %w", err)
		}
	}
	if _, err := c.db.ReleaseAgentLocks(ctx, agentID); err != nil {
		return fmt.Errorf("merge: release locks: %w", err)
	}
	c.log.Warn("merge.failed_normal_path",
		"agent_id", agentID, "pr_number", prNumber,
		"head_sha", headSHA, "reason", reason)
	return nil
}

// SetExecutor wires the executor seam. Optional — without an executor
// ExecuteMerge returns ErrNoExecutor so the legacy operator-clicks-
// merge path still works (Coordinator.Reconcile remains functional).
func (c *Coordinator) SetExecutor(e Executor) { c.executor = e }

// ErrNoExecutor — ExecuteMerge was called on a Coordinator that has
// no Executor wired. Production wiring calls SetExecutor at boot
// after the gh-version check passes.
var ErrNoExecutor = errors.New("merge: no executor wired")

// errStr renders nil-safe error text for log lines.
func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

