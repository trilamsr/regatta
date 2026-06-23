# Aggregator gates (check, ci-check, ci, ci-integration, pre-push-check) and
# the top-level help target. Lives in its own file so adding a new check or
# sub-target only edits this file — siblings touching unrelated .mk files do
# not cascade-rebase (memory/feedback_cascade_rebase_root_cause).
.PHONY: help check ci-check ci ci-integration integration pre-push-check pr-body-check check-docs check-go check-property check-stale-todo check-meta check-go-shard-coverage check-go-shard-coverage-test

help:  ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# `check` runs the GATES + GENERATORS against the codebase. Edit code? `make check`.
# `check-meta` runs the SELF-TESTS (fixtures that prove the gate / generator
# scripts work) — wasted cycles on every pre-push because the fixtures don't
# depend on the PR diff. Demoted to nightly + path-filter in MAY-30
# (.github/workflows/check-meta-nightly.yml). Edit a gate or generator?
# `make check-meta` validates the gate/generator logic.
check: doc-check prose-dup check-no-bare-sleep check-state-tier-order check-prompt-parity check-stale-refs check-no-repo-specific-slugs check-no-bare-pragma check-no-bare-time-unix check-file-line-budget check-migration-numbers check-spec-sections check-doc-links check-docker-env-parity check-env-canonical check-alert-severity-label check-go-shard-coverage lint tidy-check mod-verify verify-vendored-assets go-check property-test slo-compile-test  ## Local gate; <60s. `vet` dropped — golangci-lint enables govet (.golangci.yml).

# CI parallelization shards. Together cover the same gate set as `make check`
# (plus `stale-todo` for `check-stale-todo`). Local `make check` and
# `make ci-check` remain the serial pre-push entrypoints; the shards exist so
# .github/workflows/ci.yml can fan them out into parallel jobs without
# duplicating the target list per shard.
check-docs: doc-check prose-dup check-no-bare-sleep check-state-tier-order check-prompt-parity check-stale-refs check-migration-numbers check-spec-sections check-go-shard-coverage  ## CI shard: bash-script doc/citation gates. Fast (~30s).

# MAY-30: gate + generator self-tests (fixtures for scripts/check-*.sh,
# scripts/doc-check.sh, scripts/gen-*.sh). Run nightly in
# .github/workflows/check-meta-nightly.yml; also fires on push when the diff
# touches a gate / generator script, its self-test, or a wiring Makefile.
# 19 targets total: 15 scripts/check-*_test.sh fixtures + doc-check-test
# + specs-index-test (gen-specs-readme) + gen-boot-status-test
# + check-meta-coverage-test (drift watchdog asserting this list stays complete).
check-meta: doc-check-test specs-index-test gen-boot-status-test check-no-bare-sleep-test check-state-tier-order-test check-prompt-parity-test check-reviewer-verdict-test check-release-notes-local-test check-stale-refs-test check-tdd-redfirst-test check-migration-numbers-test check-spec-sections-test check-doc-links-test check-docker-env-parity-test check-alert-severity-label-test check-byte-equal-pin-test check-mock-vs-real-test check-prose-dup-test check-tdd-test check-go-shard-coverage-test check-meta-coverage-test  ## Nightly: gate + generator self-tests. Edit a gate or generator? Run this locally before push.

check-go: tidy-check mod-verify verify-vendored-assets go-check  ## CI shard: Go module + race-test sweep. `lint` runs in its own job. Slow (~3-5min, setup-go cached).

check-property: property-test slo-compile-test  ## CI shard: property tests + SLO compile determinism. ~30-60s.

check-stale-todo: stale-todo  ## CI shard: cross-tree TODO age scan. ~30s.

ci-check: check stale-todo  ## CI gate; supersedes `check` with longer-running scans (stale-todo).

ci: ci-check  ## CI entrypoint. CI also runs lint as a separate job via golangci-lint-action for redundancy; `make check` runs the same linter locally so PR-time lint failures show up before push.

ci-integration: ## Nightly-only: e2e + integration tests that cost Anthropic tokens / write to real fixture repos. Requires REGATTA_E2E_* env (see tests/e2e/loopclosure/loop_closure_test.go). Skips when env absent. Tag split rationale: docs/engineer/testing-conventions.md.
	go test -tags=e2e -count=1 -timeout=30m ./tests/e2e/...
	go test -tags=integration_gh -count=1 -timeout=10m ./internal/orchestrator/prwatch/... ./internal/orchestrator/scheduler/... ./internal/orchestrator/adapter/githubissues/... ./internal/gates/l4/...
	go test -tags=docker -count=1 -timeout=15m ./tests/docker/...

integration: ## Build-tag-gated integration tests: tests/docker (compose stack) + tests/integration (CLI subprocess + migration replay). Wired into a dedicated CI job so unit-test feedback stays fast (R-MEGA-3 INT-5).
	go test -tags=docker -count=1 -timeout=15m ./tests/docker/...
	go test -tags=integration -count=1 -timeout=10m ./tests/integration/...

pre-push-check: check  ## Local pre-push gate. Runs `make check` + PR-body release-notes block sanity check + demoted byte-equal-pin / phase-x-leak hints (MAY-31; hint = informational, exit always 0).
	bash scripts/check-release-notes-local.sh
	@# MAY-31 hints: byte-equal-pin + phase-x-leak demoted from required check.
	@# Reviewer subagent + adversarial-review-every-step cover these now;
	@# hints are operator-glance only — never fail the push.
	@bash scripts/check-byte-equal-pin_test.sh || echo "pre-push hint: byte-equal-pin test regressed (non-fatal; MAY-31)"
	@bash scripts/check-phase-x-leak.sh || echo "pre-push hint: phase-x-leak detected in active spec (non-fatal; MAY-31; review for self-host filter)"

pr-body-check:  ## Validate an intended PR body BEFORE `gh pr create`: make pr-body-check FILE=body.md (checks ```release-notes fence + [CATEGORY]).
	@test -n "$(FILE)" || { echo "usage: make pr-body-check FILE=<body.md>"; exit 2; }
	bash scripts/check-release-notes-local.sh --body-file "$(FILE)"
