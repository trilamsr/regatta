#!/usr/bin/env bash
# check-prompt-parity.sh - fail when a `feedback_*` slug listed under
# the `## Anchored rules (worker-prompt parity)` section of the
# implementer dispatch template is absent from the spawned-worker
# prompt builder source.
#
# Why this gate: per session-retro 2026-06-04, `defaultPromptBuilder`
# shipped as a Phase-S1 stub for months while CLAUDE.md gained new
# universal rules. Drift was caught only at late-stage L4 review.
# This gate makes the dispatch template authoritative: any slug added
# to `## Anchored rules` MUST land in the prompt builder, else CI fails.
#
# Escape hatch: append ` <!-- prompt-parity-skip: <reason> -->` to a
# slug bullet to mark it intentionally reviewer-only / operator-only.
set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

: "${PROMPT_SRC:=$REPO_ROOT/internal/orchestrator/spawner/claude.go}"
: "${TEMPLATE_SRC:=$REPO_ROOT/docs/engineer/dispatch-templates/implementer.md}"

if [ ! -f "$PROMPT_SRC" ]; then
  echo "check-prompt-parity: PROMPT_SRC not found: $PROMPT_SRC"
  exit 2
fi
if [ ! -f "$TEMPLATE_SRC" ]; then
  echo "check-prompt-parity: TEMPLATE_SRC not found: $TEMPLATE_SRC"
  exit 2
fi

ANCHOR_HEADER='## Anchored rules (worker-prompt parity)'
if ! grep -qF "$ANCHOR_HEADER" "$TEMPLATE_SRC"; then
  echo "check-prompt-parity: missing '$ANCHOR_HEADER' section in $TEMPLATE_SRC"
  echo "  Fix: add the section listing slugs the spawned worker prompt MUST carry."
  exit 1
fi

ANCHOR_BLOCK=$(awk -v hdr="$ANCHOR_HEADER" '
  $0 == hdr { in_block = 1; next }
  in_block && /^## / { exit }
  in_block { print }
' "$TEMPLATE_SRC")

REQUIRED_SLUGS=$(printf '%s\n' "$ANCHOR_BLOCK" \
  | grep -v 'prompt-parity-skip' \
  | grep -oE 'feedback_[a-z_]+' \
  | sort -u)

PROMPT_SLUGS=$(grep -oE 'feedback_[a-z_]+' "$PROMPT_SRC" | sort -u)

MISSING=$(comm -23 <(printf '%s\n' "$REQUIRED_SLUGS") <(printf '%s\n' "$PROMPT_SLUGS"))

if [ -n "$MISSING" ]; then
  echo "check-prompt-parity: prompt builder $PROMPT_SRC is missing required slugs from $TEMPLATE_SRC:"
  printf '  - %s\n' $MISSING
  echo ""
  echo "Fix one of:"
  echo "  (a) Add the slug to defaultPromptBuilder so spawned workers receive the rule."
  echo "  (b) Annotate the bullet with <!-- prompt-parity-skip: <reason> --> if reviewer-only."
  exit 1
fi

exit 0
