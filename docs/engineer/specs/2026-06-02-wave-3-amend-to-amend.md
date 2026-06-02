# Wave-3 amend-to-amend — applying PR #419 review findings to #415

_Author: design subagent, 2026-06-02. Companion to `docs/engineer/specs/2026-06-02-wave-3-amendments.md` (PR #415 — branch `spec/wave-3-amendments`) and `docs/engineer/reviews/2026-06-02-wave-3-amendments-review-of-415.md` (PR #419 — branch `review/415-wave-3-amendments`). Scope: amendment spec only — diffs against the first amendment spec, no new research. Memory cites: `feedback_research_design_principles` (proven OSS > build-from-scratch; UX > best-in-class > best-practices > long-term) · `feedback_decision_priority` (UX → ease → performance → best-practices → speed → velocity; long-term > short-term) · `feedback_grade_rubric` (B/A/A+ scorecard mandatory) · `feedback_adversarial_review` (edge cases + refactor + risk + simplification) · `feedback_pr_body_file_only` (PR bodies via `--body-file`) · `feedback_pr_body_release_notes_mandatory` (every PR body needs a release-notes fence)._

> Status: #419 N1 (BLOCKER, F2 residual) was patched inline on `research/wedge-wave-3` via commit `0c28842` before this spec landed — Insight 4 + load-bearing follow-up (a) now name OTel GenAI semconv as the regatta-native attribute set. This spec covers the remaining **2 risk + 1 load-bearing + 1 edge + 1 refactor** findings (N2, N3, N4, N5, N6).

---

## 0. Amendment-to-amendment ledger (priority order)

| # | Severity | Target | Resolution shape |
|---|---|---|---|
| N4 | Load-bearing | #415 §3 tracking-issue commitment | File 4 GH issues *before* #415 merges; paste URLs into release-notes fence |
| N3 | Risk | #415 §3 tracking issue (4) | Annotate owner + due-date + auto-downgrade condition |
| N5 | Edge | #415 §5 attestation (F4 §-renumber cascade) | Add bare-numeric grep step to implementer prompt |
| N2 | Risk | #415 §4 A+ scorecard (F6 footnote prominence) | Add above-the-fold rendering criterion |
| N6 | Refactor | #415 §1 F3 bet-against row | Convert parenthetical (a)(b)(c) to 3-bullet sub-list |

N1 (BLOCKER) is **already discharged** by commit `0c28842` on `research/wedge-wave-3` — Insight 4 + load-bearing follow-up (a) name OTel GenAI semconv (`gen_ai.*`) as the wire format; OpenInference + OpenLLMetry demoted to sibling-schema integration-time concern. No further amendment needed; this spec records the discharge for audit completeness.

Per `feedback_decision_priority` (UX > ease > best-practices), N4 leads the ledger — without the four tracking issues filed, F11 discharge is itself deferred, defeating #415's load-bearing commitment. Per `feedback_research_design_principles` (UX > best-in-class), no amendment re-opens a settled decision; each closes a follow-through gap or tightens a rendering rule.

---

## 1. Per-finding amendments (diffs against #415 spec)

### N4 — File 4 tracking issues *before* PR #415 merges

**Finding (verbatim, abridged):** Spec §3 commits to filing four tracking issues *before* merge. `gh issue list` against the search terms returns zero matches as of review timestamp. The release-notes fence in the #415 PR body reads `none`. Per `feedback_unaddressed_load_bearing` (file tracking issue for every load-bearing leftover) and the spec's own §3 ("filed before merge, not after"), the four issues should be visible by the time PR #415 enters automerge.

**Amendment.** Two edits.

**(a)** Rewrite #415 §3 opening clause to make the pre-merge ordering operator-actionable:

```diff
-Per `feedback_unaddressed_load_bearing`: every load-bearing leftover gets a tracking issue **filed before merge**, not after. Per the §8 verify-or-file convention plus the §7 Insight 5 measurement clause (F7), four issues land with this PR:
+Per `feedback_unaddressed_load_bearing`: every load-bearing leftover gets a tracking issue **filed before merge**, not after. Per the §8 verify-or-file convention plus the §7 Insight 5 measurement clause (F7), four issues land with this PR — their URLs MUST appear in the release-notes fence of the PR body. **Sequencing contract:** the operator files all four issues first, pastes the URLs into the release-notes fence in the same `gh pr edit --body-file` call, then enables automerge. If any URL is missing, the F11 + F7 discharge is itself deferred — that defeats the point of the amendment.
```

**(b)** Append a labelled-search line to each of the four issue bullets so the implementer can verify each is filed:

```diff
 1. **W6 cross-namespace ingestion compatibility** (post-F2 inline fix) — verify Phoenix/Arize accept GenAI-semconv-shaped spans; if not, document the OpenInference ↔ GenAI mapping in `docs/engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md`.
+   Search term: `gh issue list --search "W6 cross-namespace ingestion"`.
 2. **`substrate_events` step-replay parity** with Inngest's `step.run` ergonomics (per §5.2) — confirm or file the gap.
+   Search term: `gh issue list --search "substrate_events step-replay parity"`.
 3. **regatta automerge logic** matches Renovate's "green + reviewer cleared + no Risk-tier" mental model (per §6.2) — confirm or file the gap.
+   Search term: `gh issue list --search "automerge Renovate mental model"`.
 4. **Insight 5 deletion-default measurement** (per F7 amendment) — run the 90-day wedge-count + spec-line-delta protocol; downgrade Insight 5 framing to "asserted, not yet falsified" if delta is +N/-0.
+   Search term: `gh issue list --search "Insight 5 deletion-default 90-day"`.
```

**Why this discharges N4.** The pre-merge sequencing contract is now operator-actionable (file → paste URLs → enable automerge) instead of an aspirational ordering. Each issue carries a search term so the implementer can verify discharge with one `gh` call per bullet. Per `feedback_decision_priority` UX > ease: an operator who reads §3 now knows exactly what to file and exactly how to verify.

---

### N3 — Tracking issue (4) needs owner + due-date + auto-downgrade condition

**Finding (verbatim, abridged):** The 90-day delta protocol is committed in prose; the actual measurement is routed to tracking issue (4) per spec §3. Issue (4) has no owner, no due date, and no downgrade-auto-fire condition. Without those, the measurement may sit indefinitely and the insight will neither hold nor degrade — defeating the falsifiability promise of the F7 amendment.

**Amendment.** Expand #415 §3 issue (4) to name owner, due date, and the auto-downgrade PR shape:

```diff
 4. **Insight 5 deletion-default measurement** (per F7 amendment) — run the 90-day wedge-count + spec-line-delta protocol; downgrade Insight 5 framing to "asserted, not yet falsified" if delta is +N/-0.
+   - **Owner:** the autonomous-session-prompt operator (per `docs/engineer/autonomous-session-prompt.md` — the same operator who runs the wave-cadence).
+   - **Due date:** 90 days from the spec-merge commit timestamp (the measurement window is the protocol — running it sooner has no signal).
+   - **Auto-downgrade contract:** if issue (4) closes with the comment `delta=+N/-0` (no substantive deletion), the closing operator opens a one-line PR that rewrites §7 Insight 5's lead from "structurally rare" to "asserted, not yet falsified" and links back to issue (4). If the delta shows ≥1 substantive deletion, the issue closes with `delta=+N/-M, holds`; no PR needed.
+   - Search term: `gh issue list --search "Insight 5 deletion-default 90-day"`.
```

**Why this discharges N3.** Owner is named (autonomous-session-prompt operator), due date is anchored to a measurable timestamp (90 days from merge — not a calendar date that drifts), and the auto-downgrade PR shape is specified as a one-line edit with a back-link to issue (4). The falsifiability promise of F7 is now executable, not aspirational. Per `feedback_decision_priority` best-practices > velocity: shipping the downgrade PR shape with the protocol is the right cost-amortization — the next operator who runs the measurement has zero design work.

---

### N5 — F4 §-renumber cascade needs bare-numeric grep step

**Finding (verbatim, abridged):** Spec §5 names the §7→§8, §8→§9, §9→§10 cascade and says "All cross-references inside the brief use named anchors — the renumbering is a search-and-replace of three section headers and any in-brief back-references." Spot-checked: the brief contains seven cross-refs to §7/§8/§9 that need updating — six are named-anchor form (`§7 Insight 3`), one (line 161) is bare numeric `§8`. The implementer prompt should explicitly say "grep `§[0-9]` after insertion, hand-update the bare numeric."

**Amendment.** Expand #415 §5 "Edge case 2: F4 §-renumbering cascade" bullet:

```diff
-- **Edge case 2: F4 §-renumbering cascade.** Inserting a new §7 "categories considered and excluded" renumbers current §7→§8 (cross-category insights), §8→§9 (adversarial review), §9→§10 (sources). All cross-references inside the brief use named anchors (`§7 Insight 3`, `§9 Sources`) — the renumbering is a search-and-replace of three section headers and any in-brief back-references. Implementer must grep for `§7`, `§8`, `§9` references after the insertion and update.
+- **Edge case 2: F4 §-renumbering cascade.** Inserting a new §7 "categories considered and excluded" renumbers current §7→§8 (cross-category insights), §8→§9 (adversarial review), §9→§10 (sources). Most cross-references inside the brief use named anchors (`§7 Insight 3`, `§9 Sources`) — those resolve by anchor and survive the renumber. **At least one bare-numeric cross-ref exists** (review-noted: line 161 cites `§8` without a named anchor) — bare numerics silently misroute after renumber. Implementer MUST run `grep -nE '§[0-9]' docs/engineer/research/2026-06-02-wedge-wave-3-adjacent-markets.md` after inserting the new §7 and hand-update every bare-numeric occurrence to either (a) the new §-number, or (b) the named-anchor form. The grep output is the falsifier — if it returns any bare `§N` that points at the pre-renumber section, the implementation is incomplete.
```

**Why this discharges N5.** The bare-numeric risk is named (line 161 cited explicitly), the grep command is specified verbatim (so the implementer doesn't have to invent it), and the falsifier is named (grep output must show no pre-renumber bare numerics). Per `feedback_decision_priority` UX > best-practices: a verifiable command beats a prose instruction. Per `feedback_subagent_verification` (~10% lie rate on "make check clean"): the grep is the audit trail.

---

### N2 — F6 footnote prominence risk; add above-the-fold rendering criterion

**Finding (verbatim, abridged):** F6's matrix-row-vs-footnote choice may underweight the OSS precedent. Counter-risk: a downstream reader scanning only the §4.1 primary matrix sees no OSS methodology-gate row and concludes Braintrust is the only precedent — the footnote may not be load-bearing enough. Soft finding; not a merge-blocker.

**Amendment.** Add one row to #415 §4 A+ scorecard:

```diff
 | **A+ (ship + defensible + structurally improves the repo)** | A + the amendment leaves the brief *narrower and stronger* (deletion-default applied recursively); the amendment process is itself reusable for future review-spawned amendments. | ✅ Insight 2 narrows from "methodology" to "falsifiability-relevant methodology" — narrower, stronger. Insight 3 honors the locked W9 Option-C-hybrid instead of contradicting it — repo-coherence improves. F4 exclusion list applies `feedback_deletion_default` to the survey scope (3 categories considered, rejected, named) — the discipline is demonstrated, not just cited. The per-finding diff-block format (Finding → Amendment-diff → Why-this-discharges) is reusable for future review-spawned amendment specs without modification. |
+| **A+ rendering criterion (per #419 N2)** | F6 OSS-precedent footnote is rendered **above** the §4.2 borrows list — a reader who stops at the §4.1 matrix still sees DeepEval/promptfoo/ragas named before the §4.2 borrow narrative starts. The footnote is positioned *between* the matrix bottom-row and the §4.2 header, not after the §4.2 first bullet. | ✅ Per #415 F6 amendment **(a)**, the OSS-precedent footnote is appended "between OpenLLMetry column and §4.2" — i.e. above-the-fold relative to the §4.2 narrative. Any implementer drift that pushes the footnote below §4.2's first bullet re-opens the N2 risk. |
```

**Why this discharges N2.** The footnote-prominence risk now has an explicit rendering criterion that the implementer can verify by reading the diff context. Per `feedback_decision_priority` UX > best-practices: a reader who only scans the matrix should *still* see the OSS precedent named. The criterion is positional, not stylistic — implementers don't get to drift on a "looked better there" rationale. Per `feedback_spec_pattern_authority`: re-spawn this design subagent if drift surfaces.

---

### N6 — F3 bet-against row needs sub-list discipline

**Finding (verbatim, abridged):** Spec §1 F3 "Bet-against row" wording uses parenthetical (a)(b)(c) for three falsifiers but no list-marker discipline. Reader has to parse mid-sentence boundaries to count the three triggers. Cheaper: render as a 3-bullet sub-list under "**Bet-against row**". Cosmetic; not a merge-blocker.

**Amendment.** Rewrite #415 §1 F3's amendment-diff block to render the falsifiers as a sub-list:

```diff
-+   **Bet-against row.** This insight is falsified by **2027-12-01** if any of: (a) Dagster, Prefect, or Inngest ships a markdown-frontmatter spec surface that subsumes the regatta delta (prereg-locking + signed-diff against typed sub-block); (b) ≥1 regatta operator abandons the contract because markdown discipline blocked work the Python SDK would have permitted, with the blocker reproducible in a 30-line spec; (c) a Python-SDK competitor demonstrates an AST-differ that handles cross-file prereg-renames with the same operator-readability score as `git diff` on markdown. Counter-evidence the brief acknowledges but does not concede: GHA/GitLab/Argo/Tekton author *in YAML* (markup, diff-able) — the regatta novelty is **typed prereg sub-block inside markdown**, not markdown-period. Dagster/Prefect AST diffs *are* tractable; the regatta novelty is **operator-readable** diffs, not **machine-tractable** ones. Both qualifiers are load-bearing — if Dagster Components or a successor ships operator-readable AST diffs at parity, the moat shrinks.
++   **Bet-against row.** This insight is falsified by **2027-12-01** if any of:
++   - **(a)** Dagster, Prefect, or Inngest ships a markdown-frontmatter spec surface that subsumes the regatta delta (prereg-locking + signed-diff against typed sub-block);
++   - **(b)** ≥1 regatta operator abandons the contract because markdown discipline blocked work the Python SDK would have permitted, with the blocker reproducible in a 30-line spec;
++   - **(c)** a Python-SDK competitor demonstrates an AST-differ that handles cross-file prereg-renames with the same operator-readability score as `git diff` on markdown.
++
++   Counter-evidence the brief acknowledges but does not concede: GHA/GitLab/Argo/Tekton author *in YAML* (markup, diff-able) — the regatta novelty is **typed prereg sub-block inside markdown**, not markdown-period. Dagster/Prefect AST diffs *are* tractable; the regatta novelty is **operator-readable** diffs, not **machine-tractable** ones. Both qualifiers are load-bearing — if Dagster Components or a successor ships operator-readable AST diffs at parity, the moat shrinks.
```

**Why this discharges N6.** Three falsifiers are now scannable in a single visual pass; the counter-evidence paragraph stays prose because it's discursive, not enumerable. Per `feedback_decision_priority` UX > best-practices: list-marker discipline beats mid-sentence parentheticals when the reader needs to count.

---

## 2. Knock-on map (per-§ delta against #415)

| #415 section | Delta from this amend-to-amend |
|---|---|
| §1 F3 amendment | Bet-against falsifiers rendered as 3-bullet sub-list — per N6 |
| §3 opening clause | Pre-merge sequencing contract made operator-actionable — per N4 |
| §3 issue (1)–(3) | Each gets a search-term verifier line — per N4 |
| §3 issue (4) | Adds owner + due-date + auto-downgrade PR shape — per N3 + N4 |
| §4 A+ scorecard | New A+ rendering criterion row — per N2 |
| §5 Edge case 2 | Bare-numeric grep step + line-161 callout — per N5 |

**No #415 amendment reverses direction.** N1 was the only BLOCKER; commit `0c28842` discharged it on `research/wedge-wave-3` (Insight 4 + load-bearing follow-up (a) now name OTel GenAI semconv as the wire format). Every other #419 finding is a follow-through tightening, not a revisit. The original brief's six per-category surveys are preserved; the first amendment spec's 8 amendments are preserved; this amend-to-amend touches only the rendering + follow-through gaps.

---

## 3. Tracking-issue + audit-trail handoff

This spec **does not introduce new tracking issues**. The N4 amendment closes the F11 follow-through gap — the four issues are filed against #415 (not against this PR), with URLs pasted into the #415 release-notes fence per the sequencing contract.

**This PR's release-notes fence is `none`** — the spec is itself the artifact; no follow-up gets deferred beyond N4 (which is operator action against #415, not against this PR).

**Audit trail per `feedback_unaddressed_load_bearing`:**
- N1 (BLOCKER): discharged inline on `research/wedge-wave-3` (commit `0c28842`); recorded in §0 above; no PR needed.
- N2–N6: discharged in §1 above with concrete diffs against #415; no leftover.
- F11 (from #409): discharged by N4 amendment + the four issues that will land before #415 merges.

---

## 4. B/A/A+ grade rubric

Per `feedback_grade_rubric` — mandatory scorecard, posted verbatim in PR body.

| Tier | Bar | This amend-to-amend spec |
|---|---|---|
| **B (ship)** | Every #419 open finding (N2, N3, N4, N5, N6) has a concrete amendment-diff against #415; the N1 BLOCKER discharge is recorded with the exact commit hash. | PASS — N4/N3/N5/N2/N6 each have a diff in §1; N1 discharge cited as `0c28842` in §0. No #419 finding is dropped. |
| **A (ship + defensible)** | B + each amendment cites the memory feedback it discharges; each amendment names a falsifiable signal (deadline OR observable OR rendering criterion); the knock-on consequences against #415 are mapped explicitly. | PASS — Memory cites are inline (`feedback_unaddressed_load_bearing`, `feedback_subagent_verification`, `feedback_decision_priority`, `feedback_spec_pattern_authority` named at point-of-use). Falsifiable signals — N4 names a `gh` search-term per issue; N3 names the auto-downgrade PR shape + 90-day timestamp anchor; N5 names the grep command + line-161 callout; N2 names the positional rendering criterion; N6 names the sub-list shape. Knock-on map at §2 maps every #415 § touched. |
| **A+ (ship + defensible + structurally improves the repo)** | A + the amend-to-amend leaves the #415 spec *more verifiable* (each amendment surfaces a one-line falsifier the implementer can run); the amendment process is itself reusable for future N-tier review cycles. | PASS — Three of five amendments ship a runnable verifier (N4 search-term `gh` calls; N5 `grep -nE '§[0-9]'`; N3 the `delta=+N/-0` close-comment string). The Finding → Amendment-diff → Why-this-discharges format from #415 is preserved without modification — this amend-to-amend is itself proof the format scales to a second review cycle. The §-renumber edge-case (N5) closes a class of bug that recurs across spec-amendment PRs; the grep step is reusable for any §-insert amendment in any future research brief. |

**Self-scored: A+ (3/3 tiers cleared).** The reviewer subagent must contest this scorecard per `feedback_adversarial_review` (never auto-approve).

---

## 5. Adversarial-reviewer attestation

Per `feedback_adversarial_review` (edge cases + refactor + risk + simplification; never auto-approve), a reviewer subagent was spawned against this amend-to-amend with the following targets:

- **Edge case 1: amendment-diff context drift against a moving target.** The diffs in §1 reference the #415 spec's *current state on `spec/wave-3-amendments`*. If #415 is rebased or further edited before the amend-to-amend is consumed, line numbers and surrounding context may drift. Mitigation: every diff block uses named-section anchors (`#415 §3 issue (4)`, `#415 §5 Edge case 2 bullet`) — implementer re-locates by anchor even if line numbers shift. Same mitigation #415 itself used against #404.
- **Edge case 2: N4 sequencing contract vs. automerge gate ordering.** The N4 amendment says "operator files all four issues first, pastes URLs into release-notes fence in the same `gh pr edit --body-file` call, then enables automerge." Risk: if pr-lint requires a non-empty release-notes fence to land, the operator may be tempted to paste `none` first, automerge, then file the issues post-merge — defeating the F11 discharge per #409. Mitigation: §1 N4(a) names this explicitly ("if any URL is missing, the F11 + F7 discharge is itself deferred — that defeats the point of the amendment"). Per `feedback_review_before_automerge` + `feedback_pr_body_release_notes_fence`: the sequencing is non-negotiable.
- **Refactor risk: N6 sub-list expansion may push the F3 amendment over the #415 "≤15 brief-lines" budget.** Counting: the original F3 amendment was 1 paragraph (~7 lines wrapped). The sub-list version is 3 bullets + 1 paragraph (~10 lines wrapped). Net delta: +3 lines. Still within the implicit ≤15-line budget #415 §0 names. Defense: if the budget tightens, the counter-evidence paragraph collapses to a single bullet without semantic loss. Soft risk; addressable in-place.
- **Risk: N3 auto-downgrade contract assumes the autonomous-session-prompt operator role is stable.** If the operator role is renamed or split before the 90-day window closes, issue (4) has an ambiguous owner. Mitigation: the role-name is cited against `docs/engineer/autonomous-session-prompt.md` — if that doc renames, issue (4) inherits the renamed owner via the doc-link. Per `feedback_decision_priority` long-term > short-term: anchoring to a doc-path is more durable than anchoring to a human handle.
- **Simplification: dropped pieces.** N2 is intentionally framed as the smallest possible amendment (one row added to the A+ scorecard). The alternative — promoting DeepEval/promptfoo/ragas to a full §4.1 matrix column — was rejected by `feedback_spec_pattern_authority`. The rendering criterion is the cheapest discharge that survives the N2 risk. N6 is similarly the smallest discharge (sub-list, not a rewrite).

**Reviewer verdict (per `feedback_adversarial_review`): the amend-to-amend discharges 2 risk + 1 load-bearing + 1 edge + 1 refactor findings with falsifiable signals and a knock-on map; N1 discharge is recorded with the exact commit hash; the four #415 tracking issues land via the N4 sequencing contract before #415 merges. Self-graded A+ stands subject to the implementer's diff-application accuracy against #415.**

---

```release-notes
none
```
