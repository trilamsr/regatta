#!/usr/bin/env bash
# check-stale-refs.sh - fail when a PR deletes files but other tracked
# files still reference the deleted basenames. Closes the 8-round-reviewer
# trap where stale refs slipped past adjacent-file review.
#
# Algorithm:
#   1. Resolve base ref (live merge-base preferred over BASE_SHA env).
#   2. List deleted files in PR diff (`--diff-filter=D`).
#   3. For each deleted file, grep its basename + (if .sh) basename-without-
#      -extension across the worktree at HEAD, excluding .git/, vendor/,
#      and the PR diff itself.
#   4. Operator escape: `<!-- stale-refs-justified: <reason ≥4 chars> -->`
#      in the PR body bypasses the gate (use for historical refs in shipped
#      specs per feedback_drop_ceremony).
#
# Exit:
#   0 = no deletions OR all refs cleared OR escape present
#   1 = stale refs found

set -uo pipefail

usage() {
  cat <<EOF >&2
Usage: check-stale-refs.sh [--body-file <path>] [--base <ref>] [--head <ref>]
Closes the stale-ref trap. Per feedback_deletion_sweep_full_repo.
EOF
  exit 3
}

BODY_FILE=""
BASE_REF=""
HEAD_REF=""

while [ $# -gt 0 ]; do
  case "$1" in
    --body-file) BODY_FILE="$2"; shift 2 ;;
    --base)      BASE_REF="$2"; shift 2 ;;
    --head)      HEAD_REF="$2"; shift 2 ;;
    --help|-h)   usage ;;
    *) echo "check-stale-refs: unknown flag $1" >&2; exit 3 ;;
  esac
done

# Operator escape — read PR body if supplied OR via BODY env. In CI
# (GITHUB_REF=refs/pull/N/merge) auto-fetch via `gh pr view` so `make check`
# honors the escape without an explicit --body-file plumb. Local invocations
# without BODY/--body-file remain strict (PR-context unknowable offline).
body=""
if [ -n "$BODY_FILE" ] && [ -f "$BODY_FILE" ]; then
  body=$(cat "$BODY_FILE")
elif [ -n "${BODY:-}" ]; then
  body="$BODY"
elif command -v gh >/dev/null 2>&1; then
  pr=""
  if [ -n "${GITHUB_REF:-}" ]; then
    pr=$(echo "$GITHUB_REF" | sed -n 's|refs/pull/\([0-9]\{1,\}\)/.*|\1|p')
  fi
  if [ -z "$pr" ] && [ -n "${GITHUB_HEAD_REF:-}" ]; then
    pr=$(gh pr list --head "$GITHUB_HEAD_REF" --state open --json number --jq '.[0].number' 2>/dev/null || true)
  fi
  if [ -z "$pr" ]; then
    branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)
    if [ -n "$branch" ] && [ "$branch" != "main" ] && [ "$branch" != "HEAD" ]; then
      pr=$(gh pr list --head "$branch" --state open --json number --jq '.[0].number' 2>/dev/null || true)
    fi
  fi
  if [ -n "$pr" ]; then
    body=$(gh pr view "$pr" --json body --jq .body 2>/dev/null || true)
  fi
fi

if printf '%s' "$body" | grep -qE '<!--[[:space:]]*stale-refs-justified:[[:space:]]*[^[:space:]-][^[:space:]]{3,}[[:space:]]*-->'; then
  echo "check-stale-refs: stale-refs-justified escape present; skipping"
  exit 0
fi

# Resolve base ref. Prefer live origin/main merge-base.
base="${BASE_REF:-${BASE_SHA:-}}"
head="${HEAD_REF:-${HEAD_SHA:-HEAD}}"

if git rev-parse --verify origin/main >/dev/null 2>&1; then
  live_base="$(git merge-base origin/main "$head" 2>/dev/null || echo "")"
  if [ -n "$live_base" ]; then base="$live_base"; fi
fi

if [ -z "$base" ]; then
  echo "check-stale-refs: no base ref resolvable; nothing to check"
  exit 0
fi

# List deleted files in this PR.
deleted_files=$(git diff --name-only --diff-filter=D "$base" "$head" 2>/dev/null || true)

if [ -z "$deleted_files" ]; then
  echo "check-stale-refs: no file deletions in PR diff"
  exit 0
fi

exclude_args=(
  --exclude-dir=.git
  --exclude-dir=vendor
  --exclude-dir=node_modules
  --exclude-dir=.regatta
)

stale_hits=""
deleted_count=0
while IFS= read -r f; do
  [ -z "$f" ] && continue
  bn=$(basename "$f")
  deleted_count=$((deleted_count + 1))

  # Skip generic basenames that would over-match.
  case "$bn" in
    README.md|LICENSE|CHANGELOG.md|.gitignore|.gitattributes|go.mod|go.sum) continue ;;
  esac

  # Stem without extension for scripts.
  stem="${bn%.*}"

  # Word-boundary grep reduces noise. Match either basename or stem — but skip
  # the stem when it names a still-tracked file: deleting a suffixed variant
  # (X.bak, X.operator-runtime) must not flag the live refs to the real file X
  # it shares a stem with. dirname yields "." for root-level files, so the
  # ls-files probe becomes "./stem".
  if [ "$bn" = "$stem" ] || git ls-files --error-unmatch "$(dirname "$f")/$stem" >/dev/null 2>&1; then
    pattern="\\b${bn}\\b"
  else
    pattern="\\b(${bn}|${stem})\\b"
  fi

  hits=$(grep -rnE "$pattern" "${exclude_args[@]}" . 2>/dev/null || true)

  if [ -n "$hits" ]; then
    stale_hits="${stale_hits}--- $f ---"$'\n'"${hits}"$'\n'
  fi
done <<< "$deleted_files"

if [ -n "$stale_hits" ]; then
  echo "check-stale-refs: stale references to deleted files detected:" >&2
  printf '%s' "$stale_hits" | sed 's/^/  /' >&2
  echo >&2
  echo "Fix: remove the stale references, OR add to PR body:" >&2
  echo "  <!-- stale-refs-justified: <reason ≥4 chars> -->" >&2
  echo "  (justified when refs are historical accuracy in shipped specs," >&2
  echo "   per feedback_drop_ceremony)" >&2
  exit 1
fi

echo "check-stale-refs: clean (${deleted_count} deleted file(s), no stale refs)"
