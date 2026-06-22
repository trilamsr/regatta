#!/usr/bin/env bash
# check-file-line-budget.sh - fail when a known composition-root file
# exceeds its line budget. Closes the cascade-rebase class: god-files at
# composition roots (cmd/regatta/serve.go, internal/orchestrator/state/machine.go)
# silently grow until ≥3 PRs touch them per session, then every sibling
# PR goes DIRTY and rebases conflict. The fix per `feedback_cascade_rebase_root_cause`
# is structural split before the storm, NOT N rebase cycles after.
#
# Rule: each file in BUDGETS has a hard line-count ceiling. Operator
# forced to split at growth boundary — naturally land seam-by-seam,
# not under merge pressure.
#
# Add a path here only after a cascade-rebase incident actually fired
# on that file. Do NOT pre-populate hypothetically.
#
# Scope: every path listed in BUDGETS, relative to REPO_ROOT.
# Override via REPO_ROOT for the fixture test runner.
#
# Exit: 0 clean, 1 on first over-budget file (lists all violations).

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
: "${REPO_ROOT:=$(cd -- "$SCRIPT_DIR/.." && pwd)}"

if [ ! -d "$REPO_ROOT" ]; then
  echo "check-file-line-budget: REPO_ROOT not found: $REPO_ROOT (skipping)"
  exit 0
fi

cd "$REPO_ROOT"

# path:budget pairs. Add only after a real cascade-rebase incident.
BUDGETS=(
  "cmd/regatta/serve.go:2000"
  "internal/orchestrator/state/machine.go:1500"
)

violations=0
for entry in "${BUDGETS[@]}"; do
  path=${entry%:*}
  budget=${entry#*:}
  if [ ! -f "$path" ]; then
    continue
  fi
  lines=$(wc -l < "$path")
  if [ "$lines" -gt "$budget" ]; then
    echo "check-file-line-budget: $path: $lines lines > budget $budget"
    violations=$((violations + 1))
  fi
done

if [ "$violations" -gt 0 ]; then
  echo "check-file-line-budget: $violations file(s) over budget. Split before the next cascade-rebase incident (see feedback_cascade_rebase_root_cause)."
  exit 1
fi

echo "check-file-line-budget: ${#BUDGETS[@]} path(s) within budget"
exit 0
