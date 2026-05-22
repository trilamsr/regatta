package spawner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// WorktreeManager creates and removes git worktrees rooted under
// <RepoRoot>/.regatta/worktrees. Each agent gets a deterministic path
// keyed by agent ID so a restart can find and reap orphaned trees
// without consulting state.DB.
//
// Concurrency: Create/Remove serialize on an internal mutex; the
// underlying `git worktree` commands hold a global git lock for the
// duration of each call, so coarse serialization is the simplest
// correctness-preserving approach.
type WorktreeManager struct {
	repoRoot   string
	branchBase string
	runner     CommandRunner
	mu         sync.Mutex
}

// CommandRunner is the seam tests use to substitute a fake git
// binary. Production callers leave it nil; New supplies execRunner.
type CommandRunner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

// WorktreeManagerConfig is the constructor input.
type WorktreeManagerConfig struct {
	// RepoRoot is the absolute path of the repo whose worktrees this
	// manager owns.
	RepoRoot string

	// BranchBase is the branch prefix used when creating new
	// worktrees. Default: "regatta/agent". A worktree for agent 7
	// uses branch "regatta/agent-7".
	BranchBase string

	// Runner overrides the command runner. Tests use this to inject
	// a fake git binary; production code MUST NOT call this.
	Runner CommandRunner
}

// NewWorktreeManager constructs a manager. RepoRoot is resolved to
// an absolute path; nonexistent paths are rejected eagerly.
func NewWorktreeManager(cfg WorktreeManagerConfig) (*WorktreeManager, error) {
	if cfg.RepoRoot == "" {
		return nil, errors.New("spawner: WorktreeManagerConfig.RepoRoot is required")
	}
	abs, err := filepath.Abs(cfg.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("spawner: resolve repo root: %w", err)
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("spawner: repo root %s is not a directory", abs)
	}
	if cfg.BranchBase == "" {
		cfg.BranchBase = "regatta/agent"
	}
	runner := cfg.Runner
	if runner == nil {
		runner = execRunner
	}
	return &WorktreeManager{
		repoRoot:   abs,
		branchBase: cfg.BranchBase,
		runner:     runner,
	}, nil
}

// PathFor returns the deterministic worktree path for agentID. The
// path is identical across processes so a restart can locate an
// orphaned tree.
func (w *WorktreeManager) PathFor(agentID int64) string {
	return filepath.Join(w.repoRoot, ".regatta", "worktrees", fmt.Sprintf("agent-%d", agentID))
}

// BranchFor returns the deterministic branch name for agentID.
func (w *WorktreeManager) BranchFor(agentID int64) string {
	return fmt.Sprintf("%s-%d", w.branchBase, agentID)
}

// Create runs `git worktree add` at PathFor(agentID) tracking a new
// branch off baseRef. Returns the absolute worktree path on success.
// If the worktree already exists (e.g. from a previous crashed run),
// the call is a no-op and returns the existing path.
func (w *WorktreeManager) Create(ctx context.Context, agentID int64, baseRef string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	path := w.PathFor(agentID)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("spawner: stat %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("spawner: mkdir worktrees parent: %w", err)
	}
	if baseRef == "" {
		baseRef = "HEAD"
	}
	branch := w.BranchFor(agentID)
	// `-B` creates or resets the branch so a stale branch from a
	// previous run does not block re-creation.
	out, err := w.runner(ctx, w.repoRoot, "git", "worktree", "add", "-B", branch, path, baseRef)
	if err != nil {
		return "", fmt.Errorf("spawner: git worktree add: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return path, nil
}

// Remove tears down the worktree at PathFor(agentID). The branch is
// left intact (it may carry the agent's work; deleting it loses
// history). Returns nil if the worktree does not exist; idempotent.
func (w *WorktreeManager) Remove(ctx context.Context, agentID int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	path := w.PathFor(agentID)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("spawner: stat %s: %w", path, err)
	}
	out, err := w.runner(ctx, w.repoRoot, "git", "worktree", "remove", "--force", path)
	if err != nil {
		// Fall back to a plain rm; the worktree-list metadata is
		// pruned lazily by git on the next operation.
		if rmErr := os.RemoveAll(path); rmErr != nil {
			return fmt.Errorf("spawner: git worktree remove: %w (output: %s); rm fallback: %w", err, strings.TrimSpace(string(out)), rmErr)
		}
		// Run `git worktree prune` so the leftover metadata does not
		// haunt future operations.
		_, _ = w.runner(ctx, w.repoRoot, "git", "worktree", "prune")
	}
	return nil
}

// execRunner is the production CommandRunner. It runs the command in
// dir with no environment changes and returns the combined output.
func execRunner(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}
