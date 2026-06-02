# Implementer dispatch template

Code-writing subagent. Substitute `<VARS>` then paste into Task dispatch.

## Variables
- `<TASK-ID>` — wave/task tag (e.g. `S1-T2`, `cost-gov-W3-T7`).
- `<SPEC-PATH>` — canonical spec under `docs/engineer/specs/`.
- `<BRANCH-NAME>` — `feat/...` | `fix/...` | `chore/...`.
- `<FILE-SCOPE>` — paths this dispatch may touch (file-disjoint vs siblings).
- `<MEMORY-RULES>` — comma-separated `feedback_*` filenames to cite.
- `<PR-TYPE>` — `feat` | `fix` | `refactor` | `chore` | `docs` | `ci` (drives scorecard + reviewer skip).

## Preamble blocks (paste verbatim)

WORKTREE
- `git worktree add ../regatta-<TASK-ID> -b <BRANCH-NAME> origin/main && cd ../regatta-<TASK-ID>`. All edits here. Never push from primary.

TDD
- Failing test FIRST. Capture failing output in PR body. Then impl. Then green. Order matters per `feedback_tdd_discipline`.

ADVERSARIAL REVIEW
- After green, spawn reviewer subagent against this template's sibling `reviewer.md`. Address Risk-tier+ findings (inline-fix OR file `[followup]` issue + cite #).
- PHASE-S-RELAX: auto-skip reviewer when `git diff --name-only origin/main...HEAD | grep -vE '^(docs/|\.github/|scripts/|.*\.md$)'` returns empty. Per `feedback_review_proportional`.

A+ SCORECARD
- PR body MUST include `## A+ Rubric Scorecard` section. Each B/A/A+ criterion from `<SPEC-PATH>` marked PASS/FAIL/N-A + one-line evidence + claimed tier. Per `feedback_grade_rubric`.
- PHASE-S-RELAX: required only when `<PR-TYPE>` ∈ {feat}. Refactor / chore / docs / ci skip the scorecard.

DOC-CHECK
- Pre-push grep banned phrases — token list (11 entries) lives in `scripts/doc-check.sh` (`banned_tokens` array). Reword hits to falsifiable claims (version pin, benchmark, named reference). Per `feedback_doc_check_banned_phrases`.

RELEASE NOTES
- PR body MUST contain a ```release-notes ... ``` fence (one line: user-visible change OR `none (internal)`). Body-edit alone won't retrigger pr-lint — needs a new commit. Per `feedback_pr_body_release_notes_fence`.

NO SIGNATURES
- No `Co-Authored-By`, no AI footer, no "Generated with" tags. Anywhere. Per `feedback_no_signatures`.

MEMORY CITES
- Cite `<MEMORY-RULES>` in PR body footer (path-relative, e.g. `memory/feedback_root_cause`). Reviewer checks citations resolve.

## Per-dispatch payload
- Task: `<TASK-ID>`
- Spec: `<SPEC-PATH>` (canonical; deviations require design-subagent re-spawn per `feedback_spec_pattern_authority`)
- File scope: `<FILE-SCOPE>` (stay disjoint with sibling implementers)
- PR type: `<PR-TYPE>`
- Migration number (if schema): pinned in dispatch prompt, never picked by implementer (`feedback_migration_number_lock`).

## RECURRING-FAILURE TRAPS (2026-06-02 session)

These four traps cost a re-spin each in the last loop. Implementers paste these as constraints into their own working notes.

1. Test/Fuzz/Benchmark godocs — 1 line max. `scripts/doc-check.sh` test-godoc gate rejects multi-line. Multi-paragraph context belongs in the spec doc, not the test file. Test names encode intent. Per `feedback_test_godoc_one_line`.

2. `gh pr create` / `gh pr edit` MUST use `--body-file <path>`. HEREDOC bodies (`--body "$(cat <<EOF ... EOF)"`) escape backticks and silently break the release-notes fence detector. Write the body to `/tmp/pr-<branch>.md` first, then `gh pr create --body-file /tmp/pr-<branch>.md`. Per `feedback_pr_body_file_only`.

3. Comment-noise gate trip-traps. Two patterns over-match legitimate prose:
   - `[Rr]eviewer\s+[A-Z]` matches "reviewer-Request" / "reviewer-JSON" → hyphenate (`reviewer-Request`, `reviewer-JSON`) or lowercase the following token.
   - `# --- Section ---` banner comment regex (source files only) → use plain `# Section.` instead.

4. GH base-sha drift workaround (per #343; root-cause fix tracked in #347): if `check-tdd` flags a file that is not in your diff, the workflow's `BASE_SHA` env is stale from a `main` merge that landed after PR-open. Add `[DOCS]`, `[CI]`, or `[CHORE]` category prefix at the start of the ```release-notes``` block to opt out of the test-first check. Remove the prefix once #347 lands.

## Definition of done
- [ ] worktree branch, not primary
- [ ] failing test landed first (commit log shows it)
- [ ] `make pre-push-check` green locally
- [ ] reviewer subagent cleared OR auto-skip condition met
- [ ] scorecard in PR body OR `<PR-TYPE>` exempt
- [ ] release-notes fence present
- [ ] no banned phrases
- [ ] no signatures
- [ ] memory rules cited
- [ ] worktree removed after merge (`feedback_worktree_cleanup_post_merge`)
