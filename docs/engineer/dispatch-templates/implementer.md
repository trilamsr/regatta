# Implementer dispatch template

Code-writing subagent. Substitute `<VARS>` then paste into Task dispatch.

## Anti-patterns (block-list)

NEVER:
1. Put `Reviewer-recommendation:` or `Reviewer-agent-id:` in commit messages. The gate (`scripts/check-reviewer-verdict.sh`) reads PR body only — commit-message tokens are invisible. Per `feedback_no_self_tagged_approve`.
2. Enable `gh pr merge --auto` on load-bearing PRs carrying agent-id tokens. The gate fails closed with `automerge_with_agent_id_on_load_bearing`. End with `gh pr ready <N>` + operator-merge handoff. Per `feedback_no_implementer_automerge`.
3. Operate in `.claude/worktrees/operator-docker-soak/` or any shared-named worktree when ≥1 other agent uses it concurrently. HEAD clobber + lost work. Use orchestrator-pinned `regatta/agent-<N>` branch in the pre-created worktree. Per `feedback_keep_orchestrator_branch_name`.
4. Self-tag `Reviewer-recommendation: APPROVE` as the implementer. Independent reviewer subagent dispatches in a separate slot — implementer ends with `gh pr ready <N>` only. Per `feedback_no_self_tagged_approve`.

## Variables
- `<TASK-ID>` — wave/task tag (e.g. `S1-T2`, `cost-gov-W3-T7`).
- `<SPEC-PATH>` — canonical spec under `docs/engineer/specs/`.
- `<BRANCH-NAME>` — `feat/...` | `fix/...` | `chore/...`.
- `<FILE-SCOPE>` — paths this dispatch may touch (file-disjoint vs siblings).
- `<MEMORY-RULES>` — comma-separated `feedback_*` filenames to cite.
- `<PR-TYPE>` — `feat` | `fix` | `refactor` | `chore` | `docs` | `ci` (drives reviewer skip).

## Comments: zero by default

Write NO comments unless removing the comment would leave a future reader confused about WHY. A clear name + signature + types document the WHAT.

Hard rules (reviewer rejects on hit):
- No restating the symbol name in its own godoc ("// Foo returns a Foo.").
- No restating the signature ("// Bar takes an int and returns a string.").
- No section banners (`// ====`, `// ----`, `// *** Setup ***`).
- No multi-paragraph implementation narration.
- No untagged TODO/FIXME/XXX/HACK (cite `#NNN` on same line or omit).
- No commented-out code blocks.
- No comments referencing the current PR / wave / reviewer ("// added in #732", "// per Wave-D split", "// reviewer finding #2").
- Test/Fuzz/Benchmark godocs: 1 line max (CLAUDE.md gate).

Allowed (WHY-only):
- Exported godoc: symbol name + WHY in 1 sentence ("// UpperBound is the inclusive ceiling enforced by the budget gate.").
- Non-obvious invariant or workaround that would surprise a reader ("// HACK: pin random seed to keep golden-file stable across go versions.").
- Cross-file contract reference ("// Pairs with internal/X.Foo — drift here breaks ZZ.").

Net comment-density of any new prod `.go` file should stay low. Default to no comment; reviewer subagent lens 9 (comment-sweep) catches WHAT-narration drift.

## Preamble blocks (paste verbatim)

WORKTREE (harness-managed — do NOT create your own)
- You are ALREADY inside the harness-provided worktree at `.claude/worktrees/agent-<id>/`. First action: `pwd` + `git branch --show-current` + `git remote -v` to confirm.
- If `pwd` does NOT show `.claude/worktrees/agent-<id>/`, STOP and report. Do not improvise a working directory.
- NEVER run `git clone` or `git worktree add` from a subagent. The harness pre-creates the worktree; your job is to `cd` into the printed path, nothing more. (#188)
- NEVER write code under `/tmp/`. `/tmp/` is for ephemeral logs ONLY (`/tmp/cicheck.log`, `/tmp/pr-<branch>.md`). Code, tests, specs, edits → harness worktree only.
- Negative example (DO NOT DO THIS): `git clone git@github.com:trilamsr/regatta.git /tmp/regatta-<slug>/ && cd /tmp/regatta-<slug>/` — leaves main worktree with stray edits, no remote, no pushable branch (#188).
- Never push from the primary checkout.
- One writer per worktree (MAY-271): if `pwd` lands you in a worktree that already shows uncommitted changes from another author or a divergent branch, STOP and report — main thread must confirm the prior owner is terminal before re-dispatch. Stale file mtime ≠ dead. Re-dispatching into a still-live worktree clobbers HEAD + loses work. Per `feedback_agent_liveness_not_timestamp` (canonical operator memory slug).
- gopls cross-worktree noise: repo root ships `go.work` with `use ./` only, so gopls scopes the active module to the primary checkout. Sibling worktrees (`.claude/worktrees/agent-*/`) are out-of-workspace and may surface "file is within module …" warnings in tool results when an editor session straddles trees. Ignore those — they are diagnostic noise, not build errors. Verify with `go env GOWORK` (non-empty) and `go build ./...` (clean) before treating any cross-tree warning as load-bearing. (closes #777)

VERIFY BEFORE ACTING (cheap-check-first; subagent/reviewer output is a LEAD, not ground truth)
- Audit main before building: BEFORE writing any code, verify the target isn't already shipped — `git ls-tree -r origin/main --name-only | grep <expected-path>` OR `git log --oneline origin/main | grep '(#<issue>)'`. Self-abort + report "already on main" if it is. Per `feedback_audit_main_before_implementing` (MAY-92 + MAY-70 dispatched but already shipped → wasted invocations).
- Verify premise before deletion: BEFORE any deletion / untracking PR, grep for what CONSUMES the target — `git grep <basename> -- . ':!.gitignore'` PLUS check `docker-compose*` / `Dockerfile*` / `*.yaml` volume mounts + CI configs. A file that is gitignored BUT tracked (`git ls-files <path>` non-empty AND path matches `.gitignore`) is deliberately-committed infra, NOT junk — do not delete it as cruft. Per `feedback_premise_before_deletion` (#1290 deleted a docker-mounted file judged "junk").
- Spot-check subagent claims: investigator / reviewer output is a LEAD, not GROUND TRUTH (~10% wrong file:line, false "already shipped"). Open 2-3 cited file:line refs before acting; run a 1-min local measurement before recommending CI/perf/memory changes. Per `feedback_validate_before_ship`, `feedback_subagent_output_verify`.

TDD
- Failing test FIRST. Capture failing output in PR body. Then impl. Then green. Order matters per `feedback_tdd_discipline`.
- Red-first ORDERING is load-bearing: the failing test MUST land in a SEPARATE commit BEFORE the impl commit so `git log --reverse` shows red→green. Do NOT claim "red-first" / "TDD" when test + impl are one commit. If a single commit is unavoidable, state that truthfully in the PR body + justify — do not over-claim. Per `feedback_honest_tdd_claims`.

ADVERSARIAL REVIEW
- After green, spawn reviewer subagent against this template's sibling `reviewer.md`. Address Risk-tier+ findings (inline-fix OR file `[followup]` issue + cite #).
- PHASE-S-RELAX: auto-skip reviewer when `git diff --name-only origin/main...HEAD | grep -vE '^(docs/|\.github/|scripts/|.*\.md$)'` returns empty. Per `feedback_review_proportional`.

SELF-GRADE (voluntary, no CI gate)
- Operator may self-rate against the spec's B/A/A+ rubric for own visibility. No required format. No token shape enforced. No `## A+ Rubric Scorecard` section required. Per `feedback_grade_rubric` (deprecated MAY-32 — scorecard voluntary; reviewer subagent provides equivalent adversarial check).

DOC-CHECK
- Reword vague marketing language to falsifiable claims (version pin, benchmark, named reference). Reviewer subagent catches drift.

RELEASE NOTES
- PR body MUST contain a ```release-notes ... ``` fence (one line: user-visible change OR `none (internal)`). Body-edit alone won't retrigger pr-lint — needs a new commit. Per `feedback_release_notes_fence_missing` + `CLAUDE.md` §CI gates "PR body hygiene".

NO SIGNATURES
- No `Co-Authored-By`, no AI footer, no "Generated with" tags. Anywhere. Per `feedback_no_signatures`.

NO AUTOMERGE FROM IMPLEMENTER
- NEVER run `gh pr merge --auto` (or any automerge-enabling form). End with `gh pr ready <N>` + operator-merge handoff. The reviewer-verdict gate fails closed when `autoMergeRequest != null` AND `Reviewer-agent-id:` is present on a load-bearing PR — agent-written APPROVE + agent-enabled automerge leaves zero operator window between APPROVE-token landing and merge. Per `feedback_no_implementer_automerge` (closes #1046).

STOP AT `gh pr ready` (no self-revise)
- After pushing + opening PR + running `gh pr ready <N>`, the implementer MUST STOP. Do NOT self-revise. Do NOT pre-emptively address potential reviewer findings. Do NOT push fixup commits. Wait for explicit feedback from operator-dispatched independent reviewer OR operator. If you draft a self-critique alongside, that is fine — but do NOT commit/push it. Self-revising creates stale-PR races with reviewer dispatch (operator wastes a reviewer slot reviewing already-changed code; wave D session 2026-06-21 hit this multiple times). Per `feedback_stop_at_pr_ready`.

MEMORY CITES
- Cite `<MEMORY-RULES>` in PR body footer (path-relative, e.g. `memory/feedback_root_cause`). Reviewer checks citations resolve.

CI-CHECK OUTPUT COMPRESSION
- Report `make ci-check` via grep-then-tail (`feedback_subagent_cicheck_compress`):
  ```
  make ci-check 2>&1 | tee /tmp/cicheck.log | grep -E "^(FAIL|ok|---|Error|error:|PASS)" | tail -40
  echo "exit=$?"
  ```
  If grep empty AND exit≠0 → fallback `tail -50 /tmp/cicheck.log`. Main thread re-runs full (~10% lie rate per `feedback_subagent_verification`).

PRE-COMMIT `make check` MANDATORY
- After implementing + before `git add`: run `make check` and verify exit=0. Do NOT stage or commit on failure. Fix root cause + re-run. Use the same compressed-report form as CI-CHECK above. Skipping → post-push gate failure → re-investigate + re-fix + re-push round-trip (~25min cost per offender, hit 2× session 2026-06-10 on #1208/#1214). Per `feedback_pre_commit_make_check`.

SHARED-PRIMITIVE OWNERSHIP
- Before edit, scan composition roots (`cmd/regatta/serve.go`, `internal/orchestrator/state/machine.go`, `Makefile`) for sibling-touch. Defer to named OWNER if assigned. `docs/engineer/specs/README.md` used to belong here but is now gitignored + regenerated locally. (`feedback_parallel_safety`, `feedback_conflict_anticipation`)

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

INDEPENDENT REVIEW
- Solo implementers ship at B-tier by default. Spawn reviewer to pull up to A. (`feedback_adversarial_review`)

## Anchored rules (worker-prompt parity)

These slugs MUST be cited by `internal/orchestrator/spawner/claude.go::defaultPromptBuilder` so spawned worker subprocesses receive them inline (CC subprocess auto-loads CLAUDE.md but the per-dispatch prompt is authoritative for context budgeting). `scripts/check-prompt-parity.sh` enforces. Add a slug here only when the rule is worker-actionable mid-task. Reviewer-only / operator-only rules stay out of this list. Closes #901.

- `feedback_tdd_discipline`
- `feedback_comments_discipline`
- `feedback_deletion_default`
- `feedback_deletion_sweep_full_repo`
- `feedback_pr_body_hygiene`
- `feedback_review_proportional`
- `feedback_no_implementer_automerge`
- `feedback_keep_orchestrator_branch_name`
- `feedback_no_self_tagged_approve`
- `feedback_pre_commit_make_check`
- `feedback_colocated_test_required`
- `feedback_audit_main_before_implementing`
- `feedback_premise_before_deletion`
- `feedback_validate_before_ship`
- `feedback_subagent_output_verify`
- `feedback_honest_tdd_claims`
- `feedback_stop_at_pr_ready`

Escape hatch: append ` <!-- prompt-parity-skip: <reason> -->` to a bullet to mark a slug intentionally kept here but not pushed to the prompt.

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
- [ ] release-notes fence present
- [ ] no vague marketing language
- [ ] no signatures
- [ ] memory rules cited
- [ ] worktree removed after merge (`CLAUDE.md` §Worktree discipline)

## RECURRING-FAILURE TRAPS (2026-06-02 session)

1. **Test/Fuzz/Benchmark godocs 1 line max** per `feedback_test_godoc_one_line`. `scripts/doc-check.sh` test-godoc gate rejects multi-line. Multi-paragraph context belongs in the spec doc, not test files.

   **REPEAT OFFENDER 2026-06-02** — W5/W9/W2 all failed CI on this rule. Before push, run:
   ```bash
   git diff --name-only origin/main...HEAD | grep -E '_test\.go$' | xargs -I{} awk '/^\/\/ Test|^\/\/ Fuzz|^\/\/ Benchmark/{c=1; n=NR} c && /^\/\//{if(NR>n) print FILENAME":"n": multi-line godoc"; if(NR==n)c=2} c==2 && !/^\/\//{c=0}' {}
   ```
   Must return empty. CONCRETE FIX: collapse `// TestX pins behavior A: when input I, expect output O; ensures bug #N doesn't recur` (3 lines wrapped) → `// TestX asserts O on I (#N).` (1 line, hard wrap-free). Drop sub-clauses, the test name + body carry intent.

2. **`gh pr create` / `gh pr edit` MUST use `--body-file <path>`** per `CLAUDE.md` §CI gates "PR body hygiene". HEREDOC bodies (`--body "$(cat <<EOF ... EOF)"`) escape backticks and silently break the release-notes fence detector. Write body to `/tmp/pr-<branch>.md` first.

3. **Comment-noise gate trip-traps** per #333 followup. Regex was tightened in #371; if it still over-matches your prose, hyphenate the matching token (`reviewer-Request` / `reviewer-JSON`) or lowercase the following capital. Banner regex rejects `# --- Section ---` — use plain `# Section.` instead.

4. **GH base-sha drift workaround** per #343 (root-cause fix #347 merged): if check-tdd flags a file that isn't in your diff, the workflow's BASE_SHA env was stale. Now resolved live via `git merge-base`. If you still see ghost flags, add `[DOCS]` / `[CI]` / `[CHORE]` / `[REFACTOR]` category prefix to the release-notes block to opt out of check-tdd.

5. **Release-notes fence ALWAYS required** per `feedback_release_notes_fence_missing`. Every PR body MUST include a triple-fence ` ```release-notes ` block with `[PREFIX] one-line summary` inside — even `[DOCS]` PRs.

6. **Deletion sweep MANDATORY** per `feedback_deletion_sweep_full_repo`. Before opening any PR that deletes a file or symbol, run `git grep` for the basename + stem across the WHOLE worktree (not just adjacent files). Strip stale refs in the same PR OR add `<!-- stale-refs-justified: <reason ≥4 chars> -->` to the body for historical-accuracy specs. `scripts/check-stale-refs.sh` enforces in `make check` — fail-closed. Closes 8-round-reviewer trap (PR #1275, session 2026-06-10).

7. **Rebase `--theirs` vs `--ours` is counterintuitive** (closes #779). During `git rebase` replay, git treats the rebase target (main) as `--ours` and the commit being replayed (your PR's work) as `--theirs` — opposite of `git merge` semantics where `--ours` = current branch. Lost #772 (~10 sites #760 migration) this way. Resolution snippet (also in `CLAUDE.md` §Worktree discipline "Rebase conflict resolution"):
   ```
   git checkout --theirs <conflict-file>
   git add <file>
   git rebase --continue
   ```
   (Note: `docs/engineer/specs/README.md` is no longer tracked — it is gitignored and regenerated locally via `make specs-index`, so it never appears in rebase conflicts.)
