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

run_case_docs_spec_requires_token() {
  local body diff_file
  body=$(mktemp)
  diff_file=$(mktemp)
  write_body "$body" <<'EOF'
## Summary
- new spec

```release-notes
[DOCS] new spec
```
EOF
  printf 'docs/engineer/specs/2026-06-08-new.md\n' > "$diff_file"
  if "$GATE" --body-file "$body" --load-bearing --changed-paths-file "$diff_file" 2>&1 | grep -qE "Reviewer-recommendation"; then
    pass "[DOCS] spec change without token fails (load-bearing carve-out)"
  else
    fail "[DOCS] spec change without token should fail — specs are load-bearing"
  fi
  rm -f "$body" "$diff_file"
}

run_case_docs_brief_requires_token() {
  local body diff_file
  body=$(mktemp)
  diff_file=$(mktemp)
  write_body "$body" <<'EOF'
## Summary
- design brief

```release-notes
[DOCS] new brief
```
EOF
  printf 'docs/engineer/briefs/2026-06-08-thing.md\n' > "$diff_file"
  if "$GATE" --body-file "$body" --load-bearing --changed-paths-file "$diff_file" 2>&1 | grep -qE "Reviewer-recommendation"; then
    pass "[DOCS] brief change without token fails (load-bearing carve-out)"
  else
    fail "[DOCS] brief change without token should fail — briefs are load-bearing"
  fi
  rm -f "$body" "$diff_file"
}

run_case_docs_template_requires_token() {
  local body diff_file
  body=$(mktemp)
  diff_file=$(mktemp)
  write_body "$body" <<'EOF'
```release-notes
[DOCS] tweak dispatch template
```
EOF
  printf 'docs/engineer/dispatch-templates/implementer.md\n' > "$diff_file"
  if "$GATE" --body-file "$body" --load-bearing --changed-paths-file "$diff_file" 2>&1 | grep -qE "Reviewer-recommendation"; then
    pass "[DOCS] dispatch-template change without token fails (load-bearing carve-out)"
  else
    fail "[DOCS] dispatch-template change without token should fail — templates are agent-rule surface"
  fi
  rm -f "$body" "$diff_file"
}

run_case_docs_claudemd_requires_token() {
  local body diff_file
  body=$(mktemp)
  diff_file=$(mktemp)
  write_body "$body" <<'EOF'
```release-notes
[DOCS] CLAUDE.md tweak
```
EOF
  printf 'CLAUDE.md\n' > "$diff_file"
  if "$GATE" --body-file "$body" --load-bearing --changed-paths-file "$diff_file" 2>&1 | grep -qE "Reviewer-recommendation"; then
    pass "[DOCS] CLAUDE.md change without token fails (load-bearing carve-out)"
  else
    fail "[DOCS] CLAUDE.md change without token should fail — agent-rule surface"
  fi
  rm -f "$body" "$diff_file"
}

run_case_docs_runbook_still_skips() {
  local body diff_file
  body=$(mktemp)
  diff_file=$(mktemp)
  write_body "$body" <<'EOF'
```release-notes
[DOCS] runbook refresh
```
EOF
  printf 'docs/operator/runbook.md\n' > "$diff_file"
  if "$GATE" --body-file "$body" --load-bearing --changed-paths-file "$diff_file" >/dev/null 2>&1; then
    pass "[DOCS] runbook change still auto-skips (proportional)"
  else
    fail "[DOCS] runbook change should still auto-skip per feedback_review_proportional"
  fi
  rm -f "$body" "$diff_file"
}

run_case_docs_spec_with_token_passes() {
  local body diff_file
  body=$(mktemp)
  diff_file=$(mktemp)
  write_body "$body" <<'EOF'
## Summary
- new spec

Reviewer-agent-id: cavecrew-reviewer-spec123
Reviewer-recommendation: APPROVE

```release-notes
[DOCS] new spec
```
EOF
  printf 'docs/engineer/specs/2026-06-08-new.md\n' > "$diff_file"
  if "$GATE" --body-file "$body" --load-bearing --changed-paths-file "$diff_file" >/dev/null 2>&1; then
    pass "[DOCS] spec change WITH token passes"
  else
    fail "[DOCS] spec change WITH token should pass"
  fi
  rm -f "$body" "$diff_file"
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

  # Self-tag denylist: 'main-thread-adversarial-self' agent-id rejected on load-bearing PR.
  body=$(mktemp); paths_file=$(mktemp)
  write_body "$body" <<'EOF'
## PR
Reviewer-agent-id: main-thread-adversarial-self
Reviewer-recommendation: APPROVE
```release-notes
[FEAT] x
```
EOF
  printf '%s\n' "CLAUDE.md" > "$paths_file"
  if "$GATE" --body-file "$body" --changed-paths-file "$paths_file" >/dev/null 2>&1; then
    fail "self-tag 'main-thread-adversarial-self' must be rejected on load-bearing PR"
  else
    pass "self-tag 'main-thread-adversarial-self' rejected on load-bearing PR"
  fi
  rm -f "$body" "$paths_file"

  # Self-tag denylist: 'self-tagged-defer' agent-id rejected on load-bearing PR.
  body=$(mktemp); paths_file=$(mktemp)
  write_body "$body" <<'EOF'
## PR
Reviewer-agent-id: self-tagged-defer
Reviewer-recommendation: APPROVE
```release-notes
[FEAT] x
```
EOF
  printf '%s\n' "scripts/check-foo.sh" > "$paths_file"
  if "$GATE" --body-file "$body" --changed-paths-file "$paths_file" >/dev/null 2>&1; then
    fail "self-tag 'self-tagged-defer' must be rejected on load-bearing PR"
  else
    pass "self-tag 'self-tagged-defer' rejected on load-bearing PR"
  fi
  rm -f "$body" "$paths_file"

  # Min-length: agent-id <12 chars rejected on load-bearing PR.
  body=$(mktemp); paths_file=$(mktemp)
  write_body "$body" <<'EOF'
## PR
Reviewer-agent-id: short
Reviewer-recommendation: APPROVE
```release-notes
[FEAT] x
```
EOF
  printf '%s\n' "Makefile.d/foo.mk" > "$paths_file"
  if "$GATE" --body-file "$body" --changed-paths-file "$paths_file" >/dev/null 2>&1; then
    fail "agent-id <12 chars must be rejected on load-bearing PR"
  else
    pass "agent-id <12 chars rejected on load-bearing PR"
  fi
  rm -f "$body" "$paths_file"

  # Harness-shape: 17-char hex agent-id 'a6614259e2388c0ee' accepted on load-bearing PR.
  body=$(mktemp); paths_file=$(mktemp)
  write_body "$body" <<'EOF'
## PR
Reviewer-agent-id: a6614259e2388c0ee
Reviewer-recommendation: APPROVE
```release-notes
[FEAT] x
```
EOF
  printf '%s\n' "scripts/check-foo.sh" > "$paths_file"
  if "$GATE" --body-file "$body" --changed-paths-file "$paths_file" >/dev/null 2>&1; then
    pass "harness-shape agent-id accepted on load-bearing PR"
  else
    fail "harness-shape agent-id 'a6614259e2388c0ee' should pass on load-bearing PR"
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

run_case_load_bearing_approve_without_agent_id_fails() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

Reviewer-recommendation: APPROVE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing >/dev/null 2>&1; then
    fail "load-bearing + APPROVE without Reviewer-agent-id should exit non-zero (self-tagged approval)"
  else
    pass "load-bearing + APPROVE without Reviewer-agent-id fails (self-tagged approval blocked)"
  fi
  rm -f "$body"
}

run_case_load_bearing_missing_token
run_case_load_bearing_approve_passes
run_case_load_bearing_approve_without_agent_id_fails
run_case_load_bearing_revise_fails
run_case_load_bearing_block_fails
run_case_chore_release_notes_skips
run_case_docs_release_notes_skips
run_case_not_load_bearing_skips
run_case_fenced_revise_bare_approve_passes
run_case_fenced_approve_bare_revise_fails
run_case_docs_spec_requires_token
run_case_docs_brief_requires_token
run_case_docs_template_requires_token
run_case_docs_claudemd_requires_token
run_case_docs_runbook_still_skips
run_case_docs_spec_with_token_passes
run_case_multiple_tokens_uses_last
run_case_load_bearing_path_classifier

echo "---"
echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
