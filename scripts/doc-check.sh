#!/usr/bin/env bash
# doc-check.sh - repo-wide markdown gates for regatta.
#
# Three gates today, each a separate section:
#   1. Markdown link integrity (relative .md links resolve to a file)
#   2. Banned-phrase lint     (vague marketing language fails closed)
#   3. Em-dash / en-dash diff gate (only flags lines added by THIS PR)
#
# More gates land as regatta grows (Go test-name parity once the binary
# exists, RFC section assertions once docs/rfcs/ ships, etc.). Each new
# gate gets its own labeled section so removal stays cheap.
#
# Exits 0 on clean, 1 on the first failing gate.

set -euo pipefail

# --- Markdown link integrity ------------------------------------------------
#
# Drift pattern this gate closes: a doc rename / deletion leaves a
# [text](relative/path) link pointing at nothing. Link rot is its own
# bug class and the one a doc-cleanup sweep is most likely to introduce.
#
# Scope: every tracked markdown file's relative links to *.md files in
# the repo. External URLs (http/https) are out of scope. Anchors (#frag)
# are stripped - we only verify the file resolves, not the fragment.

link_dead=""
link_count=0
while IFS= read -r mdfile; do
  # Extract every [text](target) markdown link, skipping links that live
  # inside fenced code blocks (``` ... ```). Plans and design docs
  # frequently quote content inside fenced blocks; those links resolve
  # relative to the doc that owns them, not the quoting doc.
  while IFS= read -r target; do
    [ -z "$target" ] && continue
    # External URLs + bare anchors (out of scope).
    case "$target" in
      "#"*) continue ;;
      http://*|https://*|mailto:*) continue ;;
    esac
    # Strip the #anchor suffix if present; we resolve files, not fragments.
    target="${target%%#*}"
    [ -z "$target" ] && continue
    # Only .md targets (other files like .yaml change less often and
    # would explode the scope; revisit if drift surfaces).
    case "$target" in
      *.md) ;;
      *) continue ;;
    esac
    link_count=$((link_count + 1))
    # Resolve relative to the directory of the source file.
    src_dir=$(dirname "$mdfile")
    resolved="$src_dir/$target"
    if [ ! -e "$resolved" ]; then
      link_dead="${link_dead}${mdfile} -> ${target} (resolves to ${resolved})\n"
    fi
  done < <(perl -ne 'BEGIN{$in=0} if (/^```/) { $in = !$in; next } next if $in; while (/\[[^\]]*\]\(([^)]+)\)/g) { print "$1\n"; }' "$mdfile")
done < <(git ls-files '*.md' 2>/dev/null)

if [ -n "$link_dead" ]; then
  echo "doc-check: dead markdown link(s) detected:"
  printf '%b' "$link_dead" | sed 's/^/  - /'
  echo
  echo "Either fix the link target, point at the new location, or drop the reference."
  exit 1
fi

echo "doc-check: $link_count markdown link(s) resolve to on-disk files"

# --- Banned-phrase lint -----------------------------------------------------
#
# Drift pattern this gate closes: vague marketing language ("robust",
# "production-grade", "blazing-fast", "world-class") is a falsifiability
# failure. Each token below has bitten a real review cycle somewhere;
# fail closed on the next occurrence.
#
# Exemptions:
#   - research/**            - raw extracts from external sources
#   - docs/rfcs/**           - RFCs may quote prior art (when the
#                              directory exists)
#
# Tokens are matched case-insensitively with \b word boundaries, so
# "robust" matches but "robustness" does not. Tokens listed with
# `[- ]` accept either a hyphen or a space, so "production-grade"
# and "production grade" both match.

banned_tokens=(
  "blazing[- ]fast"
  "production[- ]grade"
  "world[- ]class"
  "best[- ]in[- ]class"
  "industry[- ]leading"
  "cutting[- ]edge"
  "lightning[- ]fast"
  "battle[- ]tested"
  "enterprise[- ]grade"
  "rock[- ]solid"
  "robust"
)

# Single regex pass: union the tokens, wrap with \b boundaries.
banned_union=$(IFS='|'; echo "${banned_tokens[*]}")
banned_regex="\\b(${banned_union})\\b"

mdfiles=$(git ls-files '*.md' 2>/dev/null \
  | grep -vE '^(research/|docs/rfcs/)' \
  || true)
mdcount=$(echo "$mdfiles" | grep -c . || true)

if [ -n "$mdfiles" ]; then
  banned_hits=$(echo "$mdfiles" | tr '\n' '\0' \
    | xargs -0 grep -niE -- "$banned_regex" 2>/dev/null || true)

  if [ -n "$banned_hits" ]; then
    echo "doc-check: banned-phrase hit(s) detected:"
    echo "$banned_hits" | sed 's/^/  - /'
    echo
    echo "Reword to a falsifiable claim, or move the prose into research/ or docs/rfcs/."
    exit 1
  fi
fi

echo "doc-check: banned-phrase lint clean across $mdcount markdown file(s) outside exemptions"

# --- Em-dash + en-dash diff-scope gate --------------------------------------
#
# Catches em-dashes (U+2014) and en-dashes (U+2013) introduced by THIS
# PR's added lines in *.md and *.go files. Existing-tree usage is
# grandfathered; only added lines are checked. Em-dashes and en-dashes
# are a common AI-style punctuation tell, and regatta is built by AI
# agents - the gate is more load-bearing here than in most repos.
#
# Base ref resolution:
#   - GITHUB_BASE_REF in CI (set on pull_request events)
#   - origin/main otherwise
#   - skipped with a notice when no base ref resolves (detached HEAD,
#     shallow clone without origin/main)
#
# Exemptions: research/, docs/rfcs/, .claude/ (skill content quoted
# from external sources).
#
# Mutation-verify pattern: commit an em-dash in a tracked .md or .go
# file, run `bash scripts/doc-check.sh`, watch the gate fail with the
# file:line citation, then `git reset --hard HEAD~1`.

base_ref="${GITHUB_BASE_REF:-origin/main}"
if ! git rev-parse --verify "$base_ref" >/dev/null 2>&1; then
  base_ref="origin/$base_ref"
fi

if ! git rev-parse --verify "$base_ref" >/dev/null 2>&1; then
  echo "doc-check: em-dash diff gate skipped (base ref not resolvable)"
else
  diff_hits=$(git diff --unified=0 "$base_ref"...HEAD -- '*.md' '*.go' 2>/dev/null \
    | python3 -c '
import re, sys
exempt = re.compile(r"^(research/|docs/rfcs/|\.claude/)")
bad = re.compile("[—–]")
file = ""
skip = False
lineno = 0
for raw in sys.stdin:
    line = raw.rstrip("\n")
    if line.startswith("+++ "):
        file = line[6:]
        skip = bool(exempt.match(file))
        continue
    if line.startswith("@@ "):
        m = re.search(r"\+(\d+)", line)
        lineno = int(m.group(1)) if m else 0
        continue
    if skip:
        continue
    if line.startswith("+") and not line.startswith("+++"):
        if bad.search(line):
            print(f"{file}:{lineno}: {line[1:]}")
        lineno += 1
')

  if [ -n "$diff_hits" ]; then
    echo "doc-check: em-dash or en-dash detected in PR-diff additions (vs $base_ref):"
    echo "$diff_hits" | sed 's/^/  - /'
    echo
    echo "Replace em-dash with hyphen-space or semicolon; en-dash with hyphen."
    echo "Existing tree usage is grandfathered; only lines added in this PR are checked."
    exit 1
  fi
  echo "doc-check: em-dash + en-dash diff gate clean (vs $base_ref)"
fi
