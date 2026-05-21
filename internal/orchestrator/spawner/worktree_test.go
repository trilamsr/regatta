package spawner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPathForAndBranchForAreDeterministic(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWorktreeManager(WorktreeManagerConfig{RepoRoot: dir})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if got := w.PathFor(7); !filepath.IsAbs(got) {
		t.Fatalf("PathFor must return absolute path, got %s", got)
	}
	if w.PathFor(7) == w.PathFor(8) {
		t.Fatalf("PathFor not unique per agent")
	}
	if got := w.BranchFor(7); got != "regatta/agent-7" {
		t.Fatalf("BranchFor=%q want regatta/agent-7", got)
	}
}

type fakeRunner struct {
	mu       sync.Mutex
	calls    []runnerCall
	failArg  string
	failWith error
}

type runnerCall struct {
	dir, name string
	args      []string
}

func (f *fakeRunner) run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, runnerCall{dir: dir, name: name, args: append([]string(nil), args...)})
	if f.failArg != "" {
		for _, a := range args {
			if a == f.failArg {
				return []byte("synthetic git failure"), f.failWith
			}
		}
	}
	// Simulate `git worktree add` by mkdir-p the target path.
	if len(args) >= 4 && args[0] == "worktree" && args[1] == "add" {
		path := args[len(args)-2]
		if err := os.MkdirAll(path, 0o755); err != nil {
			return nil, err
		}
	}
	if len(args) >= 4 && args[0] == "worktree" && args[1] == "remove" {
		path := args[len(args)-1]
		_ = os.RemoveAll(path)
	}
	return nil, nil
}

func newFakeWM(t *testing.T) (*WorktreeManager, *fakeRunner) {
	t.Helper()
	dir := t.TempDir()
	f := &fakeRunner{}
	w, err := NewWorktreeManager(WorktreeManagerConfig{RepoRoot: dir, Runner: f.run})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return w, f
}

func TestCreateCallsGitWorktreeAdd(t *testing.T) {
	w, f := newFakeWM(t)
	got, err := w.Create(context.Background(), 7, "main")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got != w.PathFor(7) {
		t.Fatalf("Create returned %s, want %s", got, w.PathFor(7))
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("worktree not created on disk: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0].args[0] != "worktree" || f.calls[0].args[1] != "add" {
		t.Fatalf("expected single `git worktree add`, got %+v", f.calls)
	}
}

func TestCreateIsIdempotent(t *testing.T) {
	w, f := newFakeWM(t)
	if _, err := w.Create(context.Background(), 7, "main"); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if _, err := w.Create(context.Background(), 7, "main"); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("second Create should be a no-op, got %d calls", len(f.calls))
	}
}

func TestCreatePropagatesGitFailure(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{failArg: "add", failWith: errors.New("synthetic")}
	w, err := NewWorktreeManager(WorktreeManagerConfig{RepoRoot: dir, Runner: f.run})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = w.Create(context.Background(), 7, "main")
	if err == nil {
		t.Fatal("expected Create to surface the git failure")
	}
	if _, statErr := os.Stat(w.PathFor(7)); statErr == nil {
		t.Fatalf("worktree dir leaked despite failure")
	}
}

func TestRemoveIsIdempotent(t *testing.T) {
	w, _ := newFakeWM(t)
	// Removing a non-existent worktree is fine.
	if err := w.Remove(context.Background(), 99); err != nil {
		t.Fatalf("remove missing: %v", err)
	}
	// Create + remove + remove.
	if _, err := w.Create(context.Background(), 7, "main"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := w.Remove(context.Background(), 7); err != nil {
		t.Fatalf("remove 1: %v", err)
	}
	if _, err := os.Stat(w.PathFor(7)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree dir not removed: %v", err)
	}
	if err := w.Remove(context.Background(), 7); err != nil {
		t.Fatalf("remove 2: %v", err)
	}
}

func TestRemoveFallsBackToRmOnGitFailure(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{}
	w, err := NewWorktreeManager(WorktreeManagerConfig{RepoRoot: dir, Runner: f.run})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	path, err := w.Create(context.Background(), 7, "main")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Switch to a runner that fails `worktree remove`; the manager
	// must fall back to rm and leave the path gone.
	f.failArg = "remove"
	f.failWith = fmt.Errorf("synthetic remove failure")
	if err := w.Remove(context.Background(), 7); err != nil {
		t.Fatalf("remove with git failure: %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("worktree dir not cleaned up after fallback: %v", statErr)
	}
}

func TestNewRejectsMissingRoot(t *testing.T) {
	_, err := NewWorktreeManager(WorktreeManagerConfig{RepoRoot: "/no/such/path"})
	if err == nil {
		t.Fatal("expected error for missing repo root")
	}
}
