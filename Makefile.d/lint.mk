# Lint + doc-quality gates. Owned by repo-consistency wedge.
.PHONY: doc-check doc-check-test prose-dup stale-todo verify-vendored-assets lint tidy-check mod-verify check-phase-x-leak check-phase-x-leak-test check-tbd check-tbd-test check-comment-density check-comment-density-test check-no-bare-sleep check-no-bare-sleep-test check-state-tier-order check-state-tier-order-test check-prompt-parity check-prompt-parity-test check-reviewer-verdict-test check-byte-equal-pin-test check-no-repo-specific-slugs check-migration-numbers check-migration-numbers-test check-spec-sections check-spec-sections-test check-mock-vs-real check-mock-vs-real-test next-migration

doc-check:  ## Run repo-wide doc gates (markdown links, banned phrases, em-dash diff, comment-noise).
	bash scripts/doc-check.sh

doc-check-test:  ## Assert banned-phrase gate strips fenced + inline backtick spans (#329 regression guard).
	bash scripts/doc-check_test.sh

prose-dup:  ## Fail if a previously-deduped prose phrase reappears in 2+ markdown files.
	bash scripts/check-prose-dup.sh

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

check-doc-links:  ## Fail when a markdown-link `](path)` body under docs/ or CLAUDE.md references a non-existent intra-repo file.
	bash scripts/check-doc-links.sh

check-doc-links-test:  ## Smoke test for check-doc-links.sh (broken / existing / external / testdata / anchor-suffix).
	bash scripts/check-doc-links_test.sh

check-pr-body-close-keywords-test:  ## Smoke test for check-pr-body-close-keywords.sh (catches `closes #N #M` space-form which GitHub doesn't auto-close).
	bash scripts/check-pr-body-close-keywords_test.sh

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

check-byte-equal-pin-test:  ## Fixture-driven test for check-byte-equal-pin.sh (byte-equal claim w/o parity gate → fail; claim+gate → pass; no-claim/escape → skip). Gate itself runs in pr-lint workflow against the live PR body (closes #1031).
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
