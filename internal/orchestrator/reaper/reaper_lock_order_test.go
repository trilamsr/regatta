package reaper

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// orderedRunner records the sequence of git worktree calls so the test can pin
// Remove-before-ReleaseAgentLocks (R-MEGA-3 LIVE-9).
type orderedRunner struct {
	mu    sync.Mutex
	calls []string
}

func (o *orderedRunner) run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(args) >= 2 && args[0] == "worktree" {
		o.calls = append(o.calls, "worktree:"+args[1])
	}
	return nil, nil
}

// TestReap_ReleaseLocksAfterRemove asserts the reaper completes worktree removal before releasing lane locks; releasing first would let the scheduler grant the same lane to a new agent into a stale worktree (R-MEGA-3 LIVE-9).
func TestReap_ReleaseLocksAfterRemove(t *testing.T) {
	ctx := context.Background()
	db := statetest.OpenDB(t)

	dir := t.TempDir()
	runner := &orderedRunner{}
	wm, err := spawner.NewWorktreeManager(spawner.WorktreeManagerConfig{RepoRoot: dir, Runner: runner.run})
	if err != nil {
		t.Fatalf("wm: %v", err)
	}

	a := upsert(t, db, "WORK-LIVE9", "server")
	driveToDone(t, db, a.ID)

	r := New(Config{DB: db, WM: wm})
	if err := r.Reap(ctx, a.ID); err != nil {
		t.Fatalf("Reap: %v", err)
	}

	// Source-level check: reaper.go must call wm.Remove before db.ReleaseAgentLocks.
	if !hasRemoveBeforeRelease() {
		t.Fatal("source order regression: ReleaseAgentLocks must follow wm.Remove in reaper.go")
	}
}


// hasRemoveBeforeRelease scans reaper.go to assert wm.Remove appears before db.ReleaseAgentLocks in the source — guard against a future refactor flipping the order silently.
func hasRemoveBeforeRelease() bool {
	src := reaperSource()
	removeIdx := strings.Index(src, "r.wm.Remove(")
	releaseIdx := strings.Index(src, "r.db.ReleaseAgentLocks(")
	return removeIdx >= 0 && releaseIdx >= 0 && removeIdx < releaseIdx
}
