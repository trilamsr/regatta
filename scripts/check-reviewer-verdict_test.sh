#!/usr/bin/env bash
# check-reviewer-verdict_test.sh asserts the reviewer-verdict gate fails
# closed on load-bearing PRs without APPROVE recommendation.

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
    pass "fenced REVISE + bare APPROVE → bare APPROVE wins"
  else
    fail "fenced REVISE + bare APPROVE should pass — fenced tokens must be stripped"
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
    pass "fenced APPROVE + bare REVISE → bare REVISE wins"
  else
    fail "fenced APPROVE + bare REVISE should fail — fenced tokens must be stripped"
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
  # Per N1 (#1264): docs/engineer/specs/*.md no longer auto-flagged load-bearing.
  # Operator may spawn reviewer voluntarily; auto-skip per feedback_review_proportional.
  printf 'docs/engineer/specs/2026-06-08-new.md\n' > "$diff_file"
  if "$GATE" --body-file "$body" --load-bearing --changed-paths-file "$diff_file" >/dev/null 2>&1; then
    pass "[DOCS] spec change auto-skips (N1 narrowed classifier)"
  else
    fail "[DOCS] spec change should auto-skip per N1 (#1264)"
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
  # Per N1 (#1264): docs/engineer/briefs/*.md no longer auto-flagged load-bearing.
  printf 'docs/engineer/briefs/2026-06-08-thing.md\n' > "$diff_file"
  if "$GATE" --body-file "$body" --load-bearing --changed-paths-file "$diff_file" >/dev/null 2>&1; then
    pass "[DOCS] brief change auto-skips (N1 narrowed classifier)"
  else
    fail "[DOCS] brief change should auto-skip per N1 (#1264)"
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
  # Per N1 (#1264): docs/engineer/dispatch-templates/*.md no longer auto-flagged.
  printf 'docs/engineer/dispatch-templates/implementer.md\n' > "$diff_file"
  if "$GATE" --body-file "$body" --load-bearing --changed-paths-file "$diff_file" >/dev/null 2>&1; then
    pass "[DOCS] dispatch-template change auto-skips (N1 narrowed classifier)"
  else
    fail "[DOCS] dispatch-template change should auto-skip per N1 (#1264)"
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
  # Per N1 (#1264): CLAUDE.md no longer auto-flagged. Operator may spawn reviewer voluntarily.
  printf 'CLAUDE.md\n' > "$diff_file"
  if "$GATE" --body-file "$body" --load-bearing --changed-paths-file "$diff_file" >/dev/null 2>&1; then
    pass "[DOCS] CLAUDE.md change auto-skips (N1 narrowed classifier)"
  else
    fail "[DOCS] CLAUDE.md change should auto-skip per N1 (#1264)"
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
  # Post-N1: spec paths auto-skip, so token presence is moot. Use internal/web/
  # to verify token-present path still passes on load-bearing prod surface.
  printf 'internal/web/dashboard.go\n' > "$diff_file"
  if "$GATE" --body-file "$body" --load-bearing --changed-paths-file "$diff_file" >/dev/null 2>&1; then
    pass "load-bearing internal/web/ change WITH token passes"
  else
    fail "load-bearing internal/web/ change WITH token should pass"
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
    pass "stale REVISE + fresh APPROVE → last token wins"
  else
    fail "stale REVISE + fresh APPROVE should pass — last token must win"
  fi
  rm -f "$body"
}

run_case_load_bearing_path_classifier() {
  # Retro audit 2026-06-08: 5 structural refactors self-tagged
  # APPROVE bypassing review because the workflow path classifier missed
  # agent-rule + CI-gate surfaces. When --changed-paths-file lists any of
  # Makefile, Makefile.d/*, .github/workflows/*, scripts/check-*.sh,
  # the script MUST treat the PR as load-bearing AND require the
  # Reviewer-recommendation token even when the release-notes category is
  # [DOCS]/[CHORE]/[CI]/[NONE]/[CHANGE].
  # Extended 2026-06-09: internal/web/ (operator dashboard UX) and
  # internal/obs/ (event vocabulary) are load-bearing — dashboard XSS,
  # broken polling, and event-name drift silently break monitoring.
  # Narrowed 2026-06-10 (N1 #1264): CLAUDE.md, dispatch-templates,
  # specs, briefs REMOVED from auto-flag — solo doc PRs auto-skip
  # per feedback_review_proportional.
  local paths
  for path in \
    "Makefile" \
    "Makefile.d/check.mk" \
    ".github/workflows/pr-lint.yml" \
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

  # APPROVE on a load-bearing-by-path PR still passes. Post-N1 use Makefile
  # as the load-bearing trigger (CLAUDE.md no longer auto-flagged).
  local body paths_file
  body=$(mktemp)
  paths_file=$(mktemp)
  write_body "$body" <<'EOF'
## Summary

Touches Makefile.

Reviewer-agent-id: cavecrew-reviewer-abc123
Reviewer-recommendation: APPROVE

```release-notes
[CHORE] update Makefile
```
EOF
  printf '%s\n' "Makefile" > "$paths_file"
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
  printf '%s\n' "Makefile" > "$paths_file"
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
    pass "automerge + agent-id on load-bearing fails with stderr token"
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

echo "---"
echo "PASS=$PASS FAIL=$FAIL"
[ "$FAIL" -eq 0 ]
