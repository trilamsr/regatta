#!/usr/bin/env bash
# check-prompt-parity_test.sh asserts the prompt-parity gate fails closed
# on slug drift and passes on aligned state.

set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
GATE="$SCRIPT_DIR/check-prompt-parity.sh"
PASS=0
FAIL=0

fail() {
  echo "FAIL: $1"
  FAIL=$((FAIL + 1))
}

pass() {
  echo "ok: $1"
  PASS=$((PASS + 1))
}

mktmp() {
  mktemp -d -t prompt-parity-test.XXXXXX
}

run_case_missing_slug() {
  local tmp prompt_src tmpl
  tmp=$(mktmp)
  prompt_src="$tmp/claude.go"
  tmpl="$tmp/implementer.md"
  cat > "$prompt_src" <<'EOF'
// defaultPromptBuilder cites feedback_tdd_discipline only.
EOF
  cat > "$tmpl" <<'EOF'
## Anchored rules (worker-prompt parity)

- `feedback_tdd_discipline`
- `feedback_comments_discipline`
EOF
  if PROMPT_SRC="$prompt_src" TEMPLATE_SRC="$tmpl" "$GATE" >/dev/null 2>&1; then
    fail "missing slug should exit non-zero"
  else
    pass "missing slug exits non-zero"
  fi
  rm -rf "$tmp"
}

run_case_aligned_ok() {
  local tmp prompt_src tmpl
  tmp=$(mktmp)
  prompt_src="$tmp/claude.go"
  tmpl="$tmp/implementer.md"
  cat > "$prompt_src" <<'EOF'
// cites feedback_tdd_discipline + feedback_comments_discipline.
EOF
  cat > "$tmpl" <<'EOF'
## Anchored rules (worker-prompt parity)

- `feedback_tdd_discipline`
- `feedback_comments_discipline`
EOF
  if PROMPT_SRC="$prompt_src" TEMPLATE_SRC="$tmpl" "$GATE" >/dev/null 2>&1; then
    pass "aligned slug set exits 0"
  else
    fail "aligned slug set should exit 0"
  fi
  rm -rf "$tmp"
}

run_case_escape_hatch() {
  local tmp prompt_src tmpl
  tmp=$(mktmp)
  prompt_src="$tmp/claude.go"
  tmpl="$tmp/implementer.md"
  cat > "$prompt_src" <<'EOF'
// cites feedback_tdd_discipline only.
EOF
  cat > "$tmpl" <<'EOF'
## Anchored rules (worker-prompt parity)

- `feedback_tdd_discipline`
- `feedback_comments_discipline` <!-- prompt-parity-skip: reviewer-only context, not worker-actionable -->
EOF
  if PROMPT_SRC="$prompt_src" TEMPLATE_SRC="$tmpl" "$GATE" >/dev/null 2>&1; then
    pass "escape-hatched slug exits 0"
  else
    fail "escape-hatched slug should exit 0"
  fi
  rm -rf "$tmp"
}

run_case_anchored_section_missing() {
  local tmp prompt_src tmpl
  tmp=$(mktmp)
  prompt_src="$tmp/claude.go"
  tmpl="$tmp/implementer.md"
  cat > "$prompt_src" <<'EOF'
// cites feedback_tdd_discipline.
EOF
  cat > "$tmpl" <<'EOF'
## Other section

- `feedback_tdd_discipline`
EOF
  if PROMPT_SRC="$prompt_src" TEMPLATE_SRC="$tmpl" "$GATE" >/dev/null 2>&1; then
    fail "missing Anchored-rules section should exit non-zero"
  else
    pass "missing Anchored-rules section exits non-zero"
  fi
  rm -rf "$tmp"
}

run_case_missing_slug
run_case_aligned_ok
run_case_escape_hatch
run_case_anchored_section_missing

echo "---"
echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
