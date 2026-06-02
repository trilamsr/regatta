#!/usr/bin/env bash
# Smoke test for scripts/agent-status.sh. Runs the script in --no-network
# mode and asserts on exit code + structural lines so we never depend on
# the live GH state of the host.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
target="$script_dir/agent-status.sh"

failed=0
pass=0

assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  if printf '%s\n' "$haystack" | grep -q "$needle"; then
    pass=$((pass + 1))
    echo "PASS $name"
  else
    failed=$((failed + 1))
    echo "FAIL $name: stdout missing '$needle'"
    echo "  output:"
    printf '%s\n' "$haystack" | sed 's/^/    /'
  fi
}

# Case 1: --help prints usage and exits 0.
out=$("$target" --help 2>&1) && rc=0 || rc=$?
if [ "$rc" -ne 0 ]; then
  echo "FAIL help-exit-zero: got $rc"
  failed=$((failed + 1))
else
  pass=$((pass + 1))
  echo "PASS help-exit-zero"
fi
assert_contains "help-mentions-worktrees" "$out" "Active local worktrees"

# Case 2: unknown flag fails with exit 2.
out=$("$target" --bogus 2>&1) && rc=0 || rc=$?
if [ "$rc" -ne 2 ]; then
  echo "FAIL unknown-flag-exit-2: got $rc"
  failed=$((failed + 1))
else
  pass=$((pass + 1))
  echo "PASS unknown-flag-exit-2"
fi

# Case 3: --no-network prints the worktree panel and the skip notice.
out=$("$target" --no-network 2>&1) && rc=0 || rc=$?
if [ "$rc" -ne 0 ]; then
  echo "FAIL no-network-exit-zero: got $rc"
  failed=$((failed + 1))
else
  pass=$((pass + 1))
  echo "PASS no-network-exit-zero"
fi
assert_contains "no-network-shows-worktrees-header" "$out" "== agent worktrees =="
assert_contains "no-network-shows-skip-notice"    "$out" "skipping PR / issue panels"

echo
echo "agent-status smoke: $pass passed, $failed failed"
exit "$failed"
