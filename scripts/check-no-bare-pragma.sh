#!/usr/bin/env bash
# check-no-bare-pragma.sh - fail when `_pragma=` appears in any *.go file
# outside the canonical DSN builder (internal/orchestrator/state/state.go)
# and its tests. Closes the R24 class: cmd/regatta/resume.go shipped a
# hand-rolled `?_pragma=journal_mode(WAL)` DSN that diverged from
# state.DSN() once state.DSN() gained busy_timeout + synchronous +
# _txlock; the resume path silently lost 4 pragmas.
#
# Rule: only state.DSN() may author the sqlite DSN string.
#
# Scope: every tracked *.go under REPO_ROOT (defaults to repo root).
# Override via REPO_ROOT for the fixture test runner.
#
# Exit: 0 clean, 1 on first violation (lists every hit before exit).

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
: "${REPO_ROOT:=$(cd -- "$SCRIPT_DIR/.." && pwd)}"

if [ ! -d "$REPO_ROOT" ]; then
  echo "check-no-bare-pragma: REPO_ROOT not found: $REPO_ROOT (skipping)"
  exit 0
fi

cd "$REPO_ROOT"

ALLOW_RE='^(internal/orchestrator/state/state\.go|internal/orchestrator/state/state_dsn_test\.go)$'

violations=0
while IFS= read -r line; do
  file=${line%%:*}
  rest=${line#*:}
  content=${rest#*:}
  if echo "$file" | grep -Eq "$ALLOW_RE"; then
    continue
  fi
  # Escape hatch for genuine cases that can't import state (cycle prevention).
  if echo "$content" | grep -Eq '// allow-bare-pragma:'; then
    continue
  fi
  echo "check-no-bare-pragma: $line"
  violations=$((violations + 1))
done < <(git grep -nE '_pragma=' -- '*.go' 2>/dev/null || true)

if [ "$violations" -gt 0 ]; then
  echo "check-no-bare-pragma: $violations violation(s). Route the DSN through internal/orchestrator/state::DSN(path)."
  exit 1
fi

echo "check-no-bare-pragma: ok"
exit 0
