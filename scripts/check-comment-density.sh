#!/usr/bin/env bash
# check-comment-density.sh - comment-density gate on new prod .go files.
#
# Rule (per CLAUDE.md §Comments + implementer.md §Comments):
#   New prod *.go files in the PR diff must hold comment density ≤ 5%.
#   Files that pre-existed on base get a warn-only ceiling of 10% on the
#   added LOC. Operator escape: PR body marker
#       <!-- comment-density-justified: <reason> -->
#
# Density formula:
#   single-line `// ` comments / total LOC (post-strip blank).
#   Multi-line /* ... */ blocks are NOT scanned — repo convention is `//`.
#
# Inputs:
#   --base <ref>        base ref (default origin/main); diff cone is base...HEAD
#   --body-file <path>  PR body for allowlist marker (optional)
#   --pr <number>       fetch body via gh (optional)
#
# Exit:
#   0 pass (clean, or warn-only, or allowlisted)
#   1 fail (new prod file > 5% density)

set -euo pipefail

usage() {
  cat <<'USAGE'
check-comment-density.sh — fail when new prod .go files exceed 5% comment density.

Usage:
  check-comment-density.sh [--base <ref>] [--body-file <path>] [--pr <n>]

Escape: include in PR body
  <!-- comment-density-justified: <reason> -->
USAGE
}

base="origin/main"
body_file=""
pr_number=""

while [ $# -gt 0 ]; do
  case "$1" in
    --base)      base="${2:-}"; shift 2 ;;
    --body-file) body_file="${2:-}"; shift 2 ;;
    --pr)        pr_number="${2:-}"; shift 2 ;;
    -h|--help)   usage; exit 0 ;;
    *)
      echo "check-comment-density: unknown arg: $1" >&2
      usage >&2
      exit 64
      ;;
  esac
done

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "check-comment-density: not a git tree" >&2
  exit 64
fi

if ! git rev-parse --verify "$base" >/dev/null 2>&1; then
  echo "check-comment-density: base ref not found: $base (skip)"
  exit 0
fi

body=""
if [ -n "$body_file" ] && [ -f "$body_file" ]; then
  body=$(cat "$body_file")
elif [ -n "$pr_number" ] && command -v gh >/dev/null 2>&1; then
  body=$(gh pr view "$pr_number" --json body --jq .body 2>/dev/null || true)
fi

if printf '%s' "$body" | grep -qE 'comment-density-justified:'; then
  echo "check-comment-density: allowlisted via PR body marker (justified)"
  exit 0
fi

changed=$(git diff --name-only --diff-filter=AM "$base"...HEAD -- '*.go' 2>/dev/null || true)

prod_files=$(printf '%s\n' "$changed" | grep -E '\.go$' | grep -vE '_test\.go$' || true)

if [ -z "$prod_files" ]; then
  echo "check-comment-density: ok (no prod .go changes vs $base)"
  exit 0
fi

density_for() {
  local file="$1"
  if [ ! -f "$file" ]; then
    printf '0 0'
    return
  fi
  local loc cmt
  loc=$(grep -cvE '^[[:space:]]*$' "$file" || true)
  cmt=$(grep -cE '^[[:space:]]*//' "$file" || true)
  if [ "$loc" -eq 0 ]; then
    printf '0 0'
    return
  fi
  printf '%s %s' "$cmt" "$loc"
}

fail=0
warn=0

while IFS= read -r f; do
  [ -z "$f" ] && continue

  if git cat-file -e "$base":"$f" 2>/dev/null; then
    pre_existing=1
  else
    pre_existing=0
  fi

  read -r cmt loc <<<"$(density_for "$f")"
  if [ "$loc" -eq 0 ]; then continue; fi

  pct=$(( cmt * 100 / loc ))

  if [ "$pre_existing" -eq 0 ]; then
    if [ "$pct" -gt 5 ]; then
      echo "::error::comment-density fail: $f at ${pct}% (${cmt}/${loc} lines) exceeds 5% ceiling for NEW prod files"
      fail=$((fail + 1))
    fi
  else
    if [ "$pct" -gt 10 ]; then
      echo "::warning::comment-density warn: $f at ${pct}% (${cmt}/${loc} lines) exceeds 10% drift ceiling on existing file"
      warn=$((warn + 1))
    fi
  fi
done <<<"$prod_files"

if [ "$fail" -gt 0 ]; then
  echo "check-comment-density: ${fail} new prod .go file(s) over 5% density."
  echo "  Cut WHAT-narration to bare WHY-only — or add <!-- comment-density-justified: <reason> --> to PR body."
  exit 1
fi

if [ "$warn" -gt 0 ]; then
  echo "check-comment-density: ${warn} warn-only (>10% on pre-existing files); not blocking."
  exit 0
fi

echo "check-comment-density: ok ($(printf '%s\n' "$prod_files" | wc -l | tr -d ' ') prod file(s) scanned)"
exit 0
