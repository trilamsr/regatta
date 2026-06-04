# Boot-prompt auto-status generator. Local-only operator tool; intentionally
# NOT in `make check` (depends on live `gh` calls).
.PHONY: gen-boot-status gen-boot-status-test

gen-boot-status:  ## Regenerate auto-shipped + auto-priority blocks in docs/engineer/autonomous-session-prompt.md from gh queries. Local-only.
	bash scripts/gen-boot-status.sh

gen-boot-status-test:  ## Assert gen-boot-status generator: marker splice, idempotency, missing-marker error.
	bash scripts/gen-boot-status_test.sh
