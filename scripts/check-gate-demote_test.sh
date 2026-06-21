#!/usr/bin/env bash
# check-gate-demote_test.sh - assert MAY-31 demote: byte-equal-pin + phase-x-leak
# are not part of `make check` / `make check-docs` but ARE invoked from
# `make pre-push-check`. Catches accidental re-promotion in a rebase / merge.
set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
CI_MK="$REPO_ROOT/Makefile.d/ci.mk"

fail=0

assert_absent() {
  local target="$1" pattern="$2"
  local line
  line=$(grep -E "^${target}: " "$CI_MK" || true)
  if [ -z "$line" ]; then
    echo "FAIL ${target}: target missing in $CI_MK"
    fail=1
    return
  fi
  if echo "$line" | grep -qE "(^|[[:space:]])${pattern}([[:space:]]|$)"; then
    echo "FAIL ${target}: still depends on '${pattern}' — MAY-31 demote regressed"
    fail=1
  else
    echo "ok   ${target} does not depend on ${pattern}"
  fi
}

assert_present_in_pre_push() {
  local pattern="$1"
  # Extract pre-push-check recipe body (header + indented lines until next blank).
  local body
  body=$(awk '/^pre-push-check:/{flag=1} flag{print} flag && /^$/{exit}' "$CI_MK")
  if ! echo "$body" | grep -qE "${pattern}"; then
    echo "FAIL pre-push-check: '${pattern}' not invoked from pre-push-check recipe"
    fail=1
  else
    echo "ok   pre-push-check invokes ${pattern}"
  fi
}

# AC2 + AC4: byte-equal-pin-test + phase-x-leak* removed from check / check-docs.
for tgt in check check-docs; do
  assert_absent "$tgt" "check-byte-equal-pin-test"
  assert_absent "$tgt" "check-phase-x-leak"
  assert_absent "$tgt" "check-phase-x-leak-test"
done

# AC2: byte-equal-pin-test still runs as a pre-push hint.
assert_present_in_pre_push "check-byte-equal-pin"

# AC5: phase-x scan still runs as a pre-push hint (one-line grep replaces script).
assert_present_in_pre_push "phase-x"

if [ "$fail" -ne 0 ]; then
  echo "check-gate-demote_test: FAIL"
  exit 1
fi
echo "check-gate-demote_test: PASS"
exit 0
