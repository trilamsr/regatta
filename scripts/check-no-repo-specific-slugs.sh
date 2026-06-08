#!/usr/bin/env bash
# check-no-repo-specific-slugs.sh — fail closed when the bundled-default
# prompt assets carry references that only make sense inside this repo.
#
# The bundled assets ship inside the regatta binary and are injected
# into worker subprocesses spawned against arbitrary target repos. A
# `feedback_*` memory slug, a `scripts/check-*.sh` lint-script path, or
# a `docs/engineer/specs/` reference would point the worker at a file
# that does not exist on the target — degrading prompt quality silently.
#
# Spec: docs/engineer/specs/2026-06-08-regatta-on-arbitrary-repo.md
#       §3 L1, acceptance L1.3.

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

: "${ASSETS_DIR:=$REPO_ROOT/internal/orchestrator/prompt/assets}"

if [ ! -d "$ASSETS_DIR" ]; then
  echo "check-no-repo-specific-slugs: ASSETS_DIR not found: $ASSETS_DIR"
  exit 2
fi

fail=0

scan() {
  local label="$1" pattern="$2"
  local hits
  hits=$(grep -RnE "$pattern" "$ASSETS_DIR" 2>/dev/null || true)
  if [ -n "$hits" ]; then
    echo "check-no-repo-specific-slugs: $label found in bundled prompt assets:"
    printf '%s\n' "$hits" | sed 's/^/  /'
    fail=1
  fi
}

# feedback_* memory slugs — point at this-repo memory dir; meaningless on a target.
scan "feedback_* slug"          'feedback_[a-z][a-z0-9_]+'
# scripts/check-*.sh — point at this-repo CI gates; the target has its own.
scan "scripts/check-*.sh path"  'scripts/check-[a-z0-9_-]+\.sh'
# regatta-specific source paths — meaningless on a target repo.
scan "internal/ path"           'internal/orchestrator/spawner/claude\.go'
scan "docs/engineer/specs path" 'docs/engineer/specs/'
scan "docs/engineer/dispatch-templates path" 'docs/engineer/dispatch-templates/'
scan "docs/engineer/briefs path" 'docs/engineer/briefs/'
scan "Makefile.d path"          'Makefile\.d/'
scan ".regatta/items path"      '\.regatta/items/'

if [ "$fail" -ne 0 ]; then
  echo ""
  echo "Fix: reword the bundled-default content to repo-agnostic prose."
  echo "Per spec §3 L1.3, bundled defaults must carry zero of these tokens."
  exit 1
fi

exit 0
