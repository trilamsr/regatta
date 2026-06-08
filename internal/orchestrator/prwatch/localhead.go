package prwatch

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

type PathFn func(agentID int64) string

// NewWorktreeLocalHeadFn wires the BUG-1051 divergence probe by shelling git rev-parse HEAD in the agent's worktree; ok=false on any miss so the watcher stays silent rather than emitting a false-positive WARN.
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
