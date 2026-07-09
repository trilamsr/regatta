# Aggregator gates (check, ci-check, ci, ci-integration, pre-push-check) and
# the top-level help target. Lives in its own file so adding a new check or
# sub-target only edits this file — siblings touching unrelated .mk files do
# not cascade-rebase (memory/feedback_cascade_rebase_root_cause).
.PHONY: help check ci-check ci ci-integration integration pre-push-check check-docs check-go check-property

help:  ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# `check` runs the local pre-push gate set. Solo-op trust model: Go compiler +
# `go vet` (via golangci-lint) + `go test -race` + govulncheck cover the
# correctness class; the surviving bash gates catch bug-classes those Go tools
# do not (deleted-file references, flaky `time.Sleep` in test loops, banned
# doc phrases).
check: doc-check check-no-bare-sleep check-docker-env-parity check-env-canonical lint tidy-check mod-verify verify-vendored-assets go-check property-test slo-compile-test  ## Local gate; <60s.

# CI parallelization shards. Local `make check` and `make ci-check` remain the
# serial pre-push entrypoints; the shards exist so .github/workflows/ci.yml
# can fan them out into parallel jobs without duplicating the target list per
# shard.
check-docs: doc-check check-no-bare-sleep  ## CI shard: bash-script doc/citation gates. Fast (~30s).

check-go: tidy-check mod-verify verify-vendored-assets go-check  ## CI shard: Go module + race-test sweep. `lint` runs in its own job. Slow (~3-5min, setup-go cached).

check-property: property-test slo-compile-test  ## CI shard: property tests + SLO compile determinism. ~30-60s.

ci-check: check  ## CI gate. Alias of `check` since the stale-todo shard was culled 2026-07-08.

ci: ci-check  ## CI entrypoint. CI also runs lint as a separate job via golangci-lint-action for redundancy; `make check` runs the same linter locally so PR-time lint failures show up before push.

ci-integration: ## Nightly-only: e2e + integration tests that cost Anthropic tokens / write to real fixture repos. Requires REGATTA_E2E_* env (see tests/e2e/loopclosure/loop_closure_test.go). Skips when env absent. Tag split rationale: docs/engineer/testing-conventions.md.
	go test -tags=e2e -count=1 -timeout=30m ./tests/e2e/...
	go test -tags=integration_gh -count=1 -timeout=10m ./internal/orchestrator/prwatch/... ./internal/orchestrator/scheduler/... ./internal/orchestrator/adapter/githubissues/...
	go test -tags=docker -count=1 -timeout=15m ./tests/docker/...

integration: ## Build-tag-gated integration tests: tests/docker (compose stack) + tests/integration (CLI subprocess + migration replay). Wired into a dedicated CI job so unit-test feedback stays fast (R-MEGA-3 INT-5).
	go test -tags=docker -count=1 -timeout=15m ./tests/docker/...
	go test -tags=integration -count=1 -timeout=10m ./tests/integration/...

pre-push-check: check  ## Local pre-push gate. Alias of `check` since the release-notes-local sub-gate was culled 2026-07-08 (pr-lint workflow enforces release-notes on server side).
