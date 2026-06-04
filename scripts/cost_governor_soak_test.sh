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

# Single timestamp source — re-used by every test + propagated to the
# soak script via SOAK_NOW_MS so NTP midnight jumps cannot drift bucket
# boundaries between fixture INSERT time and soak read time.
TEST_NOW_MS=$(( $(date +%s) * 1000 ))

make_db() {
  # $1=path, $2=ms_now, remaining args=offsets-in-hours-back-from-now to insert rows for.
  local path=$1 now_ms=$2
  shift 2
  if ! sqlite3 "$path" 'CREATE TABLE substrate_events (id INTEGER PRIMARY KEY, kind TEXT, written_at INTEGER);'; then
    echo "make_db: CREATE TABLE failed for $path" >&2
    return 1
  fi
  local off
  for off in "$@"; do
    local ts=$(( now_ms - off * 3600 * 1000 ))
    if ! sqlite3 "$path" "INSERT INTO substrate_events (kind, written_at) VALUES ('budget_reconciled', $ts);"; then
      echo "make_db: INSERT failed for $path off=$off" >&2
      return 1
    fi
  done
  local rows
  rows=$(sqlite3 "$path" 'SELECT count(*) FROM substrate_events;')
  if [ "$rows" -ne "$#" ]; then
    echo "make_db: row count mismatch (got $rows want $#) for $path" >&2
    return 1
  fi
}

test_7_day_green_passes() {
  local dir
  dir=$(mktemp -d)
  trap 'rm -rf "$dir"' RETURN
  local db="$dir/regatta.db"
  local now_ms=$TEST_NOW_MS
  local offsets=()
  local h
  for h in $(seq 1 167); do offsets+=("$h"); done
  if ! make_db "$db" "$now_ms" "${offsets[@]}"; then
    echo "FAIL test_7_day_green_passes: make_db failed" >&2
    fail=1
    return
  fi
  SOAK_NOW_MS="$now_ms" REGATTA_DB="$db" bash "$SOAK" >/dev/null 2>"$dir/err"
  local rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "FAIL test_7_day_green_passes: expected exit 0, got $rc; stderr=$(cat "$dir/err")" >&2
    fail=1
  else
    echo "ok test_7_day_green_passes"
  fi
}

test_24h_gap_fails() {
  local dir
  dir=$(mktemp -d)
  trap 'rm -rf "$dir"' RETURN
  local db="$dir/regatta.db"
  local now_ms=$TEST_NOW_MS
  # Skip the 3-days-ago bucket entirely (hours 48-71).
  local offsets=()
  local h
  for h in 1 24 25 26 27 96 97 120 144; do offsets+=("$h"); done
  if ! make_db "$db" "$now_ms" "${offsets[@]}"; then
    echo "FAIL test_24h_gap_fails: make_db failed" >&2
    fail=1
    return
  fi
  SOAK_NOW_MS="$now_ms" REGATTA_DB="$db" bash "$SOAK" >/dev/null 2>"$dir/err"
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
}

# Verifies finding #1 fix: setup errors must fail the test, not pass
# silently. Pre-create the DB path as a read-only directory so make_db's
# sqlite3 CREATE TABLE cannot succeed; the function must return non-zero.
test_setup_error_fails_test() {
  local dir
  dir=$(mktemp -d)
  trap 'chmod -R u+rwx "$dir" 2>/dev/null; rm -rf "$dir"' RETURN
  local db="$dir/regatta.db"
  mkdir -p "$db"
  chmod 000 "$db"
  if make_db "$db" "$TEST_NOW_MS" 1 24 48 2>"$dir/err"; then
    echo "FAIL test_setup_error_fails_test: make_db returned 0 on unwritable path; setup error was silently swallowed" >&2
    fail=1
  else
    echo "ok test_setup_error_fails_test"
  fi
}

test_7_day_green_passes
test_24h_gap_fails
test_setup_error_fails_test

if [ "$fail" -ne 0 ]; then
  echo "cost_governor_soak_test: FAIL" >&2
  exit 1
fi
echo "cost_governor_soak_test: PASS"
exit 0
