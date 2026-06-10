#!/usr/bin/env bash
# check-pr-body-close-keywords.sh — fail when a PR body uses the
# space-separated `closes #N #M #M+` form. GitHub auto-close only
# closes the FIRST issue number on such a line; the rest stay open.
#
# Catches the 2026-06-08 trap where PR titles like
# "[FIX] foo closes #A #B #C" only closes the first; GitHub ignores #B #C.
#
# Inputs:
#   --pr <number>        fetch body via `gh pr view`
#   --body-file <path>   read body from a local file (CI + fixtures)
#   --title-file <path>  optional; also scan the PR title (single line)
#   --skip               short-circuit pass (operator-discretion escape)
#
# Exit:
#   0 — body clean OR no close keywords present.
#   1 — body contains `closes #N #M` (or `fixes`/`resolves` variants); details on stderr.
#   2 — usage error.

set -uo pipefail

PR_NUM=""
BODY_FILE=""
TITLE_FILE=""
SKIP=0

while [ $# -gt 0 ]; do
  case "$1" in
    --pr) PR_NUM="$2"; shift 2 ;;
    --body-file) BODY_FILE="$2"; shift 2 ;;
    --title-file) TITLE_FILE="$2"; shift 2 ;;
    --skip) SKIP=1; shift ;;
    -h|--help) grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "check-pr-body-close-keywords: unknown flag: $1" >&2; exit 2 ;;
  esac
done

if [ "$SKIP" -eq 1 ]; then exit 0; fi

if [ -n "$PR_NUM" ] && [ -n "$BODY_FILE" ]; then
  echo "check-pr-body-close-keywords: --pr and --body-file are mutually exclusive" >&2
  exit 2
fi

if [ -n "$PR_NUM" ]; then
  BODY_FILE=$(mktemp)
  TITLE_FILE=$(mktemp)
  trap 'rm -f "$BODY_FILE" "$TITLE_FILE"' EXIT
  gh pr view "$PR_NUM" --json body --jq '.body' > "$BODY_FILE" 2>/dev/null || {
    echo "check-pr-body-close-keywords: gh pr view $PR_NUM failed" >&2
    exit 2
  }
  gh pr view "$PR_NUM" --json title --jq '.title' > "$TITLE_FILE" 2>/dev/null || true
fi

if [ -z "$BODY_FILE" ] || [ ! -f "$BODY_FILE" ]; then
  echo "check-pr-body-close-keywords: no body source (use --pr or --body-file)" >&2
  exit 2
fi

violations=0
scan_file() {
  local label=$1
  local file=$2
  [ -f "$file" ] || return 0
  # Match "<keyword> #N <space> #M" — i.e. the multi-issue space-separated form.
  # The keyword set is GitHub's auto-close family (case-insensitive).
  # A clean form has at most one #N per keyword, OR commas between keywords.
  while IFS= read -r match; do
    [ -z "$match" ] && continue
    printf '%s: bad close-keyword form: %s\n' "$label" "$match" >&2
    printf '  Fix: write "closes #N, closes #M" (comma + repeated keyword) or one keyword per line.\n' >&2
    violations=$((violations + 1))
  done < <(grep -iE '\b(closes|fixes|resolves)[[:space:]]+#[0-9]+([[:space:]]+#[0-9]+)+\b' "$file" || true)
}

scan_file "PR body" "$BODY_FILE"
if [ -n "$TITLE_FILE" ]; then
  scan_file "PR title" "$TITLE_FILE"
fi

if [ "$violations" -gt 0 ]; then
  echo "check-pr-body-close-keywords: $violations bad close-keyword line(s) found" >&2
  exit 1
fi

exit 0
