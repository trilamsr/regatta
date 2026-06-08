package prwatch

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// NewWorktreeLocalHeadFn shells git rev-parse HEAD in the agent's worktree for the BUG-1051 divergence probe; ok=false on miss keeps the watcher silent.
func NewWorktreeLocalHeadFn(pathFor func(agentID int64) string) func(ctx context.Context, agentID int64) (string, bool) {
	return func(ctx context.Context, agentID int64) (string, bool) {
		dir := pathFor(agentID)
		if dir == "" {
			return "", false
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(probeCtx, "git", "-C", dir, "rev-parse", "HEAD").Output() //#nosec G204 -- git binary literal; dir from internal PathFor seam.
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
