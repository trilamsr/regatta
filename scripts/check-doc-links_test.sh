#!/usr/bin/env bash
# Smoke test for scripts/check-doc-links.sh.

set -uo pipefail

ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$ROOT"

GATE="bash scripts/check-doc-links.sh"
PASS=0
FAIL=0

run_case() {
  local name=$1
  local expect_rc=$2
  local file_content=$3

  local tmpfile="docs/engineer/briefs/TEST-check-doc-links-$$.md"
  printf '%s\n' "$file_content" > "$tmpfile"
  git add "$tmpfile" >/dev/null 2>&1

  $GATE >/dev/null 2>&1
  local rc=$?

  git rm -f "$tmpfile" >/dev/null 2>&1
  rm -f "$tmpfile"

  if [ "$rc" -eq "$expect_rc" ]; then
    printf 'ok: %s (rc=%d)\n' "$name" "$rc"
    PASS=$((PASS + 1))
  else
    printf 'FAIL: %s want rc=%d got rc=%d\n' "$name" "$expect_rc" "$rc" >&2
    FAIL=$((FAIL + 1))
  fi
}

run_case 'broken intra-repo ref fails' 1 'See [missing](docs/engineer/specs/9999-non-existent.md) for details.'
run_case 'existing intra-repo ref passes' 0 'See [self-host](docs/engineer/briefs/2026-06-01-self-host-first.md) for details.'
run_case 'external URL skipped' 0 'See [github](https://github.com/example/repo) for details.'
run_case 'testdata ref skipped' 0 'See [fixture](scripts/testdata/9999-not-real.md) for details.'
run_case 'anchor-suffix stripped before stat' 0 'See [self-host §1](docs/engineer/briefs/2026-06-01-self-host-first.md#section-1) for details.'

printf -- '---\nPASS=%d FAIL=%d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
