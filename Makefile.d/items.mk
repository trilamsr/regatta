# Autonomous-loop item generators (boot-prompt -> items; gh issues -> items).
.PHONY: items followups

items:  ## Regenerate .regatta/items/*.md from docs/engineer/autonomous-session-prompt.md. Idempotent.
	go run ./cmd/boot-prompt-to-items

followups:  ## Regenerate .regatta/items/gh-issue-*.md from GH [followup]-labeled issues. Idempotent.
	go run ./cmd/gh-followup-to-items
