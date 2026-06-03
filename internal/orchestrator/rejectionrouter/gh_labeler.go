package rejectionrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// GHLabeler labels PRs via the `gh` CLI. The router knows the agent ID;
// the labeler resolves agent → branch → PR number → `gh pr edit`. This
// indirection exists because `gh pr edit` requires a PR number or URL,
// not the work_item_id (which is brief-derived and bears no relation to
// any PR). See issue #476 for the failure mode the prior shape caused:
// every escalation against a brief-derived work_item silently failed to
// label because `gh pr edit <work_item_id>` returns "not found".
//
// `gh pr edit --add-label` is idempotent — re-labeling an already-
// labeled PR exits 0 — so retries are safe.
type GHLabeler struct {
	// Repo is the "owner/name" passed via --repo on every `gh` call.
	// Empty falls back to gh's default-repo resolution (current git
	// remote).
	Repo string

	// BranchFn maps agent ID → the branch name the agent pushes to.
	// Production callers wire this to spawner.WorktreeManager.BranchFor
	// so the lookup matches whatever BranchBase the manager was
	// configured with. Nil falls back to the spawner default
	// ("regatta/agent-<id>"); a drift test guards this default.
	BranchFn func(agentID int64) string

	// Resolver maps a branch name → its open PR number. Nil falls back
	// to `gh pr list --head <branch> --json number`. Tests substitute a
	// stub so the unit suite does not require the gh CLI.
	Resolver func(ctx context.Context, branch string) (int, error)

	// Editor applies the label given a resolved PR number. Nil falls
	// back to `gh pr edit <prNum> --add-label <label>`. Tests substitute
	// a stub.
	Editor func(ctx context.Context, prNum int, label string) error
}

// AddLabel resolves agentID → branch → PR number, then attaches label.
// Errors include the branch + PR number context so the daemon log
// carries enough detail to diagnose without re-running.
func (g GHLabeler) AddLabel(ctx context.Context, agentID int64, label string) error {
	branchFn := g.BranchFn
	if branchFn == nil {
		branchFn = defaultBranchFor
	}
	branch := branchFn(agentID)

	resolver := g.Resolver
	if resolver == nil {
		resolver = g.resolvePRViaGH
	}
	prNum, err := resolver(ctx, branch)
	if err != nil {
		return fmt.Errorf("rejectionrouter: resolve PR for branch %q (agent %d): %w", branch, agentID, err)
	}

	editor := g.Editor
	if editor == nil {
		editor = g.editPRViaGH
	}
	if err := editor(ctx, prNum, label); err != nil {
		return fmt.Errorf("rejectionrouter: gh pr edit %d --add-label %s (branch %q, agent %d): %w",
			prNum, label, branch, agentID, err)
	}
	return nil
}

// defaultBranchFor mirrors spawner.WorktreeManager's default BranchFor
// ("regatta/agent-<id>"). The drift guard lives in
// TestGHLabeler_DefaultBranchFn_MatchesWorktreeManager.
func defaultBranchFor(agentID int64) string {
	return fmt.Sprintf("regatta/agent-%d", agentID)
}

// GHListArgs returns the argv for `gh pr list` such that the branch
// literal cannot be reparsed as a flag. The branch is bound to
// `--head` via the `=` syntax (`--head=<branch>`) so a hostile branch
// string starting with `--` (issue #500) cannot escape its flag-value
// position into the flag namespace. Exported so a drift-guard test
// can pin the binding shape; production callers go through
// (GHLabeler).resolvePRViaGH.
func GHListArgs(repo, branch string) []string {
	args := []string{"pr", "list",
		"--state", "open",
		"--json", "number",
		"--limit", "1",
		"--head=" + branch,
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	return args
}

// GHEditArgs returns the argv for `gh pr edit` with the positional
// PR-number fenced behind `--` so a future shape change cannot let it
// reparse as a flag. Today prNum is an int (cannot start with `--`)
// so the separator is defense-in-depth — pinning the contract at the
// construction site makes the audit cheap.
func GHEditArgs(repo string, prNum int, label string) []string {
	args := []string{"pr", "edit",
		"--add-label", label,
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	args = append(args, "--", strconv.Itoa(prNum))
	return args
}

// ghPRListEntry is the projection of `gh pr list --json number` rows.
type ghPRListEntry struct {
	Number int `json:"number"`
}

// resolvePRViaGH runs `gh pr list --head <branch> --state open --json number`
// and returns the first PR number. Returns an error if no PR matches —
// callers (router.sweepUnlabeled) keep the agent in `escalated` without
// a `labeled` audit row so the next Tick retries.
func (g GHLabeler) resolvePRViaGH(ctx context.Context, branch string) (int, error) {
	args := GHListArgs(g.Repo, branch)
	cmd := exec.CommandContext(ctx, "gh", args...) //nolint:gosec // G204: literal binary; branch is derived from BranchFn(agentID)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return 0, fmt.Errorf("gh pr list: %w (%s)", err, stderr)
	}
	var entries []ghPRListEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return 0, fmt.Errorf("parse gh pr list json: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if len(entries) == 0 {
		return 0, fmt.Errorf("no open PR for branch %q", branch)
	}
	return entries[0].Number, nil
}

// editPRViaGH runs `gh pr edit -- <prNum> --add-label <label>`.
func (g GHLabeler) editPRViaGH(ctx context.Context, prNum int, label string) error {
	args := GHEditArgs(g.Repo, prNum, label)
	cmd := exec.CommandContext(ctx, "gh", args...) //nolint:gosec // G204: literal binary; prNum is an int, label comes from typed config
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
