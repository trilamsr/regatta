package prwatch

import (
	"context"
	"testing"
)

// TestWatcher_EmitsAgentPRDirtyOnce asserts the watcher emits exactly one agent_pr_dirty event when a PR enters DIRTY and stays there across sweeps (#operator-console-S0).
func TestWatcher_EmitsAgentPRDirtyOnce(t *testing.T) {
	lister := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {{Number: 1, HeadRefOid: "sha-x", State: "OPEN", MergeStateStatus: "DIRTY"}},
	}}
	w, db := newTestWatcher(t, lister)
	driveToRunning(t, db, "WORK-1")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := w.Sweep(ctx); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}

	events, err := db.ListEventsByKindSince(ctx, "agent_pr_dirty", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Errorf("agent_pr_dirty events=%d want 1 (dedupe)", len(events))
	}
}

// TestWatcher_AgentPRDirty_ReArmAfterTransitionBack asserts a DIRTY→CLEAN→DIRTY cycle yields two emissions (#operator-console-S0).
func TestWatcher_AgentPRDirty_ReArmAfterTransitionBack(t *testing.T) {
	lister := &stubLister{byBranch: map[string][]PullRequest{
		"regatta/agent-1": {{Number: 1, HeadRefOid: "sha-x", State: "OPEN", MergeStateStatus: "DIRTY"}},
	}}
	w, db := newTestWatcher(t, lister)
	driveToRunning(t, db, "WORK-1")
	ctx := context.Background()

	if err := w.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	lister.byBranch["regatta/agent-1"] = []PullRequest{{Number: 1, HeadRefOid: "sha-x", State: "OPEN", MergeStateStatus: "CLEAN"}}
	if err := w.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	lister.byBranch["regatta/agent-1"] = []PullRequest{{Number: 1, HeadRefOid: "sha-x", State: "OPEN", MergeStateStatus: "DIRTY"}}
	if err := w.Sweep(ctx); err != nil {
		t.Fatal(err)
	}

	events, err := db.ListEventsByKindSince(ctx, "agent_pr_dirty", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("DIRTY→CLEAN→DIRTY cycle: got %d events want 2", len(events))
	}
}
