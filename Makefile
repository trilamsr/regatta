.PHONY: help check ci-check doc-check go-check cover vet lint tidy-check mod-verify install-hooks uninstall-hooks stale-todo ci prose-dup property-test

help:  ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

doc-check:  ## Run repo-wide doc gates (markdown links, banned phrases, em-dash diff, comment-noise).
	bash scripts/doc-check.sh

go-check:  ## Build and test every Go package.
	go build -buildvcs=false ./...
	go test ./...

property-test:  ## Run rapid property tests with spec-mandated check count (200).
	go test -race -run TestListSpawnable_PropertyTopologicalReady ./internal/orchestrator/state/... -rapid.checks=200

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

check: doc-check prose-dup vet lint tidy-check mod-verify go-check property-test  ## Local gate; <60s. Single source of truth for what is verified locally.

ci-check: check stale-todo  ## CI gate; supersedes `check` with longer-running scans (stale-todo).

ci: ci-check  ## CI entrypoint. CI also runs lint as a separate job via golangci-lint-action for redundancy; `make check` runs the same linter locally so PR-time lint failures show up before push.
