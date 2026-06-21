#!/usr/bin/env bash
# Local pre-push PR-body hygiene catch — fail/warn before pr-lint wastes a CI
# round-trip. Two checks, two failure classes:
#   MAY-100 fence:  the intended PR body has a ```release-notes block + [CATEGORY].
#   MAY-73 placement: the Reviewer-recommendation: APPROVE token sits in a COMMIT
#                     MESSAGE instead of the PR body — check-reviewer-verdict reads
#                     the body only, so a misplaced token ships review-less.
#
# Usage:
#   check-release-notes-local.sh                  # body from open PR (no PR => skip)
#   check-release-notes-local.sh --body-file F    # validate F as the intended PR body
#   check-release-notes-local.sh --scan-commits R # warn if any commit in range R
#                                                 #   (e.g. origin/main..HEAD) carries
#                                                 #   a Reviewer-recommendation: token

set -euo pipefail
cats='FEATURE|CHANGE|BUGFIX|FIX|SECURITY|PERF|DOCS|CHORE|CI|REFACTOR'

# MAY-73: scan commit messages in a push range for a misplaced verdict token.
# Exit non-zero so a pre-push hook can surface it (still bypassable with
# --no-verify); the token belongs in the PR body, not a commit message.
if [ "${1:-}" = "--scan-commits" ]; then
  range="${2:-}"
  [ -n "$range" ] || { echo "check-release-notes: --scan-commits requires a commit range"; exit 2; }
  hits=$(git log --format='%H %s' "$range" 2>/dev/null \
    | while read -r sha _; do
        if git log -1 --format='%B' "$sha" | grep -qiE '^Reviewer-(recommendation|agent-id):'; then
          echo "$sha"
        fi
      done) || true
  if [ -n "$hits" ]; then
    echo "check-release-notes: Reviewer-recommendation/agent-id token found in commit message(s):" >&2
    printf '  %s\n' $hits >&2
    echo "  -> that token belongs in the PR BODY (check-reviewer-verdict reads the body only)." >&2
    echo "  -> move it to the PR body before push; bypass with 'git push --no-verify' if intentional." >&2
    exit 1
  fi
  echo "check-release-notes: no misplaced verdict token in commit range"
  exit 0
fi

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
