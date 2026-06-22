// merge subcommand tree. Today: `merge status` prints the awaiting_merge
// queue — agents the auto-merge worker (#612) has lined up, with their
// recorded intent + last probe outcome. Operator runs this to answer
// "where is the merge queue at?" during long autonomous loops (#586).
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

const subcmdMerge = "merge"

// mergeStatusDeps injects stdio + clock + DSN so the unit tests don't
// have to env-mutate the DB path. Same shape as costDeps.
type mergeStatusDeps struct {
	Stdout io.Writer
	Stderr io.Writer
	Clock  func() time.Time
	DSN    string
}

func runMerge(args []string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "regatta merge: expected sub-subcommand (status)")
		return 2
	}
	switch args[0] {
	case "status": //nolint:goconst // sub-subcommand verb shared with cost/secret; constants are locally scoped per parent verb for readability.
		return runMergeStatus(args[1:])
	default:
		_, _ = fmt.Fprintf(os.Stderr, "regatta merge: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runMergeStatus(args []string) int {
	return runMergeStatusWith(mergeStatusDeps{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Clock:  time.Now,
		DSN:    state.DSN(defaultDBPath(args)),
	}, args)
}

func runMergeStatusWith(deps mergeStatusDeps, args []string) int {
	fs := flag.NewFlagSet("merge status", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	_ = fs.String("db", defaultStateDB(), "Path to sqlite state DB")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(deps.Stderr, "Usage: regatta merge status [--db <path>]")
		_, _ = fmt.Fprintln(deps.Stderr, "Lists agents in awaiting_merge with their PR + intent + last probe outcome.")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	db, err := state.OpenWithClock(ctx, deps.DSN, deps.Clock)
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta merge status: open db:", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	agents, err := db.ListAgentsByState(ctx, state.AgentAwaitingMerge)
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta merge status: list awaiting_merge:", err)
		return 1
	}

	tw := tabwriter.NewWriter(deps.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "AGENT_ID\tWORK_ITEM\tPR\tHEAD_SHA\tINTENT_AT\tLAST_PROBE\tPROBE_AT")
	for _, a := range agents {
		intent, intentAt, hasIntent := loadIntent(ctx, db, a.ID)
		probeKind, probeAt := loadLastProbeEvent(ctx, db, a.ID)
		pr := "-"
		sha := "(no intent)"
		intentAtStr := "-"
		if hasIntent {
			pr = fmt.Sprintf("%d", intent.PRNumber)
			sha = shortSHA(intent.HeadSHA)
			intentAtStr = intentAt.UTC().Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			a.ID, a.WorkItemID, pr, sha, intentAtStr, probeKind, probeAt,
		)
	}
	if err := tw.Flush(); err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta merge status: flush:", err)
		return 1
	}
	if len(agents) == 0 {
		_, _ = fmt.Fprintln(deps.Stdout, "no awaiting merges.")
	}
	return 0
}

// loadIntent fetches the latest merge_intent payload + its created_at
// for an awaiting_merge agent. The created_at is the seam the operator
// uses to spot a stuck queue — an intent hours old means Reconcile has
// not been able to drive the merge home.
func loadIntent(ctx context.Context, db *state.DB, agentID int64) (merge.IntentPayload, time.Time, bool) {
	row := db.SQL().QueryRowContext(ctx,
		`SELECT payload_json, created_at FROM events
		  WHERE agent_id = ? AND kind = ?
		  ORDER BY id DESC LIMIT 1`,
		agentID, merge.EventKindMergeIntent)
	var raw string
	var created int64
	if err := row.Scan(&raw, &created); err != nil {
		return merge.IntentPayload{}, time.Time{}, false
	}
	var p merge.IntentPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return merge.IntentPayload{}, time.Time{}, false
	}
	return p, time.Unix(created, 0).UTC(), true
}

// loadLastProbeEvent reads the most recent merge_executed / merge_completed /
// merge_failed event for an agent so the operator sees the last gh-side
// signal in the table. Returns ("-", "-") when no probe-equivalent event
// is on file (e.g. brand-new awaiting_merge before the worker drained).
func loadLastProbeEvent(ctx context.Context, db *state.DB, agentID int64) (string, string) {
	row := db.SQL().QueryRowContext(ctx,
		`SELECT kind, payload_json, created_at FROM events
		  WHERE agent_id = ? AND kind IN (?, ?, ?, ?)
		  ORDER BY id DESC LIMIT 1`,
		agentID,
		merge.EventKindMergeExecuted,
		merge.EventKindMergeCompleted,
		merge.EventKindMergeFailed,
		merge.EventKindMergeRecovered,
	)
	var kind, payload string
	var created int64
	if err := row.Scan(&kind, &payload, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) || isNoRowsLike(err) {
			return "-", "-"
		}
		return "-", "-"
	}
	outcome := kind
	if kind == merge.EventKindMergeExecuted {
		// merge_executed payload carries the outcome enum — surface that
		// rather than the generic kind so the operator sees e.g. "merged"
		// or "sha_diverged" directly.
		var p merge.ExecutedPayload
		if err := json.Unmarshal([]byte(payload), &p); err == nil && p.Outcome != "" {
			outcome = p.Outcome
		}
	}
	return outcome, time.Unix(created, 0).UTC().Format(time.RFC3339)
}

// shortSHA returns the 7-char prefix git+gh use in their own outputs;
// the operator's eye is calibrated to that length.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// isNoRowsLike is the duck-typed sql.ErrNoRows check — Scan wraps
// ErrNoRows differently across driver versions, so the substring
// fallback keeps the empty-queue path quiet on every driver.
func isNoRowsLike(err error) bool {
	return err != nil && (errors.Is(err, sql.ErrNoRows) ||
		err.Error() == "sql: no rows in result set")
}
