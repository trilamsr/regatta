#!/usr/bin/env bash
# check_state_tier_order_test.sh - fixture cases for check-state-tier-order.sh.
#
# Plan #795 (Option E hybrid): pure subpackages under
# internal/orchestrator/state/{jsonscan,edgeagg,transitions,cycle,approvals_shadow}
# MUST NOT import the parent `state` package. The gate is mechanical
# enforcement of the spec §4.2 one-direction tier order so a future
# contributor cannot silently invert the dependency.
#
# Cases:
#   A. clean tree                                       -> exit 0
#   B. injected subpackage with `state` import          -> exit 1
#   C. missing subpackage (subset of names exist)       -> exit 0
#   D. injected subpackage imports state in *_test.go   -> exit 0 (test-only ok)

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
CHECK="$REPO_ROOT/scripts/check-state-tier-order.sh"

passes=0
fails=0
failed_names=()

# Case A: run against the live tree. Plan acceptance: returns 0 against
# current main + all phases. Reuses the actual repo, no fixture needed.
run_case_a() {
  local name="A-live-tree-clean" out got_exit
  out=$(bash "$CHECK" 2>&1)
  got_exit=$?
  if [ "$got_exit" -eq 0 ]; then
    passes=$((passes + 1))
    echo "ok   $name (exit=0)"
  else
    fails=$((fails + 1))
    failed_names+=("$name")
    echo "FAIL $name: want exit=0 got=$got_exit"
    printf '%s\n' "$out" | sed 's/^/  | /'
  fi
}

# Cases B-D use an isolated module fixture so the gate can be exercised
# without mutating the live state/ tree. The fixture shares the project
# module path so the import-string matches.
make_fixture_module() {
  local root="$1"
  mkdir -p "$root/internal/orchestrator/state/jsonscan"
  mkdir -p "$root/internal/orchestrator/state/transitions"
  cat >"$root/go.mod" <<'EOF'
module github.com/trilamsr/regatta

go 1.23
EOF
  cat >"$root/internal/orchestrator/state/state.go" <<'EOF'
package state

type DB struct{}
EOF
  cat >"$root/internal/orchestrator/state/transitions/tables.go" <<'EOF'
package transitions

const Sentinel = 1
EOF
}

run_case_b_leak() {
  local name="B-injected-leak-fails" tmp out got_exit
  tmp=$(mktemp -d)
  make_fixture_module "$tmp"
  cat >"$tmp/internal/orchestrator/state/jsonscan/scan.go" <<'EOF'
package jsonscan

import _ "github.com/trilamsr/regatta/internal/orchestrator/state"

const Sentinel = 1
EOF
  out=$( (cd "$tmp" && bash "$CHECK") 2>&1)
  got_exit=$?
  if [ "$got_exit" -ne 0 ] && printf '%s\n' "$out" | grep -qE "jsonscan.*imports.*state"; then
    passes=$((passes + 1))
    echo "ok   $name (exit=$got_exit)"
  else
    fails=$((fails + 1))
    failed_names+=("$name")
    echo "FAIL $name: want exit!=0 + message names jsonscan; got=$got_exit"
    printf '%s\n' "$out" | sed 's/^/  | /'
  fi
  rm -rf "$tmp"
}

run_case_c_missing() {
  local name="C-missing-subpkg-ok" tmp out got_exit
  tmp=$(mktemp -d)
  make_fixture_module "$tmp"
  out=$( (cd "$tmp" && bash "$CHECK") 2>&1)
  got_exit=$?
  if [ "$got_exit" -eq 0 ]; then
    passes=$((passes + 1))
    echo "ok   $name (exit=0)"
  else
    fails=$((fails + 1))
    failed_names+=("$name")
    echo "FAIL $name: want exit=0 got=$got_exit"
    printf '%s\n' "$out" | sed 's/^/  | /'
  fi
  rm -rf "$tmp"
}

run_case_d_test_only() {
  local name="D-test-only-import-ok" tmp out got_exit
  tmp=$(mktemp -d)
  make_fixture_module "$tmp"
  mkdir -p "$tmp/internal/orchestrator/state/edgeagg"
  cat >"$tmp/internal/orchestrator/state/edgeagg/edgeagg.go" <<'EOF'
package edgeagg

const Sentinel = 1
EOF
  cat >"$tmp/internal/orchestrator/state/edgeagg/edgeagg_test.go" <<'EOF'
package edgeagg_test

import (
	"testing"

	_ "github.com/trilamsr/regatta/internal/orchestrator/state"
)

func TestSentinel(t *testing.T) {}
EOF
  out=$( (cd "$tmp" && bash "$CHECK") 2>&1)
  got_exit=$?
  if [ "$got_exit" -eq 0 ]; then
    passes=$((passes + 1))
    echo "ok   $name (exit=0)"
  else
    fails=$((fails + 1))
    failed_names+=("$name")
    echo "FAIL $name: want exit=0 (test-only deps excluded) got=$got_exit"
    printf '%s\n' "$out" | sed 's/^/  | /'
  fi
  rm -rf "$tmp"
}

run_case_a
run_case_b_leak
run_case_c_missing
run_case_d_test_only

echo "---"
echo "passes=$passes fails=$fails"
if [ "$fails" -ne 0 ]; then
  echo "failed: ${failed_names[*]}"
  exit 1
fi
exit 0
