#!/usr/bin/env bash
# check-byte-equal-pin.sh - fail when a PR body claims its target/gate/route/
# schema set is byte-equal pre/post but the diff carries no mechanical drift
# gate.
#
# Drift pattern this closes: CLAUDE.md §CI gates "Byte-equal-refactor pin"
# rule (#985) mandates that any refactor whose correctness story is
# "set is byte-equal pre/post" MUST ship a mechanical gate — the PR-body
# claim alone is rejected because drift surfaces in the next sibling PR,
# not the current one. The rule was documented but had no enforcement until
# now; this gate is the enforcement (#1031).
#
# Gate:
#   Inputs:
#     --body-file <path>             PR body markdown.
#     --changed-paths-file <path>    newline-delimited paths from `git diff`.
#
#   Detection: the body (outside any ```-fenced block, to ignore stale quotes)
#   carries a line matching:
#     - "byte[- ]?equal" (case-insensitive)
#   AND
#     - one of these noun anchors near it: target, gate, route, schema, set,
#       list, table, map.
#
#   Pin requirements (any ONE of):
#     1. A changed path matching `scripts/check-*-parity.sh` (the canonical
#        drift-gate template, e.g. scripts/check-prompt-parity.sh).
#     2. A changed Go test file whose name matches `*byte_equal*_test.go` OR
#        `*parity*_test.go` (case-insensitive). Test-name shape Test*ByteEqual*
#        or Test*Parity* is conventionally encoded in file naming; we match
#        files because the diff lists paths, not symbols.
#     3. An operator escape comment in the body:
#        `<!-- byte-equal-justified: <reason> -->` (reason required, ≥4 chars).
#
# Exit:
#   0 pass — no claim, claim with mechanical pin, or operator escape.
#   1 fail — claim detected, no pin or escape.
#   3 usage error.
set -uo pipefail

BODY_FILE=""
PATHS_FILE=""

while [ $# -gt 0 ]; do
  case "$1" in
    --body-file) BODY_FILE="$2"; shift 2 ;;
    --changed-paths-file) PATHS_FILE="$2"; shift 2 ;;
    -h|--help)
      grep '^#' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "check-byte-equal-pin: unknown flag: $1" >&2
      exit 3
      ;;
  esac
done

if [ -z "$BODY_FILE" ] || [ ! -f "$BODY_FILE" ]; then
  echo "check-byte-equal-pin: --body-file <path> required" >&2
  exit 3
fi
if [ -z "$PATHS_FILE" ] || [ ! -f "$PATHS_FILE" ]; then
  echo "check-byte-equal-pin: --changed-paths-file <path> required" >&2
  exit 3
fi

# Strip ```-fenced blocks from the body so a claim quoted inside a stale
# example, a release-notes fence, or any other code fence does not count as
# a load-bearing assertion of the current PR. Mirrors check-reviewer-verdict.sh.
BODY_STRIPPED=$(awk '
  /^```/ { in_fence = !in_fence; next }
  !in_fence { print }
' "$BODY_FILE")

# Detection: case-insensitive "byte-equal" or "byte equal" plus a nearby
# noun anchor on the same line.
CLAIM_HIT=$(printf '%s\n' "$BODY_STRIPPED" \
  | grep -iE 'byte[- ]?equal' \
  | grep -iE '(target|gate|route|schema|set|list|table|map)' \
  | head -1 || true)

if [ -z "$CLAIM_HIT" ]; then
  exit 0
fi

# Operator escape: `<!-- byte-equal-justified: <reason> -->` (reason ≥4 chars).
if printf '%s\n' "$BODY_STRIPPED" \
  | grep -qiE '<!--[[:space:]]*byte-equal-justified:[[:space:]]*.{4,}[[:space:]]*-->'; then
  exit 0
fi

# Pin requirement: scan changed paths for a parity script or a byte-equal /
# parity Go test file. Case-insensitive on basename so naming variations
# (ByteEqual, byte_equal, byteequal, Parity, parity) all count.
PIN_HIT=""
while IFS= read -r changed_path; do
  [ -z "$changed_path" ] && continue
  case "$changed_path" in
    scripts/check-*-parity.sh)
      PIN_HIT="$changed_path"
      break
      ;;
  esac
  base=$(basename "$changed_path" | tr '[:upper:]' '[:lower:]')
  case "$base" in
    *byte_equal*_test.go|*byteequal*_test.go|*parity*_test.go)
      PIN_HIT="$changed_path"
      break
      ;;
  esac
done < "$PATHS_FILE"

if [ -n "$PIN_HIT" ]; then
  exit 0
fi

cat >&2 <<EOF
check-byte-equal-pin: PR body claims a byte-equal set but no mechanical pin landed.
  Claim line: ${CLAIM_HIT}
  Per CLAUDE.md §CI gates "Byte-equal-refactor pin" (#985, enforced #1031):
  PR-body claim alone is rejected — drift surfaces in the next sibling PR.

  Resolution (any one of):
    1. Add a parity drift gate: scripts/check-<thing>-parity.sh
       (template: scripts/check-prompt-parity.sh).
    2. Add a Go test whose file matches *byte_equal*_test.go OR *parity*_test.go
       (test name conventionally Test*ByteEqual* / Test*Parity*).
    3. Add operator escape in PR body (reason required):
       <!-- byte-equal-justified: <reason ≥4 chars> -->
EOF
exit 1
