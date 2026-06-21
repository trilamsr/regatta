#!/usr/bin/env bash
# check-meta-coverage_test.sh - assert `make check-meta` enumerates every
# gate self-test script on disk and that none of them remain in `make check`
# / `make check-docs` (MAY-30: gate self-tests are META, run nightly).
#
# Cases:
#   A. every scripts/check-*_test.sh + scripts/check_state_tier_order_test.sh
#      is reachable via `make -n check-meta` (as a `bash scripts/...` invocation)
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

# Enumerate every gate self-test script on disk. Two naming conventions:
#   scripts/check-<name>_test.sh   (dash form, 13 files)
#   scripts/check_state_tier_order_test.sh  (underscore form, 1 file)
# Portable across macOS bash 3.2 (no mapfile).
META_SCRIPTS=()
while IFS= read -r line; do
  META_SCRIPTS+=("$line")
done < <(ls scripts/check-*_test.sh scripts/check_state_tier_order_test.sh 2>/dev/null | sort -u)

if [ "${#META_SCRIPTS[@]}" -lt 14 ]; then
  echo "FAIL setup: expected >=14 gate self-test scripts on disk, found ${#META_SCRIPTS[@]}"
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
