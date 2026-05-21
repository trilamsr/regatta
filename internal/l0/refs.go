package l0

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/trilamsr/regatta/schemas"
)

// CheckRefs runs L0 against the diff between baseRef and headRef in repoDir.
//
// The diff base is `git merge-base baseRef headRef` (see testdata/README.md §1).
// This closes the TOCTOU window where the spec mutates on the base branch
// mid-flight: comparing the PR head against the literal base tip would show
// the base's tightening as a "removal" by the PR. Diffing against the merge
// base shows only what the PR actually changed.
//
// CheckMergeCommit is the §7 re-run path; both share this diff-base helper.
func CheckRefs(ctx context.Context, cfg Config, repoDir, baseRef, headRef string) (schemas.GateResult, error) {
	mergeBase, err := gitMergeBase(ctx, repoDir, baseRef, headRef)
	if err != nil {
		return schemas.GateResult{}, fmt.Errorf("merge-base %s %s: %w", baseRef, headRef, err)
	}
	diff, err := gitDiff(ctx, repoDir, mergeBase, headRef)
	if err != nil {
		return schemas.GateResult{}, fmt.Errorf("diff %s..%s: %w", mergeBase, headRef, err)
	}
	return Check(cfg, ParseUnifiedDiff(diff)), nil
}

// CheckMergeCommit re-runs L0 on a merge commit (testdata/README.md §7). The
// diff is the merge commit against its base-side parent — typically the
// first parent (the branch being merged into), which is what GitHub uses for
// the "base" of a PR merge commit.
//
// This catches the case where a PR passed L0 at PR-head time, then the base
// branch tightened a criterion before the merge, and the merged tree would
// regress that criterion. Comparing the merge tree against the post-tighten
// base shows the regression.
func CheckMergeCommit(ctx context.Context, cfg Config, repoDir, mergeCommit string) (schemas.GateResult, error) {
	parent, err := gitFirstParent(ctx, repoDir, mergeCommit)
	if err != nil {
		return schemas.GateResult{}, fmt.Errorf("first parent of %s: %w", mergeCommit, err)
	}
	diff, err := gitDiff(ctx, repoDir, parent, mergeCommit)
	if err != nil {
		return schemas.GateResult{}, fmt.Errorf("diff %s..%s: %w", parent, mergeCommit, err)
	}
	return Check(cfg, ParseUnifiedDiff(diff)), nil
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
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
