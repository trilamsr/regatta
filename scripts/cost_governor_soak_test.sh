#!/usr/bin/env bash
# Fixture tests for scripts/cost-governor-soak.sh. Builds a synthetic
# substrate DB with `budget_reconciled` rows, runs the soak script, and
# asserts exit code + stderr match the bucket-gap contract.

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
SOAK="$SCRIPT_DIR/cost-governor-soak.sh"

if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "cost_governor_soak_test: sqlite3 not in PATH; skipping" >&2
  exit 0
fi

fail=0

make_db() {
  # $1=path, $2=ms_now, remaining args=offsets-in-hours-back-from-now to insert rows for.
  local path=$1 now_ms=$2
  shift 2
  sqlite3 "$path" 'CREATE TABLE substrate_events (id INTEGER PRIMARY KEY, kind TEXT, written_at INTEGER);' >/dev/null
  local off
  for off in "$@"; do
    local ts=$(( now_ms - off * 3600 * 1000 ))
    sqlite3 "$path" "INSERT INTO substrate_events (kind, written_at) VALUES ('budget_reconciled', $ts);" >/dev/null
  done
}

test_7_day_green_passes() {
  local dir
  dir=$(mktemp -d)
  local db="$dir/regatta.db"
  local now_ms=$(( $(date +%s) * 1000 ))
  local offsets=()
  local h
  for h in $(seq 1 167); do offsets+=("$h"); done
  make_db "$db" "$now_ms" "${offsets[@]}"
  REGATTA_DB="$db" bash "$SOAK" >/dev/null 2>"$dir/err"
  local rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "FAIL test_7_day_green_passes: expected exit 0, got $rc; stderr=$(cat "$dir/err")" >&2
    fail=1
  else
    echo "ok test_7_day_green_passes"
  fi
  rm -rf "$dir"
}

test_24h_gap_fails() {
  local dir
  dir=$(mktemp -d)
  local db="$dir/regatta.db"
  local now_ms=$(( $(date +%s) * 1000 ))
  # Skip the 3-days-ago bucket entirely (hours 48-71).
  local offsets=()
  local h
  for h in 1 24 25 26 27 96 97 120 144; do offsets+=("$h"); done
  make_db "$db" "$now_ms" "${offsets[@]}"
  REGATTA_DB="$db" bash "$SOAK" >/dev/null 2>"$dir/err"
  local rc=$?
  if [ "$rc" -eq 0 ]; then
    echo "FAIL test_24h_gap_fails: expected non-zero exit, got 0" >&2
    fail=1
  elif ! grep -q 'missing budget_reconciled' "$dir/err"; then
    echo "FAIL test_24h_gap_fails: stderr missing 'missing budget_reconciled' marker; got: $(cat "$dir/err")" >&2
    fail=1
  else
    echo "ok test_24h_gap_fails"
  fi
  rm -rf "$dir"
}

test_7_day_green_passes
test_24h_gap_fails

if [ "$fail" -ne 0 ]; then
  echo "cost_governor_soak_test: FAIL" >&2
  exit 1
fi
echo "cost_governor_soak_test: PASS"
exit 0
