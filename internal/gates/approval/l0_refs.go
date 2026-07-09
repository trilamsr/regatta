package approval

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// L0CheckRefs runs L0 against the diff between baseRef and headRef in repoDir.
// Uses merge-base to avoid the TOCTOU where a base-branch tightening reads
// as a PR-side removal. Pairs with L0CheckMergeCommit for the §7 re-run.
func L0CheckRefs(ctx context.Context, cfg L0Config, repoDir, baseRef, headRef string) (schemas.GateResult, error) {
	mergeBase, err := gitMergeBase(ctx, repoDir, baseRef, headRef)
	if err != nil {
		return schemas.GateResult{}, fmt.Errorf("merge-base %s %s: %w", baseRef, headRef, err)
	}
	diff, err := gitDiff(ctx, repoDir, mergeBase, headRef)
	if err != nil {
		return schemas.GateResult{}, fmt.Errorf("diff %s..%s: %w", mergeBase, headRef, err)
	}
	return L0Check(cfg, L0ParseUnifiedDiff(diff)), nil
}

// L0CheckMergeCommit re-runs L0 on a merge commit (testdata/README.md §7).
// Catches PR-head-passes-then-base-tightens regression by diffing merge
// tree against post-tighten first parent.
func L0CheckMergeCommit(ctx context.Context, cfg L0Config, repoDir, mergeCommit string) (schemas.GateResult, error) {
	parent, err := gitFirstParent(ctx, repoDir, mergeCommit)
	if err != nil {
		return schemas.GateResult{}, fmt.Errorf("first parent of %s: %w", mergeCommit, err)
	}
	diff, err := gitDiff(ctx, repoDir, parent, mergeCommit)
	if err != nil {
		return schemas.GateResult{}, fmt.Errorf("diff %s..%s: %w", parent, mergeCommit, err)
	}
	return L0Check(cfg, L0ParseUnifiedDiff(diff)), nil
}

func gitMergeBase(ctx context.Context, dir, a, b string) (string, error) {
	out, err := runGit(ctx, dir, "merge-base", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func gitFirstParent(ctx context.Context, dir, ref string) (string, error) {
	out, err := runGit(ctx, dir, "rev-parse", ref+"^1")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func gitDiff(ctx context.Context, dir, from, to string) (string, error) {
	// --no-color and --no-ext-diff for deterministic output;
	// --find-renames matches §5 rename handling.
	return runGit(ctx, dir, "diff", "--no-color", "--no-ext-diff", "--find-renames", from, to)
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // args are internal, not user input
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
