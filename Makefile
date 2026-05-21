.PHONY: help check doc-check go-check cover vet lint tidy-check mod-verify hooks ci

help:  ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

doc-check:  ## Run repo-wide doc gates (markdown links, banned phrases, em-dash diff, comment-noise).
	bash scripts/doc-check.sh

go-check:  ## Build and test every Go package.
	go build ./...
	go test ./...

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

hooks:  ## Install repo-managed Git hooks (sets core.hooksPath to .githooks).
	@git config core.hooksPath .githooks
	@echo "Hooks installed under .githooks/ - all two now active:"
	@echo "  pre-commit  -> make check"
	@echo "  pre-push    -> make ci"

check: doc-check vet tidy-check mod-verify go-check  ## Single source of truth for what is verified locally and in CI.

ci: check  ## CI entrypoint: `check` only. CI runs lint as a separate job via golangci-lint-action; `make lint` remains available locally.
