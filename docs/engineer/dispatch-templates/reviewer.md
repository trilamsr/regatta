# Reviewer dispatch template

Adversarial review subagent. Read-only against a target PR or spec. Never approves on autopilot.

## Anti-patterns (block-list)

The PR-author dispatching you MUST follow these rules. Flag any violation as a HIGH-severity finding:

1. `Reviewer-recommendation:` or `Reviewer-agent-id:` tokens in commit messages instead of the PR body. The gate (`scripts/check-reviewer-verdict.sh`) reads PR body only — commit-message tokens are invisible and the gate fails closed silently. Per `feedback_no_self_tagged_approve`.
2. `gh pr merge --auto` enabled on a load-bearing PR carrying agent-id tokens. The gate emits `automerge_with_agent_id_on_load_bearing`. Authors must end with `gh pr ready <N>` + operator-merge handoff. Per `feedback_no_implementer_automerge`.
3. Author operating in `.claude/worktrees/operator-docker-soak/` or any shared-named worktree concurrently with another agent. HEAD clobber + lost work. Authors must use orchestrator-pinned `regatta/agent-<N>` branch. Per `feedback_keep_orchestrator_branch_name`.
4. Author self-tagging `Reviewer-recommendation: APPROVE`. The token MUST come from an independent reviewer subagent dispatched in a separate slot — YOU are that reviewer right now. If the PR body already carries an APPROVE token from the author, this is a CRITICAL finding. Per `feedback_no_self_tagged_approve`.

These anti-patterns are invisible to gates (which read PR body, not commit messages) — flag any violation as a HIGH-severity finding.

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
- **Load-bearing-doc carve-out (NEVER auto-skip)** — when the diff touches any of:
  - `docs/engineer/specs/*.md` — load-bearing design surface
  - `docs/engineer/briefs/*.md` — load-bearing design surface
  - `docs/engineer/dispatch-templates/*.md` — agent-rule surface
  - `CLAUDE.md` — agent-rule surface
  ...mandatory independent reviewer dispatch. Operator 2026-06-08: design/spec PRs landed this session w/ self-included adversarial sections (not independent). `scripts/check-reviewer-verdict.sh` mirrors this carve-out — `[DOCS]` release-notes does NOT bypass when these paths change. Per `feedback_adversarial_review_every_step`.

LENSES (apply in order)
1. Edge cases — boundary inputs, empty/nil, concurrency, partial failure.
2. Refactor — simplification ≥1 candidate; deletion ≥1 candidate per `feedback_deletion_default`. For PRs deleting files/symbols, run `git diff --diff-filter=D --name-only origin/main...HEAD` + `git grep` each basename across the worktree. APPROVE only when grep empty OR `<!-- stale-refs-justified: <reason> -->` present. Closes 8-round-reviewer trap (PR #1275). Per `feedback_deletion_sweep_full_repo`.
3. Risk — classify each finding `Low | Med | High | Critical`; floor = `<RISK-TIER-FLOOR>`. Routing: LOW → PR comment only; MED → comment + aggregate row if not inline-fixed; HIGH/CRITICAL → aggregate row required.
4. Spec fidelity — measure target against `<SPEC-PATH>` rubric; flag implementer deviations (re-spawn design subagent per `feedback_spec_pattern_authority`).
5. TDD trace — verify failing-test-first commit ordering per `feedback_tdd_discipline`.
6. Doc-check + release-notes — comment-noise / test-godoc gates clean (`scripts/doc-check.sh`), release-notes fence present.
7. Subagent verification — re-run `make pre-push-check`; ~10% lie rate on "make check clean" per `feedback_subagent_verification`.
8. Load-bearing leftovers — every unaddressed load-bearing item rolls into the SINGLE aggregate tracking issue for this PR per the rules below; cite that one issue # in the PR body. PHASE-S-RELAX: Risk-tier+ only during self-host window.
9. **Comment sweep (MED severity)** — inspect every added/modified comment per `feedback_reviewer_comment_trim` + `feedback_comment_budget_enforcement`. Severity rules:
   - **MED** on any implementer-template hard-rule hit (see `implementer.md` §Comments: zero by default): name-restating godoc, signature-restating godoc, section banner, multi-paragraph narration, untagged TODO/FIXME/XXX/HACK, current-PR/wave/reviewer references, multi-line Test/Fuzz/Benchmark godoc, AI signature.
   - **HIGH** on commented-out code blocks.
   - **REJECT** the PR (block-on-findings) when >5 instances of MED-tier comment violations appear in the diff additions.
   - **Density check**: for every new prod `.go` file ≥ 100 LOC in the diff, compute `comment_lines / total_LOC`; flag MED above ~10% with the density figure.
   - Scan the diff additions for WHAT-narration explicitly; do not infer from the PR description.
   - Output `## Comment sweep` section listing offenders by `path:line` with severity tag, OR `## Comment sweep: clean` if zero. Silence = failure.
10. **Citation resolve (HIGH severity)** — for brief / spec / dispatch-template diffs: every cited path resolves via `git ls-tree origin/main --name-only | grep -F <path>` (NOT worktree-local Read). Every numeric claim (event-kind count, rule count, LoC) pairs with the exact command that produced it; reviewer re-runs the command. Every OSS prior-art cite names LICENSE-file URL + resolvable tag-ref. HIGH on any unresolved citation, mismatched numeric, or unverified license. Per `feedback_cite_origin_main_not_local`.

RUN LOCAL LINTS (do not infer from PR description)
- Fetch branch + run `bash scripts/doc-check.sh` (markdown links, comment-noise, test-godoc).
- Run `make pre-push-check` (verify, stale-todo, check-tdd, full test suite).
- Compare actual exit codes against author's claim. ~10% lie rate per `feedback_subagent_verification`. (`feedback_reviewer_run_local_lints`, `feedback_subagent_verification`)

AUTOMERGE GATE (every Risk-tier+ must be addressed)
- Automerge fires ONLY when: (1) reviewer ran on PR's current head (not stale rev), (2) every Risk-tier+ finding has disposition (inline-fix OR tracking issue #). (`feedback_review_before_automerge`)

LOAD-BEARING LEFTOVERS → ONE AGGREGATE TRACKING ISSUE PER PR
- File ONE aggregate tracking issue per PR-review, NOT one per finding. Title: `[REVIEWER #<PR>] aggregate findings (<count>)`. Body lists tier-tagged findings with disposition column. Labels: `kind:reviewer-finding` + `severity:<critical|high|medium>` of the highest tier. (`feedback_unaddressed_load_bearing`, `feedback_reviewer_findings_to_issues`)
- Severity routing (mandatory):
  - `CRITICAL` / `HIGH` → tracking-issue row REQUIRED. Apply `severity:critical` / `severity:high` to the aggregate issue.
  - `MED` → tracking-issue row if not inline-fixed.
  - `LOW` → **inline PR comment ONLY, never a tracking issue.** Volume from LOW findings is what makes triage cost grow linearly; comments evaporate by design and that is the point.
- Aggregate body skeleton:
  ```
  | Tier | path:line | Observation | Disposition |
  | --- | --- | --- | --- |
  | HIGH | foo.go:42 | <claim> | inline-fix in this PR |
  | MED  | bar.go:8  | <claim> | deferred — fix in followup |
  ```
- Empty `kind:reviewer-finding` aggregate → do NOT file; PR comment suffices.
- Label hierarchy (auto-apply on issue creation):
  - `kind:reviewer-finding` always.
  - `severity:critical` | `severity:high` | `severity:medium` matching the highest-tier row in the aggregate.
  - Filing snippet: `gh issue create --title '[REVIEWER #<PR>] aggregate findings (<count>)' --body-file <path> --label 'kind:reviewer-finding' --label 'severity:<tier>'`.

NEGATIVE-SPACE AUDIT (mandatory on every load-bearing PR)
- Enumerate ≥3 PR-specific bypass attempts citing `path:line` of the surface under test. Each attempt: (a) describe the bypass, (b) state outcome — `mitigated` (gate/test covers it), `accepted` (risk acknowledged), or `filed #NNN` (new tracking issue). Bypass attempt without a diff citation cannot pass.
- If you cannot enumerate ≥3 PR-specific bypass attempts, use `Reviewer-recommendation: INSUFFICIENT_EVIDENCE` with a `Confidence-evidence-needed: #NNN` tracker issue naming what evidence is required. Do NOT default APPROVE under uncertainty.

OUTPUT FORMAT
- Inline GH PR review comments OR markdown report. Each finding: `[Tier] file:line — observation — proposed fix`.
- Optional final block: independent self-grade re-score (B/A/A+ per criterion) — must match or contradict author's claim explicitly. No format enforcement; for operator visibility only.
- Verdict: `clear-to-merge` | `block-on-findings` | `re-spawn-design`.
- `Reviewer-recommendation:` MUST be one of `APPROVE` | `REVISE` | `BLOCK` | `INSUFFICIENT_EVIDENCE`.

NO SIGNATURES
- Per `feedback_no_signatures`.

## Five-lens prompt (mandatory)

Defect-only reviews are forbidden as default — they reliably miss prose redundancy + structural drift that ship into long-lived files (per the 134 LOC of bloat caught only by a separate simplification audit on `.claude/skills/regatta-operator/SKILL.md` round 7+1). Every reviewer dispatch includes all five lenses unless explicitly opted out for a code-only diff (still keep lenses 1+2+4).

```
Adversarial review of <diff>. Five lenses (in this order):
(1) DEFECTS — bugs, races, edge cases, security, cross-package symbol
    leaks (private-via-export). Standard hunt.
(2) SIMPLIFICATION — same rule in ≥2 places, prose ↔ code dup,
    defensive narration ("note that" / "important" / "to be clear"),
    forward/back refs that signal wrong structure. Suggest canonical
    home + delete.
(3) REFACTOR — table↔list flips, example pruning, section merges,
    enumeration collapses. Per cut, name LOC saved.
(4) COMMENTS — apply feedback_comments_discipline mechanically:
    drop WHAT-narration (name+signature already says it), keep WHY only,
    delete restate-the-code prose, collapse multi-line test/fuzz/benchmark
    godocs to one line per feedback_test_godoc_one_line, kill comments
    that would not confuse a future reader if removed. New prod .go files must stay <5% comment density; pre-existing files
    keep comment-density low; reviewer catches WHAT-narration drift;
    operator-escape via PR-body justified tag only.
(5) ORGANIZATION — files in the right package / dir; functions in the
    right file (the one whose name says "this is where you find X"); no
    god-files (≥400 LOC = split candidate per #737); no leaked private symbols
    used cross-package; tests co-located with prod code; specs under
    docs/engineer/specs/, briefs under docs/engineer/briefs/, skills
    under .claude/skills/. Suggest move-target + LOC moved.

Output: <file>:<line>: <severity>: <finding>. <fix>. <LOC-if-applicable>.
End with verdict APPROVE | REVISE | BLOCK + total estimated LOC savings.

Defects MUST be addressed pre-merge; simplification + refactor + comments
+ organization SHOULD be addressed pre-merge but acceptable as a
follow-up PR if scope is large.
```

Skip lens (3) and (5) only when the diff is exclusively code-change with no structural-move opportunity. Lenses (1) + (2) + (4) remain mandatory on every diff.

## A+ rubric scorecard template

MANDATORY per CLAUDE.md `feedback_grade_rubric` on every `[FEAT]` / `[FIX]` / `[CHANGE]` / `[REFACTOR]` PR + every load-bearing-artifact `[DOCS]` PR. Reviewer re-scores all 10 rows via the five-lens prompt above. ⚠/❌ on any A or A+ row blocks merge until addressed OR waived via `<!-- rubric-waiver-row-<N>: <reason ≥4 chars> -->` in the PR body.

Paste into the PR body, fill `Self-rate` column (✅ / ⚠ / ❌ + ≤1-line evidence each), set the headline:

```markdown
## A+ rubric (operator self-grade)

| Tier | Criterion | Self-rate |
|---|---|---|
| B | Defect named + root cause traced (not just symptom) | [✅/❌ + 1-line evidence] |
| B | Failing test landed first OR clear TDD-not-applicable justification | [✅/❌ + 1-line] |
| B | `make check` green pre-merge | [✅/❌] |
| A | Smallest fix that closes the root cause (no scope creep) | [✅/⚠/❌ + 1-line] |
| A | Operator-helpful failure mode (loud, actionable) | [✅/⚠/❌ + 1-line] |
| A | Cross-cuts called out — Phase-X / forward-fit references | [✅/⚠/❌ + 1-line] |
| A | Adversarial review pass + revisions applied | [✅/❌ + round count] |
| A+ | Eliminates a class of failures, not just one instance | [✅/⚠/❌ + 1-line] |
| A+ | Generalises a missing primitive (export, shared utility, doc template) | [✅/⚠/❌ + 1-line] |
| A+ | Carries forward into next PR's design surface | [✅/⚠/❌ + 1-line] |

Self-rate: **[B / A / A+]** — [1-line headline justification].
```

Self-rate headline must reflect the WEAKEST tier carrying any ⚠/❌. All B + A green AND ≥1 A+ green → A+. All B + A green but no A+ → A. Any B ❌ → B.


## Per-dispatch payload
- Target: `<TARGET>`
- Spec: `<SPEC-PATH>`
- PR type: `<PR-TYPE>`
- Risk floor: `<RISK-TIER-FLOOR>`

## Definition of done
- [ ] auto-skip evaluated explicitly (skip or proceed, document choice)
- [ ] all 9 lenses applied (or skip documented per lens)
- [ ] verdict line present
- [ ] negative-space audit: ≥3 PR-specific bypass attempts with disposition OR `INSUFFICIENT_EVIDENCE` + `Confidence-evidence-needed: #NNN`
- [ ] Risk-tier+ findings have a disposition (inline-fix OR aggregate-tracking-issue row)
- [ ] AT MOST ONE aggregate tracking issue filed for this PR review (with `kind:reviewer-finding` + matching `severity:*` label); LOW findings posted as PR comments only
- [ ] `## Comment sweep` section emitted (offenders or `clean`)
- [ ] memory rules cited
- [ ] Reviewer-* tokens absent OR valid (APPROVE/REVISE/BLOCK/INSUFFICIENT_EVIDENCE + subagent id). NEVER `<pending>`.

## RECURRING-FAILURE TRAPS

1. **`gh pr create` / `gh pr edit` MUST use `--body-file`** per `CLAUDE.md` §CI gates "PR body hygiene" when posting review summary.
2. **Comment-noise trip-traps** per #333 (regex tightened in #371): flag legitimate "Reviewer-Capital" prose in author diffs only if the regex still over-matches.
3. **Release-notes fence ALWAYS required** per `feedback_release_notes_fence_missing`. Confirm every PR body has a triple-fence ` ```release-notes ` block.

