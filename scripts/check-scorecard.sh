#!/usr/bin/env bash
# check-scorecard.sh - per-criterion citation gate.
#
# Drift pattern this closes: authors vibes-grade scorecard tiers; reviewers
# do not independently verify. Result: criteria silently fail with no test
# anchor, no file:line, no tracking issue. Audit confirmed 5 wedges over-
# graded ~1.5 tiers in a single session. This gate forces every PASS mark
# to cite evidence in the same line so the claim is falsifiable in CI.
#
# Rule:
#   For every criterion in the scorecard section marked `[x]` (or `[X]`),
#   the same line MUST contain at least one of:
#     a) a test-name token matching ^Test|^Fuzz|^Benchmark|^Example
#     b) a file:line anchor like path/to/file.go:42
#     c) an issue ref `#NNN` (tracking deferral)
#     d) an explicit N/A rationale: `N/A — <text>` or `N-A — <text>`
#        or DEFERRED / `see #NNN` / `(see #NNN)`
#
# Exemptions (skip the gate):
#   - PR body has no scorecard section AND release-notes category is
#     [CHORE] / [DOCS] / [CI] / [NONE] / [CHANGE] / [REFACTOR].
#   - --skip flag passed (caller-side decision, used by CI when category
#     is exempt).
#
# Inputs:
#   --pr <number>       fetch body via `gh pr view`
#   --body-file <path>  read body from a local file (fixtures + offline)
#   --skip              short-circuit pass (for CI category-skip wiring)
#
# Exit:
#   0 on pass (all marks cited, or category-exempt).
#   1 on missing/empty scorecard when required.
#   2 on uncited [x] mark — error message lists each offender + line no.

set -euo pipefail

usage() {
  cat <<'USAGE'
check-scorecard.sh — enforce per-criterion citations in PR scorecard.

Usage:
  check-scorecard.sh --pr <number>
  check-scorecard.sh --body-file <path>
  check-scorecard.sh --skip

Reads PR body, parses the scorecard section, fails if any [x] mark
lacks a Test*-name / file:line / #issue / N-A citation.
USAGE
}

pr_number=""
body_file=""
skip=0

while [ $# -gt 0 ]; do
  case "$1" in
    --pr)
      pr_number="${2:-}"
      shift 2
      ;;
    --body-file)
      body_file="${2:-}"
      shift 2
      ;;
    --skip)
      skip=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "check-scorecard: unknown arg: $1" >&2
      usage >&2
      exit 64
      ;;
  esac
done

if [ "$skip" -eq 1 ]; then
  echo "check-scorecard: skipped (caller flagged category-exempt)"
  exit 0
fi

# Load body text.
if [ -n "$body_file" ]; then
  if [ ! -f "$body_file" ]; then
    echo "check-scorecard: --body-file not found: $body_file" >&2
    exit 64
  fi
  body=$(cat "$body_file")
elif [ -n "$pr_number" ]; then
  if ! command -v gh >/dev/null 2>&1; then
    echo "check-scorecard: gh CLI not available; need --body-file for offline" >&2
    exit 64
  fi
  body=$(gh pr view "$pr_number" --json body --jq .body)
else
  echo "check-scorecard: must pass --pr <n>, --body-file <path>, or --skip" >&2
  usage >&2
  exit 64
fi

# Category short-circuit: the release-notes fence is the source of truth
# for PR type (pr-lint.yml validates it upstream). [CHORE]/[DOCS]/[CI]/
# [NONE]/[CHANGE]/[REFACTOR] PRs are exempt regardless of whether the
# author pasted a partial/malformed scorecard skeleton (#853 — was
# falling through scorecard parse and failing on uncited rows).
category=$(printf '%s\n' "$body" | awk '
  /^```release-notes/ { capture=1; next }
  capture && /^```/    { exit }
  capture              { print }
' | grep -oE '\[[A-Z]+\]' | head -1 || true)

case "$category" in
  "[CHORE]"|"[DOCS]"|"[CI]"|"[NONE]"|"[CHANGE]"|"[REFACTOR]")
    echo "check-scorecard: release-notes $category is exempt"
    exit 0
    ;;
esac

# Locate scorecard section. Match common header variants (case-insensitive).
# Captures from the header until the next `##` heading or EOF.
section=$(printf '%s\n' "$body" | awk '
  BEGIN { capture=0 }
  /^## / {
    if (capture) { exit }
    # Case-insensitive header match for "scorecard" or "rubric".
    h=tolower($0)
    if (h ~ /scorecard/ || h ~ /rubric/) { capture=1; next }
  }
  capture { print }
')

if [ -z "$(printf '%s' "$section" | tr -d '[:space:]')" ]; then
  echo "::error::No scorecard section found in PR body." >&2
  echo "::error::NOTE: [CHORE]/[DOCS]/[CI]/[NONE]/[CHANGE]/[REFACTOR] PRs auto-skip the scorecard requirement, but ONLY when the release-notes prefix appears INSIDE a triple-backtick \`release-notes\` fenced block. If you intended a chore-class PR, wrap your release-notes line in a fence:" >&2
  echo "::error::    \`\`\`release-notes" >&2
  echo "::error::    [CHORE] <one-line summary>" >&2
  echo "::error::    \`\`\`" >&2
  echo "::error::Otherwise add a '## A+ Scorecard' (or 'Rubric Scorecard') section with cited criteria per docs/engineer/dispatch-templates/reviewer.md." >&2
  exit 1
fi

# Scan every line in the scorecard section that carries an `[x]` mark.
# Each mark on a line is a separate criterion claim and must carry
# citation evidence on that same line.
#
# Citation regexes (any one satisfies):
#   test name:   \b(Test|Fuzz|Benchmark|Example)[A-Z_][A-Za-z0-9_]+
#   file:line:   [A-Za-z0-9_./-]+\.[A-Za-z]+:[0-9]+
#   issue ref:   #[0-9]+
#   N/A clause:  N/A|N-A|DEFERRED|see\s+#[0-9]+
#
# `[x]` markers inside an inline-code span (backticks) are ignored to
# prevent false positives from prose like "use `[x]` to mark done".
# Strategy: strip backtick spans only for the [x] count; scan citations
# against the original line so backtick-wrapped cites stay visible (#741).
# Fenced ``` blocks are intentionally kept — most scorecards live inside.

total=0
missing=()
line_no=0
while IFS= read -r line; do
  line_no=$((line_no + 1))
  # Backtick-stripped view: defends [x] count from prose like "use `[x]`".
  scrub=$(printf '%s' "$line" | sed -E 's/`[^`]*`//g')
  # Trailing `|| true` swallows grep's no-match exit-1 under
  # `set -o pipefail` so the command substitution stays clean.
  count=$({ printf '%s' "$scrub" | grep -oE '\[[xX]\]' || true; } | wc -l | tr -d ' ')
  if [ "$count" -eq 0 ]; then
    continue
  fi
  total=$((total + count))
  # Citation scan uses the ORIGINAL line so backticked cites count (#741).
  has_cite=0
  # Test-name token.
  if printf '%s' "$line" | grep -qE '\b(Test|Fuzz|Benchmark|Example)[A-Z_][A-Za-z0-9_]+'; then
    has_cite=1
  fi
  # file:line anchor.
  if [ "$has_cite" -eq 0 ] && printf '%s' "$line" | grep -qE '[A-Za-z0-9_./-]+\.[A-Za-z]+:[0-9]+'; then
    has_cite=1
  fi
  # Issue reference.
  if [ "$has_cite" -eq 0 ] && printf '%s' "$line" | grep -qE '#[0-9]+'; then
    has_cite=1
  fi
  # N/A clause (case-sensitive on the literal; rationale text after).
  if [ "$has_cite" -eq 0 ] && printf '%s' "$line" | grep -qE 'N/A|N-A|DEFERRED'; then
    has_cite=1
  fi
  if [ "$has_cite" -eq 0 ]; then
    # Identify the criterion code(s) on the line for the operator
    # message. Common formats: `(a)`, `B1`, `A2`, `A+1`.
    crit=$({ printf '%s' "$line" | grep -oE '\(([a-z]|[0-9]+)\)|\b[ABC][+]?[0-9]+\b' || true; } | tr '\n' ' ')
    if [ -z "$crit" ]; then crit="(unlabeled)"; fi
    # For lines with multiple [x] all sharing the line, surface each
    # uncited count. We treat the whole line as one offender row.
    missing+=("line ${line_no}: ${count} [x] mark(s) uncited — criteria ${crit}— no Test*/file:line/#issue/N-A on row")
  fi
done <<EOF
$section
EOF

if [ "$total" -eq 0 ]; then
  # Exempt categories already short-circuited at top; here we know the
  # PR is scorecard-required and the section has no [x] marks.
  echo "::error::Scorecard section present but contains no [x] marks. Mark at least one criterion." >&2
  exit 1
fi

if [ "${#missing[@]}" -gt 0 ]; then
  echo "::error::Scorecard has uncited [x] marks (${#missing[@]} offending row(s) / ${total} total criteria scanned):" >&2
  for m in "${missing[@]}"; do
    echo "  - $m" >&2
  done
  echo "::error::Each [x] MUST cite Test*-name OR file:line OR #issue OR N/A rationale on the SAME line." >&2
  exit 2
fi

echo "scorecard: ${total}/${total} criteria cited"
exit 0
