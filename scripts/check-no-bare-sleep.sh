#!/usr/bin/env bash
# check-no-bare-sleep.sh - reject `time.Sleep` lexically nested inside a `for`
# block in any `*_test.go` file. Forces migration to state-driven settle
# (testutil.Eventually / testutil.EventuallyT / testutil.AssertStable), which
# fails fast on timeout, short-circuits on success, and never burns a fixed
# wall-clock budget.
#
# Why: bare polling Sleeps are the dominant flake source — slow CI runners
# overshoot the settle window, fast CI runners pad it. They also hide the
# observable predicate from the failing-test record. `testutil.Eventually`
# captures the predicate, fails the test with the last observed value, and
# returns the moment the predicate holds.
#
# Heuristic: walk every `*_test.go` file. Track `for ... {` blocks via brace
# depth — when a `time.Sleep(` line appears with for-depth > 0, fail.
# Excludes:
#   - filenames ending `_bench_test.go` (benchmarks measure wall-clock by design)
#   - lines carrying `// allow-sleep:` directive (with a reason)
#
# Scope: every `*_test.go` file under TESTS_ROOT (defaults to repo root).
# Override via TESTS_ROOT for the fixture test runner.
#
# Exit: 0 clean, 1 on first violation (lists every hit before exit).

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)

: "${TESTS_ROOT:=$REPO_ROOT}"

if [ ! -d "$TESTS_ROOT" ]; then
  echo "check-no-bare-sleep: TESTS_ROOT not found: $TESTS_ROOT (skipping)"
  exit 0
fi

# scan_file <path> -> emits "<file>:<lineno>: time.Sleep in for-loop" for every
# violation. Perl one-pass:
#   - Track brace depth via opening `{` and closing `}` counts per line.
#   - Track a stack of for-block depth thresholds: when we see `for ...{`,
#     push current depth+1 (the depth INSIDE the for body). When depth drops
#     below the top stack entry, pop.
#   - If a line contains `time.Sleep(` AND the for-stack is non-empty AND the
#     line does not carry `// allow-sleep:`, emit it.
#   - Strip string-literal and `//`-comment spans before brace counting so
#     stray braces in strings or comments don't desync the depth tracker.
scan_file() {
  perl - "$1" <<'PERL'
use strict;
use warnings;

my $path = shift @ARGV;
open(my $fh, '<', $path) or die "open $path: $!";

my $depth = 0;            # current brace depth
my @for_stack = ();       # stack of "this is the depth INSIDE a for body"
my $lineno = 0;
my $in_block_comment = 0;

while (my $raw = <$fh>) {
    $lineno++;
    chomp(my $line = $raw);

    # Preserve the original line for emission; build a sanitized copy for
    # brace + token scanning.
    my $scan = $line;

    # Strip block comments /* ... */ that open + close on this line.
    $scan =~ s{/\*.*?\*/}{}g;

    # Handle a block comment that opens here and continues.
    if ($in_block_comment) {
        if ($scan =~ s{^.*?\*/}{}) {
            $in_block_comment = 0;
        } else {
            next;
        }
    }
    if ($scan =~ s{/\*.*$}{}) {
        $in_block_comment = 1;
    }

    # Track `// allow-sleep:` BEFORE stripping line comments — the directive
    # must survive the strip pass.
    my $allow = ($line =~ m{//\s*allow-sleep:});

    # Strip line comments.
    $scan =~ s{//.*$}{};

    # Strip string literals — both "..." and `...` raw strings — so stray
    # braces in strings don't desync the tracker.
    $scan =~ s{"(?:\\.|[^"\\])*"}{""}g;
    $scan =~ s{`[^`]*`}{``}g;

    # Detect `for` at the start of a for-statement on this line. Match:
    #   for {
    #   for cond {
    #   for i := 0; i < N; i++ {
    #   for k, v := range x {
    # The `{` may be on this line OR (rarely) the next; Go style normally
    # keeps it on the same line, which matches gofmt.
    my $for_opens_here = 0;
    if ($scan =~ /\bfor\b/) {
        # Confirm a `{` after `for` on this same line.
        if ($scan =~ /\bfor\b[^{}]*\{/) {
            $for_opens_here = 1;
        }
    }

    # Count braces on the sanitized line.
    my $opens  = () = $scan =~ /\{/g;
    my $closes = () = $scan =~ /\}/g;

    # Compute depth BEFORE this line's net change (so a Sleep on the SAME
    # line as the opening `for {` is recognized as inside, not above).
    my $depth_before = $depth;

    # Apply opens first (so we know the depth INSIDE the for body if this
    # line opens one).
    $depth += $opens;

    # If this line opens a for, record the depth INSIDE its body. That depth
    # equals depth_before + (the for's contribution to opens). We can't
    # disambiguate multiple `{` on one line cheaply — but for-blocks in Go
    # test code reliably open on their own line. Conservative rule: push
    # the post-opens depth. While current depth >= pushed depth, we are
    # inside the for body.
    if ($for_opens_here) {
        push @for_stack, $depth;
    }

    # Check for a Sleep call on this line.
    if (@for_stack && $scan =~ /\btime\.Sleep\s*\(/) {
        unless ($allow) {
            print "$path:$lineno: time.Sleep inside for-loop (#G3) — migrate to testutil.Eventually / testutil.EventuallyT / testutil.AssertStable, or annotate `// allow-sleep: <reason>` if legitimately non-polling\n";
        }
    }

    # Apply closes.
    $depth -= $closes;
    if ($depth < 0) { $depth = 0; }  # defensive: never below zero

    # Pop for-stack entries whose threshold is now above current depth.
    while (@for_stack && $for_stack[-1] > $depth) {
        pop @for_stack;
    }
}

close($fh);
PERL
}

scanned=0
violations=0
violation_lines=""

# Walk the tree. macOS find doesn't support -printf, so use -print0 + while.
while IFS= read -r -d '' f; do
  base=$(basename -- "$f")
  # Allowlist: benchmark files measure wall-clock.
  case "$base" in
    *_bench_test.go) continue ;;
  esac
  scanned=$((scanned + 1))
  hits=$(scan_file "$f")
  if [ -n "$hits" ]; then
    violation_lines="${violation_lines}${hits}"$'\n'
    n=$(printf '%s\n' "$hits" | grep -c .)
    violations=$((violations + n))
  fi
done < <(find "$TESTS_ROOT" \
  -type f -name '*_test.go' \
  -not -path '*/vendor/*' \
  -not -path '*/.git/*' \
  -not -path '*/.claude/worktrees/*' \
  -not -path '*/scripts/testdata/*' \
  -print0)

if [ "$violations" -gt 0 ]; then
  echo "check-no-bare-sleep: $violations bare-Sleep-in-for-loop violation(s) across $scanned test file(s):"
  printf '%s' "$violation_lines" | sed 's/^/  - /'
  echo
  echo "Polling sleeps are flaky on CI (slow runners overshoot, fast runners pad)."
  echo "Migrate to state-driven settle:"
  echo "  - testutil.Eventually   — predicate-based, short-circuits on success."
  echo "  - testutil.EventuallyT  — variant carrying *testing.T for fail-fast."
  echo "  - testutil.AssertStable — asserts a value stays stable across a window."
  echo "If the Sleep is legitimately non-polling (e.g. wall-clock observation,"
  echo "mtime resolution, goroutine-leak settle), annotate with a reason:"
  echo "  time.Sleep(d) // allow-sleep: <one-line reason>"
  exit 1
fi

echo "check-no-bare-sleep: $scanned test file(s) scanned; no bare Sleep-in-for violations"
exit 0
