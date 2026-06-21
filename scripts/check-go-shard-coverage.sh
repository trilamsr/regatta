#!/usr/bin/env bash
# check-go-shard-coverage.sh - fail when the union of per-shard package
# lists under scripts/go-shards/shard-*.txt does not exactly equal
# `go list ./...`. Mechanical drift gate per feedback_byte_equal_pin.
#
# Why this gate: ci.yml fans verify-go into 4 file-disjoint shards
# whose union MUST cover every Go package. A new package added without
# a shard assignment is silently dropped from CI. This gate fails
# closed when the union diverges from `go list ./...`.
set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

: "${SHARD_DIR:=$REPO_ROOT/scripts/go-shards}"
: "${GO_LIST_CMD:=go list ./...}"

if [ ! -d "$SHARD_DIR" ]; then
  echo "check-go-shard-coverage: SHARD_DIR not found: $SHARD_DIR" >&2
  exit 2
fi

shopt -s nullglob
shard_files=("$SHARD_DIR"/shard-*.txt)
shopt -u nullglob
if [ "${#shard_files[@]}" -eq 0 ]; then
  echo "check-go-shard-coverage: no shard-*.txt files under $SHARD_DIR" >&2
  exit 1
fi

tmp=$(mktemp -d -t go-shard-cov.XXXXXX)
trap 'rm -rf "$tmp"' EXIT

union="$tmp/union.txt"
expected="$tmp/expected.txt"
: > "$union"

all_pkgs="$tmp/all-pkgs.txt"
: > "$all_pkgs"
for f in "${shard_files[@]}"; do
  # strip blank lines + comments
  grep -vE '^[[:space:]]*(#|$)' "$f" >> "$all_pkgs" || true
done

# duplicate detection: same package in 2+ shards (run before dedupe)
dupes="$tmp/dupes.txt"
sort "$all_pkgs" | uniq -d > "$dupes" || true
if [ -s "$dupes" ]; then
  echo "check-go-shard-coverage: packages appear in multiple shards:" >&2
  sed 's/^/  /' "$dupes" >&2
  exit 1
fi

sort -u "$all_pkgs" > "$union"

# Allow caller override (tests) but default to repo root
if [ -n "${EXPECTED_LIST_FILE:-}" ]; then
  sort -u "$EXPECTED_LIST_FILE" > "$expected"
else
  (cd "$REPO_ROOT" && $GO_LIST_CMD) | sort -u > "$expected"
fi

if ! diff -u "$expected" "$union" > "$tmp/diff.txt"; then
  echo "check-go-shard-coverage: shard union != \`$GO_LIST_CMD\`" >&2
  echo "  Fix: edit scripts/go-shards/shard-*.txt so the union matches." >&2
  echo "" >&2
  echo "Missing from shards (need to add):" >&2
  comm -23 "$expected" "$union" | sed 's/^/  + /' >&2
  echo "" >&2
  echo "In shards but not in \`go list\` (stale, need to remove):" >&2
  comm -13 "$expected" "$union" | sed 's/^/  - /' >&2
  exit 1
fi

echo "check-go-shard-coverage: ${#shard_files[@]} shards cover $(wc -l < "$expected" | tr -d ' ') packages, no overlap."
