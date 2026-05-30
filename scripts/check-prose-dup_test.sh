#!/usr/bin/env bash
# check-prose-dup_test.sh - black-box tests for check-prose-dup.sh.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check="$script_dir/check-prose-dup.sh"

failed=0
pass=0

run_case() {
  local name="$1"; shift
  local want_exit="$1"; shift
  local want_stdout_match="$1"; shift
  local setup="$1"; shift

  tmp=$(mktemp -d)
  pushd "$tmp" >/dev/null

  mkdir -p docs
  "$setup" "$tmp"

  out=$(PROSE_DUP_ROOT="$tmp" "$check" 2>&1) && got_exit=0 || got_exit=$?

  if [ "$got_exit" != "$want_exit" ]; then
    echo "FAIL $name: exit=$got_exit want $want_exit"
    echo "  output: $out"
    failed=$((failed + 1))
  elif [ -n "$want_stdout_match" ] && ! echo "$out" | grep -q "$want_stdout_match"; then
    echo "FAIL $name: stdout missing '$want_stdout_match'"
    echo "  output: $out"
    failed=$((failed + 1))
  else
    pass=$((pass + 1))
    echo "PASS $name"
  fi

  popd >/dev/null
  rm -rf "$tmp"
}

setup_clean() {
  local root="$1"
  cat >"$root/docs/a.md" <<'MD'
# A
First doc. Talks about cats.
MD
  cat >"$root/docs/b.md" <<'MD'
# B
Different doc. Talks about dogs.
MD
}

setup_phrase_dup() {
  local root="$1"
  cat >"$root/docs/a.md" <<'MD'
# A
Promotion criteria (concurrency 1 -> 2): >=20 PRs merged in the lane.
MD
  cat >"$root/docs/b.md" <<'MD'
# B
Promotion criteria (concurrency 1 -> 2): >=20 PRs merged in the lane.
MD
}

setup_link_instead() {
  local root="$1"
  cat >"$root/docs/a.md" <<'MD'
# A
Promotion criteria (concurrency 1 -> 2): >=20 PRs merged in the lane.
MD
  cat >"$root/docs/b.md" <<'MD'
# B
See [promotion criteria](a.md) for thresholds.
MD
}

setup_seed_only_in_one() {
  local root="$1"
  cat >"$root/docs/a.md" <<'MD'
# A
Promotion criteria (concurrency 1 -> 2): >=20 PRs merged in the lane.
MD
}

run_case "no duplicate phrases"     0 ""                setup_clean
run_case "duplicated phrase"        1 "duplicate"       setup_phrase_dup
run_case "linked, not duplicated"   0 ""                setup_link_instead
run_case "single occurrence only"   0 ""                setup_seed_only_in_one

echo
echo "$pass passed, $failed failed"
exit "$failed"
