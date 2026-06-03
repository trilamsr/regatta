# Designer dispatch template

Design-doc subagent. Output: spec under `docs/engineer/specs/YYYY-MM-DD-<slug>.md`.

## Variables
- `<TOPIC>` — one-line problem statement.
- `<SPEC-SLUG>` — `YYYY-MM-DD-<short-slug>` (date locked at dispatch).
- `<SCOPE>` — what's in / out of scope (self-host filter applied).
- `<PHASE>` — `S1` | `S2` | `S3` | `X` (drives self-host filter).
- `<MEMORY-RULES>` — `feedback_*` + `wedge_*` filenames to cite.
- `<REFERENCES>` — proven OSS or prior-art systems to study before writing.

## Preamble blocks (paste verbatim)

WORKTREE (harness-managed — do NOT create your own)
- You are ALREADY inside the harness-provided worktree at `.claude/worktrees/agent-<id>/`. First action: `pwd` + `git branch --show-current` + `git remote -v` to confirm.
- If `pwd` does NOT show `.claude/worktrees/agent-<id>/`, STOP and report. Do not improvise a working directory.
- NEVER run `git clone` or `git worktree add` from a subagent. The harness pre-creates the worktree; your job is to `cd` into the printed path, nothing more. (#188)
- NEVER write spec or code under `/tmp/`. `/tmp/` is for ephemeral logs ONLY. Spec output → `docs/engineer/specs/<SPEC-SLUG>.md` inside the harness worktree.
- Negative example (DO NOT DO THIS): `git clone git@github.com:trilamsr/regatta.git /tmp/regatta-spec-<slug>/ && cd /tmp/regatta-spec-<slug>/` — leaves stray edits in main worktree, no remote, no pushable branch (#188).

RESEARCH + DESIGN
- Prefer adopting proven OSS over reimplementation. Study `<REFERENCES>` first; cite version + commit-sha + license. Priority: UX > quality bar matching reference systems > ecosystem conventions > long-term repo+user benefit. Per `feedback_research_design_principles`.

GRADE RUBRIC
- Spec MUST end with a B/A/A+ rubric — each tier names falsifiable acceptance criteria (test names, metric thresholds, named artifacts). Implementer scorecards measure against this. Per `feedback_grade_rubric`.

SELF-HOST FILTER
- Every claim filtered by "does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?". Keep → in scope. Defer → Phase X with explicit reopen-trigger (external customer ask OR 30-day-green). Per `docs/engineer/briefs/2026-06-01-self-host-first.md` §1.

ADVERSARIAL REVIEW ON SPEC
- After draft, spawn reviewer subagent (see sibling `reviewer.md`) targeting: simplification opportunities, deletion candidates, edge cases, risk tiers, OSS reuse the spec missed. Fix findings inline OR cite as deferred with reopen-trigger.

DOC-CHECK
- No banned phrases (`scripts/doc-check.sh`, 11 tokens). Reword to falsifiable claims (version pin, benchmark, named reference). Pre-push grep mandatory. Per `CLAUDE.md` §CI gates "Banned-phrase gate".

DELETION DEFAULT
- Spec answers "what got smaller?" Additions need A+ defense. Per `feedback_deletion_default`.

RELEASE NOTES
- Spec PR body needs ```release-notes ... ``` fence (typically `none (internal)` for design-only). Per `feedback_release_notes_fence_missing` + `CLAUDE.md` §CI gates "PR body hygiene".

NO SIGNATURES
- No `Co-Authored-By`, no AI footer. Per `feedback_no_signatures`.

MEMORY CITES
- Cite `<MEMORY-RULES>` in PR body footer + inline in spec where load-bearing.

OUTPUT-PATH SLUG MUST BE EXACT
- Dispatch prompt MUST specify exact `<SPEC-SLUG>` (date + canonical short slug). Plan-subagent picking own slug produces dup files (`2026-06-01-cost-gov-w1-tasks.md` vs `2026-06-01-cost-governor-w1-tasks.md`). (`feedback_plan_subagent_dup_files`)

CROSS-DOC LINK PHASING
- Sibling docs that cross-link each other (e.g. `docs/operator/foo.md` ↔ `docs/engineer/runbooks/foo.md`) fail doc-check per-PR because each PR sees only its own added file. Co-locate in ONE PR OR phase-land with strip-then-restore. (`feedback_cross_doc_link_phasing`)

DESIGN ITERATION LOCAL (no per-revision PR)
- Strategic design + review chains iterate LOCAL: edit-in-place in one worktree, ONE PR lands final converged doc. Avoid 25-PR sprawl. (`feedback_design_iteration_local`)

## Per-dispatch payload
- Topic: `<TOPIC>`
- Slug: `<SPEC-SLUG>`
- Scope: `<SCOPE>`
- Phase: `<PHASE>`
- References to study: `<REFERENCES>`

## Definition of done
- [ ] spec at `docs/engineer/specs/<SPEC-SLUG>.md`
- [ ] B/A/A+ rubric section present with falsifiable criteria
- [ ] OSS references cited with version + sha + license
- [ ] self-host filter applied explicitly
- [ ] reviewer subagent cleared (no auto-skip — specs are load-bearing)
- [ ] release-notes fence present
- [ ] no banned phrases, no signatures
- [ ] memory rules cited
- [ ] PR opened against `main`; worktree removed after merge

## RECURRING-FAILURE TRAPS

1. **`gh pr create` / `gh pr edit` MUST use `--body-file`** per `CLAUDE.md` §CI gates "PR body hygiene". HEREDOC bodies escape backticks.
2. **GH base-sha drift workaround** per #343 (fix #347 merged): tag release-notes with `[DOCS]` for spec-only PRs to skip check-tdd.
3. **Release-notes fence ALWAYS required** per `feedback_release_notes_fence_missing`. Spec PR body MUST include a triple-fence ` ```release-notes ` block with `[DOCS] one-line summary (#issue)` inside. Without the fence, `check-scorecard.sh` falls through to the error branch even on `[DOCS]` PRs — the category-exempt branch only fires when the fence is present.
4. **Scorecard rubric in spec MUST use bare citation tokens** per `feedback_scorecard_citation_token_outside_backticks`. The A+ rubric you ship in the spec will be copy-pasted into the implementer PR body. Wrapping `TestX` or `file.go:42` in backticks in the spec rubric template propagates invisible-token failures to the implementer. Write tokens bare in the spec rubric scorecard template.
