#!/usr/bin/env bash
# Verify a PR body has a fenced release-notes block with a [CATEGORY]
# line — pre-push catch for the most common pr-lint round-trip.
#
# Usage:
#   check-release-notes-local.sh                  # body from open PR (no PR => skip)
#   check-release-notes-local.sh --body-file F    # validate F as intended PR body

set -euo pipefail
cats='FEATURE|CHANGE|BUGFIX|SECURITY|PERF|DOCS|CHORE|CI'

if [ "${1:-}" = "--body-file" ]; then
  [ -f "${2:-}" ] || { echo "check-release-notes: --body-file requires an existing path"; exit 2; }
  body=$(cat "$2")
else
  command -v gh >/dev/null || { echo "check-release-notes: gh not installed; skipping"; exit 0; }
  body=$(gh pr view --json body --jq .body 2>/dev/null || true)
  [ -z "$body" ] && { echo "check-release-notes: no PR yet; skipping (pass --body-file to validate intended body)"; exit 0; }
fi

printf '%s\n' "$body" | grep -qE '^```release-notes' || { echo "check-release-notes: body missing \`\`\`release-notes block"; exit 1; }
printf '%s\n' "$body" | grep -qE "^\\[($cats)\\]" || { echo "check-release-notes: \`\`\`release-notes block missing [CATEGORY] line ($cats)"; exit 1; }
echo "check-release-notes: ok"
