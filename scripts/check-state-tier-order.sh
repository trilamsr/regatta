#!/usr/bin/env bash
# check-state-tier-order.sh - mechanical enforcement of plan #795 Option E.
#
# Pure-helper subpackages under internal/orchestrator/state/ MUST NOT import
# the parent `state` package. The hybrid split keeps `*DB` + receivers in the
# root and peels pure data + free functions into:
#   - jsonscan/           (T1) JSON-array scanner
#   - edgeagg/            (T2) edge aggregation + type aliases re-exported via state/aliases.go
#   - transitions/        (T3) agent + work-item edge tables
#   - cycle/              (T4) pure DFS cycle check
#   - approvals_shadow/   (T5) divergence classifier + config
#
# A reverse import (subpackage → state) collapses the tier and re-creates the
# god-package via the back door. Spec §6 R1 calls this out: type aliases in
# state/aliases.go re-export edgeagg.EdgeRow, so a subpackage that imports
# state for a constant blows the build with a cycle. This gate fires before
# that, with a readable message naming the offending package.
#
# Scope: only non-test deps (per spec §6 R8). A subpackage `_test.go` is
# allowed to import `state` for a helper without flipping the gate. `go list`
# is invoked without `-test` so test-only edges stay invisible.
#
# Exit: 0 clean, 1 on first leak (lists every hit before exit).
#
# Per-spec opt-out: setting STATE_TIER_ORDER_SKIP=1 returns 0 immediately.
# Used by the fixture test only; do not set in CI.

set -uo pipefail

if [ "${STATE_TIER_ORDER_SKIP:-0}" = "1" ]; then
  echo "check-state-tier-order: skipped via STATE_TIER_ORDER_SKIP"
  exit 0
fi

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

# Allow tests to point at a fixture module by cd'ing first; default to repo root.
if [ -f "./go.mod" ] && grep -q '^module github.com/trilamsr/regatta' ./go.mod 2>/dev/null; then
  WORK_DIR="$(pwd)"
else
  WORK_DIR="$REPO_ROOT"
fi

STATE_IMPORT_PATH="github.com/trilamsr/regatta/internal/orchestrator/state"
STATE_DIR="$WORK_DIR/internal/orchestrator/state"

if [ ! -d "$STATE_DIR" ]; then
  echo "check-state-tier-order: state dir not found: $STATE_DIR (skipping)"
  exit 0
fi

# Spec §4.2 enumerates the pure-helper subpackages. Each is checked
# individually so a missing one (Tn not yet merged) is a no-op rather than
# a script error. dbtest/, migrations/, substrate/ are NOT in this list:
# substrate is an infrastructure tier that legitimately may grow a state
# dep later; dbtest is a test helper; migrations is data-only.
pure_subpkgs=(
  jsonscan
  edgeagg
  transitions
  cycle
  approvals_shadow
)

leaks=0
scanned=0
leak_lines=""

cd "$WORK_DIR"

for sub in "${pure_subpkgs[@]}"; do
  pkg_dir="internal/orchestrator/state/$sub"
  if [ ! -d "$pkg_dir" ]; then
    continue
  fi
  if ! ls "$pkg_dir"/*.go >/dev/null 2>&1; then
    continue
  fi
  scanned=$((scanned + 1))

  # `go list -deps` reports the transitive non-test import graph rooted at the
  # named package. Test deps are excluded (no `-test` flag) so subpackage tests
  # may legitimately import state.
  deps=$(go list -deps "./$pkg_dir/..." 2>/dev/null || true)
  if printf '%s\n' "$deps" | grep -qxF "$STATE_IMPORT_PATH"; then
    leaks=$((leaks + 1))
    leak_lines="${leak_lines}  - $pkg_dir/ imports $STATE_IMPORT_PATH (reverse tier — Option E violation)"$'\n'
  fi
done

if [ "$leaks" -gt 0 ]; then
  echo "check-state-tier-order: $leaks subpackage(s) reverse-import state:"
  printf '%s' "$leak_lines"
  echo
  echo "Plan #795 Option E (docs/engineer/specs/2026-06-04-state-package-split-design.md §4.2):"
  echo "  Pure subpackages (jsonscan/edgeagg/transitions/cycle/approvals_shadow) are a"
  echo "  one-way tier below state/. Receivers stay on *DB in the root package; pure"
  echo "  data + free functions live in the subpackages. A reverse import re-creates"
  echo "  the god-package via the back door."
  exit 1
fi

echo "check-state-tier-order: $scanned pure subpackage(s) scanned; tier order clean"
exit 0
