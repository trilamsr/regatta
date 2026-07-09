# Designer dispatch template

Design-doc subagent. Output: spec under `docs/engineer/specs/YYYY-MM-DD-<slug>.md`.

## Anti-patterns (block-list)

NEVER (apply to YOU as designer, AND propagate to any implementer you dispatch downstream):
1. Put `Reviewer-recommendation:` or `Reviewer-agent-id:` in commit messages. Those tokens belong in the PR BODY (honor-system since the mechanical gate was culled 2026-07-08). Per `feedback_no_self_tagged_approve`.
2. Enable `gh pr merge --auto`. End with `gh pr ready <N>` + operator-merge handoff. Per `feedback_no_implementer_automerge`.
3. Operate in `.claude/worktrees/operator-docker-soak/` or any shared-named worktree when ≥1 other agent uses it concurrently. HEAD clobber + lost work. Use orchestrator-pinned `regatta/agent-<N>` branch in the pre-created worktree. Per `feedback_keep_orchestrator_branch_name`.
4. Self-tag `Reviewer-recommendation: APPROVE` as the author. Independent reviewer subagent dispatches in a separate slot — author ends with `gh pr ready <N>` only. Per `feedback_no_self_tagged_approve`.

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

SELF-HOST FILTER
- Every claim filtered by "does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?". Keep → in scope. Defer → Phase X with explicit reopen-trigger (external customer ask OR 30-day-green). Per `docs/engineer/briefs/2026-06-01-self-host-first.md` §1.

ADVERSARIAL REVIEW ON SPEC
- After draft, spawn reviewer subagent (see sibling `reviewer.md`) targeting: simplification opportunities, deletion candidates, edge cases, risk tiers, OSS reuse the spec missed. Fix findings inline OR cite as deferred with reopen-trigger.
- **Mandatory independent reviewer before PR open**: designer MUST request a fresh `Agent(reviewer-subagent)` (NOT a self-included adversarial section) before opening the PR. Cite reviewer agentId + `Reviewer-recommendation: APPROVE` in PR body footer. Honor-system since the mechanical gate was culled 2026-07-08 — `[DOCS]` release-notes on specs/briefs/templates/CLAUDE.md still does NOT waive the independent review. Per `feedback_adversarial_review_every_step`.

DOC-CHECK
- No banned phrases (`scripts/doc-check.sh`, 11 tokens). Reword to falsifiable claims (version pin, benchmark, named reference). Pre-push grep mandatory. Per `CLAUDE.md` §CI gates "Banned-phrase gate".

PRE-COMMIT `make check` MANDATORY
- After editing brief/spec/template + before `git add`: run `bash scripts/doc-check.sh` AND `make check` (catches CUE schema + golden-byte-equal regressions + doc-link gate + Phase-X leak). Verify exit=0. Do NOT stage or commit on failure. Skipping → post-push gate failure → re-investigate + re-fix + re-push round-trip. Per `feedback_pre_commit_make_check`.

DELETION DEFAULT
- Spec answers "what got smaller?" Additions need A+ defense. Per `feedback_deletion_default`.

RELEASE NOTES
- Spec PR body needs ```release-notes ... ``` fence (typically `none (internal)` for design-only). Per `feedback_release_notes_fence_missing` + `CLAUDE.md` §CI gates "PR body hygiene".

NO SIGNATURES
- No `Co-Authored-By`, no AI footer. Per `feedback_no_signatures`.

MEMORY CITES
- Cite `<MEMORY-RULES>` in PR body footer + inline in spec where load-bearing.

OUTPUT-PATH SLUG MUST BE EXACT
- Dispatch prompt MUST specify exact `<SPEC-SLUG>` (date + canonical short slug). Plan-subagent picking own slug produces dup files (e.g. two near-identical date-stamped files differing only in punctuation of the short slug). (`feedback_plan_subagent_dup_files`)

CROSS-DOC LINK PHASING
- Sibling docs that cross-link each other (e.g. `docs/operator/foo.md` ↔ `docs/engineer/runbooks/foo.md`) fail doc-check per-PR because each PR sees only its own added file. Co-locate in ONE PR OR phase-land with strip-then-restore. (`feedback_cross_doc_link_phasing`)

DESIGN ITERATION LOCAL (no per-revision PR)
- Strategic design + review chains iterate LOCAL: edit-in-place in one worktree, ONE PR lands final converged doc. Avoid 25-PR sprawl. (`feedback_design_iteration_local`)

UMBRELLA SPEC → ONE TRACKING ISSUE WITH TASK-LIST CHECKBOXES
- A spec covering N slices files ONE umbrella tracking issue with a markdown task-list, not N pre-filed slice issues. GitHub auto-renders the checkboxes as a progress bar; sub-tasks are tracked without separate issues.
- Body skeleton (paste into the umbrella issue body, or directly into the spec PR body if a separate umbrella issue is not warranted):
  ```
  ## Slices
  - [ ] Slice 1: <name> — dispatch via <designer|implementer>
  - [ ] Slice 2: <name>
  - [ ] Slice 3: <name>
  ```
- Slice tracking issues are created ON DEMAND at dispatch time (one per implementer wave), NOT pre-filed up-front. Pre-filing N slice issues at spec-merge time floods the tracker and inflates triage cost. Labels for the umbrella: `kind:wedge` (cross-cutting) or `kind:feat` (single-feature umbrella) + the slice-cluster label (e.g. `regatta-on-arbitrary-repo`).

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
3. **Release-notes fence ALWAYS required** per `feedback_release_notes_fence_missing`. Spec PR body MUST include a triple-fence ` ```release-notes ` block with `[DOCS] one-line summary (#issue)` inside.
