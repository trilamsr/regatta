#!/usr/bin/env bash
# check-tbd.sh - mechanical gate against unresolved `TBD` placeholders in
# engineer docs.
#
# Bare `TBD` (owner: TBD, issue umbrella: TBD, #TBD-followup, etc.) is prose
# debt — it ships unresolved, dies in PR-body limbo, and reviewer audits have
# to re-discover the gaps. The resolution path is one of:
#   1. Replace TBD with concrete content (a real owner, a real issue ref).
#   2. Cite an issue `#NNN` on the SAME LINE as the TBD; the gate treats this
#      as a tracking-issue handoff and lets the placeholder stand.
#   3. Wrap the placeholder in an HTML comment that carries an issue ref:
#      `<!-- TODO(#NNN) -->` (or any `<!-- ... #NNN ... -->`). This is the
#      pattern for inline "fill this in later" markers that already have an
#      issue to land them.
#   4. Type bare TBDs inside a ```release-notes fence — that block is
#      operator scratch space for drafting a PR body in the spec; gate
#      ignores TBDs there.
#   5. Wrap literal meta-mentions in backticks — mirroring the doc-check.sh
#      banned-phrase policy, a token wrapped in `inline-code` spans counts
#      as a meta-mention rather than absorbed scope.
#
# Scope: every `*.md` file under docs/engineer/{specs,plans,briefs}/
# (overridable via DOCS_ROOT for the fixture test — when DOCS_ROOT is set the
# script walks DOCS_ROOT/{specs,plans,briefs}/).
#
# Exit: 0 clean, 1 on first bare TBD (lists every hit before exit).
set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

: "${DOCS_ROOT:=$REPO_ROOT/docs/engineer}"

SUBDIRS=(specs plans briefs)

# scan_doc <path> -> emits "<file>:<lineno>: <line>" for every bare TBD.
#
# Filter pipeline (Perl one-pass):
#   - Track release-notes fence open/close — skip lines inside.
#   - Skip HTML-comment lines that carry a `#NNN` issue ref anywhere on the
#     line (covers `<!-- TODO(#NNN) ... -->` and multi-token variants).
#   - Skip lines that carry both `TBD` AND a `#NNN` ref on the same line.
#   - Emit lines that still carry a bare `TBD` token.
scan_doc() {
  perl -ne '
    BEGIN { $in_rel_notes = 0 }
    chomp(my $line = $_);
    if ($line =~ /^```release-notes\b/) { $in_rel_notes = 1; next }
    if ($in_rel_notes) {
      if ($line =~ /^```\s*$/) { $in_rel_notes = 0 }
      next;
    }
    # Strip inline backtick spans first — meta-mentions are exempt
    # (mirrors scripts/doc-check.sh banned-phrase policy).
    my $stripped = $line;
    $stripped =~ s/`[^`]*`//g;
    next unless $stripped =~ /\bTBD\b/;
    # HTML-comment with issue ref anywhere on the original line.
    next if $line =~ /<!--.*#\d+.*-->/;
    # Bare TBD with same-line issue citation.
    next if $stripped =~ /#\d+/;
    printf("%s:%d: %s\n", $ARGV, $., $line);
  ' "$1"
}

hits=""
hit_count=0
scanned=0

for sub in "${SUBDIRS[@]}"; do
  dir="$DOCS_ROOT/$sub"
  [ -d "$dir" ] || continue
  while IFS= read -r -d '' docfile; do
    scanned=$((scanned + 1))
    out=$(scan_doc "$docfile")
    if [ -n "$out" ]; then
      while IFS= read -r row; do
        [ -z "$row" ] && continue
        hits="${hits}${row}"$'\n'
        hit_count=$((hit_count + 1))
      done <<< "$out"
    fi
  done < <(find "$dir" -maxdepth 1 -type f -name '*.md' -print0)
done

if [ "$hit_count" -gt 0 ]; then
  echo "check-tbd: $hit_count bare \`TBD\` placeholder(s) detected across $scanned doc(s):"
  printf '%s' "$hits" | sed 's/^/  - /'
  echo
  echo "Resolution: (1) replace TBD with concrete content, (2) cite \`#NNN\` on"
  echo "the same line, (3) wrap in \`<!-- TODO(#NNN) -->\`, or (4) move into a"
  echo "\`\`\`release-notes fence if it is a PR-body placeholder."
  exit 1
fi

echo "check-tbd: $scanned doc(s) scanned; no bare \`TBD\` placeholders"
exit 0
