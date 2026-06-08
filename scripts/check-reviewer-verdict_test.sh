#!/usr/bin/env bash
# check-reviewer-verdict_test.sh asserts the reviewer-verdict gate fails
# closed on load-bearing PRs without APPROVE recommendation per #899.

set -u
# pipefail intentionally omitted: tests assert the gate exits non-zero and pipe
# the output into grep — pipefail would propagate the gate's failure exit
# through the pipeline regardless of grep's match result.

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
GATE="$SCRIPT_DIR/check-reviewer-verdict.sh"
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

run_case_load_bearing_missing_token() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing 2>&1 | grep -qE "Reviewer-recommendation"; then
    pass "load-bearing without token fails with explicit hint"
  else
    fail "load-bearing without token should fail with explicit hint"
  fi
  rm -f "$body"
}

run_case_load_bearing_approve_passes() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing >/dev/null 2>&1; then
    pass "load-bearing + APPROVE exits 0"
  else
    fail "load-bearing + APPROVE should exit 0"
  fi
  rm -f "$body"
}

run_case_load_bearing_revise_fails() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
Reviewer-recommendation: REVISE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing 2>&1 | grep -qE "REVISE|BLOCK"; then
    pass "REVISE recommendation fails"
  else
    fail "REVISE recommendation should fail"
  fi
  rm -f "$body"
}

run_case_load_bearing_block_fails() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
Reviewer-recommendation: BLOCK

```release-notes
[FIX] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing 2>&1 | grep -qE "REVISE|BLOCK"; then
    pass "BLOCK recommendation fails"
  else
    fail "BLOCK recommendation should fail"
  fi
  rm -f "$body"
}

run_case_chore_release_notes_skips() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Trivial doc tweak.

```release-notes
[CHORE] trim trailing whitespace
```
EOF
  if "$GATE" --body-file "$body" --load-bearing >/dev/null 2>&1; then
    pass "[CHORE] category auto-skips reviewer gate"
  else
    fail "[CHORE] category should auto-skip reviewer gate"
  fi
  rm -f "$body"
}

run_case_docs_release_notes_skips() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
```release-notes
[DOCS] update README
```
EOF
  if "$GATE" --body-file "$body" --load-bearing >/dev/null 2>&1; then
    pass "[DOCS] category auto-skips reviewer gate"
  else
    fail "[DOCS] category should auto-skip reviewer gate"
  fi
  rm -f "$body"
}

run_case_not_load_bearing_skips() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" >/dev/null 2>&1; then
    pass "no --load-bearing flag → skip"
  else
    fail "no --load-bearing flag should skip"
  fi
  rm -f "$body"
}

run_case_fenced_revise_bare_approve_passes() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

Example of a stale draft kept for context:

```
Reviewer-recommendation: REVISE
```

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing >/dev/null 2>&1; then
    pass "fenced REVISE + bare APPROVE → bare APPROVE wins (#922)"
  else
    fail "fenced REVISE + bare APPROVE should pass — fenced tokens must be stripped (#922)"
  fi
  rm -f "$body"
}

run_case_fenced_approve_bare_revise_fails() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

```
Reviewer-recommendation: APPROVE
```

Reviewer-recommendation: REVISE

```release-notes
[FIX] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing 2>&1 | grep -qE "REVISE|BLOCK"; then
    pass "fenced APPROVE + bare REVISE → bare REVISE wins (#922)"
  else
    fail "fenced APPROVE + bare REVISE should fail — fenced tokens must be stripped (#922)"
  fi
  rm -f "$body"
}

run_case_multiple_tokens_uses_last() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

Initial pass:
Reviewer-recommendation: REVISE

After addressing findings:
Reviewer-agent-id: cavecrew-reviewer-xyz789
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing >/dev/null 2>&1; then
    pass "stale REVISE + fresh APPROVE → last token wins (#923)"
  else
    fail "stale REVISE + fresh APPROVE should pass — last token must win (#923)"
  fi
  rm -f "$body"
}

run_case_load_bearing_path_classifier() {
  # Retro audit 2026-06-08 (#985 #986): 5 structural refactors self-tagged
  # APPROVE bypassing review because the workflow path classifier missed
  # agent-rule + CI-gate surfaces. When --changed-paths-file lists any of
  # CLAUDE.md, Makefile, Makefile.d/*, .github/workflows/*, scripts/check-*.sh,
  # docs/engineer/dispatch-templates/*, the script MUST treat the PR as
  # load-bearing AND require the Reviewer-recommendation token even when the
  # release-notes category is [DOCS]/[CHORE]/[CI]/[NONE]/[CHANGE].
  local paths
  for path in \
    "CLAUDE.md" \
    "Makefile" \
    "Makefile.d/check.mk" \
    ".github/workflows/pr-lint.yml" \
    "docs/engineer/dispatch-templates/implementer.md" \
    "scripts/check-scorecard.sh"; do
    local body paths_file
    body=$(mktemp)
    paths_file=$(mktemp)
    write_body "$body" <<EOF
## Summary

Touches $path.

\`\`\`release-notes
[CHORE] tweak $path
\`\`\`
EOF
    printf '%s\n' "$path" > "$paths_file"
    if "$GATE" --body-file "$body" --changed-paths-file "$paths_file" 2>&1 | grep -qE "Reviewer-recommendation"; then
      pass "load-bearing path classifier flags $path even with [CHORE] release-notes"
    else
      fail "load-bearing path classifier should flag $path even with [CHORE] release-notes"
    fi
    rm -f "$body" "$paths_file"
  done

  # APPROVE on a load-bearing-by-path PR still passes.
  local body paths_file
  body=$(mktemp)
  paths_file=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Touches CLAUDE.md.

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[DOCS] update CLAUDE.md
```
EOF
  printf '%s\n' "CLAUDE.md" > "$paths_file"
  if "$GATE" --body-file "$body" --changed-paths-file "$paths_file" >/dev/null 2>&1; then
    pass "load-bearing-by-path + APPROVE exits 0"
  else
    fail "load-bearing-by-path + APPROVE should exit 0"
  fi
  rm -f "$body" "$paths_file"

  # Non-load-bearing paths still honor [CHORE] auto-skip.
  body=$(mktemp)
  paths_file=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Touches docs/usage/foo.md.

```release-notes
[CHORE] tweak docs
```
EOF
  printf '%s\n' "docs/usage/foo.md" > "$paths_file"
  if "$GATE" --body-file "$body" --changed-paths-file "$paths_file" >/dev/null 2>&1; then
    pass "non-load-bearing path + [CHORE] still auto-skips"
  else
    fail "non-load-bearing path + [CHORE] should auto-skip"
  fi
  rm -f "$body" "$paths_file"
}

run_case_load_bearing_missing_token
run_case_load_bearing_approve_passes
run_case_load_bearing_revise_fails
run_case_load_bearing_block_fails
run_case_chore_release_notes_skips
run_case_docs_release_notes_skips
run_case_not_load_bearing_skips
run_case_fenced_revise_bare_approve_passes
run_case_fenced_approve_bare_revise_fails
run_case_multiple_tokens_uses_last
run_case_load_bearing_path_classifier

echo "---"
echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
