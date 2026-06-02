#!/usr/bin/env bash
# stale-todo.sh: fail if any tracked TODO|FIXME|XXX has lived past
# the no-deferred-debt window without an issue reference.
#
# Spec D16 + C16: every TODO/FIXME/XXX cites an issue (#NNN, GH-NNN,
# or a URL containing /issues/) OR has been added within the last
# WINDOW_DAYS. Beyond the window, untagged markers fail closed.
#
# Falsifier: add `// TODO: think about this` to any .go file, commit,
# wait WINDOW_DAYS, run the script - it fails citing file:line.
# Bypass: add an issue ref (`// TODO(#42): think about this`) or
# delete the marker.

set -euo pipefail

# PHASE-S-RELAX: default raised 7→30d during self-host window so
# in-flight max-velocity dispatch does not file churn-issues for week-old
# TODOs that are still in active scope. Restore 7d at 30-day-green trigger.
# Memory: feedback_gate_relaxation_phase_s.
WINDOW_DAYS="${WINDOW_DAYS:-30}"
window_seconds=$((WINDOW_DAYS * 86400))
now=$(date +%s)
fail=0

# Exempt paths: research/ is raw notes; .claude/ is upstream skill
# content; testdata/ corpora may quote external sources verbatim;
# scripts/stale-todo.sh names the markers as the scanner's contract.
exempt_re='^(research/|\.claude/|testdata/|scripts/stale-todo\.sh$|\.github/workflows/stale-todo\.yml$)'

# Only scan files git tracks. Avoid binaries by limiting extensions.
files=$(git ls-files '*.go' '*.md' '*.sh' '*.yml' '*.yaml' '*.json' '*.cue' 2>/dev/null \
  | grep -vE "$exempt_re" || true)

if [ -z "$files" ]; then
  echo "stale-todo: no tracked files in scope."
  exit 0
fi

while IFS= read -r file; do
  [ -z "$file" ] && continue
  hits=$(grep -nE '\b(TODO|FIXME|XXX)\b' -- "$file" 2>/dev/null || true)
  [ -z "$hits" ] && continue
  while IFS= read -r hit; do
    line_no=$(printf '%s\n' "$hit" | cut -d: -f1)
    line_content=$(printf '%s\n' "$hit" | cut -d: -f2-)
    # Tagged forms: TODO(#123), FIXME(GH-7), or a URL with /issues/.
    if printf '%s' "$line_content" | grep -qE '(TODO|FIXME|XXX)\(([#A-Za-z0-9_-]*[0-9]+)\)|/issues/[0-9]+'; then
      continue
    fi
    # Untagged. Allow if added within the window.
    blame=$(git blame -L "$line_no,$line_no" --porcelain -- "$file" 2>/dev/null | head -3 || true)
    ts=$(printf '%s\n' "$blame" | awk '/^author-time / {print $2; exit}')
    if [ -z "$ts" ]; then
      echo "stale-todo: $file:$line_no: untagged marker (blame unavailable; treat as stale)" >&2
      fail=1
      continue
    fi
    age=$(( now - ts ))
    if [ "$age" -gt "$window_seconds" ]; then
      days=$(( age / 86400 ))
      echo "stale-todo: $file:$line_no: untagged marker is $days days old (>${WINDOW_DAYS}d): ${line_content}" >&2
      fail=1
    fi
  done <<< "$hits"
done <<< "$files"

if [ "$fail" -ne 0 ]; then
  cat >&2 <<EOF

Cite an issue: TODO(#NNN), FIXME(#NNN), or include a /issues/NNN URL.
Or delete the marker. Spec D16 + C16: no deferred debt past ${WINDOW_DAYS}
days without a tracked consumer.
EOF
  exit 1
fi

echo "stale-todo: clean (window=${WINDOW_DAYS}d)"
