#!/usr/bin/env bash
# check-tdd-redfirst_test.sh - black-box tests for check-tdd-redfirst.sh.
#
# Each case builds a throwaway git repo with a base commit on origin/main,
# then a feature branch whose COMMIT ORDERING is the thing under test. A
# setup function receives no args and stages/commits the branch itself, so
# cases control how many commits exist and in what order. Invokes the gate
# with BASE_SHA / HEAD_SHA / BODY env vars; asserts exit code + stdout/stderr
# substring.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check="$script_dir/check-tdd-redfirst.sh"

failed=0
pass=0

# Stage the helpers each case reuses to write the canonical foo.go / foo_test.go.
write_prod() {
  mkdir -p internal/foo
  cat > internal/foo/foo.go <<EOF
package foo

func Foo() int { return 42 }
EOF
}
write_test() {
  mkdir -p internal/foo
  cat > internal/foo/foo_test.go <<EOF
package foo

import "testing"

func TestFoo(t *testing.T) {
  if Foo() != 42 { t.Fatal("nope") }
}
EOF
}

run_case() {
  local name="$1"; shift
  local want_exit="$1"; shift
  local want_match="$1"; shift  # may be empty
  local setup="$1"; shift       # bash function name; builds the branch
  local body="${1:-}"

  tmp=$(mktemp -d)
  pushd "$tmp" >/dev/null

  git init -q -b main
  git config user.email t@t
  git config user.name t

  cat > go.mod <<EOF
module example.com/x

go 1.22
EOF
  git add go.mod
  git commit -q -m base
  base=$(git rev-parse HEAD)
  git update-ref refs/remotes/origin/main HEAD
  git checkout -q -b feature

  "$setup"
  head=$(git rev-parse HEAD)

  out=$(BASE_SHA="$base" HEAD_SHA="$head" BODY="$body" "$check" 2>&1) && got_exit=0 || got_exit=$?

  if [ "$got_exit" != "$want_exit" ]; then
    echo "FAIL $name: exit=$got_exit want $want_exit"
    echo "  output: $out"
    failed=$((failed + 1))
  elif [ -n "$want_match" ] && ! echo "$out" | grep -q "$want_match"; then
    echo "FAIL $name: output missing '$want_match'"
    echo "  output: $out"
    failed=$((failed + 1))
  else
    pass=$((pass + 1))
    echo "PASS $name"
  fi

  popd >/dev/null
  rm -rf "$tmp"
}

# RED: test+impl in ONE commit, no justification -> gate FAILS.
setup_single_commit_no_justify() {
  write_test
  write_prod
  git add -A
  git commit -q -m "feat: foo with test"
}

# GREEN: test commit lands BEFORE impl commit -> gate passes.
setup_test_before_impl() {
  write_test
  git add -A
  git commit -q -m "test: failing TestFoo"
  write_prod
  git add -A
  git commit -q -m "feat: Foo impl"
}

# RED-as-pass: test+impl in ONE commit WITH justification escape -> passes.
setup_single_commit_justified() {
  setup_single_commit_no_justify
}

# GREEN: only a test file added (no prod) -> out of scope, passes.
setup_test_only() {
  write_test
  git add -A
  git commit -q -m "test: standalone"
}

# GREEN: only prod added (no co-located test) -> out of scope here; the
# co-located-test PRESENCE gate (check-tdd.sh) owns that failure mode.
setup_prod_only() {
  write_prod
  git add -A
  git commit -q -m "feat: Foo impl"
}

# RED: impl commit lands BEFORE the test commit (impl-first) -> FAILS.
setup_impl_before_test() {
  write_prod
  git add -A
  git commit -q -m "feat: Foo impl"
  write_test
  git add -A
  git commit -q -m "test: TestFoo after the fact"
}

# GREEN: editing a pre-existing prod file (not a new add) ships a new test
# in one commit -> gate is add-scoped, so no red-first claim, passes.
setup_edit_existing_prod() {
  # Land foo.go on origin/main first, then branch.
  write_prod
  git add -A
  git commit -q -m "seed foo.go"
  git update-ref refs/remotes/origin/main HEAD
  git checkout -q -b feature2
  # Now edit foo.go AND add the test in one commit.
  cat > internal/foo/foo.go <<EOF
package foo

func Foo() int { return 43 }
EOF
  write_test
  git add -A
  git commit -q -m "change: bump + test"
}

# Special harness for the edit-existing case: it re-seeds origin/main, so
# it cannot reuse run_case's base. Drive the gate directly.
run_edit_existing_case() {
  local name="edit_existing_prod_out_of_scope"
  tmp=$(mktemp -d)
  pushd "$tmp" >/dev/null
  git init -q -b main
  git config user.email t@t
  git config user.name t
  cat > go.mod <<EOF
module example.com/x

go 1.22
EOF
  git add go.mod
  git commit -q -m base
  setup_edit_existing_prod
  head=$(git rev-parse HEAD)
  out=$(HEAD_SHA="$head" BODY="" "$check" 2>&1) && got_exit=0 || got_exit=$?
  if [ "$got_exit" != 0 ]; then
    echo "FAIL $name: exit=$got_exit want 0"
    echo "  output: $out"
    failed=$((failed + 1))
  elif ! echo "$out" | grep -q "out of scope"; then
    echo "FAIL $name: output missing 'out of scope'"
    echo "  output: $out"
    failed=$((failed + 1))
  else
    pass=$((pass + 1))
    echo "PASS $name"
  fi
  popd >/dev/null
  rm -rf "$tmp"
}

run_case single_commit_no_justify_fails 1 "ONE commit"        setup_single_commit_no_justify
run_case impl_before_test_fails         1 "added BEFORE"      setup_impl_before_test
run_case test_before_impl_passes        0 "test-first"        setup_test_before_impl
run_case single_commit_justified_passes 0 "escape present"    setup_single_commit_justified \
  '<!-- tdd-single-commit-justified: fixture+script land together -->'
run_case test_only_passes               0 "out of scope"      setup_test_only
run_case prod_only_passes               0 "out of scope"      setup_prod_only
run_edit_existing_case

echo
echo "summary: $pass passed, $failed failed"
exit $failed
