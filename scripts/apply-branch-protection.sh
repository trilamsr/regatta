#!/usr/bin/env bash
# apply-branch-protection.sh - apply the rules described in
# .github/branch-protection.yml to the `main` branch via gh api.
# Idempotent; requires `gh auth login` with `repo` scope.
#
# Companion to regatta's own `verifyrepo` audit. apply-* writes the
# intent into the live repo; verifyrepo asserts the live repo matches.
# Keep both in sync with .github/branch-protection.yml.
#
# To change a setting: edit .github/branch-protection.yml (intent),
# update the JSON payload below (live state), update verifyrepo's
# expected values, then re-run.
set -euo pipefail

if ! command -v gh >/dev/null 2>&1; then
    echo "error: 'gh' CLI is required (https://cli.github.com/)" >&2
    exit 1
fi

repo=$(gh repo view --json nameWithOwner --jq '.nameWithOwner' 2>/dev/null || true)
if [ -z "$repo" ]; then
    echo "error: could not determine repo (run from a clone with origin set)" >&2
    exit 1
fi

echo "Applying branch protection to $repo:main ..."

gh api \
    -X PUT "repos/${repo}/branches/main/protection" \
    -H "Accept: application/vnd.github+json" \
    --input - <<'JSON'
{
  "required_status_checks": {
    "strict": false,
    "contexts": [
      "verify",
      "pr-lint"
    ]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "required_approving_review_count": 0,
    "require_code_owner_reviews": false,
    "dismiss_stale_reviews": true
  },
  "restrictions": null,
  "required_linear_history": true,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "required_conversation_resolution": true,
  "required_signatures": false
}
JSON

echo "Branch protection applied. Verify in Settings -> Branches or with:"
echo "  gh api repos/${repo}/branches/main/protection --jq ."
