package rejectionrouter

import (
	"context"
	"fmt"
	"os/exec"
)

// GHLabeler labels PRs via the `gh` CLI. The work_item_id is assumed
// to be the PR number expressed as a string (matching the existing
// adapter convention in cmd/gh-followup-to-items). `gh pr edit
// --add-label` is idempotent — re-labeling an already-labeled PR
// exits 0 — so retries are safe.
type GHLabeler struct {
	// Repo is the "owner/name" passed via --repo. Empty falls back to
	// gh's default-repo resolution (current git remote).
	Repo string
}

// AddLabel runs `gh pr edit <workItemID> --add-label <label>`.
// Returns a wrapped error including stderr so the daemon log carries
// enough context to diagnose without re-running.
func (g GHLabeler) AddLabel(ctx context.Context, workItemID, label string) error {
	args := []string{"pr", "edit", workItemID, "--add-label", label}
	if g.Repo != "" {
		args = append(args, "--repo", g.Repo)
	}
	cmd := exec.CommandContext(ctx, "gh", args...) //nolint:gosec // G204: literal binary; args from typed inputs (work_item_id + label)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh pr edit %s --add-label %s: %w (%s)",
			workItemID, label, err, string(out))
	}
	return nil
}
