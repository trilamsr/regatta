#!/usr/bin/env bash
# check-byte-equal-pin_test.sh - fixture cases for check-byte-equal-pin.sh.
#
# The gate fails closed when a PR body claims the target/gate/route/schema set
# is "byte-equal pre/post" AND the changed-paths diff carries none of:
#   (1) a scripts/check-*-parity.sh file (the canonical drift-gate template),
#   (2) a Go test name matching Test.*ByteEqual.* or Test.*Parity.*,
#   (3) a `<!-- byte-equal-justified: <reason> -->` operator escape in body.
#
# Closes #1031.
#
# Cases:
#   A. byte-equal claim + paths include scripts/check-*-parity.sh -> exit 0
#   B. byte-equal claim + paths include Test*ByteEqual* / Parity test -> exit 0
#   C. byte-equal claim + no parity script + no parity test         -> exit 1
#   D. no byte-equal claim in body                                  -> exit 0 (skip)
#   E. byte-equal claim + operator escape comment in body           -> exit 0
#   F. claim only inside a generic fenced quote block               -> exit 0 (skip)
set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/.." && pwd)
CHECK="$REPO_ROOT/scripts/check-byte-equal-pin.sh"
FIXTURES="$REPO_ROOT/scripts/testdata/check-byte-equal-pin"

passes=0
fails=0
failed_names=()

run_case() {
  # run_case <name> <expected-exit> <body-file> <paths-file> [grep-pattern]
  local name="$1" want_exit="$2" body="$3" paths="$4" pattern="${5:-}"
  local out got_exit
  out=$(bash "$CHECK" --body-file "$body" --changed-paths-file "$paths" 2>&1)
  got_exit=$?
  local ok=1
  if [ "$got_exit" -ne "$want_exit" ]; then
    ok=0
  elif [ -n "$pattern" ] && ! printf '%s\n' "$out" | grep -qE "$pattern"; then
    ok=0
  fi
  if [ "$ok" -eq 1 ]; then
    passes=$((passes + 1))
    echo "ok   $name (exit=$got_exit)"
  else
    fails=$((fails + 1))
    failed_names+=("$name")
    echo "FAIL $name: want exit=$want_exit got=$got_exit pattern=${pattern:-<none>}"
    printf '%s\n' "$out" | sed 's/^/  | /'
  fi
}

run_case "A-claim-with-parity-script-passes" 0 \
  "$FIXTURES/A-claim-with-parity-script.body.md" \
  "$FIXTURES/A-claim-with-parity-script.paths.txt"

run_case "B-claim-with-byteequal-test-passes" 0 \
  "$FIXTURES/B-claim-with-byteequal-test.body.md" \
  "$FIXTURES/B-claim-with-byteequal-test.paths.txt"

run_case "C-claim-without-pin-fails" 1 \
  "$FIXTURES/C-claim-without-pin.body.md" \
  "$FIXTURES/C-claim-without-pin.paths.txt" \
  "byte-equal"

run_case "D-no-claim-skips" 0 \
  "$FIXTURES/D-no-claim.body.md" \
  "$FIXTURES/D-no-claim.paths.txt"

run_case "E-claim-with-escape-passes" 0 \
  "$FIXTURES/E-claim-with-escape.body.md" \
  "$FIXTURES/E-claim-with-escape.paths.txt"

run_case "F-claim-only-in-fenced-quote-skips" 0 \
  "$FIXTURES/F-claim-in-fenced-block.body.md" \
  "$FIXTURES/F-claim-in-fenced-block.paths.txt"

echo "---"
echo "passes=$passes fails=$fails"
if [ "$fails" -ne 0 ]; then
  echo "failed: ${failed_names[*]}"
  exit 1
fi
exit 0
