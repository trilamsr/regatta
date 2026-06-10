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
  # Extended 2026-06-09 (#1133): internal/web/ (operator dashboard UX) and
  # internal/obs/ (event vocabulary) are load-bearing — dashboard XSS,
  # broken polling, and event-name drift silently break monitoring.
  local paths
  for path in \
    "CLAUDE.md" \
    "Makefile" \
    "Makefile.d/check.mk" \
    ".github/workflows/pr-lint.yml" \
    "docs/engineer/dispatch-templates/implementer.md" \
    "scripts/check-scorecard.sh" \
    "internal/web/dashboard.go" \
    "internal/web/static/dashboard.css" \
    "internal/web/templates/_agents.tmpl" \
    "internal/obs/events.go" \
    ".claude/skills/regatta-operator/SKILL.md" \
    ".claude/skills/audit-session/SKILL.md"; do
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

run_case_self_tagged_author_equals_reviewer_fails() {
  # Closes self-tag loophole: author writing own APPROVE token == zero
  # adversarial pass. When --pr-author matches Reviewer-agent-id, fail.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

Reviewer-agent-id: trilamsr
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing --pr-author trilamsr 2>&1 | grep -qE "self-tagged|same.*author|independent"; then
    pass "author == Reviewer-agent-id fails as self-tagged"
  else
    fail "author == Reviewer-agent-id should fail as self-tagged (no adversarial pass)"
  fi
  rm -f "$body"
}

run_case_distinct_reviewer_passes() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

Reviewer-agent-id: cavecrew-reviewer-xyz789
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing --pr-author trilamsr >/dev/null 2>&1; then
    pass "distinct Reviewer-agent-id from author passes"
  else
    fail "distinct Reviewer-agent-id from author should pass"
  fi
  rm -f "$body"
}

run_case_missing_reviewer_agent_id_on_load_bearing_fails() {
  # APPROVE without Reviewer-agent-id line on a load-bearing PR fails —
  # author identity is unverifiable.
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
  if "$GATE" --body-file "$body" --load-bearing --pr-author trilamsr 2>&1 | grep -qE "Reviewer-agent-id"; then
    pass "load-bearing APPROVE missing Reviewer-agent-id fails"
  else
    fail "load-bearing APPROVE missing Reviewer-agent-id should fail"
  fi
  rm -f "$body"
}

run_case_operator_escape_skips_self_tag_check() {
  # Operator escape hatch: <!-- reviewer-skip-justified: <reason> -->
  # in body bypasses the self-tag check (rare; trivial doc/typo/dep-bump).
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Trivial typo fix in internal/orchestrator/scheduler/scheduler.go

<!-- reviewer-skip-justified: typo fix, <1 LoC, no behavior change -->

Reviewer-agent-id: trilamsr
Reviewer-recommendation: APPROVE

```release-notes
[FIX] typo
```
EOF
  if "$GATE" --body-file "$body" --load-bearing --pr-author trilamsr >/dev/null 2>&1; then
    pass "operator escape hatch bypasses self-tag check"
  else
    fail "operator escape hatch should bypass self-tag check"
  fi
  rm -f "$body"
}

run_case_operator_escape_too_short_rejected() {
  # Operator escape requires a reason ≥4 chars — empty or single-word
  # justifications are rejected so the escape stays auditable.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

<!-- reviewer-skip-justified: x -->

Reviewer-agent-id: trilamsr
Reviewer-recommendation: APPROVE

```release-notes
[FIX] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing --pr-author trilamsr 2>&1 | grep -qE "self-tagged|reviewer-skip-justified|allowlist"; then
    pass "operator escape with <4 char reason rejected"
  else
    fail "operator escape with <4 char reason should be rejected"
  fi
  rm -f "$body"
}

run_case_automerge_with_agent_id_on_load_bearing_fails() {
  # Closes #1046: orchestrator rushed self-tagged APPROVE + automerge in 19s.
  # When --automerge-enabled is passed AND the PR is load-bearing AND
  # Reviewer-agent-id is present, the gate MUST fail closed — agent both
  # wrote its own APPROVE and enabled automerge, leaving zero operator
  # window between APPROVE-token landing and merge.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

Reviewer-agent-id: a69bfa533ee180bb7
Reviewer-recommendation: APPROVE

```release-notes
[FIX] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing --automerge-enabled 2>&1 | grep -qE "automerge_with_agent_id_on_load_bearing"; then
    pass "automerge + agent-id on load-bearing fails with stderr token (#1046)"
  else
    fail "automerge + agent-id on load-bearing should fail with stderr token automerge_with_agent_id_on_load_bearing"
  fi
  rm -f "$body"
}

run_case_automerge_without_agent_id_uses_normal_path() {
  # When automerge is enabled but no Reviewer-agent-id is present, the
  # standard missing-agent-id path fires — automerge guard does not
  # short-circuit before the normal load-bearing checks.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

Reviewer-recommendation: APPROVE

```release-notes
[FIX] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing --automerge-enabled 2>&1 | grep -qE "Reviewer-agent-id"; then
    pass "automerge + missing agent-id still fails normal path"
  else
    fail "automerge + missing agent-id should fail normal missing-token path"
  fi
  rm -f "$body"
}

run_case_automerge_on_non_load_bearing_passes() {
  # Automerge guard only fires on load-bearing PRs — trivial doc/chore
  # PRs with automerge are still allowed (no review requirement applies).
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

```release-notes
[DOCS] tweak readme
```
EOF
  if "$GATE" --body-file "$body" --automerge-enabled >/dev/null 2>&1; then
    pass "automerge on non-load-bearing passes (no review requirement)"
  else
    fail "automerge on non-load-bearing should pass"
  fi
  rm -f "$body"
}

run_case_no_author_flag_still_enforces_allowlist() {
  # Even without --pr-author, the independent-reviewer allowlist runs:
  # a bare author login as Reviewer-agent-id fails the allowlist shape
  # check (harness-hex OR named-subagent prefix).
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

Reviewer-agent-id: trilamsr
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing 2>&1 | grep -qE "allowlist"; then
    pass "no --pr-author flag still rejects non-allowlist agent-id"
  else
    fail "no --pr-author flag should still enforce allowlist"
  fi
  rm -f "$body"
}

run_case_operator_opened_marker_bypasses_self_tag() {
  # Closes #1089: operator-opened PRs (operator wrote impl after agent
  # tool-denial / death) carry `<!-- operator-opened: <reason> -->` so
  # the author-mismatch check above doesn't fail by definition. Still
  # requires Reviewer-agent-id from the allowlist.
  local body
  body=$(mktemp)
  write_body "$body" <<'BODY_EOF'
## Summary

Operator wrote the impl after agent #1 hit a force-push denial.

internal/orchestrator/scheduler/scheduler.go
internal/orchestrator/state/agents.go

<!-- operator-opened: force-push-denied-on-agent-1 -->

Reviewer-agent-id: a7e408d8466d8c67b
Reviewer-recommendation: APPROVE

```release-notes
[FIX] thing
```
BODY_EOF
  if "$GATE" --body-file "$body" --load-bearing --pr-author trilamsr >/dev/null 2>&1; then
    pass "operator-opened marker bypasses self-tag check"
  else
    fail "operator-opened marker should bypass self-tag check"
  fi
  rm -f "$body"
}

run_case_operator_opened_marker_too_short_rejected() {
  # The operator-opened reason MUST be ≥4 chars so a future audit can
  # tell why the marker fired. <4 char or empty reason falls through to
  # the self-tag mismatch check.
  local body
  body=$(mktemp)
  write_body "$body" <<'BODY_EOF'
## Summary

Operator impl.

internal/orchestrator/scheduler/scheduler.go

<!-- operator-opened: x -->

Reviewer-agent-id: trilamsr
Reviewer-recommendation: APPROVE

```release-notes
[FIX] thing
```
BODY_EOF
  if "$GATE" --body-file "$body" --load-bearing --pr-author trilamsr 2>&1 | grep -qE "self-tagged|operator-opened"; then
    pass "operator-opened with <4 char reason rejected"
  else
    fail "operator-opened with <4 char reason should be rejected"
  fi
  rm -f "$body"
}

run_case_operator_opened_does_not_bypass_allowlist() {
  # Closes the reviewer-a0dc08561c8f24bbf coverage gap: the
  # operator-opened marker MUST NOT bypass the allowlist. A
  # self-tagged Reviewer-agent-id (e.g. the operator's GH login or
  # a freeform string like "self-tagged-defer") should fail even
  # with a valid marker, because the marker is positioned AFTER the
  # allowlist check in verdict.sh.
  local body
  body=$(mktemp)
  write_body "$body" <<'BODY_EOF'
## Summary

Operator wrote impl, BUT used their own GH login as Reviewer-agent-id.

internal/orchestrator/scheduler/scheduler.go

<!-- operator-opened: force-push-denied-on-agent-1 -->

Reviewer-agent-id: trilamsr
Reviewer-recommendation: APPROVE

```release-notes
[FIX] thing
```
BODY_EOF
  if "$GATE" --body-file "$body" --load-bearing --pr-author trilamsr 2>&1 | grep -qE "allowlist|self-tagged"; then
    pass "operator-opened still rejects non-allowlist Reviewer-agent-id"
  else
    fail "operator-opened MUST NOT bypass allowlist check"
  fi
  rm -f "$body"
}

run_case_operator_opened_still_requires_reviewer_id() {
  # Even with a valid operator-opened marker, the load-bearing PR MUST
  # carry a Reviewer-agent-id token — the missing-token check fires
  # before the marker check.
  local body
  body=$(mktemp)
  write_body "$body" <<'BODY_EOF'
## Summary

Operator wrote impl.

internal/orchestrator/scheduler/scheduler.go

<!-- operator-opened: agent-3-died-mid-cycle -->

Reviewer-recommendation: APPROVE

```release-notes
[FIX] thing
```
BODY_EOF
  if "$GATE" --body-file "$body" --load-bearing --pr-author trilamsr 2>&1 | grep -qE "Reviewer-agent-id"; then
    pass "operator-opened still requires Reviewer-agent-id"
  else
    fail "operator-opened MUST still require Reviewer-agent-id"
  fi
  rm -f "$body"
}

run_case_quality_all_three_sections_present_passes() {
  # Closes #1062: on a load-bearing PR with APPROVE, all three review-quality
  # sections (A+ delta, Negative-space audit, Reviewer confidence) MUST be
  # present and non-empty. Positive case.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

## A+ delta
Live integration test against `regatta serve` would close the B->A+ gap.

## Negative-space audit
1. mitigated — stale token shadowed by bare token; ```-strip prevents.
2. accepted — author can re-edit body post-merge; out of scope.
3. filed — #999 tracks edge case.

## Reviewer confidence
APPROVE — independent reviewer ran lenses 1-9 on HEAD, no HIGH/MED findings.

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing >/dev/null 2>&1; then
    pass "all 3 quality sections present + APPROVE → pass (#1062)"
  else
    fail "all 3 quality sections present + APPROVE should pass (#1062)"
  fi
  rm -f "$body"
}

run_case_quality_missing_a_plus_delta_fails() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

## Negative-space audit
1. mitigated — bypass A.
2. mitigated — bypass B.
3. accepted — bypass C; tracked in #999.

## Reviewer confidence
APPROVE — no gaps.

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing 2>&1 | grep -qE "missing_a_plus_delta"; then
    pass "missing ## A+ delta fails with token (#1062)"
  else
    fail "missing ## A+ delta should fail with stderr token missing_a_plus_delta (#1062)"
  fi
  rm -f "$body"
}

run_case_quality_missing_negative_space_fails() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

## A+ delta
Live integration test would close the gap.

## Reviewer confidence
APPROVE — no gaps.

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing 2>&1 | grep -qE "missing_negative_space_audit"; then
    pass "missing ## Negative-space audit fails with token (#1062)"
  else
    fail "missing ## Negative-space audit should fail with stderr token missing_negative_space_audit (#1062)"
  fi
  rm -f "$body"
}

run_case_quality_missing_reviewer_confidence_fails() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

## A+ delta
Live integration test would close the gap.

## Negative-space audit
1. mitigated — bypass A.
2. mitigated — bypass B.
3. accepted — bypass C.

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing 2>&1 | grep -qE "missing_reviewer_confidence"; then
    pass "missing ## Reviewer confidence fails with token (#1062)"
  else
    fail "missing ## Reviewer confidence should fail with stderr token missing_reviewer_confidence (#1062)"
  fi
  rm -f "$body"
}

run_case_quality_empty_a_plus_section_fails() {
  # Heading present but body empty (just whitespace) → fail.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

## A+ delta

## Negative-space audit
1. mitigated.
2. mitigated.
3. accepted.

## Reviewer confidence
APPROVE.

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing 2>&1 | grep -qE "missing_a_plus_delta"; then
    pass "empty ## A+ delta body fails (#1062)"
  else
    fail "empty ## A+ delta body should fail with stderr token missing_a_plus_delta (#1062)"
  fi
  rm -f "$body"
}

run_case_quality_a_plus_not_applicable_escape_passes() {
  # `<!-- a-plus-not-applicable: <reason> -->` allows empty ## A+ delta.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

## A+ delta

<!-- a-plus-not-applicable: mechanical refactor, no behavior change -->

## Negative-space audit
1. mitigated — byte-equal pin script lands w/ this PR.
2. mitigated — table-driven coverage in *_test.go matches.
3. accepted — reviewer can rerun byte-equal script post-merge.

## Reviewer confidence
APPROVE — byte-equal pin verifies pre/post.

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[REFACTOR] split file
```
EOF
  if "$GATE" --body-file "$body" --load-bearing >/dev/null 2>&1; then
    pass "a-plus-not-applicable escape passes (#1062)"
  else
    fail "a-plus-not-applicable escape should pass (#1062)"
  fi
  rm -f "$body"
}

run_case_quality_negative_space_not_applicable_escape_passes() {
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

## A+ delta
N/A reason captured below.

## Negative-space audit

<!-- negative-space-not-applicable: pure-deletion PR, no new surface -->

## Reviewer confidence
APPROVE — deletion-only.

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[REFACTOR] drop dead code
```
EOF
  if "$GATE" --body-file "$body" --load-bearing >/dev/null 2>&1; then
    pass "negative-space-not-applicable escape passes (#1062)"
  else
    fail "negative-space-not-applicable escape should pass (#1062)"
  fi
  rm -f "$body"
}

run_case_quality_insufficient_evidence_verdict_passes() {
  # Closes #1062 c3: INSUFFICIENT_EVIDENCE is a recognized verdict and the
  # gate does not fail closed when it appears.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Changes internal/orchestrator/scheduler/scheduler.go

## A+ delta
Would need 24h soak under production-shaped load to confirm A+.

## Negative-space audit
1. mitigated — race detector run.
2. accepted — leak detector not yet wired.
3. filed — #999 tracks soak run.

## Reviewer confidence
INSUFFICIENT_EVIDENCE — see Confidence-evidence-needed.
Confidence-evidence-needed: 24h soak run results from staging, tracked in #999.

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: INSUFFICIENT_EVIDENCE

```release-notes
[FEAT] thing
```
EOF
  if "$GATE" --body-file "$body" --load-bearing >/dev/null 2>&1; then
    pass "INSUFFICIENT_EVIDENCE verdict accepted (#1062 c3)"
  else
    fail "INSUFFICIENT_EVIDENCE verdict should be accepted (#1062 c3)"
  fi
  rm -f "$body"
}

run_case_quality_not_load_bearing_skips_section_check() {
  # Non-load-bearing PRs do NOT require the three review-quality sections.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Tweak docs.

```release-notes
[DOCS] tweak
```
EOF
  if "$GATE" --body-file "$body" >/dev/null 2>&1; then
    pass "non-load-bearing PR skips review-quality section check (#1062)"
  else
    fail "non-load-bearing PR should skip review-quality section check (#1062)"
  fi
  rm -f "$body"
}

run_case_quality_chore_autoskip_still_works() {
  # [CHORE] release-notes on non-load-bearing paths still auto-skips the
  # quality-section check — it runs only when reviewer-verdict gate runs.
  local body
  body=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

```release-notes
[CHORE] mechanical bump
```
EOF
  if "$GATE" --body-file "$body" --load-bearing >/dev/null 2>&1; then
    pass "[CHORE] release-notes still auto-skips quality-section check (#1062)"
  else
    fail "[CHORE] release-notes should still auto-skip quality-section check (#1062)"
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
run_case_self_tagged_author_equals_reviewer_fails
run_case_distinct_reviewer_passes
run_case_missing_reviewer_agent_id_on_load_bearing_fails
run_case_operator_escape_skips_self_tag_check
run_case_operator_escape_too_short_rejected
run_case_no_author_flag_still_enforces_allowlist
run_case_automerge_with_agent_id_on_load_bearing_fails
run_case_automerge_without_agent_id_uses_normal_path
run_case_automerge_on_non_load_bearing_passes
run_case_operator_opened_marker_bypasses_self_tag
run_case_operator_opened_marker_too_short_rejected
run_case_operator_opened_still_requires_reviewer_id
run_case_operator_opened_does_not_bypass_allowlist
run_case_quality_all_three_sections_present_passes
run_case_quality_missing_a_plus_delta_fails
run_case_quality_missing_negative_space_fails
run_case_quality_missing_reviewer_confidence_fails
run_case_quality_empty_a_plus_section_fails
run_case_quality_a_plus_not_applicable_escape_passes
run_case_quality_negative_space_not_applicable_escape_passes
run_case_quality_insufficient_evidence_verdict_passes
run_case_quality_not_load_bearing_skips_section_check
run_case_quality_chore_autoskip_still_works

echo "---"
echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
