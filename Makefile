.PHONY: help check doc-check go-check cover ci

help:  ## Show this help.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

doc-check:  ## Run repo-wide doc gates (markdown links, banned phrases, em-dash diff).
	bash scripts/doc-check.sh

go-check:  ## Build and test every Go package.
	go build ./...
	go test ./...

cover:  ## Print cross-package coverage; useful before declaring "done".
	go test -coverpkg=./... -coverprofile=/tmp/regatta.cover ./...
	go tool cover -func=/tmp/regatta.cover | tail -30

check: doc-check go-check  ## Single source of truth for what is verified locally and in CI.

ci: check  ## CI entrypoint (alias of `check` while regatta is pre-implementation).
