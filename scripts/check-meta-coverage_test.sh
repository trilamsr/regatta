#!/usr/bin/env bash
# check-meta-coverage_test.sh - assert `make check-meta` enumerates every
# META self-test script on disk and that none of them remain in `make check`
# / `make check-docs` (MAY-30: gate + generator self-tests are META, run nightly).
#
# META scope = fixtures that prove a gate/generator script works; they do
# NOT depend on the PR diff. Four name conventions on disk:
#   scripts/check-<name>_test.sh           (gate self-tests, dash form)
#   scripts/check_state_tier_order_test.sh (gate self-test, underscore form)
#   scripts/doc-check_test.sh              (gate self-test, not a `check-` script)
#   scripts/gen-<name>_test.sh             (generator self-tests)
#
# Cases:
#   A. every META self-test on disk is reachable via `make -n check-meta`
#   B. NONE of those scripts is reachable via `make -n check`
#   C. NONE of those scripts is reachable via `make -n check-docs`
#   D. `make -n check-meta` succeeds (target exists)

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
cd "$REPO_ROOT"

passes=0
fails=0
failed_names=()

# Enumerate every META self-test on disk. Portable across macOS bash 3.2
# (no mapfile). All four naming conventions glob-merged + de-duped.
META_SCRIPTS=()
while IFS= read -r line; do
  META_SCRIPTS+=("$line")
done < <(ls \
  scripts/check-*_test.sh \
  scripts/check_state_tier_order_test.sh \
  scripts/doc-check_test.sh \
  scripts/gen-*_test.sh \
  2>/dev/null | sort -u)

# 13 dash-form + 1 underscore-form + doc-check + 2 generators = 17 floor.
# Raise the floor when new META scripts land; mismatch = drift.
if [ "${#META_SCRIPTS[@]}" -lt 17 ]; then
  echo "FAIL setup: expected >=17 META self-test scripts on disk, found ${#META_SCRIPTS[@]}"
  printf '  - %s\n' "${META_SCRIPTS[@]}"
  exit 1
fi

CHECK_META_DRY=$(make -n check-meta 2>&1)
CHECK_META_EXIT=$?
CHECK_DRY=$(make -n check 2>&1)
CHECK_DOCS_DRY=$(make -n check-docs 2>&1)

# D. check-meta target exists and dry-run succeeds.
if [ "$CHECK_META_EXIT" -ne 0 ]; then
  fails=$((fails + 1))
  failed_names+=("D-check-meta-exists")
  echo "FAIL D-check-meta-exists: \`make -n check-meta\` exit=$CHECK_META_EXIT"
  printf '%s\n' "$CHECK_META_DRY" | sed 's/^/  | /' | head -10
else
  passes=$((passes + 1))
  echo "ok   D-check-meta-exists"
fi

# A. every gate self-test script reachable from check-meta.
for s in "${META_SCRIPTS[@]}"; do
  name="A-meta-runs-$(basename "$s")"
  if printf '%s\n' "$CHECK_META_DRY" | grep -qF "$s"; then
    passes=$((passes + 1))
    echo "ok   $name"
  else
    fails=$((fails + 1))
    failed_names+=("$name")
    echo "FAIL $name: \`make -n check-meta\` does not invoke $s"
  fi
done

# B. none of the gate self-tests reachable from check (anymore).
for s in "${META_SCRIPTS[@]}"; do
  name="B-check-skips-$(basename "$s")"
  if printf '%s\n' "$CHECK_DRY" | grep -qF "$s"; then
    fails=$((fails + 1))
    failed_names+=("$name")
    echo "FAIL $name: \`make -n check\` still invokes $s (should run only in check-meta)"
  else
    passes=$((passes + 1))
    echo "ok   $name"
  fi
done

# C. none of the gate self-tests reachable from check-docs.
for s in "${META_SCRIPTS[@]}"; do
  name="C-check-docs-skips-$(basename "$s")"
  if printf '%s\n' "$CHECK_DOCS_DRY" | grep -qF "$s"; then
    fails=$((fails + 1))
    failed_names+=("$name")
    echo "FAIL $name: \`make -n check-docs\` still invokes $s"
  else
    passes=$((passes + 1))
    echo "ok   $name"
  fi
done

echo "---"
echo "passes=$passes fails=$fails"
if [ "$fails" -ne 0 ]; then
  echo "failed: ${failed_names[*]}"
  exit 1
fi
exit 0
