//go:build unix

// tests/selfhost_smoke_test.go is the Phase S1 acceptance gate: it
// drives the self-host loop end-to-end in-process — markdown adapter
// list → AdapterSync mirror into work_items → scheduler reserve →
// spawner.Stub Spawn → stub.Complete writes the PR-merge journal — and
// asserts the four invariants of the loop fire in order.
//
// Build-tagged unix: orchestrator.PollOnce calls lockfile.Acquire,
// which is POSIX-only (see internal/orchestrator/lockfile header).
//
// Per spec docs/engineer/specs/2026-06-02-s1-t5-smoke-test.md
// (cites: feedback_research_design_principles, feedback_decision_priority,
// feedback_grade_rubric, feedback_self_improvement, feedback_pr_body_file_only,
// feedback_test_godoc_one_line).
package tests

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/adapter"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/program"
)

// TestSelfHost_FollowupIntakeToPRMerge drives the S1 loop end-to-end on one fixture followup item; asserts intake → spawn → PR-observation → merged.
func TestSelfHost_FollowupIntakeToPRMerge(t *testing.T) {
	const wantID = "SMOKE-001"
	ctx := context.Background()

	// Fixture root holds .regatta/items/SMOKE-001.md — the markdown
	// adapter scans <Root>/.regatta/items/.
	fixtureRoot := filepath.Join(repoRoot(t), "tests", "testdata", "selfhost_smoke")

	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := state.Open(ctx, state.DSN(dbPath))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ad, err := adapter.NewMarkdownCatalog(adapter.MarkdownCatalogConfig{Root: fixtureRoot})
	if err != nil {
		t.Fatalf("NewMarkdownCatalog: %v", err)
	}
	syncer, err := adaptersync.New(adaptersync.Config{Adapter: ad, DB: db})
	if err != nil {
		t.Fatalf("adaptersync.New: %v", err)
	}
	// Empty briefs FS — the smoke loop drives the adapter path only;
	// brief-source ingestion is out of scope per spec §3.
	loader, err := program.NewBriefLoader(program.BriefLoaderConfig{
		FS: fstest.MapFS{}, DB: db, Keyring: map[string][]byte{},
	})
	if err != nil {
		t.Fatalf("NewBriefLoader: %v", err)
	}
	stub := spawner.New(spawner.Config{DB: db})
	sched := scheduler.New(db, scheduler.Config{LockTTL: time.Minute})
	o := orchestrator.New(orchestrator.Config{
		AdapterSync: syncer,
		BriefLoader: loader,
		DB:          db,
		Scheduler:   sched,
		Spawner:     stub,
		DBPath:      dbPath,
	})

	if err := o.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	// A1 intake — markdown adapter parsed SMOKE-001 and AdapterSync
	// mirrored it into work_items with Source=adapter.
	wi, err := db.GetWorkItem(ctx, wantID)
	if err != nil {
		t.Fatalf("A1 intake: GetWorkItem(%s): %v", wantID, err)
	}
	if wi.Source != state.SourceAdapter {
		t.Fatalf("A1 intake: source=%q want %q", wi.Source, state.SourceAdapter)
	}
	if wi.Status != state.WorkStatusPlanned {
		t.Fatalf("A1 intake: status=%q want %q (pre-spawn)", wi.Status, state.WorkStatusPlanned)
	}

	if err := o.ScheduleOnce(ctx); err != nil {
		t.Fatalf("ScheduleOnce: %v", err)
	}

	// A2 spawn — scheduler reserved SMOKE-001 and the orchestrator
	// handed it to the spawner exactly once.
	calls := stub.Calls()
	if len(calls) != 1 {
		t.Fatalf("A2 spawn: stub.Calls()=%d want 1: %+v", len(calls), calls)
	}
	if calls[0].WorkItemID != wantID {
		t.Fatalf("A2 spawn: calls[0].WorkItemID=%q want %q", calls[0].WorkItemID, wantID)
	}

	// A3 PR observation — Complete writes the journal payload, which
	// carries the synthetic PR URL through to the on-disk row. The
	// loop's PR-observed signal is the journal write, not a live GH
	// round trip (spec §3).
	const prURL = "https://github.com/trilamsr/regatta/pull/SMOKE"
	prPayload := json.RawMessage(`{"pr_url":"` + prURL + `","completed":true}`)
	if err := stub.Complete(ctx, wantID, prPayload); err != nil {
		t.Fatalf("stub.Complete: %v", err)
	}
	out, err := db.GetLatestOutput(ctx, wantID)
	if err != nil {
		t.Fatalf("A3 PR observation: GetLatestOutput: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out.OutputJSON), &parsed); err != nil {
		t.Fatalf("A3 PR observation: journal payload not JSON: %v (%q)", err, out.OutputJSON)
	}
	if parsed["pr_url"] != prURL {
		t.Fatalf("A3 PR observation: journal pr_url=%v want %q", parsed["pr_url"], prURL)
	}

	// A4 state transition — Complete flipped planned → merged. This
	// is the loop's terminal pre-operator-merge state per brief §3.
	final, err := db.GetWorkItem(ctx, wantID)
	if err != nil {
		t.Fatalf("A4 state transition: GetWorkItem: %v", err)
	}
	if final.Status != state.WorkStatusMerged {
		t.Fatalf("A4 state transition: status=%q want %q", final.Status, state.WorkStatusMerged)
	}
}
