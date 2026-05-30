#!/usr/bin/env bash
# check-empty-dirs.sh - earn-or-delete gate for empty directories.
#
# Drift this gate closes: spec section 3 P10 + P14 ("earn the slot
# OR delete; speculative scaffolding rots into noise"). A directory
# whose only contents are a README and/or .gitkeep must declare an
# explicit "Activation trigger:" line in its README. Otherwise the
# directory is speculative and must either ship code or be deleted.
#
# Inputs:
#   EMPTY_DIR_ROOT - root to scan. Defaults to repo root.

set -euo pipefail

root="${EMPTY_DIR_ROOT:-.}"

# Find dirs whose only contents are README.md and/or .gitkeep.
# Skip .git, .claude, node_modules, vendor, dist.
empties=$(
  find "$root" -type d \
    -not -path '*/\.git*' \
    -not -path '*/\.claude*' \
    -not -path '*/node_modules*' \
    -not -path '*/vendor*' \
    -not -path '*/dist*' \
    2>/dev/null | while read -r d; do
      # Skip the root itself.
      [ "$d" = "$root" ] && continue
      # Skip dirs with subdirs that have content (the empty-ness check
      # is "no files except README/.gitkeep at any depth").
      others=$(find "$d" -mindepth 1 ! -name README.md ! -name .gitkeep \
        ! -type d 2>/dev/null)
      [ -n "$others" ] && continue
      # Skip dirs that are themselves empty of README/.gitkeep too.
      markers=$(find "$d" -maxdepth 1 \( -name README.md -o -name .gitkeep \) 2>/dev/null)
      [ -z "$markers" ] && continue
      echo "$d"
    done
)

fail=0
while IFS= read -r d; do
  [ -z "$d" ] && continue
  readme="$d/README.md"
  if [ ! -f "$readme" ]; then
    echo "check-empty-dirs: $d has marker files but no README.md. Add one with an Activation trigger line, or delete the directory." >&2
    fail=1
    continue
  fi
  if ! grep -Eqi '(^|[[:space:]#])Activation trigger($|[:[:space:]])' "$readme" 2>/dev/null; then
    echo "check-empty-dirs: $readme is missing an 'Activation trigger:' line. Empty dirs must name the concrete event (PR, milestone, customer ask) that earns them their slot - otherwise delete." >&2
    fail=1
  fi
done <<<"$empties"

if [ "$fail" -ne 0 ]; then
  echo "check-empty-dirs: spec P10/P14 - earn the slot OR delete." >&2
  exit 1
fi

echo "check-empty-dirs: ok"
