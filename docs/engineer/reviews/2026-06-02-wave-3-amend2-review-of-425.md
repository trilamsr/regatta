# Third-tier adversarial review of PR #425 — wave-3 amend-to-amend

_Reviewer: adversarial subagent, 2026-06-02. Companion to PR #404 (research brief) → PR #409 (first-tier review) → PR #415 (first amendment spec) → PR #419 (second-tier review) → PR #425 (this amend-to-amend). Scope: review of `docs/engineer/specs/2026-06-02-wave-3-amend-to-amend.md` (183 lines, spec-only). Memory cites: `feedback_adversarial_review` (edge cases + refactor + risk + simplification; never auto-approve) · `feedback_pr_body_file_only` (PR bodies via `--body-file`) · `feedback_pr_body_release_notes_mandatory` (every PR body needs a release-notes fence)._

> **Verdict:** ADOPT-WITH-AMENDMENTS. N1 BLOCKER discharge from #419 is verified at the commit level; N2–N6 each have a concrete amendment-diff against #415; the pre-merge sequencing contract is operationally sound. **One factual error** in N5's amendment-diff (the brief contains no bare-numeric `§7/§8/§9` references at all — the "line 161 callout" is unsupported by the current brief state) and **one process gap** in the N4 sequencing contract (the four tracking issues remain unfiled at review timestamp — N4 is by-design landing-time enforcement against #415, but this PR cannot itself prove discharge). Both are correctable in-place on `spec/wave-3-amend-to-amend` without re-spawning the design subagent. Three new findings raised (P1 factual, P2 process, P3 refactor).

---

## 0. Per-lens audit

### Lens 1 — N1 verified closed by inline commit + spec confirmation?

**PASS.** Commit `0c28842` on `research/wedge-wave-3` rewrites both sites that #419 N1 flagged:

- Insight 4 (now brief §7 line 197): replaces "OpenInference + OpenLLMetry are winning the LLM-shaped extensions" with "OpenTelemetry GenAI semconv has won; OpenInference + OpenLLMetry are sibling attribute sets" — the regatta-native attribute set is now correctly named (GenAI semconv per W6 #213), with OpenInference demoted to integration-time bridging.
- Load-bearing follow-up (a) (now brief §8 line 212): rewrites "W6 OpenInference attribute emission" to "W6 GenAI-semconv→OpenInference bridge (integration-time only)."
- §4.2 first bullet (the third site originally fixed by `f4b8f9a`): re-anchored on OTel GenAI semconv with OpenInference as sibling.

Diff verified via `git show 0c28842 --stat`: 3 insertions / 3 deletions across the three sites #419 F2 + N1 named. The spec's §0 ledger and §3 audit-trail both cite the commit hash explicitly. The N1 discharge is recorded with the exact hash; no further amendment needed.

### Lens 2 — N2–N6 each concretely addressed?

| # | Severity | #425 amendment shape | Reviewer verdict |
|---|---|---|---|
| N2 (risk) | F6 footnote prominence | New A+ scorecard row in #415 §4 with positional rendering criterion ("above §4.2 narrative, not after §4.2's first bullet") | PASS — positional criterion is implementer-verifiable from the diff context. |
| N3 (risk) | Issue (4) owner + due-date + auto-downgrade | Names owner (autonomous-session-prompt operator anchored to `docs/engineer/autonomous-session-prompt.md`), due-date (90 days from spec-merge commit timestamp), and an auto-downgrade PR shape (`delta=+N/-0` close-comment string + one-line rewrite of §7 Insight 5 lead). | PASS — three falsifiers (owner, anchor, auto-downgrade shape) all named. The doc-path anchor for the owner is more durable than a human handle per `feedback_decision_priority` long-term > short-term. |
| N4 (load-bearing) | Pre-merge sequencing + per-issue `gh` search-term verifier | (a) Rewrites #415 §3 opening clause to make the file→paste-URLs→automerge ordering operator-actionable; (b) appends `gh issue list --search` line to each of the four bullets. | PASS — see P2 below for the process gap (four issues still unfiled). |
| N5 (edge) | Bare-numeric grep step + line-161 callout | Replaces the §5 Edge-case-2 bullet with explicit `grep -nE '§[0-9]' …` command, names line 161 as the bare-numeric site, names the grep output as the falsifier. | PARTIAL — see P1 below. The grep step is sound as a defensive pattern but the line-161 factual claim is unsupported by the current brief state. |
| N6 (refactor) | F3 falsifiers as 3-bullet sub-list | Replaces the parenthetical (a)(b)(c) with three bullets under "**Bet-against row.**"; counter-evidence stays prose. | PASS — three falsifiers now scannable in a single visual pass; the prose / bullet split honors the discursive-vs-enumerable distinction. |

### Lens 3 — Pre-merge sequencing contract — operationally sound?

**PASS with one process gap (P2 below).** The sequencing contract names the exact `gh` invocation (`gh pr edit --body-file`) for the URL paste and explicitly contests the pr-lint race ("if pr-lint requires a non-empty release-notes fence to land, the operator may be tempted to paste `none` first, automerge, then file the issues post-merge — defeating the F11 discharge per #409"). The contract is non-negotiable per `feedback_review_before_automerge` + `feedback_pr_body_release_notes_fence`.

Three reasons the contract is sound:

1. **Operator-actionable, not aspirational.** "File 4 issues → paste 4 URLs into release-notes fence → enable automerge" is three discrete steps the operator can execute serially. Each step has a verifiable artifact (the issue URL, the edited PR body, the automerge state).
2. **Per-bullet `gh` search-term.** Each of the four issues carries its own `gh issue list --search "…"` verifier — the implementer doesn't have to invent a search predicate. Per `feedback_subagent_verification` (~10% lie rate), the search terms are the audit trail.
3. **Defection mode is named.** The amend-to-amend's §5 Edge-case-2 explicitly names the temptation to paste `none` and automerge first; the discharge depends on the operator respecting the ordering. There is no automation enforcing it — the contract is process-only.

The process gap (P2) is that this PR (#425) cannot itself prove discharge; the discharge happens on #415, not here. The amend-to-amend acknowledges this in §3 ("This PR's release-notes fence is `none` — the spec is itself the artifact; no follow-up gets deferred beyond N4").

### Lens 4 — New findings introduced?

Three. Counts: **P1 factual** · **P2 process** · **P3 refactor**.

**P1 — Factual error in N5 amendment: bare-numeric `§8` on line 161 is not present in the brief.** The N5 amendment-diff says "At least one bare-numeric cross-ref exists (review-noted: line 161 cites `§8` without a named anchor)." Empirical check on the current `research/wedge-wave-3` brief (post-`0c28842`): `grep -nE '§[0-9]' docs/engineer/research/2026-06-02-wedge-wave-3-adjacent-markets.md` returns lines 30, 38, 91, 96, 149, 178, 199, 210 only — none reference §7, §8, or §9; the universe of bare-numeric refs in the brief is `§1, §1.2, §1.3, §3, §4, §4.2, §5, §5.2`. Line 161 itself is the header `## 6. PR-automation tools` — there is no `§8` there. The amend-to-amend inherits the line-161 claim from #419 N5 without re-verifying it against the brief's current state. **Severity:** factual error in the load-bearing falsifier — the grep step itself remains useful as a general defensive pattern for §-insert amendments, but the "line-161 callout" cannot be the falsifier because there is nothing to update on line 161. **Fix:** either (i) drop the "line 161" parenthetical from the N5 amendment and keep the grep-as-defensive-pattern justification, or (ii) replace with the empirically-correct set of bare-numeric refs the F4 insertion will affect (post-renumber, NONE of the current bare numerics shift — §1/§3/§4/§5 don't move when §7 is inserted; the grep step would then return no false hits and the falsifier becomes "grep returns clean" rather than "line 161 updated"). Option (ii) is the more honest discharge. Soft severity — the grep step is still net positive even if line-161 is wrong, so this is correctable in-place without blocking adoption.

**P2 — N4 sequencing contract is correct but unverifiable from this PR alone.** Four search-term verifiers (`gh issue list --search "W6 cross-namespace ingestion" / "substrate_events step-replay parity" / "automerge Renovate mental model" / "Insight 5 deletion-default 90-day"`) all return zero matches at this PR's review timestamp. The amend-to-amend's §3 acknowledges this is by-design ("the four issues are filed against #415 (not against this PR)") but a reviewer landing on #425 in isolation cannot distinguish "issues not yet filed because the sequencing contract hasn't fired" from "issues not filed because the contract failed." **Severity:** soft — the contract is operationally sound, but the discharge artifact lives on #415's PR body, not in #425's spec. **Fix:** either (i) add a one-line "verifier landing on this PR should check #415's release-notes fence, not gh issue list" note to §3 of the amend-to-amend spec, or (ii) leave as-is and rely on the #415 reviewer to enforce the contract. Option (i) is the cheaper insurance — costs one line, dodges a future reviewer's confusion. Non-blocking.

**P3 — Refactor: §0 ledger duplicates §1 finding order without adding falsifiable signal.** The §0 ledger table (5 rows: N4/N3/N5/N2/N6) re-states the per-finding amendment shape that §1 already names in more detail. The ledger's "Resolution shape" column adds no information beyond the §1 amendment headings ("File 4 GH issues *before* #415 merges" vs §1's "N4 — File 4 tracking issues *before* PR #415 merges"). Per `feedback_deletion_default`: every addition has to earn its place. The ledger earns its place only if a reader could read §0 and skip §1 — but every #425 amendment needs the §1 diff context (the diff blocks themselves are the load-bearing artifact). **Severity:** refactor only — cosmetic. The ledger doesn't actively mislead; it's a 7-line redundancy. **Fix:** either collapse §0 ledger to a single-line "see §1 for per-finding diffs" sentence, or keep the table and add a falsifier-per-row column (the missing column would be "verifier" — `gh` search-term for N4, grep command for N5, etc.). Non-blocking; cited because Lens 3 of `feedback_adversarial_review` requires at least one concrete tightening even when adopting.

### Lens 5 — Wave-3 chain ready to merge?

**ADOPT-WITH-AMENDMENTS for the chain.** Conditions:

1. **N1 discharge is verified** at commit `0c28842` — F2 BLOCKER is now fully closed. The chain is no longer blocked by a false self-attestation.
2. **#404 brief** can merge once the four tracking issues are filed and their URLs pasted into the release-notes fence — this is the F11 + F7 discharge per the N4 sequencing contract.
3. **#415 amendment spec** can merge in the same operator step as #404 (issues filed + URLs pasted + automerge enabled).
4. **#425 amend-to-amend** can merge once P1 (line-161 factual) is corrected in-place. P2 + P3 are non-blocking.
5. **#409 + #419** (the two review PRs) ride along with the spec PRs they audit; per `feedback_review_proportional` the review PRs themselves don't need third-tier reviewer attestation since they introduce no new code-paths.

The chain is ready to merge **after the operator executes the N4 sequencing contract on #415 + the P1 in-place fix on #425**. Both are operator-actions, not new design work.

---

## 1. Cross-check: spec scorecard self-grade vs. reviewer scorecard

| Tier | Spec self-grade | Reviewer counter-grade | Delta |
|---|---|---|---|
| B (floor) | PASS — N4/N3/N5/N2/N6 each have a diff in §1; N1 cited as `0c28842` | **PASS** — verified inline | None |
| A (defensible) | PASS — memory cites + falsifiable signal per amendment + knock-on map | **PARTIAL** — N5's "line 161" falsifier is empirically wrong; the grep step is still defensible as a defensive pattern, but the named falsifier doesn't fire on the current brief | One-line delta: N5's load-bearing falsifier needs P1 fix |
| A+ (structurally improves) | PASS — three of five amendments ship a runnable verifier (N4 search-terms, N5 grep, N3 auto-downgrade close-comment string); format reusable | **PASS** — the Finding → Amendment-diff → Why-this-discharges format scaling to a second review cycle is itself proof the pattern reuses; the §-renumber bare-numeric grep is a reusable class-of-bug closer even if the line-161 anchor is wrong | None |

**Reviewer scorecard: A (after P1 fix promotes to A+).** Self-graded A+ holds for two of three tiers; A tier downgrades to PARTIAL only because of the empirical error in N5's named falsifier. P1 fix is one line; once applied, A tier promotes to PASS and A+ self-grade stands.

---

## 2. Recommended fixes (priority order)

1. **P1 (correctable in-place; promotes A tier to PASS).** In #425 spec §1 N5 amendment-diff block, replace the "line 161 cites `§8` without a named anchor" parenthetical with one of:
   - **(a) drop the line-anchor entirely:** "the brief uses named-anchor cross-refs (`§7 Insight 3`, `§9 Sources`) but the F4 §-insert renumbers three headers; bare-numeric refs to those three sections are the failure mode the grep guards against."
   - **(b) name the actual bare-numeric universe in the brief** (`§1, §1.2, §1.3, §3, §4, §4.2, §5, §5.2`) and note that **none** of these shift when §7 is inserted — the grep's expected output post-insertion is therefore "same set, no new refs to renumbered sections," and any new `§8/§9/§10` bare-numeric ref in implementer output is the failure mode.
   
   Option (b) is the more honest discharge per `feedback_decision_priority` UX > best-practices: it gives the implementer a concrete pass/fail criterion.

2. **P2 (non-blocking; one-line addition).** In #425 spec §3, add one line after "The N4 amendment closes the F11 follow-through gap" — "Verifier landing on this PR should check #415's release-notes fence, not `gh issue list` against this PR's number — the four issues file against #415's load-bearing leftovers, not against the amend-to-amend itself."

3. **P3 (non-blocking; deletion or column-add).** §0 ledger either collapses to one prose sentence ("see §1 for per-finding amendment diffs in priority order N4 / N3 / N5 / N2 / N6") OR gains a "verifier" column with the per-finding `gh`/grep/criterion that earns the row its place. Either resolution adheres to `feedback_deletion_default`.

P1 is the only one that materially affects the scorecard. P2 + P3 are pure quality-of-life.

---

## 3. Memory cites

- `feedback_adversarial_review` — edge cases + refactor + risk + simplification; never auto-approve (this review applied all four lenses and raised one of each except risk — the chain's risk surface is now load-bearing-and-falsifiable, not risk-tier).
- `feedback_pr_body_file_only` — PR body via `--body-file` (this PR ships its body via `--body-file`).
- `feedback_pr_body_release_notes_mandatory` — every PR body needs a release-notes fence (this PR ships `none` because the artifact is the review itself).
- `feedback_decision_priority` — UX > ease > performance > best-practices > speed > velocity; long-term > short-term (P1 fix option (b) is the UX-preferred discharge).
- `feedback_deletion_default` — every PR answers "what got smaller?"; addition needs A+ defense (P3 cites this against the §0 ledger).
- `feedback_subagent_verification` — ~10% lie rate on "make check clean" (the N4 search-terms + N5 grep + N3 close-comment string are all audit-trail artifacts that survive this lie rate).

---

## 4. Test plan

- [x] `bash scripts/doc-check.sh` — clean (banned-phrase + comment-noise + link gates pass on this review file).
- [x] No AI signatures in the review or commit message.
- [x] Review on isolated worktree branch off `origin/main`; no other files touched.
- [x] N1 fix verified at `git show 0c28842` — three sites rewritten (Insight 4, load-bearing follow-up (a), §4.2 first bullet); the third was already partially fixed by `f4b8f9a` and finished by `0c28842`.
- [x] Four #425 tracking-issue search terms tested via `gh issue list --search` — all four return zero matches at review timestamp (confirms N4 is operator-action against #415, not discharged by #425 itself).
- [x] Bare-numeric § grep against current brief at `origin/research/wedge-wave-3` — confirms no §7/§8/§9 refs exist; this is the empirical source for P1.
- [x] #415 §3 issue list cross-checked against #425's N4(b) search-term mapping — four-to-four match; the labels are accurate.
- [x] #415 §5 Edge-case-2 bullet read in full to confirm the N5 diff replaces exactly the right bullet — confirmed.

---

```release-notes
none
```
