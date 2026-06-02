.PHONY: help check ci-check doc-check doc-check-test go-check go-check-full cover vet lint tidy-check mod-verify install-hooks uninstall-hooks stale-todo ci prose-dup property-test property-test-full crash-recovery-property-full bench pre-push-check cleanup-branches build-tailwind verify-vendored-assets items followups mutation-test mutation-test-install agent-status agent-status-test ci-flake-report ci-flake-report-test slo-compile slo-compile-test

help:  ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

doc-check:  ## Run repo-wide doc gates (markdown links, banned phrases, em-dash diff, comment-noise).
	bash scripts/doc-check.sh

doc-check-test:  ## Assert banned-phrase gate strips fenced + inline backtick spans (#329 regression guard).
	bash scripts/doc-check_test.sh

items:  ## Regenerate .regatta/items/*.md from docs/engineer/autonomous-session-prompt.md. Idempotent.
	go run ./cmd/boot-prompt-to-items

followups:  ## Regenerate .regatta/items/gh-issue-*.md from GH [followup]-labeled issues. Idempotent.
	go run ./cmd/gh-followup-to-items

go-check:  ## Build and test every Go package with the race detector. PHASE-S-RELAX: -short during self-host window; full sweep via `make go-check-full`.
	go build -buildvcs=false ./...
	go test -short -race ./...

go-check-full:  ## Full race sweep without -short. Run weekly + before any tag. PHASE-S-RELAX restoration target — fold back into `go-check` at end of self-host phase (memory/feedback_gate_relaxation_phase_s).
	go build -buildvcs=false ./...
	go test -race ./...

property-test:  ## Run rapid property tests. PHASE-S-RELAX: 50 checks in CI/local; spec-mandated 200 via `make property-test-full`.
	go test -race -run 'TestListSpawnable_PropertyTopologicalReady|TestSubstrate_SupersedesCycleProperty|TestSubstrate_ReplayProtectionProperty' ./internal/orchestrator/state/... -rapid.checks=50
	go test -race -run 'TestSchedulerCrashRecoveryProperty' ./internal/orchestrator/scheduler/... -rapid.checks=50
	go test -race -run 'TestSpendCrashRecoveryProperty' ./internal/cost/spend/... -rapid.checks=50
	go test -race -run 'TestReaperCrashRecoveryProperty' ./internal/gates/approval/... -rapid.checks=50
	go test -race -run 'TestBridge_PrimitiveAttrRoundTrip_Property' ./internal/obs/otel/... -rapid.checks=50

property-test-full:  ## Full 200/2000-check property sweep. Run weekly + before any tag. PHASE-S-RELAX restoration target — fold back into `property-test` at end of self-host phase (memory/feedback_gate_relaxation_phase_s).
	go test -race -run 'TestListSpawnable_PropertyTopologicalReady|TestSubstrate_SupersedesCycleProperty|TestSubstrate_ReplayProtectionProperty' ./internal/orchestrator/state/... -rapid.checks=200
	go test -race -run 'TestSchedulerCrashRecoveryProperty' ./internal/orchestrator/scheduler/... -rapid.checks=2000 -timeout=5m
	go test -race -run 'TestSpendCrashRecoveryProperty' ./internal/cost/spend/... -rapid.checks=200
	go test -race -run 'TestReaperCrashRecoveryProperty' ./internal/gates/approval/... -rapid.checks=200
	go test -race -run 'TestBridge_PrimitiveAttrRoundTrip_Property' ./internal/obs/otel/... -rapid.checks=200

crash-recovery-property-full:  ## 2000-case crash-recovery property sweep. Nightly CI target; spec §3.4. ≤90s wallclock budget.
	go test -race -run 'TestSchedulerCrashRecoveryProperty' ./internal/orchestrator/scheduler/... -rapid.checks=2000 -timeout=5m

mutation-test-install:  ## Install pinned gremlins binary into $GOPATH/bin. Idempotent.
	go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0

mutation-test: mutation-test-install  ## Run gremlins against cost + scheduler packages (spec §3.2 allowlist). Developer mode (no threshold enforcement); see scripts/mutation/run-gremlins.sh for env knobs.
	NO_THRESHOLD=1 bash scripts/mutation/run-gremlins.sh

bench:  ## Run benchmark corpus (scheduler.Tick, CycleCheck, ListSpawnable, BriefLoader.Sync, schemas.Verify, canon). ~30s total at -benchtime=3x.
	go test -run=^$$ -bench=. -benchmem -benchtime=3x \
		./internal/orchestrator/scheduler/... \
		./internal/orchestrator/state/... \
		./internal/program/... \
		./contracts/schemas/... \
		./internal/canon/...

pre-push-check: check  ## Local pre-push gate. Runs `make check` + PR-body release-notes block sanity check.
	bash scripts/check-release-notes-local.sh

cleanup-branches:  ## Delete local branches + worktrees whose PRs are merged. --dry-run aware.
	bash scripts/cleanup-merged-branches.sh

agent-status:  ## Summarize autonomous-session state: agent worktrees, open PRs, recent merges, open-issue label breakdown.
	bash scripts/agent-status.sh

agent-status-test:  ## Smoke test for scripts/agent-status.sh (offline; --no-network path).
	bash scripts/agent-status_test.sh

ci-flake-report:  ## Rank tests by flake rate across recent CI runs (default top 10, last 100 runs).
	bash scripts/ci-flake-report.sh

ci-flake-report-test:  ## Smoke test for scripts/ci-flake-report.sh (offline; stubbed gh).
	bash scripts/ci-flake-report_test.sh

slo-compile:  ## Compile every slo/*.yaml -> dashboards/prometheus/rules/ via pinned Sloth (tools/sloth/version). Deterministic; same input = byte-equal output (spec §9 R3).
	bash scripts/slo-compile.sh

slo-compile-test:  ## Assert pin file exists, every slo/*.yaml has a rendered rule, and re-compile is byte-deterministic.
	bash scripts/slo-compile_test.sh

cover:  ## Print cross-package coverage; useful before declaring "done".
	go test -coverpkg=./... -coverprofile=/tmp/regatta.cover ./...
	go tool cover -func=/tmp/regatta.cover | tail -30

vet:  ## Run go vet.
	go vet ./...

lint:  ## Run golangci-lint via the module's tool directive.
	go tool golangci-lint run ./...

tidy-check:  ## Verify go.mod / go.sum are tidy without mutating; fails with a diff if not.
	@diff=$$(go mod tidy -diff); \
	if [ -n "$$diff" ]; then echo "go.mod / go.sum need tidying:"; echo "$$diff"; exit 1; fi

mod-verify:  ## Verify go.mod / go.sum hashes match upstream.
	go mod verify

stale-todo:  ## Fail if any tracked TODO|FIXME|XXX has lived past 7 days without an issue ref.
	bash scripts/stale-todo.sh

prose-dup:  ## Fail if a previously-deduped prose phrase reappears in 2+ markdown files.
	bash scripts/check-prose-dup.sh

install-hooks:  ## Install repo-managed Git hooks (sets core.hooksPath to .githooks).
	@git config core.hooksPath .githooks
	@echo "Hooks installed under .githooks/. Active hooks:"
	@echo "  prepare-commit-msg -> auto-append Signed-off-by"
	@echo "  commit-msg         -> validate Conventional Commits subject"
	@echo "  pre-commit         -> make check"
	@echo "  pre-push           -> make ci"

uninstall-hooks:  ## Detach repo-managed hooks (resets core.hooksPath).
	@git config --unset core.hooksPath || true
	@echo "Hooks detached. Git falls back to .git/hooks/."

build-tailwind:  ## Re-compile internal/web/static/tailwind.min.css from CSS source + templates. Developer-machine only (npx tailwindcss@3.4.1). Commit the output; CI does NOT run this.
	npx tailwindcss@3.4.1 -c ./internal/web/tailwind.config.js \
		-i ./internal/web/css/input.css \
		-o ./internal/web/static/tailwind.min.css \
		--minify

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

check: doc-check doc-check-test prose-dup vet lint tidy-check mod-verify verify-vendored-assets go-check property-test slo-compile-test  ## Local gate; <60s. Single source of truth for what is verified locally.

ci-check: check stale-todo  ## CI gate; supersedes `check` with longer-running scans (stale-todo).

ci: ci-check  ## CI entrypoint. CI also runs lint as a separate job via golangci-lint-action for redundancy; `make check` runs the same linter locally so PR-time lint failures show up before push.
