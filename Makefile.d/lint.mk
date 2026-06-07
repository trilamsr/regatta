# Lint + doc-quality gates. Owned by repo-consistency wedge.
.PHONY: doc-check doc-check-test prose-dup stale-todo verify-vendored-assets lint tidy-check mod-verify check-memory-citations check-memory-citations-test check-phase-x-leak check-phase-x-leak-test check-tbd check-tbd-test check-comment-density check-comment-density-test check-no-bare-sleep check-no-bare-sleep-test check-state-tier-order check-state-tier-order-test check-prompt-parity check-prompt-parity-test check-reviewer-verdict-test

doc-check:  ## Run repo-wide doc gates (markdown links, banned phrases, em-dash diff, comment-noise).
	bash scripts/doc-check.sh

doc-check-test:  ## Assert banned-phrase gate strips fenced + inline backtick spans (#329 regression guard).
	bash scripts/doc-check_test.sh

prose-dup:  ## Fail if a previously-deduped prose phrase reappears in 2+ markdown files.
	bash scripts/check-prose-dup.sh

check-memory-citations:  ## Fail if a feedback_* slug cited in CLAUDE.md/boot-prompt/templates does not resolve under MEMORY_DIR (or its archive/).
	bash scripts/check-memory-citations.sh

check-memory-citations-test:  ## Fixture-driven test for check-memory-citations.sh (live-resolve → 0, broken slug → 1).
	bash scripts/check-memory-citations_test.sh

check-phase-x-leak:  ## Fail when an active spec names a Phase-X token (tenant_id/RBAC/Stripe/Sigstore/Rekor/blackboard/Temporal) without `phase: x-forward-fit` opt-in.
	bash scripts/check-phase-x-leak.sh

check-phase-x-leak-test:  ## Fixture-driven test for check-phase-x-leak.sh.
	bash scripts/check-phase-x-leak_test.sh

check-tbd:  ## Fail when an engineer doc carries a bare `TBD` placeholder outside an HTML-comment+issue or `release-notes` fence.
	bash scripts/check-tbd.sh

check-tbd-test:  ## Fixture-driven test for check-tbd.sh.
	bash scripts/check-tbd_test.sh

check-comment-density:  ## Fail when a NEW prod .go file in the PR diff exceeds 5% comment density (#743 §Comments).
	bash scripts/check-comment-density.sh

check-comment-density-test:  ## Fixture-driven test for check-comment-density.sh (clean / dense / allowlisted / test-file / existing-file).
	bash scripts/check-comment-density_test.sh

check-no-bare-sleep:  ## Fail when a *_test.go file carries `time.Sleep` lexically nested inside a `for` block without `// allow-sleep:` directive (#760 migration target: testutil.Eventually).
	bash scripts/check-no-bare-sleep.sh

check-no-bare-sleep-test:  ## Fixture-driven test for check-no-bare-sleep.sh.
	bash scripts/check-no-bare-sleep_test.sh

check-state-tier-order:  ## Fail when a pure subpackage under internal/orchestrator/state (jsonscan/edgeagg/transitions/cycle/approvals_shadow) imports the parent `state` package (plan #795 Option E one-way tier).
	bash scripts/check-state-tier-order.sh

check-state-tier-order-test:  ## Fixture-driven test for check-state-tier-order.sh.
	bash scripts/check_state_tier_order_test.sh

check-prompt-parity:  ## Fail when defaultPromptBuilder lacks a feedback_* slug listed under implementer.md `## Anchored rules` (closes #901, session retro Impact 3).
	bash scripts/check-prompt-parity.sh

check-prompt-parity-test:  ## Fixture-driven test for check-prompt-parity.sh (missing slug → 1, aligned → 0, escape-hatch → 0).
	bash scripts/check-prompt-parity_test.sh

check-reviewer-verdict-test:  ## Fixture-driven test for check-reviewer-verdict.sh (load-bearing PR missing APPROVE → fail; CHORE/DOCS → skip). Gate itself runs in pr-lint workflow against the live PR body (closes #899).
	bash scripts/check-reviewer-verdict_test.sh

stale-todo:  ## Fail if any tracked TODO|FIXME|XXX has lived past 7 days without an issue ref.
	bash scripts/stale-todo.sh

verify-vendored-assets:  ## Assert on-disk SHA-256 of internal/web/static/htmx.min.js matches the pin in VENDORED.md. Mismatch = supply-chain tamper or accidental edit; fails CI.
	@bash -c 'set -euo pipefail; \
		ON_DISK=$$(shasum -a 256 internal/web/static/htmx.min.js | awk "{print \$$1}"); \
		PINNED=$$(grep -oE "[a-f0-9]{64}" internal/web/static/VENDORED.md | head -1); \
		if [ "$$ON_DISK" != "$$PINNED" ]; then \
			echo "verify-vendored-assets: htmx.min.js sha256 drift"; \
			echo "  on-disk: $$ON_DISK"; \
			echo "  pinned : $$PINNED"; \
			exit 1; \
		fi; \
		echo "verify-vendored-assets: htmx.min.js sha256 ok ($$ON_DISK)"'

lint:  ## Run golangci-lint via the module's tool directive.
	go tool golangci-lint run ./...

tidy-check:  ## Verify go.mod / go.sum are tidy without mutating; fails with a diff if not.
	@diff=$$(go mod tidy -diff); \
	if [ -n "$$diff" ]; then echo "go.mod / go.sum need tidying:"; echo "$$diff"; exit 1; fi

mod-verify:  ## Verify go.mod / go.sum hashes match upstream.
	go mod verify
