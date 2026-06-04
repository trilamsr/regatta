// Package merge implements the intent/outbox pattern that guards
// regatta's first irreversible external side-effect: `gh pr merge`.
// Two crash holes once the substrate calls `gh pr merge`:
// (1) awaiting_merge is excluded from Recover/Heartbeat — a crash straddling the external call is invisible to recovery;
// (2) a crash between the merge call and the completion event re-drives the merge on the next tick; post-merge the branch may be deleted so re-merge errors and recovery misreads work-already-in-main as a failure.
// Fix: write `merge_intent` (nonce = head-SHA) BEFORE the external call, `merge_completed` or `merge_failed` AFTER. Recovery enumerates awaiting_merge agents, looks up the latest intent, re-probes GitHub for real PR state — never blind-flips the FSM, never re-issues against a deleted branch. GitHub-host-agnostic: callers inject a PRProber.
package merge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Merge event kinds written to the events table — stable strings since dashboards + recovery sweep filter by these.
const (
	// EventKindMergeIntent is appended BEFORE the gh pr merge call; payload carries pr_number + head SHA (idempotency nonce).
	EventKindMergeIntent = "merge_intent"
	// EventKindMergeCompleted is appended AFTER a successful merge; payload carries the merge SHA.
	EventKindMergeCompleted = "merge_completed"
	// EventKindMergeFailed signals merge cannot proceed (closed, SHA diverged, gh API permanently rejected); reason string in payload.
	EventKindMergeFailed = "merge_failed"
	// EventKindMergeRecovered marks a dangling intent reconciled by the crash-recovery sweep; distinguishes normal-path from crash-driven completion.
	EventKindMergeRecovered = "merge_recovered"
)

// PRStatus narrows the coordinator's view of GitHub PR state to the outcomes recovery cares about.
type PRStatus int

const (
	// PRStatusUnknown signals the prober could not determine state (transient gh-API error); recovery retries next tick.
	PRStatusUnknown PRStatus = iota
	// PRStatusMerged signals PR is merged regardless of who merged it; write merge_completed, transition to done.
	PRStatusMerged
	// PRStatusOpenSHAMatches signals open AND head SHA matches intent; safe to re-issue (idempotent at same SHA).
	PRStatusOpenSHAMatches
	// PRStatusOpenSHADiverged signals open but head SHA moved; blindly re-merging would ship un-gated code.
	PRStatusOpenSHADiverged
	// PRStatusClosedUnmerged signals PR closed without merge; recovery writes merge_failed.
	PRStatusClosedUnmerged
)

// String renders a PRStatus for logging + merge_failed payload reasons — stable strings since dashboards and the W4 self-improvement detector match on these.
func (s PRStatus) String() string {
	switch s {
	case PRStatusMerged:
		return "merged"
	case PRStatusOpenSHAMatches:
		return "open_sha_matches"
	case PRStatusOpenSHADiverged:
		return "open_sha_diverged"
	case PRStatusClosedUnmerged:
		return "closed_unmerged"
	default:
		return "unknown"
	}
}

// ProbeResult is the prober's full response; MergeSHA is the merge commit SHA (distinct from the intent's head SHA), populated only when Status == PRStatusMerged.
type ProbeResult struct {
	Status   PRStatus
	MergeSHA string
}

// PRProber abstracts the GitHub PR-state lookup so the coordinator stays gh-CLI-free for tests; production shells out to `gh pr view <N> --json state,mergedAt,mergeCommit,headRefOid`. expectedSHA lets the prober compute PRStatusOpenSHADiverged without leaking SHA-comparison logic into the coordinator's reducer.
type PRProber interface {
	Probe(ctx context.Context, prNumber int, expectedSHA string) (ProbeResult, error)
}

// IntentPayload is the EventKindMergeIntent JSON shape; PRNumber + HeadSHA form the idempotency key.
type IntentPayload struct {
	PRNumber int    `json:"pr_number"`
	HeadSHA  string `json:"head_sha"`
}

// CompletedPayload is the EventKindMergeCompleted JSON shape; MergeSHA distinguishes the merge commit from the intent's head SHA, and Source ("merge_call" vs "recovery") lets dashboards count crash-driven completions separately.
type CompletedPayload struct {
	PRNumber int    `json:"pr_number"`
	HeadSHA  string `json:"head_sha"`
	MergeSHA string `json:"merge_sha"`
	Source   string `json:"source"`
}

// FailedPayload is the EventKindMergeFailed JSON shape; Reason is a stable token so the W4 detector can fingerprint occurrences across PRs.
type FailedPayload struct {
	PRNumber int    `json:"pr_number"`
	HeadSHA  string `json:"head_sha"`
	Reason   string `json:"reason"`
	Source   string `json:"source"`
}

// WriteIntent appends the merge_intent audit row BEFORE the external `gh pr merge` call, atomically with the GatesRunning→AwaitingMerge transition (caller supplies tx via state.DB.WithTx) so a crash between the transition and the intent write leaves no orphan; headSHA flows into the gh-CLI merge call so the external mutation stays idempotent against it.
func WriteIntent(ctx context.Context, tx *sql.Tx, db *state.DB, agentID int64, prNumber int, headSHA string) error {
	if prNumber <= 0 {
		return fmt.Errorf("merge: WriteIntent: pr_number must be positive, got %d", prNumber)
	}
	if headSHA == "" {
		return fmt.Errorf("merge: WriteIntent: head_sha required")
	}
	payload, err := json.Marshal(IntentPayload{PRNumber: prNumber, HeadSHA: headSHA})
	if err != nil {
		return fmt.Errorf("merge: marshal intent: %w", err)
	}
	return db.RecordEventTx(ctx, tx, agentID, EventKindMergeIntent, string(payload))
}

// LatestIntent loads the most-recent merge_intent payload for agentID; returns ErrNoIntent when none — recovery treats that as a logic bug (agent reached awaiting_merge without going through the intent gate) rather than silently re-driving.
func LatestIntent(ctx context.Context, db *state.DB, agentID int64) (IntentPayload, error) {
	row := db.SQL().QueryRowContext(ctx,
		`SELECT payload_json FROM events
		  WHERE agent_id = ? AND kind = ?
		  ORDER BY id DESC LIMIT 1`,
		agentID, EventKindMergeIntent)
	var raw string
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IntentPayload{}, ErrNoIntent
		}
		return IntentPayload{}, fmt.Errorf("merge: load intent: %w", err)
	}
	var p IntentPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return IntentPayload{}, fmt.Errorf("merge: decode intent: %w", err)
	}
	return p, nil
}

// ErrNoIntent signals no merge_intent row for the agent; coordinator transitions to crashed with reason "no_intent_on_file" because absence of intent means the substrate never authorized the merge.
var ErrNoIntent = errors.New("merge: no intent recorded for agent")
