// merge_status_test pins the `regatta merge status` operator-facing
// table: awaiting_merge agents + their latest merge_intent + last
// probe outcome. Closes #586 — without this CLI the operator has no
// way to answer "where is the merge queue at?" during long autonomous
// runs.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/merge"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// stageAwaitingMerge drives an agent through pending→…→awaiting_merge
// with a real merge_intent on file so the CLI has substrate to read.
func stageAwaitingMerge(t *testing.T, db *state.DB, workItemID string, prNumber int, headSHA string) state.Agent {
	t.Helper()
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, workItemID, "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	for _, s := range []state.AgentState{
		state.AgentSpawning, state.AgentRunning, state.AgentPROpen, state.AgentGatesRunning,
	} {
		mut := state.AgentMutation{}
		if s == state.AgentPROpen {
			sha := headSHA
			mut.PRSHA = &sha
		}
		if _, err := db.TransitionAgent(ctx, a.ID, s, mut); err != nil {
			t.Fatalf("transition %s: %v", s, err)
		}
	}
	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		return merge.WriteIntent(ctx, tx, db, a.ID, prNumber, headSHA)
	}); err != nil {
		t.Fatalf("write intent: %v", err)
	}
	if _, err := db.TransitionAgent(ctx, a.ID, state.AgentAwaitingMerge, state.AgentMutation{}); err != nil {
		t.Fatalf("transition awaiting_merge: %v", err)
	}
	got, err := db.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	return *got
}

// TestMergeStatusCLI_EmptyQueue_PrintsHeader asserts a quiet substrate prints the table header + "no awaiting merges" footer rather than blank output (#586).
func TestMergeStatusCLI_EmptyQueue_PrintsHeader(t *testing.T) {
	_, path := statetest.OpenDBWithPath(t)

	var stdout, stderr bytes.Buffer
	code := runMergeStatusWith(mergeStatusDeps{
		Stdout: &stdout, Stderr: &stderr, Clock: time.Now,
		DSN: state.DSN(path),
	}, nil)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "no awaiting merges") {
		t.Fatalf("stdout=%q; want footer 'no awaiting merges'", out)
	}
}

// TestMergeStatusCLI_WithAwaitingAgents_PrintsRows asserts each awaiting_merge agent appears with its PR#, head-SHA, intent-at, and last-probe outcome (#586).
func TestMergeStatusCLI_WithAwaitingAgents_PrintsRows(t *testing.T) {
	db, path := statetest.OpenDBWithPath(t)
	stageAwaitingMerge(t, db, "WI-1", 42, "abc123def")

	var stdout, stderr bytes.Buffer
	code := runMergeStatusWith(mergeStatusDeps{
		Stdout: &stdout, Stderr: &stderr, Clock: time.Now,
		DSN: state.DSN(path),
	}, nil)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"WI-1", "42", "abc123d" /* 7-char prefix */} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout=%q; want substring %q", out, want)
		}
	}
}

// TestMergeStatusCLI_NoSubstrateEvent_HandlesGracefully asserts an awaiting_merge agent missing its intent row prints "(no intent)" rather than crashing (#586).
func TestMergeStatusCLI_NoSubstrateEvent_HandlesGracefully(t *testing.T) {
	db, path := statetest.OpenDBWithPath(t)
	ctx := context.Background()
	a, err := db.UpsertPending(ctx, "WI-ORPHAN", "server")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	for _, s := range []state.AgentState{
		state.AgentSpawning, state.AgentRunning, state.AgentPROpen,
		state.AgentGatesRunning, state.AgentAwaitingMerge,
	} {
		if _, err := db.TransitionAgent(ctx, a.ID, s, state.AgentMutation{}); err != nil {
			t.Fatalf("transition %s: %v", s, err)
		}
	}
	// No intent written — the CLI must still print a row.

	var stdout, stderr bytes.Buffer
	code := runMergeStatusWith(mergeStatusDeps{
		Stdout: &stdout, Stderr: &stderr, Clock: time.Now,
		DSN: state.DSN(path),
	}, nil)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "WI-ORPHAN") {
		t.Fatalf("stdout=%q; orphan row missing", out)
	}
	if !strings.Contains(out, "(no intent)") {
		t.Fatalf("stdout=%q; want '(no intent)' marker for the orphan agent", out)
	}
}
