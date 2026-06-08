package prwatch

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// PathFn resolves an agent's local worktree path. Same shape as
// spawner.WorktreeManager.PathFor; surfaced as an interface here so
// prwatch does not import the spawner package.
type PathFn func(agentID int64) string

// NewWorktreeLocalHeadFn returns a LocalHeadFn that runs
// `git -C <worktree> rev-parse HEAD` to read the agent's local HEAD.
// Used by serve.go to wire the BUG-1051 branch-diverged probe.
// Missing worktree or non-git directory returns ok=false so the
// watcher stays silent on agents whose checkout the operator deleted.
func NewWorktreeLocalHeadFn(pathFor PathFn) func(agentID int64) (string, bool) {
	return func(agentID int64) (string, bool) {
		dir := pathFor(agentID)
		if dir == "" {
			return "", false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
		if err != nil {
			return "", false
		}
		sha := strings.TrimSpace(string(out))
		if sha == "" {
			return "", false
		}
		return sha, true
	}
}
