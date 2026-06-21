# Aggregator gates (check, ci-check, ci, ci-integration, pre-push-check) and
# the top-level help target. Lives in its own file so adding a new check or
# sub-target only edits this file — siblings touching unrelated .mk files do
# not cascade-rebase (memory/feedback_cascade_rebase_root_cause).
.PHONY: help check ci-check ci ci-integration pre-push-check pr-body-check check-docs check-go check-property check-stale-todo check-gate-demote-test

help:  ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: doc-check doc-check-test specs-index-test prose-dup check-no-bare-sleep check-no-bare-sleep-test check-state-tier-order check-state-tier-order-test check-prompt-parity check-prompt-parity-test check-reviewer-verdict-test check-release-notes-local-test check-stale-refs check-stale-refs-test check-tdd-redfirst-test check-no-repo-specific-slugs check-migration-numbers check-migration-numbers-test check-spec-sections check-spec-sections-test check-doc-links check-doc-links-test check-gate-demote-test lint tidy-check mod-verify verify-vendored-assets go-check property-test slo-compile-test  ## Local gate; <60s. `vet` dropped — golangci-lint enables govet (.golangci.yml).

# CI parallelization shards. Together cover the same gate set as `make check`
# (plus `stale-todo` for `check-stale-todo`). Local `make check` and
# `make ci-check` remain the serial pre-push entrypoints; the shards exist so
# .github/workflows/ci.yml can fan them out into parallel jobs without
# duplicating the target list per shard.
check-docs: doc-check doc-check-test specs-index-test prose-dup check-no-bare-sleep check-no-bare-sleep-test check-state-tier-order check-state-tier-order-test check-prompt-parity check-prompt-parity-test check-reviewer-verdict-test check-release-notes-local-test check-stale-refs check-stale-refs-test check-tdd-redfirst-test check-migration-numbers check-migration-numbers-test check-spec-sections check-spec-sections-test check-gate-demote-test  ## CI shard: bash-script doc/citation gates. Fast (~30s).

check-go: tidy-check mod-verify verify-vendored-assets go-check  ## CI shard: Go module + race-test sweep. `lint` runs in its own job. Slow (~3-5min, setup-go cached).

check-property: property-test slo-compile-test  ## CI shard: property tests + SLO compile determinism. ~30-60s.

check-stale-todo: stale-todo  ## CI shard: cross-tree TODO age scan. ~30s.

ci-check: check stale-todo  ## CI gate; supersedes `check` with longer-running scans (stale-todo).

ci: ci-check  ## CI entrypoint. CI also runs lint as a separate job via golangci-lint-action for redundancy; `make check` runs the same linter locally so PR-time lint failures show up before push.

ci-integration: ## Nightly-only: e2e + integration tests that cost Anthropic tokens / write to real fixture repos. Requires REGATTA_E2E_* env (see tests/e2e/loopclosure/loop_closure_test.go). Skips when env absent. Tag split rationale: docs/engineer/testing-conventions.md.
	go test -tags=e2e -count=1 -timeout=30m ./tests/e2e/...
	go test -tags=integration_gh -count=1 -timeout=10m ./internal/orchestrator/prwatch/... ./internal/orchestrator/scheduler/... ./internal/orchestrator/adapter/githubissues/... ./internal/gates/l4/...
	go test -tags=docker -count=1 -timeout=15m ./tests/docker/...

pre-push-check: check  ## Local pre-push gate. Runs `make check` + PR-body release-notes block sanity check + demoted byte-equal-pin / phase-x-leak hints (MAY-31; hint = informational, exit always 0).
	bash scripts/check-release-notes-local.sh
	@# MAY-31 hints: byte-equal-pin + phase-x-leak demoted from required check.
	@# Reviewer subagent + adversarial-review-every-step cover these now;
	@# hints are operator-glance only — never fail the push.
	@bash scripts/check-byte-equal-pin_test.sh || echo "pre-push hint: byte-equal-pin test regressed (non-fatal; MAY-31)"
	@hits=$$(grep -REn '\b(tenant_id|RBAC|Stripe|Sigstore|Rekor|blackboard|Temporal|htmx)\b' docs/engineer/specs/ --include='*.md' | grep -vE '^(.*phase-x/|.*:[[:space:]]*phase:[[:space:]]*x-)' || true); \
	if [ -n "$$hits" ]; then echo "pre-push hint: Phase-X tokens in active specs (non-fatal; MAY-31; review for self-host filter):"; echo "$$hits" | head -5; fi

check-gate-demote-test:  ## Assert MAY-31 demote: byte-equal-pin + phase-x-leak removed from check / check-docs; pre-push-check invokes both as hints.
	bash scripts/check-gate-demote_test.sh

pr-body-check:  ## Validate an intended PR body BEFORE `gh pr create`: make pr-body-check FILE=body.md (checks ```release-notes fence + [CATEGORY]).
	@test -n "$(FILE)" || { echo "usage: make pr-body-check FILE=<body.md>"; exit 2; }
	bash scripts/check-release-notes-local.sh --body-file "$(FILE)"
