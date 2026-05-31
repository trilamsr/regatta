#!/usr/bin/env bash
# Verify the current branch's PR body has a fenced release-notes block
# with a [CATEGORY] line — pre-push catch for the most common pr-lint
# round-trip. No PR yet => exits 0 (will check after PR opens).

set -euo pipefail
command -v gh >/dev/null || { echo "check-release-notes: gh not installed; skipping"; exit 0; }

body=$(gh pr view --json body --jq .body 2>/dev/null || true)
[ -z "$body" ] && { echo "check-release-notes: no PR yet; skipping"; exit 0; }

cats='FEATURE|CHANGE|BUGFIX|SECURITY|PERF|DOCS|CHORE|CI'
printf '%s\n' "$body" | grep -qE '^```release-notes' || { echo "check-release-notes: PR body missing \`\`\`release-notes block"; exit 1; }
printf '%s\n' "$body" | grep -qE "^\\[($cats)\\]" || { echo "check-release-notes: \`\`\`release-notes block missing [CATEGORY] line ($cats)"; exit 1; }

echo "check-release-notes: ok"
