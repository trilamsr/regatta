#!/usr/bin/env bash
# changelog-gen.sh: derive a CHANGELOG section from Conventional
# Commits between two refs. Default range: from the most recent tag
# to HEAD. Output is markdown ready to drop under "## Unreleased".
#
# Usage:
#   bash scripts/changelog-gen.sh             # tag..HEAD
#   bash scripts/changelog-gen.sh v0.1.0..    # explicit range
#
# Type-to-section mapping:
#   feat     -> Added
#   refactor -> Changed
#   fix      -> Fixed
#   security -> Security
#   perf     -> Performance
#   docs|chore|build|test|style -> Internal
#
# Spec section 5: CHANGELOG is auto-generated; releases derive
# customer release-notes from this output. Portable to macOS
# bash 3.2 (no associative arrays, no fragile eval).

set -euo pipefail

range="${1:-}"
if [ -z "$range" ]; then
  last_tag=$(git describe --tags --abbrev=0 2>/dev/null || true)
  if [ -n "$last_tag" ]; then
    range="${last_tag}..HEAD"
  else
    range="HEAD"
  fi
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# One file per bucket. Append-only; no eval games.
for bucket in added changed fixed security performance internal; do
  : > "$tmp/$bucket"
done

bucket_for() {
  case "$1" in
    feat*)     echo added ;;
    refactor*) echo changed ;;
    fix*)      echo fixed ;;
    security*) echo security ;;
    perf*)     echo performance ;;
    *)         echo internal ;;
  esac
}

while IFS= read -r subject; do
  [ -z "$subject" ] && continue
  bucket=$(bucket_for "$subject")
  printf -- '- %s\n' "$subject" >> "$tmp/$bucket"
done < <(git log --pretty=tformat:'%s' "$range")

printed=0
print_section() {
  local label="$1" file="$2"
  if [ -s "$file" ]; then
    [ "$printed" -eq 1 ] && echo
    echo "### ${label}"
    echo
    cat "$file"
    printed=1
  fi
}

print_section Added       "$tmp/added"
print_section Changed     "$tmp/changed"
print_section Fixed       "$tmp/fixed"
print_section Security    "$tmp/security"
print_section Performance "$tmp/performance"
print_section Internal    "$tmp/internal"

if [ "$printed" -eq 0 ]; then
  echo "(no Conventional Commits in ${range})"
fi
