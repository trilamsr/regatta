#!/usr/bin/env bash
# Smoke test for scripts/ci-flake-report.sh. Exercises the offline-safe
# paths (--help, unknown-flag rejection) and the no-failed-runs branch
# via a stubbed `gh` on PATH. We deliberately do not hit the live GH
# API so this runs in CI / on disconnected machines.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
target="$script_dir/ci-flake-report.sh"

failed=0
pass=0

assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  if printf '%s\n' "$haystack" | grep -q -- "$needle"; then
    pass=$((pass + 1))
    echo "PASS $name"
  else
    failed=$((failed + 1))
    echo "FAIL $name: stdout missing '$needle'"
    echo "  output:"
    printf '%s\n' "$haystack" | sed 's/^/    /'
  fi
}

# Case 1 - --help prints usage and exits 0.
out=$("$target" --help 2>&1) && rc=0 || rc=$?
if [ "$rc" -ne 0 ]; then
  echo "FAIL help-exit-zero: got $rc"
  failed=$((failed + 1))
else
  pass=$((pass + 1))
  echo "PASS help-exit-zero"
fi
assert_contains "help-mentions-flake-rate" "$out" "flake rate"
assert_contains "help-mentions-RUN_LIMIT"  "$out" "RUN_LIMIT"

# Case 2 - unknown flag fails with exit 2.
out=$("$target" --bogus 2>&1) && rc=0 || rc=$?
if [ "$rc" -ne 2 ]; then
  echo "FAIL unknown-flag-exit-2: got $rc"
  failed=$((failed + 1))
else
  pass=$((pass + 1))
  echo "PASS unknown-flag-exit-2"
fi

# Case 3 - stubbed `gh` returning zero failed runs exits 0 with the
# "nothing to tally" sentinel. We build a fake `gh` on a private PATH
# that responds only to `gh run list ... --json ...` (the only gh call
# the script makes in this branch).
stub_dir=$(mktemp -d -t ci-flake-stub.XXXXXX)
trap 'rm -rf "$stub_dir"' EXIT

cat > "$stub_dir/gh" <<'STUB'
#!/usr/bin/env bash
# Test stub: emit an empty JSON array for `gh run list`, anything else
# is a usage error the test should not provoke.
case "$1" in
  run)
    shift
    if [ "${1:-}" = "list" ]; then
      echo "[]"
      exit 0
    fi
    if [ "${1:-}" = "view" ]; then
      exit 0
    fi
    ;;
esac
echo "stub-gh: unexpected args: $*" >&2
exit 99
STUB
chmod +x "$stub_dir/gh"

out=$(PATH="$stub_dir:$PATH" "$target" 2>&1) && rc=0 || rc=$?
if [ "$rc" -ne 0 ]; then
  echo "FAIL empty-window-exit-zero: got $rc"
  failed=$((failed + 1))
  echo "  output:"
  printf '%s\n' "$out" | sed 's/^/    /'
else
  pass=$((pass + 1))
  echo "PASS empty-window-exit-zero"
fi
assert_contains "empty-window-banner"        "$out" "scanned 0 runs"
assert_contains "empty-window-nothing-tally" "$out" "nothing to tally"

# Case 4 - stubbed `gh` returning one failed run with a `--- FAIL`
# line in its log produces a table row for that test.
cat > "$stub_dir/gh" <<'STUB'
#!/usr/bin/env bash
case "$1" in
  run)
    shift
    if [ "${1:-}" = "list" ]; then
      cat <<'JSON'
[
  {"databaseId":1,"conclusion":"failure","workflowName":"CI","createdAt":"2026-06-02T00:00:00Z","headBranch":"main"},
  {"databaseId":2,"conclusion":"success","workflowName":"CI","createdAt":"2026-06-02T00:00:00Z","headBranch":"main"}
]
JSON
      exit 0
    fi
    if [ "${1:-}" = "view" ]; then
      # Emulate `gh run view --log-failed <id>` output.
      echo "test  go test  2026-06-02T00:00:00Z --- FAIL: TestExampleFlake (0.01s)"
      echo "test  go test  2026-06-02T00:00:00Z     example_test.go:42: assertion failed"
      exit 0
    fi
    ;;
esac
echo "stub-gh: unexpected args: $*" >&2
exit 99
STUB
chmod +x "$stub_dir/gh"

out=$(PATH="$stub_dir:$PATH" "$target" 2>&1) && rc=0 || rc=$?
if [ "$rc" -ne 0 ]; then
  echo "FAIL stubbed-fail-exit-zero: got $rc"
  failed=$((failed + 1))
  echo "  output:"
  printf '%s\n' "$out" | sed 's/^/    /'
else
  pass=$((pass + 1))
  echo "PASS stubbed-fail-exit-zero"
fi
assert_contains "stubbed-fail-row" "$out" "TestExampleFlake"
assert_contains "stubbed-fail-rate" "$out" "50.00%"

echo
echo "ci-flake-report smoke: $pass passed, $failed failed"
exit "$failed"
