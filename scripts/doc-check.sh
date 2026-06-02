#!/usr/bin/env bash
# doc-check.sh - repo-wide markdown gates for regatta.
#
# Two gates today, each a separate section:
#   1. Markdown link integrity (relative .md links resolve to a file)
#   2. Banned-phrase lint     (vague marketing language fails closed)
#
# More gates land as regatta grows (Go test-name parity once the binary
# exists, RFC section assertions once docs/rfcs/ ships, etc.). Each new
# gate gets its own labeled section so removal stays cheap.
#
# Exits 0 on clean, 1 on the first failing gate.

set -euo pipefail

# Helpers.
#
# strip_doc_spans <file> -> stdout
#
# Streams a markdown file with fenced code blocks (``` ... ```) and inline
# backtick spans (`...`) blanked out, preserving line count so downstream
# `grep -n` line numbers still point at the source file's real line.
#
# Why both: a doc that *names* a banned token (e.g. a style guide listing
# what's forbidden) is meta-documentation, not a marketing claim. The
# author signals "code/literal" with backticks; the gate must respect that
# signal or it fights its own documentation (#329).
strip_doc_spans() {
  perl -ne '
    BEGIN { $in_fence = 0 }
    if (/^```/) { $in_fence = !$in_fence; print "\n"; next }
    if ($in_fence) { print "\n"; next }
    s/`[^`]*`//g;
    print;
  ' "$1"
}

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
  | grep -vE '^(research/|docs/rfcs/|scripts/testdata/)' \
  || true)
mdcount=$(echo "$mdfiles" | grep -c . || true)

banned_hits=""
if [ -n "$mdfiles" ]; then
  while IFS= read -r mdfile; do
    [ -z "$mdfile" ] && continue
    # Strip fenced + inline-backtick spans before scanning so that
    # meta-docs naming the banned tokens (style guide, dispatch templates)
    # don't trip the gate they document. See strip_doc_spans helper above.
    hits=$(strip_doc_spans "$mdfile" | grep -niE -- "$banned_regex" || true)
    if [ -n "$hits" ]; then
      # Prefix each match with the source file so the user can locate it.
      banned_hits="${banned_hits}$(echo "$hits" | sed "s|^|${mdfile}:|")"$'\n'
    fi
  done <<< "$mdfiles"

  if [ -n "$banned_hits" ]; then
    echo "doc-check: banned-phrase hit(s) detected:"
    printf '%s' "$banned_hits" | sed 's/^/  - /'
    echo
    echo "Reword to a falsifiable claim, or move the prose into research/ or docs/rfcs/."
    echo "Tip: code spans (backticks) and fenced blocks are exempt — wrap literal tokens in \`...\` to mention them."
    exit 1
  fi
fi

echo "doc-check: banned-phrase lint clean across $mdcount markdown file(s) outside exemptions"

# Comment-noise diff-scope gate.
# Blocks three patterns on PR-added lines that rot in long-lived files:
# review-cycle inline tags, bare PR-number backreferences in source-code
# comments, and ASCII section-banner comments. Rationale in STYLE.md.
# Mutation-verify recipe lives in the PR that landed this gate.
#
# Base ref resolution:
#   - GITHUB_BASE_REF in CI (set on pull_request events)
#   - origin/main otherwise
#   - skipped with a notice when no base ref resolves

base_ref="${GITHUB_BASE_REF:-origin/main}"
if ! git rev-parse --verify "$base_ref" >/dev/null 2>&1; then
  base_ref="origin/$base_ref"
fi

if ! git rev-parse --verify "$base_ref" >/dev/null 2>&1; then
  echo "doc-check: comment-noise diff gate skipped (base ref not resolvable)"
else
  noise_hits=$(git diff --unified=0 "$base_ref"...HEAD -- '*.go' '*.md' '*.sh' '*.py' '*.yaml' '*.yml' 'Makefile' 'Dockerfile*' 2>/dev/null \
    | python3 -c '
import re, sys
exempt = re.compile(r"^(research/|docs/rfcs/|\.claude/)")
# reviewer-tag fires in any file; banner + pr-ref restrict to source-code
# comment leaders (// or shell #), because in markdown # introduces a
# heading and would false-positive on legitimate doc structure.
#
# Tag shape (issue #333): the gate targets review-cycle inline tags like
# "Reviewer Bob:" or "Reviewer-Alice/round-2", not prose that happens to
# put a capitalized noun after "reviewer" ("Reviewer Notifier returns",
# "Zero-reviewer Request"). Required structure:
#   1. anchor: line-start, whitespace, or '@'
#   2. literal "Reviewer" or "reviewer"
#   3. separator: whitespace or '-' (no other prose runs into the name)
#   4. one Capital+lowercase word (excludes ALL-CAPS terms like JSON)
#   5. optional /-#-joined subref segments
#   6. terminator: a colon or end-of-line (allowing trailing whitespace)
reviewer_tag = re.compile(r"(?:^|[\s@])[Rr]eviewer(?:\s+|-)[A-Z][a-z]+(?:[/#-][A-Za-z0-9-]+)*(?:\s*:|\s*$)")
source_pr_ref = re.compile(r"//.*\bPR\s*#\d+|^\s*#\s.*\bPR\s*#\d+")
source_banner = re.compile(r"^//\s*[-=]{2,}\s*\S.*\s*[-=]{2,}\s*$|^#\s*[-=]{2,}\s*\S.*\s*[-=]{2,}\s*$")
file = ""
skip = False
is_markdown = False
lineno = 0
for raw in sys.stdin:
    line = raw.rstrip("\n")
    if line.startswith("+++ "):
        file = line[6:]
        skip = bool(exempt.match(file))
        is_markdown = file.endswith(".md")
        continue
    if line.startswith("@@ "):
        m = re.search(r"\+(\d+)", line)
        lineno = int(m.group(1)) if m else 0
        continue
    if skip:
        continue
    if line.startswith("+") and not line.startswith("+++"):
        body = line[1:]
        kind = None
        if reviewer_tag.search(body):
            kind = "reviewer-tag"
        elif not is_markdown and source_pr_ref.search(body):
            kind = "pr-ref"
        elif not is_markdown and source_banner.search(body):
            kind = "banner-comment"
        if kind:
            print(f"{file}:{lineno}: [{kind}] {body.strip()}")
        lineno += 1
')

  if [ -n "$noise_hits" ]; then
    echo "doc-check: comment-noise detected in PR-diff additions (vs $base_ref):"
    echo "$noise_hits" | sed 's/^/  - /'
    echo
    echo "Patterns blocked: reviewer tags, bare \"PR #N\" refs, section-banner comments."
    echo "Why: STYLE.md defaults to no comments; these rot in long-lived files."
    echo "Existing tree usage is grandfathered; only lines added in this PR are checked."
    exit 1
  fi
  echo "doc-check: comment-noise diff gate clean (vs $base_ref)"

  # Test/Fuzz/Benchmark function godocs must be one line max — names
  # already encode intent. Detects 2+ consecutive `//` comment lines
  # in added hunks whose first line introduces a Test/Fuzz/Benchmark.
  godoc_overflows=$(git diff --unified=0 "$base_ref"...HEAD -- '*.go' 2>/dev/null | python3 -c '
import re, sys
test_re = re.compile(r"^// (Test|Fuzz|Benchmark)[A-Z]")
file, skip, lineno, streak, anchor, start = "", False, 0, 0, "", 0
def flush():
    if streak > 1 and test_re.match(anchor):
        print(f"{file}:{start}: {anchor[:80]!r} godoc is {streak} lines; max 1")
for ln in sys.stdin:
    ln = ln.rstrip("\n")
    if ln.startswith("+++ "):
        flush(); file, skip, streak = ln[6:], not ln.endswith(".go"), 0
    elif ln.startswith("@@ "):
        flush(); streak = 0
        m = re.search(r"\+(\d+)", ln); lineno = int(m.group(1)) if m else 0
    elif not skip and ln.startswith("+") and not ln.startswith("+++"):
        body = ln[1:].strip()
        if body.startswith("//"):
            if streak == 0: start, anchor = lineno, body
            streak += 1
        else:
            flush(); streak = 0
        lineno += 1
flush()
')
  if [ -n "$godoc_overflows" ]; then
    echo "doc-check: test/fuzz/benchmark godocs > 1 line (vs $base_ref):"
    echo "$godoc_overflows" | sed 's/^/  - /'
    echo "Rule: Test/Fuzz/Benchmark godocs = 1 line max. Test names encode intent."
    exit 1
  fi
  echo "doc-check: test-godoc length gate clean (vs $base_ref)"
fi
