#!/usr/bin/env bash
# check-ci-shape_test.sh - black-box tests for check-ci-shape.sh.
#
# Each case stages a fake .github/workflows tree, runs the validator
# against it, asserts exit + stdout substring.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
check="$script_dir/check-ci-shape.sh"

failed=0
pass=0

run_case() {
  local name="$1"; shift
  local want_exit="$1"; shift
  local want_stdout_match="$1"; shift
  local setup="$1"; shift

  tmp=$(mktemp -d)
  mkdir -p "$tmp/.github/workflows"
  pushd "$tmp" >/dev/null

  "$setup" "$tmp/.github/workflows"

  out=$(CI_WORKFLOWS_DIR="$tmp/.github/workflows" "$check" 2>&1) && got_exit=0 || got_exit=$?

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

setup_empty() {
  :
}

setup_gates_missing() {
  local dir="$1"
  cat >"$dir/ci.yml" <<'YAML'
name: CI
on: [pull_request]
jobs:
  verify: { runs-on: ubuntu-latest, steps: [{ run: echo ok }] }
YAML
}

setup_gates_no_always() {
  local dir="$1"
  cat >"$dir/gates.yml" <<'YAML'
name: gates
on: [pull_request]
jobs:
  aggregate:
    needs: [verify, lint]
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
YAML
}

setup_gates_no_result_check() {
  local dir="$1"
  cat >"$dir/gates.yml" <<'YAML'
name: gates
on: [pull_request]
jobs:
  aggregate:
    needs: [verify, lint]
    if: always()
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
YAML
}

setup_release_missing() {
  local dir="$1"
  cat >"$dir/gates.yml" <<'YAML'
name: gates
on: [pull_request]
jobs:
  aggregate:
    needs: [verify, lint]
    if: always()
    runs-on: ubuntu-latest
    steps:
      - name: fail if any needed job did not succeed
        run: |
          if [ "${{ needs.verify.result }}" != "success" ] || \
             [ "${{ needs.lint.result }}" != "success" ]; then
            exit 1
          fi
YAML
}

setup_release_no_tag_trigger() {
  local dir="$1"
  setup_release_missing "$dir"
  cat >"$dir/release.yml" <<'YAML'
name: release
on: [workflow_dispatch]
jobs:
  release:
    runs-on: ubuntu-latest
    steps: [{ run: echo ok }]
YAML
}

setup_release_no_provenance() {
  local dir="$1"
  setup_release_missing "$dir"
  cat >"$dir/release.yml" <<'YAML'
name: release
on:
  push:
    tags: ['v*']
jobs:
  release:
    runs-on: ubuntu-latest
    steps: [{ run: echo ok }]
YAML
}

setup_release_no_changelog_flip() {
  local dir="$1"
  setup_release_missing "$dir"
  cat >"$dir/release.yml" <<'YAML'
name: release
on:
  push:
    tags: ['v*']
permissions:
  id-token: write
  contents: write
  attestations: write
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/attest-build-provenance@main
YAML
}

setup_ok() {
  local dir="$1"
  cat >"$dir/gates.yml" <<'YAML'
name: gates
on: [pull_request]
jobs:
  aggregate:
    needs: [verify, lint]
    if: always()
    runs-on: ubuntu-latest
    steps:
      - name: fail if any needed job did not succeed
        run: |
          if [ "${{ needs.verify.result }}" != "success" ] || \
             [ "${{ needs.lint.result }}" != "success" ]; then
            exit 1
          fi
YAML
  cat >"$dir/release.yml" <<'YAML'
name: release
on:
  push:
    tags: ['v*']
permissions:
  id-token: write
  contents: write
  attestations: write
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/attest-build-provenance@main
      - name: flip CHANGELOG Unreleased to version
        run: scripts/changelog-gen.sh
YAML
}

run_case "no gates.yml present"          1 "gates.yml"        setup_empty
run_case "gates.yml absent, other yml"   1 "gates.yml"        setup_gates_missing
run_case "gates.yml lacks if: always()"  1 "if: always()"     setup_gates_no_always
run_case "gates.yml lacks .result check" 1 ".result"          setup_gates_no_result_check
run_case "release.yml missing"           1 "release.yml"      setup_release_missing
run_case "release.yml not tag-triggered" 1 "tags"             setup_release_no_tag_trigger
run_case "release.yml no provenance"     1 "provenance"       setup_release_no_provenance
run_case "release.yml no CHANGELOG flip" 1 "changelog"        setup_release_no_changelog_flip
run_case "well-formed pair"              0 ""                 setup_ok

echo
echo "$pass passed, $failed failed"
exit "$failed"
