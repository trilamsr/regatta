// Package merge implements the intent/outbox pattern that guards
// regatta's first irreversible external side-effect: `gh pr merge`.
//
// Why this package exists. PHASE AUTONOMY §11 W2 (auto-merge-on-gate-pass)
// turns merge into a substrate-driven action. Today the merge path is a
// DB status-flip with idempotent journal reconciliation; once the
// substrate calls `gh pr merge`, two crash holes open:
//
//  1. awaiting_merge is excluded from Recover/Heartbeat enumeration —
//     an agent that crashes straddling the external call is invisible
//     to recovery (never re-probed, never requeued).
//  2. A crash between the merge call and the completion event would
//     re-drive the merge on the next tick. Idempotent branch names do
//     not save us: post-merge the branch may be deleted, so the
//     re-merge errors and recovery misreads work-already-in-main as a
//     failure.
//
// c0 closes both before the policy engine (c2) may ship by writing a
// `merge_intent` audit row (nonce = head-SHA) BEFORE the external call
// and a `merge_completed` or `merge_failed` row AFTER. On crash recovery
// the Coordinator enumerates awaiting_merge agents, looks up the latest
// intent, and re-probes GitHub for the real PR state — never blind-flips
// the FSM, never re-issues the merge against a deleted branch.
//
// The package is GitHub-host-agnostic: callers inject a PRProber
// (production: a thin gh-CLI wrapper; tests: a deterministic fake).
package merge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Event kinds written to the events table by the merge coordinator.
// Stable strings — dashboards + the recovery sweep filter by these.
const (
	// EventKindMergeIntent is appended BEFORE the gh pr merge call.
	// Payload carries pr_number + head SHA (the idempotency nonce) so
	// recovery can re-probe without depending on agent.PRSHA staying
	// in sync across crashes.
	EventKindMergeIntent = "merge_intent"

	// EventKindMergeCompleted is appended AFTER a successful merge.
	// Payload carries the merge SHA so audits can correlate the
	// substrate-side row with the GitHub merge commit.
	EventKindMergeCompleted = "merge_completed"

	// EventKindMergeFailed is appended when the merge cannot proceed
	// (PR closed without merge, branch SHA diverged, gh API
	// permanently rejected the call). Payload carries a reason
	// string; recovery transitions the agent to crashed so the
	// existing crashed→pending requeue path applies.
	EventKindMergeFailed = "merge_failed"

	// EventKindMergeRecovered is a thin audit row written when the
	// crash-recovery sweep reconciles a dangling intent — distinguishes
	// "merge completed in the normal path" from "merge reconciled after
	// crash". Audit only; no state implication beyond the sibling
	// merge_completed or merge_failed row written in the same sweep.
	EventKindMergeRecovered = "merge_recovered"
)

// PRStatus is the coordinator's view of GitHub PR state, narrowed to
// the three outcomes recovery cares about. The PRProber translates
// gh-CLI output (state, mergeStateStatus, mergedAt, merge_commit_sha,
// head ref deletion) into one of these.
type PRStatus int

const (
	// PRStatusUnknown — prober could not determine state. Recovery
	// leaves the agent in awaiting_merge so the next tick retries.
	// Surfaced for transient gh-API errors (rate limit, network).
	PRStatusUnknown PRStatus = iota

	// PRStatusMerged — PR is merged. Whether the merge was ours or
	// happened outside regatta (operator clicked the button), the
	// outcome is the same: write merge_completed, transition to done.
	PRStatusMerged

	// PRStatusOpenSHAMatches — PR is still open AND the head SHA
	// matches our recorded intent. Safe to re-issue the merge call
	// (the external call is idempotent against the same SHA when
	// merge_method + branch state are unchanged).
	PRStatusOpenSHAMatches

	// PRStatusOpenSHADiverged — PR is open but the head SHA has moved
	// (force-push or new commits). Recovery treats this as a failure:
	// the substrate's gate verdict was against the old SHA, so blindly
	// re-merging would ship un-gated code.
	PRStatusOpenSHADiverged

	// PRStatusClosedUnmerged — PR was closed without merge. Recovery
	// writes merge_failed; the operator (or a future W4 heuristic)
	// decides whether to re-open.
	PRStatusClosedUnmerged
)

// String renders a PRStatus for logging + merge_failed payload reasons.
// Kept stable — dashboards and the W4 self-improvement detector may
// match on these strings.
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

// ProbeResult is the prober's full response. MergeSHA is populated
// only when Status == PRStatusMerged (the merge commit SHA, distinct
// from the head SHA recorded in the intent).
type ProbeResult struct {
	Status   PRStatus
	MergeSHA string
}

// PRProber abstracts the GitHub PR-state lookup so the coordinator
// stays gh-CLI-free for tests. Production wiring shells out to
// `gh pr view <N> --json state,mergedAt,mergeCommit,headRefOid` and
// translates the response; tests supply a deterministic fake.
type PRProber interface {
	// Probe asks GitHub for the current state of prNumber. The caller
	// passes the SHA recorded in the intent so the prober can compute
	// PRStatusOpenSHADiverged without leaking SHA-comparison logic
	// into the coordinator's reducer.
	Probe(ctx context.Context, prNumber int, expectedSHA string) (ProbeResult, error)
}

// IntentPayload is the JSON shape of EventKindMergeIntent payloads.
// PRNumber + HeadSHA together form the idempotency key. Exported so
// the coordinator's recovery sweep and operator-facing tooling
// (regatta status) can decode without reaching into private fields.
type IntentPayload struct {
	PRNumber int    `json:"pr_number"`
	HeadSHA  string `json:"head_sha"`
}

// CompletedPayload is the JSON shape of EventKindMergeCompleted
// payloads. MergeSHA distinguishes the merge commit SHA from the head
// SHA in the intent — auditors can correlate the two without parsing
// raw gh-CLI output.
type CompletedPayload struct {
	PRNumber int    `json:"pr_number"`
	HeadSHA  string `json:"head_sha"`
	MergeSHA string `json:"merge_sha"`
	// Source distinguishes "normal completion path" ("merge_call")
	// from "reconciled after crash" ("recovery") so dashboards can
	// count crash-driven completions separately.
	Source string `json:"source"`
}

// FailedPayload is the JSON shape of EventKindMergeFailed payloads.
// Reason is a stable token — the W4 self-improvement detector counts
// occurrences across PRs, so a free-form string would defeat the
// fingerprint shape.
type FailedPayload struct {
	PRNumber int    `json:"pr_number"`
	HeadSHA  string `json:"head_sha"`
	Reason   string `json:"reason"`
	Source   string `json:"source"`
}

// WriteIntent appends the merge_intent audit row for the agent. Call
// this BEFORE the external `gh pr merge` invocation, atomically with
// the GatesRunning→AwaitingMerge transition so a crash between the
// transition and the intent write leaves no orphan.
//
// The caller supplies the tx (typically via state.DB.WithTx) so the
// transition + intent row commit as one unit. nonceFromSHA is the head
// SHA of the PR at decision-time; the same SHA flows into the gh-CLI
// merge call so the external mutation stays idempotent against it.
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

// LatestIntent loads the most-recent merge_intent payload for agentID.
// Returns ErrNoIntent when the agent has no intent on file — recovery
// treats that as "agent reached awaiting_merge without going through
// the intent gate", which is a logic bug worth surfacing rather than
// silently re-driving.
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

// ErrNoIntent — LatestIntent found no merge_intent row for the agent.
// The coordinator transitions the agent to crashed with reason
// "no_intent_on_file" rather than treating it as recoverable, because
// the absence of intent means the substrate never authorized the merge.
var ErrNoIntent = errors.New("merge: no intent recorded for agent")
