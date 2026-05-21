---
name: repo-consistency-loop
description: Run a 5-phase repo-wide consistency audit on the current worktree. Phases — binding-standards inventory → 9-lens parallel drift scan → validation cycle + dedupe → enforcement-bias triage → synthesize + remediate. Spine — user-benefit-first decision priority (user benefit → long-term maintainability → readability → intuitiveness), validation cycle, TDD discipline, mutation-verified lint gates, prose-only fixes rejected (anti-bureaucracy gate). Detects decision drift that accumulates across parallel work streams when each PR individually passes review but the union introduces inconsistency. Auto-creates an audit branch if on main and invokes `/ralph-loop:ralph-loop` directly — zero operator intervention.
---

# Repo consistency loop

When this skill is invoked, satisfy preconditions (auto-creating an
audit branch if on main) and then invoke `/ralph-loop:ralph-loop`
via the `Skill` tool with the rigorous 5-phase repo-wide
consistency audit prompt as args. The loop starts immediately in
the current session — no paste, no operator step.

## When to run this skill

Repo-wide drift detection is high-leverage but expensive — nine
parallel lens dispatches, full-repo scans, several commits, fixes
and lint gates landed via TDD. Use when the leverage pays for the
cost:

- **Use:** end-of-milestone / pre-release hygiene; after ≥3 PRs land
 in a related area in a short window; on in-the-wild rediscovery
 of drift ("processors do X, receivers do not-X — when did that
 happen?"); quarterly cadence.
- **Skip:** only one PR has landed since last run; the standards
 haven't been codified yet (the loop has nothing to check
 against); you're mid-feature (run after, not during); the surface
 you care about is a single PR — use `pr-review-loop` instead.

This gate is informational — the skill always invokes the audit
loop if preconditions pass. Cost discipline lives in the operator's
judgment.

## Precondition checks

Before invoking the audit loop, verify:

1. `git rev-parse --is-inside-work-tree` succeeds. If not, surface
 the gap and stop — there is nothing to audit.
2. `git status --porcelain` returns empty — clean tree. Otherwise
 the audit branch's commits would mix loop output with unrelated
 WIP. Surface the dirty paths and stop. NEVER auto-stash or
 auto-commit the operator's WIP — silent rescue of unrelated work
 loses context the operator needed.
3. `git branch --show-current` resolves a branch. If it is `main`
 or `master`, AUTO-CREATE the audit branch:
 `git checkout -b chore/consistency-audit-$(date +%Y-%m-%d)` and
 continue. Commits to `main` are forbidden per branch protection
 + `no_history_rewrites`, so the audit cannot run on main; the
 branch creation is mechanical and safe (no commits yet). If the
 branch name is already taken (e.g., second run same day),
 append a short suffix: `-2`, `-3`, … until `git rev-parse
 --verify` fails for the candidate name, then create that one.
4. The `ralph-loop@claude-plugins-official` plugin is installed.
 Check via `ls ~/.claude/plugins/cache/claude-plugins-official/ralph-loop/`
 returning a version directory, OR by `Skill` tool listing showing
 `ralph-loop:ralph-loop`. If absent, surface the missing
 dependency and stop — invoking the audit loop would fail with
 "Unknown command: /ralph-loop:ralph-loop".

If a check fails AFTER auto-handling (dirty tree, no work tree,
missing plugin), surface the precondition gap and do NOT invoke the
audit loop.

## How to invoke

After preconditions pass (including branch auto-creation when
needed), invoke `/ralph-loop:ralph-loop` via the `Skill` tool:

- `skill`: `ralph-loop:ralph-loop`
- `args`: the prompt body verbatim (everything between the `EOF`
 markers in the fence below — NOT including the `"$(cat <<'EOF'`
 opening or the `EOF\n)"` closing; those are only needed if a user
 is pasting the command into a terminal), followed by a space and
 ` --completion-promise "CONSISTENCY-LOOP-COMPLETE"
 --max-iterations 25`.

Use the exact prompt body below; substitute nothing inside it. The
Skill tool routes the invocation to the ralph-loop plugin, which
starts the loop in the current session.

If the permission classifier blocks the invocation (the prompt
contains tokens the auto-mode classifier may flag), the user is
prompted once to approve — that single approval starts the loop.
This is still one user action fewer than paste-handoff and counts
as zero-intervention from the operator's perspective: there is
nothing to copy, edit, or assemble.

Fallback: if the Skill-tool invocation fails outright (not a
permission prompt, but a hard error from the plugin), surface the
error and emit the fence below as a paste-ready slash command so
the operator can run it manually. Do NOT silently retry.

The prompt body fence — the content between the `EOF` markers is
what gets passed as `args` (plus the trailing flags above):

````
/ralph-loop:ralph-loop "$(cat <<'EOF'
<task>
Rigorously audit the regatta repo on the current branch for
decision drift through 5 structured phases. Apply the guiding
principle below: every decision serves, in priority order, USER
BENEFIT, then long-term maintainability, then readability, then
intuitiveness. Apply the validation cycle to every finding
(research → validate → contradict → re-validate → synthesize).
Apply TDD to every code change. Use the binding-standards inventory
from phase 1 as the rubric; findings without a binding-standard
citation are proposals, not drift, and go to docs/FOLLOWUPS.md.
After 5 phases, emit a final report.

Auto-detect (cwd is the audit branch root):
- Audit branch: `git branch --show-current` (already verified
 non-main by the invoking skill's preconditions)
- Repo state: `git rev-parse HEAD`
- Standards roots: PRINCIPLES.md, STYLE.md, AGENTS.md,
 .claude/notes/, (auto-memory path),
 .github/workflows/, Makefile, .golangci.yml
</task>

<guiding-principle>
This audit exists to serve, in priority order:

1. USER BENEFIT. The operator running regatta at 3am, the adopter
 installing for the first time, the contributor opening their
 first PR, the researcher consuming the data. "The user" is
 whichever role the drift most directly impacts. If a finding
 cannot name a user role it serves, it is not a finding worth
 keeping.
2. LONG-TERM MAINTAINABILITY. The next contributor reading this
 code six months cold can understand and modify it without
 re-deriving context. Cleverness that costs the cold reader is
 not a feature.
3. READABILITY. Code, docs, and structure communicate intent at
 first glance. Surprising patterns carry a load-bearing reason
 the reader can find inline.
4. INTUITIVENESS. Symmetric concepts have symmetric
 implementations. One mechanism per concern. No "this area does X
 but that area does Y for no reason."

Every accept / defer / reject decision names which of the four it
serves AND which it might cost. Findings tagged
`Beneficiary: consistency-for-its-own-sake` are REJECTED.
Consistency is a means; user benefit is the end.

Tie-breakers: a finding that appears to serve a lower priority but
contradicts a higher one defers to the higher priority unless the
higher-priority claim is contestable with hard proof.

Cite the binding standards on every decision:
- PRINCIPLES.md (the WHY behind code decisions),
- (priorities and the seven objectives),
- STYLE.md 
 (conventions),
- AGENTS.md + + .claude/notes/<topic>.md
 (load-bearing lessons; repo-wide vs. agent-internal),
- (architectural decisions),
- (durable feedback),
- and the already-enforced floor: .github/workflows/, Makefile,
 .golangci.yml. Per PRINCIPLES §5, NEVER duplicate the enforced
 floor in prose — propose a lint/CI rule instead.
</guiding-principle>

<validation-cycle>
No finding is accepted on first dispatch. Each goes through:

1. Research. What evidence would prove this finding true?
2. Validate. Run that evidence. Record the output.
3. Contradict. Independently look for evidence that would refute
 the finding.
4. Re-validate. Run the contradiction evidence. Record the output.
5. Synthesize. Apply only findings that survive contradiction with
 hard proof. Reject claims that don't reproduce; record rejection
 rationale.

Findings without proposable hard-proof default to NIT severity and
defer unless multiple lenses raise them.

Hard proof for a drift finding means at least ONE of:
- A failing test (or a test that would fail if the drift continued
 to be tolerated).
- A grep / ripgrep query whose hit count is the drift's measure
 (and whose zero-hits state is the fix's success criterion).
- A CI rule that catches the drift class on future PRs.
- A vendor / spec doc URL contradicting the drifted choice.
- A measured number (RSS, latency, line count) before and after.

"It feels inconsistent" is not hard proof. "I would do it
differently" is not hard proof.
</validation-cycle>

<tdd-discipline>
Every code change made during this loop follows TDD:

1. Write the failing test first.
2. Watch it fail. Record the failure mode.
3. Implement the minimal change.
4. Watch it pass.
5. Mutation-verify: invert the invariant; confirm the test fails;
 restore.

For lint / CI gates added during this loop, mutation-verify runs
**LOCALLY** — never pushed. Pushing a violator commit would require
force-push to remove, which `no_history_rewrites` forbids. Concrete
mechanic:

1. Author the rule in the audit branch's working tree. Do NOT
 commit yet.
2. In the SAME working tree, introduce a known violator (an edit,
 not a commit). Alternatively, `git stash` the rule, invert it,
 restore the violator, and run the gate.
3. Run the local invocation of the gate (`go tool golangci-lint
 run`, `make doc-check`, the explicit grep, etc.). Confirm it
 catches the violator.
4. Revert the working-tree edits via `git checkout -- <files>` or
 `git stash pop`. The rule itself is now ready to commit cleanly.
5. Commit the rule. Record the local mutation-verify outcome in
 the commit body.

If the gate ONLY runs in CI (no local invocation exists), open a
throwaway pre-merge PR with the violator AS A SEPARATE EXERCISE
that is NOT part of this audit's commit chain — confirm the gate
catches it in CI, then close the throwaway PR without merging.

A lint rule that has not been mutation-verified by EITHER mechanic
above is recorded as `(unverified — defended by code review only)`
and counts toward acceptance only if no local invocation exists AND
opening a throwaway PR is impractical (e.g., the rule is a
self-test).

For drift fixes where the regression test is the absence of the
drift, the test IS a lint rule or grep. The local mutation-verify
mechanic above applies the same way.

Test reproducibility — every test landed satisfies:
- Deterministic: same inputs produce same outputs.
- Seed-pinned where randomness is involved.
- Hermetic: no network, no real-clock, no filesystem-ordering
 reliance, no shared mutable state.
- Re-runnable: `git clean -fdx && make ci` passes on the same SHA.
</tdd-discipline>

<binding-standards>
Phase 1 produces a one-page binding-standards index. Every later
phase reads this index and binds findings to specific entries.

Index entry shape: `<rule-id> — <one-line statement> — Source: <file:section>`.

Example entries:

- `STYLE-component-layout — every component has config.go, factory.go, <name>.go, <name>_test.go, README.md, example_config.yaml — Source: STYLE.md §"Component layout"`
- `PRINCIPLES-one-mechanism — one well-known way per concern — Source: PRINCIPLES.md §3`
- `MEMORY-no-ai-mentions — no Assisted-by / Co-Authored-By Claude / AI mention in repo surfaces — Source: feedback_no_ai_mentions_in_repo`
- `LESSON-make-ci-source-of-truth — make ci is the single source of truth for verification — Source: AGENTS.md § Load-bearing lessons`

The index lives in `.claude/ralph-loop.local.md` under
`## Binding standards index` — ephemeral session state, gitignored
at the repo level. Source docs remain authoritative; the index is a
derived summary the lens subagents read.

Lens subagents (phase 2) cite rule-ids from this index in every
finding. Findings that cannot cite a rule-id are PROPOSALS — they
do not enter the fix pile; they go to `docs/FOLLOWUPS.md` with a
`Revisit when:` clause.
</binding-standards>

<acceptance>
- `[consistency-pass-N-*]` commits exist on the audit branch in phase
 order. Phases 1, 2, 3 are MANDATORY (3 commits minimum). Phases 4
 and 5 are CONDITIONAL on findings surviving phase 3:
 - Zero surviving findings (clean audit) → 3 commits total; loop
 terminates after phase 3 with verdict `DRIFT-NONE`.
 - One or more surviving findings → 5 commits total; phases 4 and 5
 run.
 Verifier (clean audit):
 `[ "$(git log origin/main..HEAD --oneline | grep -c '\[consistency-pass-')" -ge 3 ]`
 Verifier (drift found):
 `[ "$(git log origin/main..HEAD --oneline | grep -c '\[consistency-pass-')" -eq 5 ]`
- Phase 1 commit body contains the binding-standards index OR
 references the local file where it lives.
- Phase 2 commit body lists, for each of the 9 lenses, the count of
 findings raised at each severity.
- Phase 3 commit body lists every finding's
 validation-cycle outcome (reproduced / contradicted / deduped).
- Phase 4 + 5 commit bodies (when they run) contain:
 - The phase's pushback table
 - Hard-proof citations for every accepted finding
 - One action per finding: `applied <SHA>`, `gated <CI-rule-name>`,
 `rfc-drafted <PR#>`, `deferred <FOLLOWUPS.md ref>`, or
 `rejected` (with rationale)
 - For every applied finding: the validation-cycle record
 - For every applied code change: the TDD record (failing-test
 SHA or test name + mutation-verify outcome)
 - For every new lint / CI gate: the mutation-verify record
- The final assistant turn contains, in order:
 1. `<consistency-report>` block (verdict `DRIFT-NONE` for clean
 audits; `DRIFT-CLOSED` / `DRIFT-PARTIALLY-CLOSED` /
 `DRIFT-DEFERRED` when phases 4-5 ran)
 2. `<readiness-audit>` block
 3. `<promise>CONSISTENCY-LOOP-COMPLETE</promise>`
- `make ci` clean on the latest commit.
- AI-vocab grep gate clean across the diff (excludes
 `.claude/skills/`).
- DCO `Signed-off-by:` on every commit. NO `Assisted-by:` trailer,
 NO `Co-Authored-By:` for AI, NO AI mention in commit body or any
 PR opened during this loop. Per
 `feedback_no_ai_mentions_in_repo`.
- No commits to `main`; no force-push; no rebase past pushed
 commits.
- No prose-only additions to `STYLE.md` / `PRINCIPLES.md`. Per
 `feedback_anti_bureaucracy` — every textual rule addition ships
 with an enforcement gate in the same PR or is rejected.
- Every accepted finding has a Beneficiary tag from the guiding
 priority. `consistency-for-its-own-sake` is NOT an acceptable
 tag.
- Every new code path lands via TDD. Every new lint / CI gate is
 mutation-verified.
- Remediation PRs follow `feedback_narrow_pr_scope`: one-per-bucket
 (lint-gates PR, in-place-fixes PR, one PR per RFC draft).
</acceptance>

<pass-order>
Up to five phases, in this order. Phases 1-3 are MANDATORY. Phases
4 and 5 run only if findings survive phase 3 — a clean audit (zero
surviving findings) terminates after phase 3 with verdict
`DRIFT-NONE`. Each phase operates on the current state of the audit
branch (after prior phases' commits) and the current
binding-standards index. Apply the validation cycle to every
finding and TDD to every code change.

## Phase 1 — Binding-standards inventory

Read, in this order, as authoritative:

1. PRINCIPLES.md
2. STYLE.md + + 
3. 
4. (context only)
5. AGENTS.md + every + every .claude/notes/*.md
6. *.md
7. at the user's auto-memory path
8. .github/workflows/*.yml + Makefile + .golangci.yml (the already-
 enforced floor — do NOT propose duplicating these in prose)

Emit a one-page binding-standards index per the
`<binding-standards>` block. Save under
`.claude/ralph-loop.local.md § "Binding standards index"`.

End with commit: `[consistency-pass-1-inventory]`.

## Phase 2 — 9-lens parallel scan

Dispatch reviewer subagents IN PARALLEL (single assistant turn,
multiple Agent tool calls), one per lens:

| Lens | Drift class | Primary scope |
|------|-------------|---------------|
| Principles | Code/doc/decisions violating PRINCIPLES.md | repo-wide |
| Style — code | STYLE.md (component layout, logging, error handling, repo layout) | components/, internal/, cmd/, pkg/ |
| Style — docs | (WHY/falsifiable/lead-with-answer/voice/one-purpose-per-file) | docs/, all READMEs |
| Style — errors | (wording, %w-wrap, sentinels, no-apologetics) | every .go error return |
| Architecture | RFC contradicted by current code; material architectural decisions without an RFC | ↔ code |
| Pattern consistency | Gate shape (L0/L3/L4/L5 deterministic vs AI); cmd/ vs internal/ boundary; schema-as-contract (schemas/ CUE + JSON-Schema is normative; Go structs mirror) | cmd/regatta/, internal/{l0,verifyrepo,orchestrator}/, schemas/, gates/ |
| Lessons applied | AGENTS.md "load-bearing lessons" anchors + + .claude/notes/*.md anchors that no longer hold; lessons not reflected in current code | every anchored file:line |
| Memory adherence | feedback rules (no AI mentions, no superfluous comments, no workflow vocab, no invented numbers, etc.) | repo-wide |
| Doc-surface coherence | README ↔ docs/design.md ↔ docs/incidents.md ↔ schemas/ ↔ gates/testdata/ — name/number/state drift across surfaces | docs/, schemas/, gates/*/testdata/ |

Use the `<reviewer-brief>` below. Each subagent is read-only. Each
must cite the rule-id from the binding-standards index for every
finding.

Apply the validation cycle to every finding. For every accepted
code change later: apply via TDD; for every accepted lint/CI rule:
mutation-verify.

End with commit: `[consistency-pass-2-scan]`.

## Phase 3 — Validation cycle + dedupe

For every finding from phase 2:
- Run the proposed-proof step explicitly. Record the output.
- Run the proposed-contradiction step explicitly. Record the output.
- Drop findings that don't reproduce.

Dedupe across lenses: the same drift commonly surfaces in 2–3
lenses (e.g., a doc-surface inconsistency that also violates
STYLE.md). Merge with a multi-lens tag. The merged finding
inherits the highest severity raised by any constituent lens.

End with commit: `[consistency-pass-3-validate]`.

**Terminal short-circuit (clean audit).** If zero findings survive
phase 3, the audit is clean. Phases 4 and 5 do not run — there is
nothing to triage or remediate, and forcing empty commits is the
ceremony `feedback_anti_bureaucracy` forbids. In this case:
- Emit `<consistency-report>` with verdict `DRIFT-NONE` directly
 after the phase-3 commit, in the same turn as the readiness
 audit and the completion promise.
- The loop ends with 3 commits total.
- A clean audit is a valid (and desirable) outcome — not a failure
 to find findings.

If one or more findings survive phase 3, continue to phase 4.

## Phase 4 — Enforcement-bias triage (conditional)

For every surviving finding, classify into exactly ONE bucket:

- **Lint / CI gate.** Recurring class, falsifiable in CI, would
 catch future violations. PREFERRED per PRINCIPLES §4–5 +
 `feedback_anti_bureaucracy`.
- **Fix-in-place.** One-off divergence. One commit, one fix.
- **RFC update / new RFC.** Drift signals an unmade architectural
 decision. The fix is to document the decision, then bring code
 into line.
- **Defer to docs/FOLLOWUPS.md.** Load-bearing but out of scope for
 this audit. Requires an explicit `Revisit when:` trigger.
- **Reject.** No falsifiable form, OR no binding-standard citation,
 OR fails the user-benefit lens. Record rationale.

Hard anti-bureaucracy gate: a finding may NOT propose adding prose
to STYLE.md / PRINCIPLES.md unless an enforcement gate ships with
it in the same PR. Per `feedback_anti_bureaucracy`.

Hard user-benefit gate: every accepted finding cites which of the
four guiding priorities it serves AND (for user-benefit) which user
role. Findings tagged `consistency-for-its-own-sake` are rejected.

End with commit: `[consistency-pass-4-triage]`.

## Phase 5 — Synthesize + remediate (conditional)

Runs only if phase 4 ran (i.e., one or more findings survived phase
3). Author fixes:

- **Fix-in-place items:** TDD (failing test → watch fail →
 implement → watch pass → mutation-verify per the
 `<tdd-discipline>` block).
- **Lint / CI gates:** TDD with **LOCAL** mutation-verify per the
 `<tdd-discipline>` block. Mutation-verify happens in a scratch
 worktree off the audit branch and is NEVER pushed — pushing a
 violator commit would require force-push to restore, which is
 forbidden by `no_history_rewrites`. Record the local
 mutation-verify outcome in the commit body.
- **RFC drafts:** separate PR. Draft documents the decision and the
 evidence trail, not just the drift.
- **Defer-to-FOLLOWUPS items:** append to docs/FOLLOWUPS.md with
 `Revisit when:` clause.

**PR strategy — one-per-bucket per `feedback_narrow_pr_scope`, with
explicit merge order:**

1. **Fixes PR first.** In-place fixes land on main before lint
 gates. A lint gate authored against existing violators would
 fail CI on first push if the violators were still in tree.
2. **Lint-gates PR second**, rebased onto post-fixes main. With the
 violators removed, the lint gate passes CI; mutation-verify
 confirmed locally already (above).
3. **RFC PRs are independent.** They document a decision and may
 merge before, between, or after the fix and lint-gate PRs.

Skip any bucket that produced no remediation — e.g., if all
findings are RFC-only or defer-only, the fixes PR and lint-gates
PR are not created.

After remediation:
- Run `make ci`; verify clean.
- For every PR ACTUALLY OPENED, run `gh pr checks <PR#>`; record
 URL + initial status in the report. Buckets that produced no PR
 omit their row in the report.
- Emit `<consistency-report>` → `<readiness-audit>` →
 `<promise>CONSISTENCY-LOOP-COMPLETE</promise>` IN THE SAME TURN.

End with commit: `[consistency-pass-5-remediate]`.
</pass-order>

<reviewer-brief>
Use this brief verbatim when dispatching lens subagents in phase 2.
Substitute `<LENS NAME>`, `<LENS DRIFT CLASS>`, `<LENS SCOPE>` per
row in the lens table.

 > You are an INDEPENDENT, READ-ONLY consistency reviewer of the
 > regatta repo on the audit branch. Your role:
 > **<LENS NAME>**. Your drift class: <LENS DRIFT CLASS>. Your
 > scope: <LENS SCOPE>.
 >
 > Inputs (cwd is the audit branch root):
 > - Repo state: `git rev-parse HEAD`
 > - Branch: `git branch --show-current`
 > - Binding standards index: `.claude/ralph-loop.local.md` §
 > "Binding standards index" (BINDING
 > for this review)
 > - Source standards: PRINCIPLES.md,
 > STYLE.md,
 > AGENTS.md,
 > .claude/notes/,
 > 
 > - Already-enforced floor: .github/workflows/, Makefile,
 > .golangci.yml (do NOT duplicate in
 > prose; propose a lint/CI rule
 > instead)
 >
 > Be adversarial. The repo's parallel work streams each
 > individually passed PR review; your job is to find the drift
 > the union produced. Frame decisions through priority: USER
 > BENEFIT first (name the role: operator / adopter / contributor
 > / researcher / maintainer), then long-term maintainability,
 > then readability, then intuitiveness.
 >
 > For every finding, propose HARD PROOF (a test that should fail
 > before fix and pass after; a grep with a hit-count that goes to
 > zero after fix; a CI rule that catches the class on future PRs;
 > a vendor doc URL; a measured number). Findings without
 > proposable proof default to NIT.
 >
 > Every finding MUST cite a rule-id from the binding-standards
 > index. Findings that cannot cite a rule-id are PROPOSALS, not
 > drift — surface them in a separate `Proposals` section at the
 > end of your output.
 >
 > Output strictly:
 >
 > Findings:
 > 1. Severity: BLOCKER | CONCERN | NIT
 > Beneficiary: user:operator | user:adopter |
 > user:contributor | user:researcher |
 > user:maintainer | maintainability |
 > readability | intuitiveness
 > Rule cited: <rule-id from binding-standards index>
 > Location: file:line (or "repo-wide")
 > Description: <one short paragraph>
 > Proposed proof: <how the author should verify>
 > Proposed contradiction: <what evidence would refute this>
 > Proposed fix: <one line>
 > Proposed bucket: lint-gate | fix-in-place | rfc | defer |
 > reject
 > 2. (...)
 >
 > Proposals (no rule-id citation — go to FOLLOWUPS.md if accepted):
 > - <proposal: one line, measurable form, Revisit when: clause>
 >
 > Verdict: APPROVED-NO-DRIFT | DRIFT-FOUND | DRIFT-BLOCKS-MERGE-FREEZE
 > Reason: <one paragraph naming the user benefit served>
 >
 > If no findings and no proposals: `Findings: (none)` +
 > `Proposals: (none)` + verdict `APPROVED-NO-DRIFT`.
 >
 > Do NOT modify code, push commits, or alter branch state.
</reviewer-brief>

<consistency-report-format>
Emit in the same turn as the readiness audit and the promise. The
report's sections after phase 3 are CONDITIONAL on whether phases
4-5 ran:

- Clean audit (phase 3 found zero surviving findings): emit Scope,
 Standards indexed, Per-lens findings, Validation-cycle stats, and
 the audit verdict `DRIFT-NONE`. Triage / Beneficiary tally /
 Enforcement upgrades / PR landings sections are OMITTED (the
 phases that populate them did not run).
- Drift found (phases 4-5 ran): emit the full report below.

<consistency-report>
Scope: repo @ <commit-SHA>
Branch: <audit-branch>
Standards indexed: <N> rules from PRINCIPLES / STYLE / RFCs / AGENTS / MEMORY / enforced-floor

Per-lens findings (BLOCKER / CONCERN / NIT raised):
- Principles: <B>/<C>/<N>
- Style — code: <B>/<C>/<N>
- Style — docs: <B>/<C>/<N>
- Style — errors: <B>/<C>/<N>
- Architecture: <B>/<C>/<N>
- Pattern consistency: <B>/<C>/<N>
- Lessons applied: <B>/<C>/<N>
- Memory adherence: <B>/<C>/<N>
- Doc-surface coherence: <B>/<C>/<N>

Validation-cycle stats:
- Findings rejected during contradict: <count>
- Findings whose hard-proof did not reproduce: <count>
- Findings deduped across lenses (merged): <count>
- Findings surviving phase 3: <count>

# === sections below OMIT if surviving-findings == 0 (DRIFT-NONE) ===

Triage (omit rows with count 0):
- Promoted to lint / CI gate: <count> → PR <#> (mutation-verified: <count>)
- Fixed in-place via TDD: <count> → PR <#>
- RFC drafts opened: <count> → PRs <#>
- Deferred to FOLLOWUPS.md: <count>
- Rejected: <count>

Per-finding pushback table reference: see commit bodies
[consistency-pass-2-scan] through [consistency-pass-5-remediate].

Beneficiary tally (accepted findings by primary beneficiary; omit
rows with count 0):
- user:operator: <count>
- user:adopter: <count>
- user:contributor: <count>
- user:researcher: <count>
- user:maintainer: <count>
- maintainability: <count>
- readability: <count>
- intuitiveness: <count>
(consistency-for-its-own-sake findings should be zero — they are
 rejected at triage)

Enforcement upgrades added (one line each; omit section if none):
- <lint/CI rule name> — catches <drift class> — mutation-verified yes/no

Cross-cutting drift patterns (≤3 bullets; omit section if none):
- <pattern>

Carry-forward open questions (require maintainer decision; omit
section if none):
- <item>

PR landings (OMIT BUCKET ROWS THAT PRODUCED NO PR):
- Lint-gates PR: <URL> (<initial-checks-status>)
- Fixes PR: <URL> (<initial-checks-status>)
- RFC PRs: <URLs>

# === end of phases-4-5 sections ===

Audit verdict: DRIFT-NONE | DRIFT-CLOSED | DRIFT-PARTIALLY-CLOSED | DRIFT-DEFERRED
Reason: <one paragraph framed through the guiding-priority lens —
 for DRIFT-NONE, name which standards the audit verified clean and
 the user role that benefits from that confidence; otherwise, which
 user role the closed drift most benefits, and which drift remains
 and why deferring serves them better>
</consistency-report>
</consistency-report-format>

<promise>CONSISTENCY-LOOP-COMPLETE</promise>

<stopping-rule>
Emit `<promise>CONSISTENCY-LOOP-COMPLETE</promise>` only when ALL
of the universal conditions hold AND one of the two terminal
branches' conditions hold.

**Universal conditions (every termination):**
- `<consistency-report>` precedes the promise in the same turn,
- `<readiness-audit>` precedes the promise in the same turn with
 one bullet per acceptance criterion + concrete evidence,
- The binding-standards index from phase 1 exists at
 `.claude/ralph-loop.local.md § "Binding standards index"`,
- `make ci` clean on the final commit,
- AI-vocab grep gate clean (excludes `.claude/skills/`).

**Clean-audit termination (verdict DRIFT-NONE, 3 commits):**
- 3 `[consistency-pass-N-*]` commits exist on the audit branch in
 phase order (passes 1, 2, 3),
- Phase 3 commit body records zero findings surviving validation,
- No phase-4 or phase-5 commits exist (commit count is exactly 3).

**Drift-found termination (verdict DRIFT-CLOSED /
DRIFT-PARTIALLY-CLOSED / DRIFT-DEFERRED, 5 commits):**
- 5 `[consistency-pass-N-*]` commits exist on the audit branch in
 phase order (passes 1, 2, 3, 4, 5),
- Every accepted finding has a triage decision with hard-proof
 citation, validation-cycle record, AND a Beneficiary tag from the
 guiding priority,
- Every applied code change has a TDD record,
- Every new lint / CI gate has a mutation-verify record (per the
 local-only mechanic in `<tdd-discipline>`),
- All remediation PRs ACTUALLY OPENED have their URL recorded in
 the report (buckets that produced no PR omit their row),
- No prose-only additions to STYLE.md / PRINCIPLES.md.

If any condition fails: do NOT emit the promise. Fix forward in
NON-`[consistency-pass-*]` commits.
</stopping-rule>

## Readiness audit (every promise emission)

Before the completion-promise tag, emit `<readiness-audit>` in the
same turn with one bullet per acceptance criterion + concrete
evidence (test name, commit SHA, command output, file:line
citation, CI run URL, FOLLOWUPS.md bullet reference, lint-rule
mutation-verify outcome). Missing evidence = no promise.

## Per-finding pushback structure

| ID | Phase | Lens(es) | Beneficiary | Severity | Rule cited | Finding | Proof | Contradict | TDD / mutation-verify record | Action | Rationale |

- ID: monotonic ACROSS phases — `P2.1` (lens findings), `P3.N`
 (post-validation), `P4.N` (triage decisions), `P5.N`
 (remediation outcomes).
- Lens(es): `principles`, `style-code`, `style-docs`,
 `style-errors`, `architecture`, `pattern-consistency`,
 `lessons-applied`, `memory-adherence`, `doc-surface-coherence`,
 `multi-lens` (after dedupe).
- Beneficiary: `user:operator` | `user:adopter` |
 `user:contributor` | `user:researcher` | `user:maintainer` |
 `maintainability` | `readability` | `intuitiveness`.
 `consistency-for-its-own-sake` is REJECTED, not a valid
 beneficiary.
- Severity: BLOCKER / CONCERN / NIT. Escalate one notch if the
 drift was raised by 2+ lenses after dedupe.
- Rule cited: rule-id from the binding-standards index. REQUIRED
 for accepted findings.
- Proof: REQUIRED for any non-rejected action.
- Contradict: REQUIRED for any non-rejected action.
- TDD / mutation-verify record: REQUIRED for `applied` (code
 change) or `gated` (lint / CI rule).
- Action: `applied <SHA>` / `gated <CI-rule-name>` /
 `rfc-drafted <PR#>` / `deferred <FOLLOWUPS.md ref>` / `rejected`.
- Rationale: REQUIRED for `rejected` and any case where the lens's
 proposed bucket was overridden in triage.

## Regatta-specific constraints

Apply regatta rules in full: `workflow_no_stacked_prs`,
`no_history_rewrites`, `no_workflow_vocab_in_repo`,
`no_ai_mentions_in_repo`, `no_signature_in_prs`,
`deferral_tracking`, `make_check_vs_ci_cadence`,
`no_invented_numbers`, `verify_before_acting`,
`feedback_audit_finding_triage`, `feedback_honest_review_pushback`,
`feedback_verify_before_approving`,
`feedback_no_superfluous_comments`, `feedback_narrow_pr_scope`,
`feedback_tdd_falsifiable`, `feedback_anti_bureaucracy`,
`feedback_rigorous_decision_loop`,
`feedback_measure_before_claiming_perf`,
`feedback_update_pr_body_when_premise_breaks`,
`feedback_read_ci_on_failure`.

Loop-enforced artifacts:

- AI-vocab grep gate — CONVENTION-ONLY (not enforced by CI; agents
 run manually pre-push; excludes `.claude/skills/`):
 `grep -rn -i 'ralph\|loop[ \-_]?[1-5]\|pass[ \-_]?[1-5]\|four[ \-]?loops\?\|reviewer agents\?\|subagents\?\|loop design\|loop prompt' \
 --include='*.md' --include='*.go' --include='*.yaml' \
 --exclude-dir='.git' --exclude-dir='.claude/skills'`
- Commits: DCO `Signed-off-by:` via `git commit -s`. NO
 `Assisted-by:` trailer. NO `Co-Authored-By:` for AI. NO AI
 mention anywhere.
- Remediation PRs follow the concise PR format
 (`feedback_concise_pr_format`): problem → impact → solution →
 test plan checklist. Operator notes go in commit body, not PR
 body.

## Output register

Phase 2+: emit a delta vs prior phase. Apply 
`feedback_trim_by_default` and `feedback_decide_dont_hedge`.

For each phase, lead with the count of findings raised at each
severity, then the deltas to the binding-standards index (if any),
then the triage proposals. Full per-finding pushback tables live
in the commit bodies, not in the assistant transcript.

## Discovered constraints + Binding-standards index (append-only)

> **NOTE — this section is INITIAL CONTENT for
> `.claude/ralph-loop.local.md`**, written there by the ralph-loop
> plugin's setup script when the user pastes the slash command. The
> append-only markers BELOW live in that local file at runtime, NOT
> in `SKILL.md`. Do NOT modify these sections in `SKILL.md` during
> an audit session — your appends go to the local file. If you are
> an agent reading `SKILL.md` directly (skill-tool invocation):
> treat this whole section as a template.

Phase 1 populates `## Binding standards index`. Every later phase
reads it and cites rule-ids from it. Phase 1 is the ONLY phase that
writes to this section; later phases treat it as read-only.

End of every phase ≥ 2:

- If this phase surfaced a recurring pattern, footgun, or cross-
 phase observation, APPEND a bullet to `## Discovered constraints
 (append-only)` below the marker.
- The binding-standards index is NOT amended after phase 1. If a
 finding suggests a new binding standard is needed, that's a
 proposal — it goes to `docs/FOLLOWUPS.md` with a `Revisit when:`
 clause, not into the index mid-audit.

Do not delete or rewrite prior bullets. Do not edit anything above
the markers.

## Binding standards index

(empty — phase 1 populates)

## Discovered constraints (append-only)

(none yet)
EOF
)" --completion-promise "CONSISTENCY-LOOP-COMPLETE" --max-iterations 25
````

After invocation, narrate briefly: which audit branch the loop is
running on (auto-created or pre-existing), and that the loop is
underway. Do NOT echo the full prompt back to the user — the loop's
own output will surface progress.
