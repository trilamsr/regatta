# Lint + doc-quality gates. Owned by repo-consistency wedge.
.PHONY: doc-check doc-check-test prose-dup stale-todo verify-vendored-assets lint tidy-check mod-verify check-no-bare-sleep check-no-bare-sleep-test check-state-tier-order check-state-tier-order-test check-prompt-parity check-prompt-parity-test check-reviewer-verdict-test check-byte-equal-pin-test check-stale-refs check-stale-refs-test check-tdd-redfirst check-tdd-redfirst-test check-no-repo-specific-slugs check-migration-numbers check-migration-numbers-test check-spec-sections check-spec-sections-test check-mock-vs-real check-mock-vs-real-test check-release-notes-local-test next-migration

doc-check:  ## Run repo-wide doc gates (markdown links, comment-noise, test-godoc length).
	bash scripts/doc-check.sh

doc-check-test:  ## Fixture-driven test for doc-check.sh comment-noise (reviewer-tag) gate.
	bash scripts/doc-check_test.sh

prose-dup:  ## Fail if a previously-deduped prose phrase reappears in 2+ markdown files.
	bash scripts/check-prose-dup.sh

check-doc-links:  ## Fail when a markdown-link `](path)` body under docs/ or CLAUDE.md references a non-existent intra-repo file.
	bash scripts/check-doc-links.sh

check-doc-links-test:  ## Smoke test for check-doc-links.sh (broken / existing / external / testdata / anchor-suffix).
	bash scripts/check-doc-links_test.sh

check-stale-refs:  ## Fail when PR deletes files but tracked files still reference them. Escape: `<!-- stale-refs-justified: <reason> -->`.
	bash scripts/check-stale-refs.sh

check-stale-refs-test:  ## Fixture-driven test for check-stale-refs.sh.
	bash scripts/check-stale-refs_test.sh

check-tdd-redfirst:  ## Fail when a PR adds a new prod .go + co-located _test.go without the test landing in an earlier commit. Escape: `<!-- tdd-single-commit-justified: <reason> -->`.
	bash scripts/check-tdd-redfirst.sh

check-tdd-redfirst-test:  ## Fixture-driven test for check-tdd-redfirst.sh (one-commit→fail, test-first→pass, escape→pass, out-of-scope→pass).
	bash scripts/check-tdd-redfirst_test.sh

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

check-byte-equal-pin-test:  ## Fixture-driven test for check-byte-equal-pin.sh. Demoted from `check` to `pre-push-check` hint in MAY-31 — script + test kept for operator-glance only; reviewer subagent covers drift.
	bash scripts/check-byte-equal-pin_test.sh

check-no-repo-specific-slugs:  ## Fail when bundled-default prompt assets (internal/orchestrator/prompt/assets/) carry feedback_* slugs or scripts/check-*.sh refs that meaningless on arbitrary target repos (spec L1.3, #965).
	bash scripts/check-no-repo-specific-slugs.sh

check-migration-numbers:  ## Fail when internal/orchestrator/state/migrations/ carries duplicates, non-contiguous tail, or PR-diff adds >1 new migration without justification (spec #971).
	bash scripts/check-migration-numbers.sh

check-migration-numbers-test:  ## Fixture-driven test for check-migration-numbers.sh (clean / duplicate / non-contiguous / known-gap / multi-add / multi-add-justified).
	bash scripts/check-migration-numbers_test.sh

next-migration:  ## Print the next free SQLite migration number (zero-padded 4 digits). Use in dispatch prompts: $$(make next-migration).
	@bash scripts/next-migration.sh

check-spec-sections:  ## Fail when a NEW or MODIFIED spec under docs/engineer/specs/ lacks one of the 7 canonical H2 sections (Problem, Design, Acceptance, Out of scope, Adversarial, Implementer brief, Reopen trigger). Pre-existing specs warn-only (closes #1032).
	bash scripts/check-spec-sections.sh

check-spec-sections-test:  ## Fixture-driven test for check-spec-sections.sh (complete / missing-acceptance strict+diff / pre-existing-warn / skeleton-prefetch opt-out / shipped opt-out).
	bash scripts/check-spec-sections_test.sh

check-mock-vs-real:  ## WARN-only ratio gate on NEW *_test.go files; >70% mock tokens vs real-infra (t.TempDir/httptest/state.Open) emits warning (closes #1088). Operator-manual; not in `check`.
	bash scripts/check-mock-vs-real.sh

check-mock-vs-real-test:  ## Fixture-driven test for check-mock-vs-real.sh (clean / high-mock / allowlisted / no-test-files).
	bash scripts/check-mock-vs-real_test.sh

check-release-notes-local-test:  ## Fixture-driven test for check-release-notes-local.sh (MAY-100 fence/[CATEGORY] + MAY-73 misplaced Reviewer-recommendation in commit msg).
	bash scripts/check-release-notes-local_test.sh

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
