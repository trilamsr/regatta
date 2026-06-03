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

CI-CHECK OUTPUT COMPRESSION
- Report `make ci-check` via grep-then-tail (`feedback_subagent_cicheck_compress`):
  ```
  make ci-check 2>&1 | tee /tmp/cicheck.log | grep -E "^(FAIL|ok|---|Error|error:|PASS)" | tail -40
  echo "exit=$?"
  ```
  If grep empty AND exit≠0 → fallback `tail -50 /tmp/cicheck.log`. Main thread re-runs full (~10% lie rate per `feedback_subagent_verification`).

SHARED-PRIMITIVE OWNERSHIP
- Before edit, scan composition roots (`cmd/regatta/serve.go`, `internal/orchestrator/state/machine.go`, `Makefile`, `docs/engineer/specs/README.md`) for sibling-touch. Defer to named OWNER if assigned. (`feedback_parallel_safety`, `feedback_conflict_anticipation`)

WINDOWS PATH TESTS
- When asserting paths against error messages or production output, canonicalize BOTH sides the same way production code does — OR platform-branch the test inputs. 8.3 short-names + `/etc`-literal paths break Windows CI silently post-merge. (`feedback_windows_path_tests`)

COMMENT BUDGET (recurring offender)
- Drop single-line WHAT-narration. Default to no comment. Long-term-benefit gate: keep only if removing leaves future reader confused about WHY. (`feedback_comments_discipline`, `feedback_comment_budget_enforcement`)
- Exported godoc: 1-line WHY-form opening with symbol name (`feedback_comments_lint_reconcile`). `// Foo is the bound enforced by gate X.` not `// Foo returns the bound.`

REVIEWER-SKIP CONDITIONS (proportional)
- Auto-skip when `git diff --name-only origin/main...HEAD | grep -vE '^(docs/|\.github/|scripts/|.*\.md$)'` is empty (docs/CI/scripts-only).
- Skip on: dep bumps with CI green + <20 LoC + no API change; body-edit-only; trivial doc strips. (`feedback_review_proportional`)

LOAD-BEARING LEFTOVERS → ISSUES
- Any finding NOT fixed inline → file tracking issue + cite # in PR body. Never leave load-bearing items in PR-body prose only. (`feedback_unaddressed_load_bearing`, `feedback_agent_load_bearing_to_issues`)

INDEPENDENT REVIEW MEASURES vs A+ RUBRIC
- Solo implementers ship at B-tier by default. Spawn reviewer to pull up to A. (`feedback_agent_pr_review`)

## Per-dispatch payload
- Task: `<TASK-ID>`
- Spec: `<SPEC-PATH>` (canonical; deviations require design-subagent re-spawn per `feedback_spec_pattern_authority`)
- File scope: `<FILE-SCOPE>` (stay disjoint with sibling implementers)
- PR type: `<PR-TYPE>`
- Migration number (if schema): pinned in dispatch prompt, never picked by implementer (`feedback_migration_number_lock`).

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

## RECURRING-FAILURE TRAPS (2026-06-02 session)

1. **Test/Fuzz/Benchmark godocs 1 line max** per `feedback_test_godoc_one_line`. `scripts/doc-check.sh` test-godoc gate rejects multi-line. Multi-paragraph context belongs in the spec doc, not test files.

   **REPEAT OFFENDER 2026-06-02** — W5/W9/W2 all failed CI on this rule. Before push, run:
   ```bash
   git diff --name-only origin/main...HEAD | grep -E '_test\.go$' | xargs -I{} awk '/^\/\/ Test|^\/\/ Fuzz|^\/\/ Benchmark/{c=1; n=NR} c && /^\/\//{if(NR>n) print FILENAME":"n": multi-line godoc"; if(NR==n)c=2} c==2 && !/^\/\//{c=0}' {}
   ```
   Must return empty. CONCRETE FIX: collapse `// TestX pins behavior A: when input I, expect output O; ensures bug #N doesn't recur` (3 lines wrapped) → `// TestX asserts O on I (#N).` (1 line, hard wrap-free). Drop sub-clauses, the test name + body carry intent.

2. **`gh pr create` / `gh pr edit` MUST use `--body-file <path>`** per `feedback_pr_body_file_only`. HEREDOC bodies (`--body "$(cat <<EOF ... EOF)"`) escape backticks and silently break the release-notes fence detector. Write body to `/tmp/pr-<branch>.md` first.

3. **Comment-noise gate trip-traps** per #333 followup. Regex was tightened in #371; if it still over-matches your prose, hyphenate the matching token (`reviewer-Request` / `reviewer-JSON`) or lowercase the following capital. Banner regex rejects `# --- Section ---` — use plain `# Section.` instead.

4. **GH base-sha drift workaround** per #343 (root-cause fix #347 merged): if check-tdd flags a file that isn't in your diff, the workflow's BASE_SHA env was stale. Now resolved live via `git merge-base`. If you still see ghost flags, add `[DOCS]` / `[CI]` / `[CHORE]` category prefix to the release-notes block to opt out.
