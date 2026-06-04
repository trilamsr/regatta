.PHONY: gen-boot-status gen-boot-status-test

gen-boot-status:  ## Regenerate auto-shipped + auto-priority blocks in docs/engineer/autonomous-session-prompt.md from gh queries. Local-only. Excludes phase-x parking issues by default.
	bash scripts/gen-boot-status.sh --exclude-label phase-x

gen-boot-status-test:  ## Assert gen-boot-status generator: marker splice, idempotency, missing-marker error.
	bash scripts/gen-boot-status_test.sh
