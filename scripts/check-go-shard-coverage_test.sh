#!/usr/bin/env bash
# check-go-shard-coverage_test.sh asserts the shard-coverage gate fails closed
# on missing packages, on stale packages, and on duplicate shard membership;
# and passes on aligned state.

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
GATE="$SCRIPT_DIR/check-go-shard-coverage.sh"
PASS=0
FAIL=0

fail() { echo "FAIL: $1"; FAIL=$((FAIL + 1)); }
pass() { echo "ok: $1"; PASS=$((PASS + 1)); }
mktmp() { mktemp -d -t go-shard-cov-test.XXXXXX; }

run_case_aligned() {
  local tmp shards expected
  tmp=$(mktmp)
  shards="$tmp/go-shards"
  expected="$tmp/expected.txt"
  mkdir -p "$shards"
  cat > "$shards/shard-1.txt" <<'EOF'
example.com/a
example.com/b
EOF
  cat > "$shards/shard-2.txt" <<'EOF'
example.com/c
EOF
  cat > "$expected" <<'EOF'
example.com/a
example.com/b
example.com/c
EOF
  if SHARD_DIR="$shards" EXPECTED_LIST_FILE="$expected" "$GATE" >/dev/null 2>&1; then
    pass "aligned shard union passes"
  else
    fail "aligned shard union should pass"
  fi
  rm -rf "$tmp"
}

run_case_missing_pkg() {
  local tmp shards expected
  tmp=$(mktmp)
  shards="$tmp/go-shards"
  expected="$tmp/expected.txt"
  mkdir -p "$shards"
  cat > "$shards/shard-1.txt" <<'EOF'
example.com/a
EOF
  cat > "$expected" <<'EOF'
example.com/a
example.com/b
EOF
  if SHARD_DIR="$shards" EXPECTED_LIST_FILE="$expected" "$GATE" >/dev/null 2>&1; then
    fail "missing pkg in shards should fail"
  else
    pass "missing pkg in shards fails closed"
  fi
  rm -rf "$tmp"
}

run_case_stale_pkg() {
  local tmp shards expected
  tmp=$(mktmp)
  shards="$tmp/go-shards"
  expected="$tmp/expected.txt"
  mkdir -p "$shards"
  cat > "$shards/shard-1.txt" <<'EOF'
example.com/a
example.com/deleted
EOF
  cat > "$expected" <<'EOF'
example.com/a
EOF
  if SHARD_DIR="$shards" EXPECTED_LIST_FILE="$expected" "$GATE" >/dev/null 2>&1; then
    fail "stale pkg in shards should fail"
  else
    pass "stale pkg in shards fails closed"
  fi
  rm -rf "$tmp"
}

run_case_duplicate_pkg() {
  local tmp shards expected
  tmp=$(mktmp)
  shards="$tmp/go-shards"
  expected="$tmp/expected.txt"
  mkdir -p "$shards"
  cat > "$shards/shard-1.txt" <<'EOF'
example.com/a
example.com/b
EOF
  cat > "$shards/shard-2.txt" <<'EOF'
example.com/b
example.com/c
EOF
  cat > "$expected" <<'EOF'
example.com/a
example.com/b
example.com/c
EOF
  if SHARD_DIR="$shards" EXPECTED_LIST_FILE="$expected" "$GATE" >/dev/null 2>&1; then
    fail "duplicate pkg across shards should fail"
  else
    pass "duplicate pkg across shards fails closed"
  fi
  rm -rf "$tmp"
}

run_case_no_shard_files() {
  local tmp shards
  tmp=$(mktmp)
  shards="$tmp/go-shards"
  mkdir -p "$shards"
  if SHARD_DIR="$shards" EXPECTED_LIST_FILE="/dev/null" "$GATE" >/dev/null 2>&1; then
    fail "empty SHARD_DIR should fail"
  else
    pass "empty SHARD_DIR fails closed"
  fi
  rm -rf "$tmp"
}

run_case_aligned
run_case_missing_pkg
run_case_stale_pkg
run_case_duplicate_pkg
run_case_no_shard_files

echo ""
echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
