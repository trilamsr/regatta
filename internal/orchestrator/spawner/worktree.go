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

// WorktreeManager creates and removes git worktrees under
// <RepoRoot>/.regatta/worktrees. Deterministic path per agent so a
// restart can find and reap orphaned trees without consulting state.DB.
// Create/Remove serialize on a mutex — `git worktree` already holds a
// global git lock per call, so coarse serialization is simplest.
type WorktreeManager struct {
	repoRoot   string
	branchBase string
	runner     CommandRunner
	mu         sync.Mutex
}

// CommandRunner is the seam tests use to substitute a fake git
// binary. Production callers leave it nil; New supplies execRunner.
type CommandRunner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

type WorktreeManagerConfig struct {
	// RepoRoot — absolute path of the repo the manager owns worktrees for.
	RepoRoot string

	// BranchBase — branch prefix for new worktrees (default
	// "regatta/agent"; agent 7 → "regatta/agent-7").
	BranchBase string

	// Runner overrides the command runner — tests inject a fake git;
	// production MUST NOT.
	Runner CommandRunner
}

// NewWorktreeManager constructs a manager. RepoRoot is absolutised;
// nonexistent paths are rejected eagerly.
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

// PathFor — deterministic worktree path; identical across processes
// so a restart can locate an orphaned tree.
func (w *WorktreeManager) PathFor(agentID int64) string {
	return filepath.Join(w.repoRoot, ".regatta", "worktrees", fmt.Sprintf("agent-%d", agentID))
}

func (w *WorktreeManager) BranchFor(agentID int64) string {
	return fmt.Sprintf("%s-%d", w.branchBase, agentID)
}

// Create runs `git worktree add` at PathFor(agentID) tracking a new
// branch off baseRef. Idempotent — an existing worktree (e.g. from a
// crashed run) returns the existing path.
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
	// `-B` resets the branch so a stale branch from a previous run
	// does not block re-creation.
	out, err := w.runner(ctx, w.repoRoot, "git", "worktree", "add", "-B", branch, path, baseRef)
	if err != nil {
		return "", fmt.Errorf("spawner: git worktree add: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return path, nil
}

// Remove tears down the worktree but leaves the branch intact (it
// may carry agent work; deleting loses history). Idempotent — nil
// when the worktree does not exist.
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
		// Fall back to plain rm + prune so leftover worktree metadata
		// does not haunt future operations.
		if rmErr := os.RemoveAll(path); rmErr != nil {
			return fmt.Errorf("spawner: git worktree remove: %w (output: %s); rm fallback: %w", err, strings.TrimSpace(string(out)), rmErr)
		}
		_, _ = w.runner(ctx, w.repoRoot, "git", "worktree", "prune")
	}
	return nil
}

// execRunner runs the command in dir with no env changes and returns
// combined output.
func execRunner(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}
