#!/usr/bin/env bash
# go-check.sh - build all Go packages, then `go test -short` with `-race`
# only on the curated subset in scripts/race-pkgs.txt. Halves local + CI
# `make check` wall-clock vs running `-race ./...` across 84 packages
# where only 30 have concurrency primitives in prod code.
#
# Full `-race ./...` sweep still runs nightly via
# .github/workflows/cross-platform-nightly.yml against `make go-check-full`.
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
cd "$REPO_ROOT"

RACE_FILE="scripts/race-pkgs.txt"
[ -f "$RACE_FILE" ] || { echo "no race-pkgs file: $RACE_FILE" >&2; exit 2; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

go list ./... | sort > "$tmp/all.txt"
grep -vE '^[[:space:]]*(#|$)' "$RACE_FILE" | sort > "$tmp/race.txt"
comm -12 "$tmp/all.txt" "$tmp/race.txt" > "$tmp/race-list.txt"
comm -23 "$tmp/all.txt" "$tmp/race.txt" > "$tmp/norace-list.txt"

echo "go-check: build $(wc -l < "$tmp/all.txt" | tr -d ' ') pkgs"
go build -buildvcs=false ./...

if [ -s "$tmp/race-list.txt" ]; then
  echo "go-check: test -race $(wc -l < "$tmp/race-list.txt" | tr -d ' ') pkgs"
  # shellcheck disable=SC2046
  go test -short -race $(cat "$tmp/race-list.txt")
fi
if [ -s "$tmp/norace-list.txt" ]; then
  echo "go-check: test $(wc -l < "$tmp/norace-list.txt" | tr -d ' ') pkgs"
  # shellcheck disable=SC2046
  go test -short $(cat "$tmp/norace-list.txt")
fi
