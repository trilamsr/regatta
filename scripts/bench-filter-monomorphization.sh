#!/usr/bin/env bash
# bench-filter-monomorphization.sh — capture baselines for #753.
#
# Records two signals at HEAD:
#   1. ns/op + B/op + allocs/op for the 3 filter.Apply[Scope]
#      monomorphization sites exercised by
#      BenchmarkSchedulerTick_FilterMonomorphization.
#   2. Stripped binary size of cmd/regatta.
#
# Comparison vs pre-#698 SHA d823a02 is a follow-up; this script only
# captures the HEAD baseline so a future regression bisect has a
# numeric anchor.
#
# Usage: scripts/bench-filter-monomorphization.sh [benchtime]
#   benchtime defaults to 1x for a fast capture; pass e.g. 3s for a
#   stable averaged baseline. Output is plain text on stdout.

set -euo pipefail

BENCHTIME="${1:-1x}"
BINOUT="${TMPDIR:-/tmp}/regatta-bench-$$"
trap 'rm -f "$BINOUT"' EXIT

echo "## filter.Apply monomorphization baseline (#753)"
echo "head_sha=$(git rev-parse --short HEAD)"
echo "go_version=$(go version)"
echo "benchtime=$BENCHTIME"
echo

echo "### bench: BenchmarkSchedulerTick_FilterMonomorphization"
go test -bench BenchmarkSchedulerTick_FilterMonomorphization \
	-benchmem -benchtime="$BENCHTIME" -run=^$ \
	./internal/orchestrator/scheduler/... \
	| grep -E '^(Benchmark|PASS|FAIL|ok|---)'
echo

echo "### binary size: cmd/regatta (-ldflags='-s -w')"
go build -ldflags='-s -w' -o "$BINOUT" ./cmd/regatta
SIZE_BYTES=$(wc -c <"$BINOUT" | tr -d ' ')
SIZE_MIB=$(awk -v b="$SIZE_BYTES" 'BEGIN{ printf "%.2f", b/1048576 }')
echo "binary_bytes=$SIZE_BYTES"
echo "binary_mib=$SIZE_MIB"
