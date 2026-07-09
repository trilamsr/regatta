# Lint + doc-quality gates. Owned by repo-consistency wedge.
.PHONY: doc-check doc-check-test stale-todo verify-vendored-assets lint tidy-check mod-verify check-no-bare-sleep check-no-bare-sleep-test-test check-tdd-test check-release-notes-local-test check-go-shard-coverage check-go-shard-coverage-test check-docker-env-parity check-docker-env-parity-test check-env-canonical next-migration

doc-check:  ## Run repo-wide doc gates (markdown links, comment-noise, test-godoc length).
	bash scripts/doc-check.sh

doc-check-test:  ## Fixture-driven test for doc-check.sh comment-noise (reviewer-tag) gate.
	bash scripts/doc-check_test.sh

check-docker-env-parity:  ## Fail when a REGATTA_* env var declared in docker-compose*.yml is not read by prod Go code (R-MEGA-2 G2).
	bash scripts/check-docker-env-parity.sh

check-docker-env-parity-test:  ## Fixture-driven self-test for check-docker-env-parity.sh.
	bash scripts/check-docker-env-parity_test.sh

check-env-canonical:  ## Fail when prod Go code reads a legacy env var name when a canonical alias exists (R-MEGA-2 G3). Escape: `// canonical-env-skip: <reason>`.
	bash scripts/check-env-canonical.sh



check-tdd-test:  ## Fixture-driven test for check-tdd.sh (gate self-test).
	bash scripts/check-tdd_test.sh

check-no-bare-sleep:  ## Fail when a *_test.go file carries `time.Sleep` lexically nested inside a `for` block without `// allow-sleep:` directive (#760 migration target: testutil.Eventually).
	bash scripts/check-no-bare-sleep.sh

check-no-bare-sleep-test:  ## Fixture-driven test for check-no-bare-sleep.sh.
	bash scripts/check-no-bare-sleep_test.sh

next-migration:  ## Print the next free SQLite migration number (zero-padded 4 digits). Use in dispatch prompts: $$(make next-migration).
	@bash scripts/next-migration.sh

check-release-notes-local-test:  ## Fixture-driven test for check-release-notes-local.sh (MAY-100 fence/[CATEGORY] + MAY-73 misplaced Reviewer-recommendation in commit msg).
	bash scripts/check-release-notes-local_test.sh

check-go-shard-coverage:  ## Fail when union of scripts/go-shards/shard-*.txt != `go list ./...`, or any package appears in 2+ shards. Mechanical drift gate.
	bash scripts/check-go-shard-coverage.sh

check-go-shard-coverage-test:  ## Fixture-driven test for check-go-shard-coverage.sh.
	bash scripts/check-go-shard-coverage_test.sh

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
