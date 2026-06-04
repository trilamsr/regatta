#!/usr/bin/env bash
# check-comment-density_test.sh - black-box fixtures for check-comment-density.sh.
#
# Each fixture builds a fake repo (init + commit base + checkout branch +
# add changes), then invokes the script with --base <ref> --body-file <pr>
# and asserts on exit code + optional stdout substring.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check="$script_dir/check-comment-density.sh"

failed=0
pass=0

new_repo() {
  local dir
  dir=$(mktemp -d)
  (
    cd "$dir"
    git init -q -b main
    git config user.email "ci@local"
    git config user.name "ci"
    git commit -q --allow-empty -m "init"
  )
  printf '%s' "$dir"
}

commit_main() {
  local dir="$1"; shift
  (
    cd "$dir"
    git add -A
    git commit -q -m "main change"
  )
}

run_case() {
  local name="$1"; shift
  local want_exit="$1"; shift
  local want_stdout_match="$1"; shift
  local repo="$1"; shift
  local body_file="$1"; shift

  out=$( cd "$repo" && "$check" --base main --body-file "$body_file" 2>&1 ) && got_exit=0 || got_exit=$?

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
  rm -rf "$repo"
}

empty_body() {
  local f
  f=$(mktemp)
  printf '```release-notes\n[FEATURE] x\n```\n## Summary\n' > "$f"
  printf '%s' "$f"
}

allowlisted_body() {
  local f
  f=$(mktemp)
  printf '```release-notes\n[FEATURE] x\n```\n## Summary\n<!-- comment-density-justified: heavy comments needed for state machine doc -->\n' > "$f"
  printf '%s' "$f"
}

# Case 1: clean — new prod file with 3% density → pass.
repo1=$(new_repo)
(
  cd "$repo1"
  git checkout -q -b feat/clean
  mkdir -p internal/clean
  cat >internal/clean/clean.go <<'EOF'
package clean

func A() int { return 1 }
func B() int { return 2 }
func C() int { return 3 }
func D() int { return 4 }
func E() int { return 5 }
func F() int { return 6 }
func G() int { return 7 }
func H() int { return 8 }
func I() int { return 9 }
func J() int { return 10 }
func K() int { return 11 }
func L() int { return 12 }
func M() int { return 13 }
func N() int { return 14 }
func O() int { return 15 }
func P() int { return 16 }
func Q() int { return 17 }
func R() int { return 18 }
func S() int { return 19 }
func T() int { return 20 }
func U() int { return 21 }
func V() int { return 22 }
func W() int { return 23 }
func X() int { return 24 }
func Y() int { return 25 }
func Z() int { return 26 }
func AA() int { return 27 }
func AB() int { return 28 }
func AC() int { return 29 }
// WHY: anchor sentinel.
func AD() int { return 30 }
EOF
  git add -A
  git commit -q -m "add clean.go"
)
body1=$(empty_body)
run_case "clean-3pct-passes" 0 "ok" "$repo1" "$body1"
rm -f "$body1"

# Case 2: dense — new prod file with 15% density → fail.
repo2=$(new_repo)
(
  cd "$repo2"
  git checkout -q -b feat/dense
  mkdir -p internal/dense
  cat >internal/dense/dense.go <<'EOF'
package dense

// A returns 1.
func A() int { return 1 }
// B returns 2.
func B() int { return 2 }
// C returns 3.
func C() int { return 3 }
// D returns 4.
func D() int { return 4 }
// E returns 5.
func E() int { return 5 }
func F() int { return 6 }
func G() int { return 7 }
func H() int { return 8 }
func I() int { return 9 }
func J() int { return 10 }
func K() int { return 11 }
func L() int { return 12 }
func M() int { return 13 }
func N() int { return 14 }
func O() int { return 15 }
EOF
  git add -A
  git commit -q -m "add dense.go"
)
body2=$(empty_body)
run_case "dense-15pct-fails" 1 "comment-density" "$repo2" "$body2"
rm -f "$body2"

# Case 3: dense file but PR body has allowlist marker → pass.
repo3=$(new_repo)
(
  cd "$repo3"
  git checkout -q -b feat/dense-allow
  mkdir -p internal/dense
  cat >internal/dense/dense.go <<'EOF'
package dense

// A returns 1.
func A() int { return 1 }
// B returns 2.
func B() int { return 2 }
// C returns 3.
func C() int { return 3 }
// D returns 4.
func D() int { return 4 }
// E returns 5.
func E() int { return 5 }
func F() int { return 6 }
func G() int { return 7 }
func H() int { return 8 }
func I() int { return 9 }
func J() int { return 10 }
func K() int { return 11 }
func L() int { return 12 }
func M() int { return 13 }
func N() int { return 14 }
func O() int { return 15 }
EOF
  git add -A
  git commit -q -m "add dense.go"
)
body3=$(allowlisted_body)
run_case "dense-allowlisted-passes" 0 "justified" "$repo3" "$body3"
rm -f "$body3"

# Case 4: test files are excluded — dense _test.go does NOT fail.
repo4=$(new_repo)
(
  cd "$repo4"
  git checkout -q -b feat/test-dense
  mkdir -p internal/t
  cat >internal/t/t_test.go <<'EOF'
package t

// TestA asserts.
import "testing"

// TestA asserts a.
func TestA(t *testing.T) {}
// TestB asserts b.
func TestB(t *testing.T) {}
// TestC asserts c.
func TestC(t *testing.T) {}
EOF
  git add -A
  git commit -q -m "add t_test.go"
)
body4=$(empty_body)
run_case "test-file-excluded-passes" 0 "ok" "$repo4" "$body4"
rm -f "$body4"

# Case 5: no .go changes at all → pass.
repo5=$(new_repo)
(
  cd "$repo5"
  git checkout -q -b docs/only
  mkdir -p docs
  printf 'hello\n' >docs/x.md
  git add -A
  git commit -q -m "doc add"
)
body5=$(empty_body)
run_case "no-go-changes-passes" 0 "ok" "$repo5" "$body5"
rm -f "$body5"

# Case 6: pre-existing dense file (added 10% more density to file that
# existed on main) → warn (exit 0, stdout has WARN).
repo6=$(new_repo)
(
  cd "$repo6"
  mkdir -p internal/old
  cat >internal/old/old.go <<'EOF'
package old

func A() int { return 1 }
func B() int { return 2 }
func C() int { return 3 }
func D() int { return 4 }
func E() int { return 5 }
EOF
  git add -A
  git commit -q -m "base"
  git checkout -q -b feat/extend
  cat >>internal/old/old.go <<'EOF'
// F returns 6.
func F() int { return 6 }
EOF
  git add -A
  git commit -q -m "extend"
)
body6=$(empty_body)
run_case "existing-extend-warns-but-passes" 0 "warn\\|ok" "$repo6" "$body6"
rm -f "$body6"

# Summary.
total=$((pass + failed))
echo "----"
echo "${pass}/${total} passed"
if [ "$failed" -gt 0 ]; then
  exit 1
fi
