# Aggregator gates (check, ci-check, ci, ci-integration, pre-push-check) and
# the top-level help target. Lives in its own file so adding a new check or
# sub-target only edits this file — siblings touching unrelated .mk files do
# not cascade-rebase (memory/feedback_cascade_rebase_root_cause).
.PHONY: help check ci-check ci ci-integration pre-push-check

help:  ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: doc-check doc-check-test specs-index-test prose-dup check-memory-citations check-memory-citations-test check-phase-x-leak check-phase-x-leak-test check-tbd check-tbd-test check-comment-density check-comment-density-test check-no-bare-sleep check-no-bare-sleep-test check-state-tier-order check-state-tier-order-test check-prompt-parity check-prompt-parity-test check-reviewer-verdict-test lint tidy-check mod-verify verify-vendored-assets go-check property-test slo-compile-test  ## Local gate; <60s. `vet` dropped — golangci-lint enables govet (.golangci.yml).

ci-check: check stale-todo  ## CI gate; supersedes `check` with longer-running scans (stale-todo).

ci: ci-check  ## CI entrypoint. CI also runs lint as a separate job via golangci-lint-action for redundancy; `make check` runs the same linter locally so PR-time lint failures show up before push.

ci-integration: ## Nightly-only: e2e + integration tests that cost Anthropic tokens / write to real fixture repos. Requires REGATTA_E2E_* env (see tests/e2e/loopclosure/loop_closure_test.go). Skips when env absent. Tag split rationale: docs/engineer/testing-conventions.md.
	go test -tags=e2e -count=1 -timeout=30m ./tests/e2e/...
	go test -tags=integration_gh -count=1 -timeout=10m ./internal/orchestrator/prwatch/... ./internal/orchestrator/scheduler/...
	go test -tags=docker -count=1 -timeout=15m ./tests/docker/...

pre-push-check: check  ## Local pre-push gate. Runs `make check` + PR-body release-notes block sanity check.
	bash scripts/check-release-notes-local.sh
