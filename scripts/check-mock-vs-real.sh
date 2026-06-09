#!/usr/bin/env bash
# check-mock-vs-real.sh - WARN gate on mock-vs-real test infra ratio.
#
# Rule (per CLAUDE.md §TDD + review):
#   New *_test.go files in the PR diff should not lean overwhelmingly on
#   mock fakes vs real-infra primitives. Ratio > 70% mock among matched
#   tokens emits a WARN (never blocks). Operator escape: PR body marker
#       <!-- mock-ratio-justified: <reason ≥4 chars> -->
#
# Token heuristics (case-sensitive grep on raw bytes):
#   mock_count = matches of /mock | Mock[A-Z] | gomock | testify/mock
#   real_count = matches of t.TempDir | httptest.NewServer | state.Open(
#
# Ratio formula: mock / (mock + real); > 0.70 over NEW test files → WARN.
# Files with zero matched tokens are skipped (no signal).
#
# Inputs:
#   --base <ref>        base ref (default origin/main); diff cone is base...HEAD
#   --body-file <path>  PR body for allowlist marker (optional)
#   --pr <number>       fetch body via gh (optional)
#
# Exit:
#   0 always — this gate is WARN-only by design (operator-manual; not in `check`).

set -euo pipefail

usage() {
  cat <<'USAGE'
check-mock-vs-real.sh — WARN when NEW *_test.go files lean >70% on mocks.

Usage:
  check-mock-vs-real.sh [--base <ref>] [--body-file <path>] [--pr <n>]

Escape: include in PR body
  <!-- mock-ratio-justified: <reason ≥4 chars> -->
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
      echo "check-mock-vs-real: unknown arg: $1" >&2
      usage >&2
      exit 64
      ;;
  esac
done

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "check-mock-vs-real: not a git tree" >&2
  exit 0
fi

if ! git rev-parse --verify "$base" >/dev/null 2>&1; then
  echo "check-mock-vs-real: base ref not found: $base (skip)"
  exit 0
fi

body=""
if [ -n "$body_file" ] && [ -f "$body_file" ]; then
  body=$(cat "$body_file")
elif [ -n "$pr_number" ] && command -v gh >/dev/null 2>&1; then
  body=$(gh pr view "$pr_number" --json body --jq .body 2>/dev/null || true)
fi

if printf '%s' "$body" | grep -qE 'mock-ratio-justified:[[:space:]]*[^[:space:]<]{4,}'; then
  echo "check-mock-vs-real: allowlisted via PR body marker (justified)"
  exit 0
fi

changed=$(git diff --name-only --diff-filter=A "$base"...HEAD -- '*_test.go' 2>/dev/null || true)

if [ -z "$changed" ]; then
  echo "check-mock-vs-real: ok (no NEW *_test.go vs $base)"
  exit 0
fi

mock_total=0
real_total=0
files_scanned=0

while IFS= read -r f; do
  [ -z "$f" ] && continue
  [ ! -f "$f" ] && continue
  files_scanned=$((files_scanned + 1))
  m=$(grep -cE '/mock|Mock[A-Z]|gomock|testify/mock' "$f" 2>/dev/null || true)
  r=$(grep -cE 't\.TempDir|httptest\.NewServer|state\.Open\(' "$f" 2>/dev/null || true)
  mock_total=$((mock_total + m))
  real_total=$((real_total + r))
done <<<"$changed"

if [ "$files_scanned" -eq 0 ]; then
  echo "check-mock-vs-real: ok (no new test files on disk)"
  exit 0
fi

total=$((mock_total + real_total))
if [ "$total" -eq 0 ]; then
  echo "check-mock-vs-real: ok (${files_scanned} new test file(s); zero matched tokens — no signal)"
  exit 0
fi

# Integer ratio: mock_pct = mock * 100 / total; WARN when > 70.
mock_pct=$(( mock_total * 100 / total ))

if [ "$mock_pct" -gt 70 ]; then
  echo "::warning::mock-vs-real ratio ${mock_pct}% (mock=${mock_total} real=${real_total}) across ${files_scanned} new test file(s) exceeds 70% mock ceiling"
  echo "check-mock-vs-real: WARN (not blocking) — add real-infra coverage (t.TempDir / httptest / state.Open) or justify via <!-- mock-ratio-justified: <reason> -->"
  exit 0
fi

echo "check-mock-vs-real: ok (mock=${mock_total} real=${real_total} → ${mock_pct}% mock across ${files_scanned} new test file(s))"
exit 0
