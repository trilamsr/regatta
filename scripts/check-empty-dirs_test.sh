#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check="$script_dir/check-empty-dirs.sh"

failed=0
pass=0

run_case() {
  local name="$1"; shift
  local want_exit="$1"; shift
  local want_stdout_match="$1"; shift
  local setup="$1"; shift

  tmp=$(mktemp -d)
  pushd "$tmp" >/dev/null

  "$setup" "$tmp"

  out=$(EMPTY_DIR_ROOT="$tmp" "$check" 2>&1) && got_exit=0 || got_exit=$?

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

setup_dir_with_trigger() {
  local root="$1"
  mkdir -p "$root/internal/foo"
  cat >"$root/internal/foo/README.md" <<'MD'
# internal/foo/
Activation trigger: when bar lands.
MD
}

setup_dir_no_trigger() {
  local root="$1"
  mkdir -p "$root/internal/foo"
  cat >"$root/internal/foo/README.md" <<'MD'
# internal/foo/
This package will do something someday.
MD
}

setup_dir_with_code() {
  local root="$1"
  mkdir -p "$root/internal/foo"
  cat >"$root/internal/foo/README.md" <<'MD'
# internal/foo/
No trigger needed - dir has actual code.
MD
  cat >"$root/internal/foo/foo.go" <<'GO'
package foo
GO
}

setup_gitkeep_only() {
  local root="$1"
  mkdir -p "$root/internal/foo"
  touch "$root/internal/foo/.gitkeep"
}

run_case "empty dir with activation trigger" 0 ""                    setup_dir_with_trigger
run_case "empty dir missing trigger"         1 "Activation trigger"  setup_dir_no_trigger
run_case "dir with code, no trigger needed"  0 ""                    setup_dir_with_code
run_case ".gitkeep only, no README"          1 "README.md"           setup_gitkeep_only

echo
echo "$pass passed, $failed failed"
exit "$failed"
