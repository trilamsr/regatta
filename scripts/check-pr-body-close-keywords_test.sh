#!/usr/bin/env bash
set -uo pipefail
GATE="bash scripts/check-pr-body-close-keywords.sh"
PASS=0
FAIL=0
run() {
  local name=$1 want=$2 body=$3
  local tmp; tmp=$(mktemp)
  printf '%s\n' "$body" > "$tmp"
  $GATE --body-file "$tmp" >/dev/null 2>&1
  local rc=$?
  if [ "$rc" -eq "$want" ]; then
    printf 'ok: %s\n' "$name"
    PASS=$((PASS + 1))
  else
    printf 'FAIL: %s want=%d got=%d\n' "$name" "$want" "$rc" >&2
    FAIL=$((FAIL + 1))
  fi
  rm -f "$tmp"
}
run 'space-separated closes #N #M fails' 1 'See closes #986 #987 #988 for details.'
run 'space-separated fixes fails' 1 'fixes #1 #2'
run 'space-separated resolves fails' 1 'Resolves #100 #200 #300'
run 'comma-separated closes passes' 0 'closes #986, closes #987, closes #988'
run 'newline-separated closes passes' 0 'closes #986
closes #987'
run 'single closes passes' 0 'closes #986'
run 'no close keyword passes' 0 'just touched docs/foo.md'
run 'closes #N inside prose passes' 0 'see PR closes #100 for the rationale'
printf -- '---\nPASS=%d FAIL=%d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
