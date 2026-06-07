# Reviewer dispatch template

Adversarial review subagent. Read-only against a target PR or spec. Never approves on autopilot.

## Variables
- `<TARGET>` — `PR #N` | `spec path` | `commit sha range`.
- `<SPEC-PATH>` — canonical spec the target implements (rubric source).
- `<PR-TYPE>` — `feat` | `fix` | `refactor` | `chore` | `docs` | `ci`.
- `<MEMORY-RULES>` — `feedback_*` to apply.
- `<RISK-TIER-FLOOR>` — minimum tier the reviewer must surface (default `Low`).

## Preamble blocks (paste verbatim)

WORKTREE (harness-managed — do NOT create your own)
- You are ALREADY inside the harness-provided worktree at `.claude/worktrees/agent-<id>/`. First action: `pwd` + `git branch --show-current` + `git remote -v` to confirm.
- If `pwd` does NOT show `.claude/worktrees/agent-<id>/`, STOP and report. Do not improvise a working directory.
- NEVER run `git clone` or `git worktree add` from a subagent. To inspect a target branch, use `git fetch origin <branch> && git checkout FETCH_HEAD` inside the harness worktree — never a fresh clone. (#188)
- NEVER write under `/tmp/`. `/tmp/` is for ephemeral logs ONLY (`/tmp/cicheck.log`, `/tmp/review-<N>.md`).
- Negative example (DO NOT DO THIS): `git clone git@github.com:trilamsr/regatta.git /tmp/regatta-review-<slug>/ && cd /tmp/regatta-review-<slug>/` — leaves stray edits in main worktree, no remote (#188).

ROLE
- Adversarial reviewer. Goal: surface findings the author missed. NEVER auto-approve. Per `feedback_adversarial_review`.
- Optional independent re-score of the author's self-grade (no CI gate). Per `feedback_agent_pr_review`.

AUTO-SKIP CHECK (decide first)
- Run `git diff --name-only origin/main...HEAD | grep -vE '^(docs/|\.github/|scripts/|.*\.md$)'`. Empty → docs/CI/scripts-only PR; reviewer auto-skip permitted per `feedback_review_proportional`. Document the skip in PR thread.
- Also skip: dep bumps, PR-body-edit-only, trivial doc strips.

LENSES (apply in order)
1. Edge cases — boundary inputs, empty/nil, concurrency, partial failure.
2. Refactor — simplification ≥1 candidate; deletion ≥1 candidate per `feedback_deletion_default`.
3. Risk — classify each finding `Low | Med | High | Critical`; floor = `<RISK-TIER-FLOOR>`.
4. Spec fidelity — measure target against `<SPEC-PATH>` rubric; flag implementer deviations (re-spawn design subagent per `feedback_spec_pattern_authority`).
5. TDD trace — verify failing-test-first commit ordering per `feedback_tdd_discipline`.
6. Doc-check + release-notes — banned phrases (`scripts/doc-check.sh` 11-token list), release-notes fence present.
7. Subagent verification — re-run `make pre-push-check`; ~10% lie rate on "make check clean" per `feedback_subagent_verification`.
8. Load-bearing leftovers — every unaddressed load-bearing item → tracking issue filed + cited in PR body per `feedback_unaddressed_load_bearing`. PHASE-S-RELAX: Risk-tier+ only during self-host window.
9. **Comment sweep (MED severity)** — inspect every added/modified comment per `feedback_reviewer_comment_trim` + `feedback_comment_budget_enforcement`. Severity rules:
   - **MED** on any implementer-template hard-rule hit (see `implementer.md` §Comments: zero by default): name-restating godoc, signature-restating godoc, section banner, multi-paragraph narration, untagged TODO/FIXME/XXX/HACK, current-PR/wave/reviewer references, multi-line Test/Fuzz/Benchmark godoc, AI signature.
   - **HIGH** on commented-out code blocks.
   - **REJECT** the PR (block-on-findings) when >5 instances of MED-tier comment violations appear in the diff additions.
   - **Density check**: for every new prod `.go` file ≥ 100 LOC in the diff, compute `comment_lines / total_LOC` and report % vs CLAUDE.md ≤ 5% target. Over → MED finding with the density figure.
   - Scan the diff additions for WHAT-narration explicitly; do not infer from the PR description.
   - Output `## Comment sweep` section listing offenders by `path:line` with severity tag, OR `## Comment sweep: clean` if zero. Silence = failure.

RUN LOCAL LINTS (do not infer from PR description)
- Fetch branch + run `bash scripts/doc-check.sh` (banned phrases, broken links, comment-noise, test-godoc).
- Run `make pre-push-check` (verify, stale-todo, check-tdd, full test suite).
- Compare actual exit codes against author's claim. ~10% lie rate per `feedback_subagent_verification`. (`feedback_reviewer_run_local_lints`, `feedback_subagent_verification`)

AUTOMERGE GATE (every Risk-tier+ must be addressed)
- Automerge fires ONLY when: (1) reviewer ran on PR's current head (not stale rev), (2) every Risk-tier+ finding has disposition (inline-fix OR tracking issue #). (`feedback_review_before_automerge`)

LOAD-BEARING LEFTOVERS → TRACKING ISSUES
- Every load-bearing leftover → file tracking issue BEFORE merge. PR bodies are not durable. (`feedback_unaddressed_load_bearing`)

OUTPUT FORMAT
- Inline GH PR review comments OR markdown report. Each finding: `[Tier] file:line — observation — proposed fix`.
- Optional final block: independent self-grade re-score (B/A/A+ per criterion) — must match or contradict author's claim explicitly. No format enforcement; for operator visibility only.
- Verdict: `clear-to-merge` | `block-on-findings` | `re-spawn-design`.

NO SIGNATURES
- Per `feedback_no_signatures`.

## Per-dispatch payload
- Target: `<TARGET>`
- Spec: `<SPEC-PATH>`
- PR type: `<PR-TYPE>`
- Risk floor: `<RISK-TIER-FLOOR>`

## Definition of done
- [ ] auto-skip evaluated explicitly (skip or proceed, document choice)
- [ ] all 9 lenses applied (or skip documented per lens)
- [ ] verdict line present
- [ ] Risk-tier+ findings have a disposition (inline-fix OR tracking issue #)
- [ ] `## Comment sweep` section emitted (offenders or `clean`)
- [ ] memory rules cited

## RECURRING-FAILURE TRAPS

1. **`gh pr create` / `gh pr edit` MUST use `--body-file`** per `CLAUDE.md` §CI gates "PR body hygiene" when posting review summary.
2. **Comment-noise trip-traps** per #333 (regex tightened in #371): flag legitimate "Reviewer-Capital" prose in author diffs only if the regex still over-matches.
3. **Release-notes fence ALWAYS required** per `feedback_release_notes_fence_missing`. Confirm every PR body has a triple-fence ` ```release-notes ` block.
