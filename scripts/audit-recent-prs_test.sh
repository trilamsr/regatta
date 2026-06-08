#!/usr/bin/env bash
# audit-recent-prs_test.sh asserts the post-merge subagent-quality audit
# flags malformed `Reviewer-recommendation` tokens and phantom-dep cites
# in fixture PR bodies (closes #1033).

set -u
# pipefail intentionally omitted — tests pipe the gate's stderr into grep
# and assert exit-code combinations independently.

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
GATE="$SCRIPT_DIR/audit-recent-prs.sh"
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

write_body() {
  cat > "$1"
}

run_case_clean_body_silent() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Bumps internal/foo dep.

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] foo
```
EOF
  local out
  out=$("$GATE" --audit-body-file "$body" --pr 1234 2>&1)
  local rc=$?
  if [ "$rc" -eq 0 ] && ! echo "$out" | grep -qE "malformed|phantom"; then
    pass "clean body emits no findings"
  else
    fail "clean body should be silent; got rc=$rc, out=$out"
  fi
  rm -f "$body"
}

run_case_malformed_token_flagged() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Reviewer-recommendation: approve with caveats

```release-notes
[FEAT] foo
```
EOF
  if "$GATE" --audit-body-file "$body" --pr 1234 2>&1 | grep -qE "malformed.*Reviewer-recommendation"; then
    pass "malformed token 'approve with caveats' flagged"
  else
    fail "malformed token should be flagged"
  fi
  rm -f "$body"
}

run_case_lowercase_approve_flagged() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
Reviewer-recommendation: approve

```release-notes
[FEAT] foo
```
EOF
  if "$GATE" --audit-body-file "$body" --pr 1234 2>&1 | grep -qE "malformed.*Reviewer-recommendation"; then
    pass "lowercase 'approve' flagged (must be exact APPROVE/REVISE/BLOCK)"
  else
    fail "lowercase token should be flagged"
  fi
  rm -f "$body"
}

run_case_extra_text_flagged() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
Reviewer-recommendation: APPROVE -- ship it

```release-notes
[FEAT] foo
```
EOF
  if "$GATE" --audit-body-file "$body" --pr 1234 2>&1 | grep -qE "malformed.*Reviewer-recommendation"; then
    pass "trailing prose after APPROVE flagged"
  else
    fail "trailing prose should be flagged"
  fi
  rm -f "$body"
}

run_case_bare_approve_passes() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] foo
```
EOF
  if "$GATE" --audit-body-file "$body" --pr 1234 2>&1 | grep -qE "malformed.*Reviewer-recommendation"; then
    fail "bare APPROVE should NOT be flagged"
  else
    pass "bare APPROVE token passes"
  fi
  rm -f "$body"
}

run_case_bare_revise_passes() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
Reviewer-recommendation: REVISE

```release-notes
[FEAT] foo
```
EOF
  if "$GATE" --audit-body-file "$body" --pr 1234 2>&1 | grep -qE "malformed.*Reviewer-recommendation"; then
    fail "bare REVISE should NOT be flagged"
  else
    pass "bare REVISE token passes"
  fi
  rm -f "$body"
}

run_case_phantom_dep_flagged() {
  local body refs_file
  body=$(mktemp)
  refs_file=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Closes #999999 and depends on #888888.

```release-notes
[FEAT] foo
```
EOF
  # known-refs file lists ONLY real PR/issue numbers. #999999 is absent → phantom.
  printf '100\n200\n300\n' > "$refs_file"
  if "$GATE" --audit-body-file "$body" --pr 1234 --known-refs-file "$refs_file" 2>&1 | grep -qE "phantom.*#999999"; then
    pass "phantom #999999 dep flagged when known-refs lacks it"
  else
    fail "phantom dep #999999 should be flagged"
  fi
  rm -f "$body" "$refs_file"
}

run_case_real_dep_not_flagged() {
  local body refs_file
  body=$(mktemp)
  refs_file=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Closes #100.

```release-notes
[FEAT] foo
```
EOF
  printf '100\n200\n' > "$refs_file"
  if "$GATE" --audit-body-file "$body" --pr 1234 --known-refs-file "$refs_file" 2>&1 | grep -qE "phantom"; then
    fail "real #100 dep should NOT be flagged"
  else
    pass "real #100 dep passes phantom check"
  fi
  rm -f "$body" "$refs_file"
}

run_case_phantom_check_skipped_without_refs_file() {
  # When --known-refs-file is absent the phantom check is skipped (cannot
  # verify without an authoritative ref list). Audit must remain useful for
  # the malformed-token check alone.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Closes #999999.

Reviewer-recommendation: APPROVE

```release-notes
[FEAT] foo
```
EOF
  if "$GATE" --audit-body-file "$body" --pr 1234 2>&1 | grep -qE "phantom"; then
    fail "phantom check should skip when --known-refs-file absent"
  else
    pass "phantom check skipped silently without --known-refs-file"
  fi
  rm -f "$body"
}

run_case_missing_token_flagged() {
  # No Reviewer-recommendation line at all → audit notes the gap.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Bumps a dep.

```release-notes
[FEAT] foo
```
EOF
  if "$GATE" --audit-body-file "$body" --pr 1234 --require-token 2>&1 | grep -qE "missing.*Reviewer-recommendation"; then
    pass "missing token flagged when --require-token set"
  else
    fail "missing token should be flagged with --require-token"
  fi
  rm -f "$body"
}

run_case_missing_token_default_silent() {
  # By default the audit does NOT require a token (handles CHORE/DOCS PRs
  # that legitimately skip review). --require-token opts in.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Bumps a dep.

```release-notes
[CHORE] dep bump
```
EOF
  local out
  out=$("$GATE" --audit-body-file "$body" --pr 1234 2>&1)
  if echo "$out" | grep -qE "missing|malformed"; then
    fail "missing token default-silent; got: $out"
  else
    pass "missing token default-silent (no --require-token)"
  fi
  rm -f "$body"
}

run_case_fenced_malformed_ignored() {
  # Tokens inside ```-fenced blocks are example/draft content; ignore them
  # (mirrors check-reviewer-verdict.sh behavior).
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Example only:

```
Reviewer-recommendation: approve sorta
```

Reviewer-recommendation: APPROVE

```release-notes
[FEAT] foo
```
EOF
  if "$GATE" --audit-body-file "$body" --pr 1234 2>&1 | grep -qE "malformed"; then
    fail "fenced malformed token should be ignored"
  else
    pass "fenced malformed token ignored; bare APPROVE wins"
  fi
  rm -f "$body"
}

run_case_clean_body_silent
run_case_malformed_token_flagged
run_case_lowercase_approve_flagged
run_case_extra_text_flagged
run_case_bare_approve_passes
run_case_bare_revise_passes
run_case_phantom_dep_flagged
run_case_real_dep_not_flagged
run_case_phantom_check_skipped_without_refs_file
run_case_missing_token_flagged
run_case_missing_token_default_silent
run_case_fenced_malformed_ignored

echo "---"
echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
