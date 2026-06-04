#!/usr/bin/env bash
# check-scorecard_test.sh - black-box fixtures for check-scorecard.sh.
#
# Each fixture writes a PR body to a temp file then invokes the script
# with --body-file. Asserts on exit code + optional stdout substring.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check="$script_dir/check-scorecard.sh"

failed=0
pass=0

run_case() {
  local name="$1"; shift
  local want_exit="$1"; shift
  local want_stdout_match="$1"; shift  # may be empty
  local body="$1"; shift

  tmp=$(mktemp)
  printf '%s\n' "$body" > "$tmp"

  out=$("$check" --body-file "$tmp" 2>&1) && got_exit=0 || got_exit=$?

  if [ "$got_exit" != "$want_exit" ]; then
    echo "FAIL $name: exit=$got_exit want $want_exit"
    echo "  output: $out"
    failed=$((failed + 1))
  elif [ -n "$want_stdout_match" ] && ! printf '%s\n' "$out" | grep -q "$want_stdout_match"; then
    echo "FAIL $name: stdout missing '$want_stdout_match'"
    echo "  output: $out"
    failed=$((failed + 1))
  else
    pass=$((pass + 1))
    echo "PASS $name"
  fi
  rm -f "$tmp"
}

# Fixture: empty body → fail (no scorecard, [FEATURE] implied via missing
# category absence — defaults to required because no exempt category).
body_empty_feature='```release-notes
[FEATURE] add foo
```
## Summary

Body has no scorecard section.
'
run_case "empty-scorecard-feature-fails" 1 "No scorecard section" "$body_empty_feature"

# Fixture (#826 trap): [CHORE] prefix written OUTSIDE the release-notes
# fence — category regex misses it, scorecard absent, operator sees a
# message that demands a scorecard instead of pointing at the fence.
# New diagnostic must mention the fence rule + the exempt category list.
body_chore_no_fence='## Summary

Chore PR, but author forgot the triple-backtick fence.

## Test plan
- [x] verified locally

[CHORE] gitignore .env
'
run_case "chore-no-fence-mentions-fence-rule" 1 "release-notes" "$body_chore_no_fence"
run_case "chore-no-fence-lists-exempt-categories" 1 "CHORE" "$body_chore_no_fence"

# Fixture: scorecard with all [x] cited → pass.
body_all_cited='```release-notes
[FEATURE] add foo
```
## Summary

Adds Foo.

## A+ Scorecard

- [x] (a) B-floor build green — TestFooBuild covers it
- [x] (b) test coverage — internal/foo/foo_test.go:42
- [x] (c) tracking issue for deferral — see #1234
- [x] (d) cardinality budget — N/A — pure CPU work, no labels
'
run_case "all-cited-passes" 0 "scorecard: 4/4 criteria cited" "$body_all_cited"

# Fixture: scorecard with one [x] uncited → fail (criterion identified).
body_one_uncited='```release-notes
[FEATURE] add foo
```
## A+ Scorecard

- [x] (a) build green — TestBuildGreen
- [x] (b) tests added — TestFoo passes
- [x] (c) docs updated — no evidence here, just vibes
- [x] (d) cardinality — N/A — pure CPU
'
run_case "one-uncited-fails" 2 "(c)" "$body_one_uncited"

# Fixture: [CHORE] category + no scorecard → pass (skip).
body_chore_no_scorecard='```release-notes
[CHORE] bump deps
```
## Summary

Dependency bump, no behavior change.
'
run_case "chore-no-scorecard-passes" 0 "exempt" "$body_chore_no_scorecard"

# Fixture: [FEATURE] + 3 cited + 1 N/A → pass.
body_feature_three_plus_na='```release-notes
[FEATURE] cost cap
```
## A+ Rubric Scorecard

- [x] (a) make-check green — TestCapBuild
- [x] (b) impl tests — internal/cost/cap/enforcer_test.go:12
- [x] (c) operator override — TestEnforcer_OperatorResume
- [x] (d) substrate event cross-link — N/A — DEFERRED to F3 (see #999)
'
run_case "feature-three-plus-na-passes" 0 "scorecard: 4/4 criteria cited" "$body_feature_three_plus_na"

# Fixture: [FEATURE] + 4 [x] no cites → fail with all surfaced.
body_four_uncited='```release-notes
[FEATURE] something
```
## A+ Scorecard

- [x] (a) vibes
- [x] (b) more vibes
- [x] (c) trust me
- [x] (d) it works on my machine
'
run_case "four-uncited-fails-all" 2 "4 offending row" "$body_four_uncited"

# Fixture: [x] inside inline-code span must NOT count (regex over-match
# defense — Risk-tier per spec).
body_inline_code_xspan='```release-notes
[CHORE] doc tweak
```
## Notes

To mark a criterion done, use `[x]` in the row.
'
run_case "inline-code-xspan-not-counted-chore" 0 "exempt" "$body_inline_code_xspan"

# Fixture: bracketed format inside fenced block (W5 style:
# "[x] (a) [x] (b) [x] (c)" on one row) → multiple marks on one line
# all need citation on that line. Without cites → fail.
body_fenced_multi_uncited='```release-notes
[FEATURE] thing
```
## Scorecard

```
W5 scorecard
B floor   : [x] (a) [x] (b) [x] (c) [x] (d)
A target  : [x] (e) [x] (f) [x] (g) [ ] (h) [x] (i) [x] (j)
```
'
run_case "fenced-multi-uncited-fails" 2 "B floor\|line" "$body_fenced_multi_uncited"

# Fixture: same fenced-multi but with test names on the row → pass.
body_fenced_multi_cited='```release-notes
[FEATURE] thing
```
## Scorecard

```
B floor   : [x] (a) TestBuild  [x] (b) TestRun  [x] (c) TestExit
A target  : [x] (d) see #42  [x] (e) N/A — out of scope
```
'
run_case "fenced-multi-cited-passes" 0 "scorecard: 5/5 criteria cited" "$body_fenced_multi_cited"

# Fixture: [CI] category + no scorecard → pass (skip).
body_ci_skip='```release-notes
[CI] add workflow
```
## Summary

CI workflow only, no impl tests on the gate yet.
'
run_case "ci-no-scorecard-passes" 0 "exempt" "$body_ci_skip"

# Fixture: [REFACTOR] category + no scorecard → pass (skip). Mechanical
# refactors (test polling migrations, code reshape with no behavior change)
# do not earn scorecard ceremony.
body_refactor_no_scorecard='```release-notes
[REFACTOR] migrate polling sites to EventuallyT
```
## Summary

Pure test-helper migration, no production behavior change.
'
run_case "refactor-no-scorecard-passes" 0 "exempt" "$body_refactor_no_scorecard"

# Fixture: [FIX] category + scorecard with all cited → pass.
body_fix_cited='```release-notes
[FIX] correct boundary
```
## A+ Scorecard

- [x] (a) regression test — TestBoundaryRegression
- [x] (b) root cause — see #555
'
run_case "fix-cited-passes" 0 "scorecard: 2/2 criteria cited" "$body_fix_cited"

# Fixture: scorecard header variant "## B / A / A+ scorecard" supported.
body_alt_header='```release-notes
[FEATURE] obs surface
```
## B / A / A+ scorecard

- [x] B1 build green — TestBuild
- [x] B2 metrics — internal/obs/metrics_test.go:88
'
run_case "alt-header-passes" 0 "scorecard: 2/2 criteria cited" "$body_alt_header"

# Fixture: citations wrapped in backticks must count (#741 regression).
# Authors routinely wrap file paths and identifiers in backticks for
# rendering. Pre-fix, sed stripped the entire backtick span before
# regex scan, making the citation invisible and the row read as uncited.
body_backticked_cites='```release-notes
[FEATURE] add bar
```
## A+ Scorecard

- [x] (a) build green — `TestBarBuild`
- [x] (b) coverage — `internal/bar/bar_test.go:42`
- [x] (c) deferral tracked — see `#1234`
- [x] (d) cardinality — `N/A` — pure CPU
'
run_case "backticked-cites-counted" 0 "scorecard: 4/4 criteria cited" "$body_backticked_cites"

# Fixture: prose-style `[x]` inside backticks within a scorecard section
# must still NOT be counted as a criterion mark. Defends against the
# Option B unwrap regression: if we unwrapped delimiters, this `[x]`
# would leak into the count.
body_prose_xspan_in_scorecard='```release-notes
[FEATURE] add baz
```
## A+ Scorecard

- [x] (a) build green — TestBazBuild
- Note: to mark a criterion done, use `[x]` in the row prose.
'
run_case "prose-xspan-in-scorecard-not-counted" 0 "scorecard: 1/1 criteria cited" "$body_prose_xspan_in_scorecard"

# Fixture (#853): [CHORE] with malformed/partial scorecard (uncited [x])
# must still short-circuit on category-exempt BEFORE the parse — implementer
# pastes a scorecard skeleton into a chore PR; today the parser flags
# uncited rows and fails before reaching the exempt branch.
body_chore_partial_scorecard='```release-notes
[CHORE] tidy
```
## A+ Scorecard

- [x] (a) vibes
- [x] (b) more vibes
'
run_case "chore-partial-scorecard-passes" 0 "exempt" "$body_chore_partial_scorecard"

# Fixture (#854): uncited row failure must echo the offending row text on
# stderr so operator does not have to re-grep the source. Pin against the
# row prose "docs updated" so the assertion fails if the script only
# surfaces a line number.
run_case "uncited-row-text-emitted" 2 "docs updated" "$body_one_uncited"

# Fixture (#854): uncited row failure must emit an expected-token-shape
# hint listing the four valid citation shapes (Test*-name, path.ext:NN,
# #NNN, N/A). Pin against the hint header "Expected".
run_case "uncited-row-hint-emitted" 2 "Expected" "$body_one_uncited"

# Fixture (#854): the hint must include a concrete example for each
# shape so the operator can copy-paste. Pin against the file:line shape
# token which is the most-easily-malformed citation form.
run_case "uncited-row-hint-shows-file-line-example" 2 "path/to/file" "$body_one_uncited"

# Fixture (#855): labeled-row uncited error surfaces the label so
# operators don't have to count rows. Pins both `(a)`-form lowercase
# parens AND `B1`/`A1`/`A+1`-form tier-letter+digit labels.
body_labeled_uncited_parens='```release-notes
[FEATURE] thing
```
## A+ Scorecard

- [x] (a) build green — TestBuild
- [x] (b) vibes only no cite
'
run_case "labeled-uncited-parens-surfaces-label" 2 "(b)" "$body_labeled_uncited_parens"

body_labeled_uncited_tier='```release-notes
[FEATURE] thing
```
## A+ Scorecard

- [x] B1 build green — TestBuild
- [x] A+1 deletion defense vibes only
'
run_case "labeled-uncited-tier-surfaces-label" 2 "A+1" "$body_labeled_uncited_tier"

# Fixture: --skip short-circuits without parsing.
tmp_skip=$(mktemp); printf 'irrelevant body\n' > "$tmp_skip"
out=$("$check" --skip 2>&1) && got_exit=0 || got_exit=$?
if [ "$got_exit" = "0" ] && printf '%s' "$out" | grep -q "skipped"; then
  pass=$((pass + 1)); echo "PASS skip-flag-short-circuits"
else
  failed=$((failed + 1)); echo "FAIL skip-flag-short-circuits: exit=$got_exit out=$out"
fi
rm -f "$tmp_skip"

# Summary.
total=$((pass + failed))
echo "----"
echo "${pass}/${total} passed"
if [ "$failed" -gt 0 ]; then
  exit 1
fi
