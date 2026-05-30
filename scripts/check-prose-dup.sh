#!/usr/bin/env bash
# check-prose-dup.sh - prose duplicate detector.
#
# Drift this gate closes: customer surface docs (docs/operator,
# docs/auditor, docs/engineer) restate paragraphs from the design
# spec or from each other rather than link to a single source. Spec
# D3: source of truth, not parallel restatement.
#
# Method: seed-phrase regression list. Each entry is a 5-7 word
# phrase that was duplicated at some point and has since been
# single-sourced; the gate fails if the phrase appears in 2+ tracked
# *.md files at once.
#
# Adding a seed: when you collapse a duplicate, add the phrase
# below. The gate then prevents the duplicate from drifting back.
#
# Inputs:
#   PROSE_DUP_ROOT - directory to scan. Defaults to repo root.

set -euo pipefail

root="${PROSE_DUP_ROOT:-.}"

# Seed phrases. Format: one phrase per line; phrases are matched
# case-insensitively as fixed strings. Keep each phrase distinctive
# (5+ content words) to avoid false positives on stop-word collisions.
seeds_file=$(mktemp)
trap 'rm -f "$seeds_file"' EXIT
cat >"$seeds_file" <<'SEEDS'
Several silent-bypass classes that GitHub does not surface itself
P2 canonical recipe (`required_approving_review_count: 2`
canary archetypes (see [`testdata/gates/canary/README.md`
Promotion criteria (concurrency 1 -> 2): >=20 PRs merged
`-trimpath` strips local paths from binaries
calibrate against 3 already-merged PRs
SEEDS

fail=0
while IFS= read -r phrase; do
  [ -z "$phrase" ] && continue
  # Count files containing the phrase. Restrict to *.md and skip
  # node_modules-style noise (none here, but cheap insurance).
  hits=$(grep -rIl --include='*.md' -F "$phrase" "$root" 2>/dev/null || true)
  count=$(echo -n "$hits" | grep -c . || true)
  if [ "$count" -gt 1 ]; then
    echo "check-prose-dup: duplicate phrase appears in $count files:" >&2
    echo "  phrase: $phrase" >&2
    echo "$hits" | sed 's/^/    /' >&2
    fail=1
  fi
done <"$seeds_file"

if [ "$fail" -ne 0 ]; then
  echo "check-prose-dup: collapse duplicates - link from the customer-surface doc to the source-of-truth section instead of restating it. Spec D3." >&2
  exit 1
fi

echo "check-prose-dup: ok"
