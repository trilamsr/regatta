#!/usr/bin/env bash
# go-check-shard.sh <N> - run verify-go shard <N> by partitioning packages
# from scripts/go-shards/shard-<N>.txt against scripts/race-pkgs.txt and
# invoking `go test -short -race` on the race-pkgs subset + `go test -short`
# on the rest. Called by Makefile target go-check-shard + ci.yml verify-go-<N>.
set -euo pipefail

SHARD="${1:?usage: go-check-shard.sh <N>}"
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
cd "$REPO_ROOT"

SHARD_FILE="scripts/go-shards/shard-${SHARD}.txt"
RACE_FILE="scripts/race-pkgs.txt"

[ -f "$SHARD_FILE" ] || { echo "no shard file: $SHARD_FILE" >&2; exit 2; }
[ -f "$RACE_FILE" ] || { echo "no race-pkgs file: $RACE_FILE" >&2; exit 2; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
grep -vE '^[[:space:]]*(#|$)' "$SHARD_FILE" | sort > "$tmp/shard.txt"
grep -vE '^[[:space:]]*(#|$)' "$RACE_FILE" | sort > "$tmp/race.txt"

comm -12 "$tmp/shard.txt" "$tmp/race.txt" > "$tmp/race-list.txt"
comm -23 "$tmp/shard.txt" "$tmp/race.txt" > "$tmp/norace-list.txt"

# `go test` builds + tests in one pass; standalone `go build` would fail on
# test-only packages (tests/, docs/engineer/runbooks/) that have no
# non-test Go files, so we skip the separate build step.

if [ -s "$tmp/race-list.txt" ]; then
  # shellcheck disable=SC2046
  echo "shard ${SHARD}: test -race $(wc -l < "$tmp/race-list.txt" | tr -d ' ') pkgs"
  go test -short -race $(cat "$tmp/race-list.txt")
fi
if [ -s "$tmp/norace-list.txt" ]; then
  # shellcheck disable=SC2046
  echo "shard ${SHARD}: test $(wc -l < "$tmp/norace-list.txt" | tr -d ' ') pkgs"
  go test -short $(cat "$tmp/norace-list.txt")
fi
