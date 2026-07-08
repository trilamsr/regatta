#!/usr/bin/env bash
# gpt-pr-review_test.sh asserts the GPT-5.5 PR-review bot driver:
# soft-skips on missing OPENAI_API_KEY, builds prompt with diff + body wrapped
# in untrusted-input delimiters, derives the verdict deterministically
# from finding counts (not model output), skips diffs over a hard byte
# cap, emits a body footer with Reviewer-agent-id + Reviewer-recommendation
# tokens, and carries the six review axes including body-vs-diff drift.

set -u

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
SCRIPT="$SCRIPT_DIR/gpt-pr-review.sh"
PASS=0
FAIL=0

fail() { echo "FAIL: $1"; FAIL=$((FAIL + 1)); }
pass() { echo "ok: $1"; PASS=$((PASS + 1)); }

run_case_missing_api_key_in_pr_mode() {
  local out rc
  out=$(env -u OPENAI_API_KEY bash "$SCRIPT" --pr 1 2>&1)
  rc=$?
  if [ "$rc" -eq 0 ] && echo "$out" | grep -qiE "OPENAI_API_KEY"; then
    pass "missing OPENAI_API_KEY soft-skips in --pr mode"
  else
    fail "missing OPENAI_API_KEY should soft-skip (rc=0) in --pr mode (rc=$rc out=$out)"
  fi
}

run_case_dry_run_emits_prompt_with_diff_and_body() {
  local diff_file body_file out
  diff_file=$(mktemp); body_file=$(mktemp)
  printf 'diff --git a/foo.go b/foo.go\n+ func Bar() {}\n' > "$diff_file"
  printf '## Summary\nadds Bar\n' > "$body_file"
  out=$(OPENAI_API_KEY=test-key bash "$SCRIPT" \
    --dry-run --diff-file "$diff_file" --body-file "$body_file" 2>&1)
  if echo "$out" | grep -qE "func Bar" && echo "$out" | grep -qE "adds Bar"; then
    pass "dry-run prompt includes diff and PR body"
  else
    fail "dry-run prompt missing diff or PR body"
  fi
  rm -f "$diff_file" "$body_file"
}

run_case_prompt_wraps_untrusted_input() {
  local diff_file body_file out
  diff_file=$(mktemp); body_file=$(mktemp)
  echo "diff" > "$diff_file"; echo "body" > "$body_file"
  out=$(OPENAI_API_KEY=test-key bash "$SCRIPT" \
    --dry-run --diff-file "$diff_file" --body-file "$body_file" 2>&1)
  if echo "$out" | grep -qiE "untrusted" \
     && echo "$out" | grep -qiE "do not follow"; then
    pass "prompt wraps diff/body w/ untrusted-input delimiter + warning"
  else
    fail "prompt missing untrusted-input warning"
  fi
  rm -f "$diff_file" "$body_file"
}

run_case_prompt_includes_six_review_axes() {
  local diff_file body_file out
  diff_file=$(mktemp); body_file=$(mktemp)
  echo "diff" > "$diff_file"; echo "body" > "$body_file"
  out=$(OPENAI_API_KEY=test-key bash "$SCRIPT" \
    --dry-run --diff-file "$diff_file" --body-file "$body_file" 2>&1)
  local axes_present=1
  for kw in correctness simplif over-engineer unnecessary "WHY" body-vs-diff; do
    if ! echo "$out" | grep -qiE "$kw"; then
      fail "prompt missing axis keyword: $kw"
      axes_present=0
    fi
  done
  if [ "$axes_present" -eq 1 ]; then
    pass "prompt carries all six review axes (correctness/simplification/over-eng/unnecessary/WHY/body-vs-diff)"
  fi
  rm -f "$diff_file" "$body_file"
}

run_case_prompt_requests_summary_section() {
  local diff_file body_file out
  diff_file=$(mktemp); body_file=$(mktemp)
  echo "diff" > "$diff_file"; echo "body" > "$body_file"
  out=$(OPENAI_API_KEY=test-key bash "$SCRIPT" \
    --dry-run --diff-file "$diff_file" --body-file "$body_file" 2>&1)
  if echo "$out" | grep -qiE "## Summary" \
     && echo "$out" | grep -qiE "## Findings"; then
    pass "prompt requests Summary + Findings sections"
  else
    fail "prompt missing Summary or Findings section directive"
  fi
  rm -f "$diff_file" "$body_file"
}

run_case_render_footer_tokens() {
  local out
  out=$(OPENAI_API_KEY=test-key bash "$SCRIPT" \
    --render-footer --recommendation APPROVE --run-id 12345 2>&1)
  if echo "$out" | grep -qE "^Reviewer-agent-id: gpt-5\.5-12345$" \
     && echo "$out" | grep -qE "^Reviewer-recommendation: APPROVE$"; then
    pass "render-footer emits bare tokens"
  else
    fail "render-footer missing tokens: $out"
  fi
}

run_case_render_footer_rejects_bad_recommendation() {
  local out rc
  out=$(OPENAI_API_KEY=test-key bash "$SCRIPT" \
    --render-footer --recommendation MAYBE --run-id 1 2>&1)
  rc=$?
  if [ "$rc" -ne 0 ] && echo "$out" | grep -qiE "APPROVE|REVISE|BLOCK"; then
    pass "render-footer rejects values outside APPROVE/REVISE/BLOCK"
  else
    fail "render-footer should reject bad value (rc=$rc out=$out)"
  fi
}

run_case_derive_verdict_from_findings() {
  local out
  # Derive subcommand: read a findings block on stdin and print verdict.
  # APPROVE iff zero HIGH and zero MED. REVISE if >=1 MED, zero HIGH.
  # BLOCK if >=1 HIGH.
  out=$(printf -- '- HIGH, foo.go:1, bug, fix\n- MED, bar.go:2, smell, fix\n' \
    | bash "$SCRIPT" --derive-verdict 2>&1)
  if [ "$out" = "BLOCK" ]; then
    pass "derive-verdict: HIGH present -> BLOCK"
  else
    fail "derive-verdict on HIGH should print BLOCK, got: $out"
  fi
  out=$(printf -- '- MED, bar.go:2, smell, fix\n' \
    | bash "$SCRIPT" --derive-verdict 2>&1)
  if [ "$out" = "REVISE" ]; then
    pass "derive-verdict: only MED -> REVISE"
  else
    fail "derive-verdict on MED-only should print REVISE, got: $out"
  fi
  out=$(printf -- 'nothing actionable\n' | bash "$SCRIPT" --derive-verdict 2>&1)
  if [ "$out" = "APPROVE" ]; then
    pass "derive-verdict: no findings -> APPROVE"
  else
    fail "derive-verdict on empty should print APPROVE, got: $out"
  fi
}

run_case_diff_too_large_skips() {
  local diff_file body_file out
  diff_file=$(mktemp); body_file=$(mktemp)
  # Generate ~250KB of diff to exceed default 200KB cap.
  yes "+ this is a long line to exhaust the cap" | head -n 6000 > "$diff_file"
  echo "body" > "$body_file"
  out=$(OPENAI_API_KEY=test-key bash "$SCRIPT" \
    --dry-run --diff-file "$diff_file" --body-file "$body_file" \
    --max-diff-bytes 200000 2>&1)
  if echo "$out" | grep -qiE "diff (too )?(large|exceeds)" \
     || echo "$out" | grep -qiE "skipped"; then
    pass "diff exceeding cap is skipped with notice"
  else
    fail "diff over cap should be skipped: $out"
  fi
  rm -f "$diff_file" "$body_file"
}

run_case_derive_verdict_ignores_prose_severity() {
  local out
  # Prose mentioning HIGH/MED must NOT vote — only anchored bullet
  # lines do. Closes independent-reviewer HIGH (substring false-positive).
  out=$(printf 'NO HIGH issues found.\nHIGHEST priority unrelated.\nMED-term plan is fine.\n' \
    | bash "$SCRIPT" --derive-verdict 2>&1)
  if [ "$out" = "APPROVE" ]; then
    pass "derive-verdict ignores prose mentions of HIGH/MED"
  else
    fail "derive-verdict false-positive on prose, got: $out"
  fi
}

run_case_missing_api_key_in_pr_mode
run_case_dry_run_emits_prompt_with_diff_and_body
run_case_prompt_wraps_untrusted_input
run_case_prompt_includes_six_review_axes
run_case_prompt_requests_summary_section
run_case_render_footer_tokens
run_case_render_footer_rejects_bad_recommendation
run_case_derive_verdict_from_findings
run_case_derive_verdict_ignores_prose_severity
run_case_diff_too_large_skips

echo
echo "passed: $PASS  failed: $FAIL"
[ "$FAIL" -eq 0 ]
