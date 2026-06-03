# Autonomous-session ops: status, flake report, branch cleanup.
.PHONY: agent-status agent-status-test ci-flake-report ci-flake-report-test cleanup-branches worktree-gc worktree-gc-apply worktree-gc-test

agent-status:  ## Summarize autonomous-session state: agent worktrees, open PRs, recent merges, open-issue label breakdown.
	bash scripts/agent-status.sh

agent-status-test:  ## Smoke test for scripts/agent-status.sh (offline; --no-network path).
	bash scripts/agent-status_test.sh

ci-flake-report:  ## Rank tests by flake rate across recent CI runs (default top 10, last 100 runs).
	bash scripts/ci-flake-report.sh

ci-flake-report-test:  ## Smoke test for scripts/ci-flake-report.sh (offline; stubbed gh).
	bash scripts/ci-flake-report_test.sh

cleanup-branches:  ## Delete local branches + worktrees whose PRs are merged. --dry-run aware.
	bash scripts/cleanup-merged-branches.sh

## Local-only ops (destructive — never wired into `make check` / `make ci-check`).

worktree-gc:  ## Dry-run: list agent worktrees whose branch is merged into origin/main.
	bash scripts/worktree-gc.sh

worktree-gc-apply:  ## Destructive: remove agent worktrees whose branch is merged into origin/main.
	bash scripts/worktree-gc.sh --apply

worktree-gc-test:  ## Smoke test for scripts/worktree-gc.sh (offline; throwaway git repo).
	bash scripts/worktree-gc_test.sh
