#!/usr/bin/env bash
# check-mock-vs-real_test.sh - black-box fixtures for check-mock-vs-real.sh.
#
# Each fixture builds a fake repo, invokes the script with --base main
# --body-file <pr>, asserts exit code + stdout substring. Gate is WARN-only:
# every fixture must exit 0; assertions read stdout for ok|warn|justified.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check="$script_dir/check-mock-vs-real.sh"

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
  elif [ -n "$want_stdout_match" ] && ! printf '%s\n' "$out" | grep -qE "$want_stdout_match"; then
    echo "FAIL $name: stdout missing pattern '$want_stdout_match'"
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
  printf '## Summary\n<!-- mock-ratio-justified: legacy fake harness pending migration -->\n' > "$f"
  printf '%s' "$f"
}

# Case 1: clean — new test file uses only real infra → ok (zero mocks).
repo1=$(new_repo)
(
  cd "$repo1"
  git checkout -q -b feat/real-only
  mkdir -p internal/clean
  cat >internal/clean/clean_test.go <<'EOF'
package clean

import (
	"net/http/httptest"
	"testing"
)

func TestA(t *testing.T) {
	dir := t.TempDir()
	_ = dir
	srv := httptest.NewServer(nil)
	defer srv.Close()
}
EOF
  git add -A
  git commit -q -m "add real-only test"
)
body1=$(empty_body)
run_case "clean-zero-mocks-ok" 0 "ok" "$repo1" "$body1"
rm -f "$body1"

# Case 2: high-mock — new test file heavy on Mock identifiers → WARN.
repo2=$(new_repo)
(
  cd "$repo2"
  git checkout -q -b feat/mock-heavy
  mkdir -p internal/heavy
  cat >internal/heavy/heavy_test.go <<'EOF'
package heavy

import "testing"

type MockStore struct{}
type MockClient struct{}
type MockReaper struct{}

func TestA(t *testing.T) {
	m := MockStore{}
	c := MockClient{}
	r := MockReaper{}
	_ = m
	_ = c
	_ = r
	// gomock fixture wiring below.
	// testify/mock not used here; bare identifier matches still trip.
	var _ MockStore
	var _ MockClient
}
EOF
  git add -A
  git commit -q -m "add mock-heavy test"
)
body2=$(empty_body)
run_case "high-mock-warns" 0 "warning|WARN" "$repo2" "$body2"
rm -f "$body2"

# Case 3: high-mock but PR body has allowlist marker → ok (justified).
repo3=$(new_repo)
(
  cd "$repo3"
  git checkout -q -b feat/mock-heavy-justified
  mkdir -p internal/heavy
  cat >internal/heavy/heavy_test.go <<'EOF'
package heavy

import "testing"

type MockStore struct{}
type MockClient struct{}
type MockReaper struct{}

func TestA(t *testing.T) {
	_ = MockStore{}
	_ = MockClient{}
	_ = MockReaper{}
}
EOF
  git add -A
  git commit -q -m "add mock-heavy test"
)
body3=$(allowlisted_body)
run_case "high-mock-allowlisted-ok" 0 "justified" "$repo3" "$body3"
rm -f "$body3"

# Case 4: no new test files at all → ok.
repo4=$(new_repo)
(
  cd "$repo4"
  git checkout -q -b docs/only
  mkdir -p docs
  printf 'hello\n' >docs/x.md
  git add -A
  git commit -q -m "doc add"
)
body4=$(empty_body)
run_case "no-test-files-ok" 0 "ok" "$repo4" "$body4"
rm -f "$body4"

# Summary.
total=$((pass + failed))
echo "----"
echo "${pass}/${total} passed"
if [ "$failed" -gt 0 ]; then
  exit 1
fi
