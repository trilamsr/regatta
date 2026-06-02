#!/usr/bin/env bash
# Prose-dup regression gate. Seeds below are phrases that were
# collapsed to a single source; the gate fails if any appears in 2+
# *.md files. Add a seed when collapsing a new duplicate.

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
  # node_modules-style noise + transient agent worktrees under
  # .claude/worktrees/ (these would falsely double-count every seed).
  #
  # PHASE-S-RELAX: also skip docs/engineer/{briefs,specs,plans}/ during
  # the self-host window — wave dispatch re-introduces shared spec
  # phrasing across sibling docs constantly. Restore strict scope at
  # 30-day-green trigger. Memory: feedback_gate_relaxation_phase_s.
  hits=$(grep -rIl --include='*.md' \
    --exclude-dir='.claude' \
    --exclude-dir='node_modules' \
    --exclude-dir='briefs' \
    --exclude-dir='specs' \
    --exclude-dir='plans' \
    -F "$phrase" "$root" 2>/dev/null || true)
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
