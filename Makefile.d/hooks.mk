# Repo-managed Git hooks installer.
.PHONY: install-hooks uninstall-hooks

install-hooks:  ## Install repo-managed Git hooks (sets core.hooksPath to .githooks).
	@git config core.hooksPath .githooks
	@echo "Hooks installed under .githooks/. Active hooks:"
	@echo "  prepare-commit-msg -> auto-append Signed-off-by"
	@echo "  commit-msg         -> validate Conventional Commits subject"
	@echo "  pre-commit         -> make check"

uninstall-hooks:  ## Detach repo-managed hooks (resets core.hooksPath).
	@git config --unset core.hooksPath || true
	@echo "Hooks detached. Git falls back to .git/hooks/."
