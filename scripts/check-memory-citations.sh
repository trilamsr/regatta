#!/usr/bin/env bash
# check-memory-citations.sh - fail when a `feedback_*` slug cited in
# CLAUDE.md, the autonomous-session prompt, or the dispatch templates
# does not resolve to a real file under MEMORY_DIR (or its archive/
# subdir). Defaults to the checked-in CI-portable fixture under
# scripts/testdata/memory/ so CI runners + fresh clones exercise the
# gate without the operator's per-machine memory dir. Operators override
# via MEMORY_DIR to point at their real `~/.claude/projects/<hash>/memory/`.
#
# Why this gate: per CLAUDE.md, slug citations are how agents reach
# operator memory. A dangling slug breaks the citation chain silently
# and is invisible to doc-check / lint / vet. EXTRA_DOC lets the test
# inject an extra source file to assert detection.

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

: "${MEMORY_DIR:=$REPO_ROOT/scripts/testdata/memory}"
: "${EXTRA_DOC:=}"

SOURCES=(
  "$REPO_ROOT/CLAUDE.md"
  "$REPO_ROOT/docs/engineer/autonomous-session-prompt.md"
  "$REPO_ROOT/docs/engineer/dispatch-templates/designer.md"
  "$REPO_ROOT/docs/engineer/dispatch-templates/implementer.md"
  "$REPO_ROOT/docs/engineer/dispatch-templates/reviewer.md"
  "$REPO_ROOT/docs/engineer/dispatch-templates/triage.md"
)

if [ -n "$EXTRA_DOC" ] && [ -f "$EXTRA_DOC" ]; then
  SOURCES+=("$EXTRA_DOC")
fi

if [ ! -d "$MEMORY_DIR" ]; then
  echo "check-memory-citations: MEMORY_DIR not found: $MEMORY_DIR (skipping; treat as portable-CI no-op)"
  exit 0
fi

missing=0
seen_any=0
# sort -u upstream already dedupes; no associative array needed (bash 3.x portable).
while IFS= read -r slug; do
  [ -z "$slug" ] && continue
  seen_any=1
  if [ -f "$MEMORY_DIR/$slug.md" ] || [ -f "$MEMORY_DIR/archive/$slug.md" ]; then
    continue
  fi
  echo "MISSING: $slug — no $MEMORY_DIR/$slug.md (or archive/)"
  missing=$((missing + 1))
done < <(grep -hoE 'feedback_[a-z0-9_]+' "${SOURCES[@]}" 2>/dev/null | sort -u)

if [ "$seen_any" -eq 0 ]; then
  echo "check-memory-citations: no feedback_* slugs cited in source set"
  exit 0
fi

if [ "$missing" -gt 0 ]; then
  echo "check-memory-citations: $missing slug(s) unresolved"
  exit 1
fi

echo "check-memory-citations: all cited slugs resolve under $MEMORY_DIR"
exit 0
