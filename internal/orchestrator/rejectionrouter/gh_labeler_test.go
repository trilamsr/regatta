package rejectionrouter_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
)

// TestGHLabeler_ResolvesPRNumberFromBranch_NotAgentID pins the fix for issue #476.
func TestGHLabeler_ResolvesPRNumberFromBranch_NotAgentID(t *testing.T) {
	const (
		agentID         int64 = 42
		wantPRNumber          = 1337
		wantBranch            = "regatta/agent-42"
		wantLabel             = "needs-human"
	)

	var gotBranch string
	resolver := func(_ context.Context, branch string) (int, error) {
		gotBranch = branch
		return wantPRNumber, nil
	}
	var gotEditArg string
	editor := func(_ context.Context, prNum int, label string) error {
		gotEditArg = strconv.Itoa(prNum)
		if label != wantLabel {
			t.Errorf("editor label=%q want %q", label, wantLabel)
		}
		return nil
	}

	g := rejectionrouter.GHLabeler{
		BranchFn: func(id int64) string { return fmt.Sprintf("regatta/agent-%d", id) },
		Resolver: resolver,
		Editor:   editor,
	}

	if err := g.AddLabel(context.Background(), agentID, wantLabel); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}

	if gotBranch != wantBranch {
		t.Errorf("resolver got branch=%q want %q", gotBranch, wantBranch)
	}
	// The bug: the original code passed agent.WorkItemID (a string like "F-1"
	// or, in tests, a stringified agent ID) straight to `gh pr edit`. The
	// fix MUST forward the *resolved PR number*, not the agentID and not a
	// WorkItemID. This assertion fails if the editor ever sees "42".
	if gotEditArg == strconv.FormatInt(agentID, 10) {
		t.Errorf("editor received agentID %q; want resolved PR number %d", gotEditArg, wantPRNumber)
	}
	if gotEditArg != strconv.Itoa(wantPRNumber) {
		t.Errorf("editor pr arg=%q want %d", gotEditArg, wantPRNumber)
	}
}

// TestGHLabeler_ResolverError_Propagates ensures a missing PR for the branch surfaces as an error.
func TestGHLabeler_ResolverError_Propagates(t *testing.T) {
	g := rejectionrouter.GHLabeler{
		BranchFn: func(id int64) string { return fmt.Sprintf("regatta/agent-%d", id) },
		Resolver: func(context.Context, string) (int, error) {
			return 0, fmt.Errorf("no PR found")
		},
		Editor: func(context.Context, int, string) error {
			t.Fatal("editor must not be called when resolver fails")
			return nil
		},
	}
	if err := g.AddLabel(context.Background(), 1, "needs-human"); err == nil {
		t.Fatal("AddLabel: want error, got nil")
	}
}

// TestGHLabeler_DefaultBranchFn_MatchesWorktreeManager guards the labeler default against drift from spawner.WorktreeManager's default base.
func TestGHLabeler_DefaultBranchFn_MatchesWorktreeManager(t *testing.T) {
	var gotBranch string
	g := rejectionrouter.GHLabeler{
		Resolver: func(_ context.Context, branch string) (int, error) {
			gotBranch = branch
			return 1, nil
		},
		Editor: func(context.Context, int, string) error { return nil },
	}
	if err := g.AddLabel(context.Background(), 7, "needs-human"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}
	// Default base in spawner.WorktreeManagerConfig is "regatta/agent" → "regatta/agent-7".
	const want = "regatta/agent-7"
	if gotBranch != want {
		t.Errorf("default branch=%q want %q (drift from spawner.WorktreeManager.BranchFor)", gotBranch, want)
	}
}
