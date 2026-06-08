#!/usr/bin/env bash
# check-spec-sections.sh — fail when a NEW or MODIFIED spec under
# `docs/engineer/specs/` is missing one of the 7 canonical H2 sections:
#
#   1. Problem
#   2. Design
#   3. Acceptance
#   4. Out of scope
#   5. Adversarial
#   6. Implementer brief
#   7. Reopen trigger   (or "Closing trigger" or "Trigger" — synonyms accepted)
#
# Pre-existing specs (unchanged vs $PR_BASE) are warn-only — the gate only
# fails on specs the PR touches. Spec template (`_template.md`), the
# generated `README.md`, anything under `archive/`, and specs carrying
# `spec_type: skeleton-prefetch` / `status: skeleton-prefetch|shipped|archived|superseded`
# in frontmatter are skipped entirely.
#
# Heading matching is keyword-based, case-insensitive on the H2 line —
# `## Problem`, `## §1 Problem`, `## 1. Problem`, `## 1) The Problem` all
# satisfy "Problem". This permits the existing §N / N. / bare prefix
# variation across in-tree specs.
#
# Env overrides (also used by the fixture test):
#   SPECS_DIR  — dir to scan (default: docs/engineer/specs).
#   PR_BASE    — git ref to diff against (default: origin/main).
#   STRICT     — when "1", fail on ANY spec missing sections, not just
#                PR-modified ones. Used by the test fixture.
#   DIFF_FILES — test seam; newline-separated paths to treat as "changed".
#                When set the script uses this instead of running `git diff`.
#
# Exit: 0 clean, 1 on first PR-modified spec with missing section(s).
set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

: "${SPECS_DIR:=$REPO_ROOT/docs/engineer/specs}"
: "${PR_BASE:=origin/main}"
: "${STRICT:=0}"

if [ ! -d "$SPECS_DIR" ]; then
  echo "check-spec-sections: SPECS_DIR not found: $SPECS_DIR (skipping)"
  exit 0
fi

# Required H2 sections — each entry is `label|regex`. The regex is applied
# (case-insensitive) to the heading text after the leading `## ` (and any
# §N / N. / N) prefix). Order matches the canonical template.
required_labels=(
  'Problem'
  'Design'
  'Acceptance'
  'Out of scope'
  'Adversarial'
  'Implementer brief'
  'Reopen trigger'
)
required_patterns=(
  'problem'
  'design'
  'acceptance'
  'out[- ]of[- ]scope'
  'adversarial'
  'implementer[- ]brief|dispatch[- ]brief'
  'reopen[- ]trigger|closing[- ]trigger|^trigger$|^trigger\b'
)

# parse_frontmatter <path> -> "<status>|<spec_type>"
parse_frontmatter() {
  python3 - "$1" <<'PY'
import sys
path = sys.argv[1]
with open(path, "r", encoding="utf-8", errors="replace") as fh:
    lines = fh.read().splitlines()
status = ""
spec_type = ""
if lines and lines[0].strip() == "---":
    end = None
    for i in range(1, len(lines)):
        if lines[i].strip() == "---":
            end = i
            break
    if end is not None:
        for raw in lines[1:end]:
            if ":" not in raw:
                continue
            k, _, v = raw.partition(":")
            k = k.strip().lower()
            v = v.strip()
            if len(v) >= 2 and v[0] == v[-1] and v[0] in ("'", '"'):
                v = v[1:-1]
            if k == "status" and not status:
                status = v.lower()
            elif k == "spec_type" and not spec_type:
                spec_type = v.lower()
print(f"{status}|{spec_type}")
PY
}

is_skipped_meta() {
  # $1 = status, $2 = spec_type
  case "$1" in
    shipped|archived|superseded|skeleton-prefetch) return 0 ;;
  esac
  case "$2" in
    skeleton-prefetch) return 0 ;;
  esac
  return 1
}

# Returns 0 when `$specfile` (path relative to repo root) was added or
# modified vs $PR_BASE.
is_changed_in_pr() {
  local rel="$1"
  if [ -n "${DIFF_FILES+x}" ]; then
    printf '%s\n' "$DIFF_FILES" | grep -qxF "$rel"
    return $?
  fi
  if ! git -C "$REPO_ROOT" rev-parse --verify "$PR_BASE" >/dev/null 2>&1; then
    return 1
  fi
  git -C "$REPO_ROOT" diff --name-only --diff-filter=AM "$PR_BASE"...HEAD -- "$rel" 2>/dev/null | grep -qxF "$rel"
}

# missing_sections <path> -> stdout: comma-separated missing labels (empty when complete).
missing_sections() {
  local path="$1"
  local headings
  # Extract every H2 line, strip leading `## `, strip any `§N`, `N.`, `N)`
  # prefix tokens, lowercase for matching.
  headings=$(grep -E '^## ' "$path" \
    | sed -E 's/^## +//' \
    | sed -E 's/^§[0-9]+ +//; s/^[0-9]+[.)] +//' \
    | tr '[:upper:]' '[:lower:]')

  local missing=""
  local i
  for i in "${!required_labels[@]}"; do
    local label="${required_labels[$i]}"
    local pat="${required_patterns[$i]}"
    if ! printf '%s\n' "$headings" | grep -qE -- "$pat"; then
      if [ -z "$missing" ]; then
        missing="$label"
      else
        missing="$missing, $label"
      fi
    fi
  done
  printf '%s' "$missing"
}

scanned=0
warn=0
fail=0
fail_lines=""
warn_lines=""

while IFS= read -r -d '' specfile; do
  base=$(basename -- "$specfile")
  case "$base" in
    _template.md|README.md) continue ;;
  esac

  meta=$(parse_frontmatter "$specfile")
  status="${meta%|*}"
  spec_type="${meta##*|}"
  if is_skipped_meta "$status" "$spec_type"; then
    continue
  fi

  scanned=$((scanned + 1))
  missing=$(missing_sections "$specfile")
  [ -z "$missing" ] && continue

  rel="${specfile#$REPO_ROOT/}"
  if [ "$STRICT" = "1" ] || is_changed_in_pr "$rel"; then
    fail=$((fail + 1))
    fail_lines="${fail_lines}${rel}: missing section(s): ${missing}"$'\n'
  else
    warn=$((warn + 1))
    warn_lines="${warn_lines}${rel}: missing section(s): ${missing}"$'\n'
  fi
done < <(find "$SPECS_DIR" -maxdepth 1 -type f -name '*.md' -print0)

if [ "$warn" -gt 0 ]; then
  echo "check-spec-sections: WARN — $warn pre-existing spec(s) missing canonical sections (not gated):"
  printf '%s' "$warn_lines" | sed 's/^/  - /'
fi

if [ "$fail" -gt 0 ]; then
  echo "check-spec-sections: FAIL — $fail PR-modified spec(s) missing canonical sections:" >&2
  printf '%s' "$fail_lines" | sed 's/^/  - /' >&2
  echo >&2
  echo "Required H2 sections (see docs/engineer/specs/_template.md):" >&2
  for l in "${required_labels[@]}"; do echo "  - $l" >&2; done
  echo >&2
  echo "Opt-out: frontmatter \`spec_type: skeleton-prefetch\` OR \`status: skeleton-prefetch|shipped|archived|superseded\`." >&2
  exit 1
fi

echo "check-spec-sections: ok — $scanned spec(s) scanned, $warn pre-existing warn, 0 PR-modified missing"
exit 0
