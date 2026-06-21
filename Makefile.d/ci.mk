# Aggregator gates (check, ci-check, ci, ci-integration, pre-push-check) and
# the top-level help target. Lives in its own file so adding a new check or
# sub-target only edits this file — siblings touching unrelated .mk files do
# not cascade-rebase (memory/feedback_cascade_rebase_root_cause).
.PHONY: help check ci-check ci ci-integration pre-push-check pr-body-check check-docs check-go check-property check-stale-todo

help:  ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: doc-check doc-check-test specs-index-test prose-dup check-phase-x-leak check-phase-x-leak-test check-no-bare-sleep check-no-bare-sleep-test check-state-tier-order check-state-tier-order-test check-prompt-parity check-prompt-parity-test check-reviewer-verdict-test check-byte-equal-pin-test check-release-notes-local-test check-stale-refs check-stale-refs-test check-no-repo-specific-slugs check-migration-numbers check-migration-numbers-test check-spec-sections check-spec-sections-test check-doc-links check-doc-links-test lint tidy-check mod-verify verify-vendored-assets go-check property-test slo-compile-test  ## Local gate; <60s. `vet` dropped — golangci-lint enables govet (.golangci.yml).

# CI parallelization shards. Together cover the same gate set as `make check`
# (plus `stale-todo` for `check-stale-todo`). Local `make check` and
# `make ci-check` remain the serial pre-push entrypoints; the shards exist so
# .github/workflows/ci.yml can fan them out into parallel jobs without
# duplicating the target list per shard.
check-docs: doc-check doc-check-test specs-index-test prose-dup check-phase-x-leak check-phase-x-leak-test check-no-bare-sleep check-no-bare-sleep-test check-state-tier-order check-state-tier-order-test check-prompt-parity check-prompt-parity-test check-reviewer-verdict-test check-byte-equal-pin-test check-release-notes-local-test check-stale-refs check-stale-refs-test check-migration-numbers check-migration-numbers-test check-spec-sections check-spec-sections-test  ## CI shard: bash-script doc/citation gates. Fast (~30s).

check-go: tidy-check mod-verify verify-vendored-assets go-check  ## CI shard: Go module + race-test sweep. `lint` runs in its own job. Slow (~3-5min, setup-go cached).

check-property: property-test slo-compile-test  ## CI shard: property tests + SLO compile determinism. ~30-60s.

check-stale-todo: stale-todo  ## CI shard: cross-tree TODO age scan. ~30s.

ci-check: check stale-todo  ## CI gate; supersedes `check` with longer-running scans (stale-todo).

ci: ci-check  ## CI entrypoint. CI also runs lint as a separate job via golangci-lint-action for redundancy; `make check` runs the same linter locally so PR-time lint failures show up before push.

ci-integration: ## Nightly-only: e2e + integration tests that cost Anthropic tokens / write to real fixture repos. Requires REGATTA_E2E_* env (see tests/e2e/loopclosure/loop_closure_test.go). Skips when env absent. Tag split rationale: docs/engineer/testing-conventions.md.
	go test -tags=e2e -count=1 -timeout=30m ./tests/e2e/...
	go test -tags=integration_gh -count=1 -timeout=10m ./internal/orchestrator/prwatch/... ./internal/orchestrator/scheduler/... ./internal/orchestrator/adapter/githubissues/... ./internal/gates/l4/...
	go test -tags=docker -count=1 -timeout=15m ./tests/docker/...

pre-push-check: check  ## Local pre-push gate. Runs `make check` + PR-body release-notes block sanity check.
	bash scripts/check-release-notes-local.sh

pr-body-check:  ## Validate an intended PR body BEFORE `gh pr create`: make pr-body-check FILE=body.md (checks ```release-notes fence + [CATEGORY]).
	@test -n "$(FILE)" || { echo "usage: make pr-body-check FILE=<body.md>"; exit 2; }
	bash scripts/check-release-notes-local.sh --body-file "$(FILE)"
