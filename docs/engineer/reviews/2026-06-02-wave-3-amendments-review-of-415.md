# Adversarial review of PR #415 — wave-3 amendments spec

_Reviewer: second-tier adversarial subagent, 2026-06-02. Target: `docs/engineer/specs/2026-06-02-wave-3-amendments.md` on branch `spec/wave-3-amendments` (PR #415). Companion to PR #409 (review of #404) and PR #404 (the brief). Scope: lens-by-lens audit of whether the amendment spec discharges the 5 load-bearing + 3 risk-tier findings #409 left open, plus any new findings the amendments themselves introduce. Memory cites: `feedback_adversarial_review` (edge cases + refactor + risk + simplification; never auto-approve) · `feedback_pr_body_file_only` (PR body via `--body-file`) · `feedback_pr_body_release_notes_mandatory` (every PR body needs a release-notes fence)._

---

## 0. Verdict at a glance

| Bucket | Count | Status |
|---|---|---|
| #409 BLOCKERs (F1, F2) marked as inline-fixed pre-amendment | 2 | **F1 clean; F2 partial — new BLOCKER N1 raised below** |
| #409 load-bearing findings the spec claims to discharge | 5 (F3, F4, F5, F6, F8) | 5 DISCHARGED (with one risk on F6 deferral choice — see N2) |
| #409 risk-tier findings the spec claims to discharge | 3 (F7, F9, F10) | 3 DISCHARGED (with one F7 follow-through gap — see N3) |
| #409 comment-noise finding (F11) | 1 | DISCHARGED on intent; ZERO of 4 tracking issues are filed yet (see N4) |
| New findings introduced by amendments | — | **N1 (BLOCKER), N2 (risk), N3 (risk), N4 (load-bearing), N5 (edge), N6 (refactor)** |

Recommendation: **do not merge until N1 is fixed**. N1 contradicts the spec's own §0 claim. Other findings are addressable in-place on `spec/wave-3-amendments` or by closing-on-merge with deferred-issue commitments.

---

## 1. Per-lens audit

### Lens 1 — Each #409 load-bearing + risk finding closed by amendment?

| # | Tier | Lens-1 verdict | Why |
|---|---|---|---|
| F1 | BLOCKER (inline-fixed) | **PASS** | Commit `f4b8f9a` reworded line 144 from `Production-grade durable execution` to `Durable-execution-pinned durable execution`. `bash scripts/doc-check.sh` clean against the spec on this worktree. |
| F2 | BLOCKER (inline-fixed) | **PARTIAL — see N1** | The commit changed the §4.2 *attribute names* (`llm.*` → `gen_ai.*`) but left the *namespace prose* unchanged. §4.2 still calls the schema "the OpenInference schema"; Insight 4 (brief line 197) still names "OpenInference + OpenLLMetry are winning the LLM-shaped extensions" and asks the implementer to "confirm W6 emits **OpenInference-shaped** attributes." Per #409 F2 recommendation: rewrite §4.2 OpenLLMetry bullet **+ Insight 4 + load-bearing follow-up (a)** to name OTel GenAI semconv as the schema W6 emits. The Insight 4 + load-bearing follow-up (a) edits are missing. Amendment spec §0 asserts F2 is fully discharged — that assertion is false against the brief evidence. |
| F3 | Load-bearing | **PASS** | Spec §1 F3 appends a "Bet-against row" with deadline **2027-12-01** and three concrete falsifiers (Dagster/Prefect/Inngest markdown-frontmatter spec; ≥1 operator abandons contract; Python-SDK AST-differ at parity). Falsifiability earned, deadline named, counter-evidence acknowledged. |
| F4 | Load-bearing | **PASS** | Spec §1 F4 inserts a new §7 "Categories considered and excluded" between current §6 and §7, with three rows (fleet mgmt / feature flags / incident response), one-line exclusion rationale each, and a named revisit-trigger per row (MVR-3 multi-instance / MVR-2 ramp gate / Risk-tier paging surface). Lens 2 (below) audits the trigger concreteness. |
| F5 | Load-bearing | **PASS** | Spec §1 F5 rewrites Insight 3 from binary memoization-vs-replay framing to three replay shapes (Inngest step-memoization, Temporal event-sourcing, Restate journal). Cites W9 redteam `docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md §3`; honors Option-C-hybrid locked decision. Lens 3 (below) re-audits this insight's defensibility. |
| F6 | Load-bearing | **PASS — see N2** | Spec §1 F6 narrows Insight 2 to "falsifiability-relevant methodology" and adds DeepEval/promptfoo/ragas as the OSS-precedent footnote under §4.1. Choice to add as footnote rather than full matrix columns is defensible (DeepEval+promptfoo+ragas are evidence-of-pattern, not surveyed-systems), but see N2 below for the trade-off. |
| F7 | Risk | **PASS — see N3** | Spec §1 F7 appends a measurement clause to Insight 5: 90-day wedge-count + spec-line-delta protocol via `git log --since=90.days --diff-filter=AD`. Downgrade condition named ("+N additions –0 deletions → aspirational"). Measurement itself is deferred to tracking issue (4). See N3 below — deferral is the right call but the spec does not say *who* runs the measurement or *when* the downgrade auto-fires. |
| F8 | Load-bearing | **PASS** | Spec §1 F8 rewrites §4.2 first bullet: DeepEval+promptfoo+ragas as the OSS precedent (Apache 2.0 / Apache 2.0 / MIT); Braintrust demoted to "the SaaS shape." Aligns with `feedback_research_design_principles` (proven OSS > build-from-scratch). §4.3 second bullet aligned via knock-on table. |
| F9 | Risk | **PASS** | Spec §1 F9 adds (a) a Restate journal-model row to §5.1 with W9 redteam citation, (b) a Step Functions negative-space anchor ("scale-proven but pattern-poor"). Both omissions #409 named are now in the brief surface, demoted from primary matrix to adjacent-rows footnote — proportional. |
| F10 | Risk | **PASS** | Spec §1 F10 annotates three Braintrust-authored articles in §9 as "vendor positioning" and adds five neutral primary sources (OTel GenAI semconv spec page, DeepEval/promptfoo/ragas READMEs, Restate journal docs, Step Functions whitepaper). Source-bias gap closed. |
| F11 | Comment-noise | **DISCHARGED ON INTENT — see N4** | Spec §3 commits to filing four tracking issues *before* merge. `gh issue list` against the listed search terms returns **zero issues filed as of review timestamp**. The spec body promises the URLs in the release-notes fence but the fence currently contains `none`. See N4. |

**Lens 1 net:** 9 of 11 findings cleanly discharged at the spec level. F2 has a load-bearing gap (N1, BLOCKER) and F11 has a follow-through gap (N4).

### Lens 2 — New §7 "categories considered and excluded" — concrete revisit triggers?

The three revisit triggers in the F4 amendment, scored against the falsifiability bar:

| Category | Revisit trigger (verbatim, abridged) | Is it observable? | Verdict |
|---|---|---|---|
| Fleet management | "Multi-instance regatta deployment lands (MVR-3+) → spawn fleet-mgmt sub-survey targeting Spire/SPIFFE spec" | YES — concrete predicate (MVR-3 milestone + multi-instance deploy event) | **PASS** |
| Feature flags | "Methodology-gate rollout needs a cohort-selection primitive (MVR-2 ramp gate) OR ≥1 operator requests a kill-switch surface" | YES — two named predicates joined by OR; both are observable inbound signals | **PASS** |
| Incident response | "Signed-verdict-events fan out to PagerDuty/Rootly/Incident.io adapters in MVP-3 W12 (notifications) OR a Risk-tier finding from `feedback_review_before_automerge` triggers a paging requirement" | YES — first predicate is a roadmap milestone, second is a labeled-inbound count | **PASS** |

All three triggers are dashboardable or roadmap-anchored. None are "if it feels right" prose. Lens 2 clears.

### Lens 3 — 5 cross-category insights still defensible post-amendment?

| Insight | Pre-amend verdict | Post-amend verdict |
|---|---|---|
| 1. markdown-as-spec moat | PARTIAL (wishful, no deadline) | **DEFENSIBLE** — F3 added 2027-12-01 deadline + three concrete falsifiers + counter-evidence acknowledgement. The "operator-readable AST diff at parity" falsifier is genuinely observable; if a Python-SDK competitor demos it, the moat shrinks per the spec's own contract. |
| 2. gate-blocks-merge methodology | PARTIAL (overclaim) | **DEFENSIBLE** — F6 narrowed the discriminator from "methodology" (broad — DeepEval/ragas/promptfoo also do this) to "falsifiability-relevant methodology" (narrow — p-hack/power/leakage/stat-test). The OSS-precedent footnote prevents the reader from concluding regatta invented the pattern. |
| 3. step-memoization > replay | PARTIAL (binary, contradicts W9) | **DEFENSIBLE** — F5 reframed to three replay shapes (memoization / event-sourcing / journal); cites W9 redteam; preserves the Option-C-hybrid lock. The new prose says "step-memoization wins green-field; event-sourcing stays in incumbent deployments; the spectrum is not binary." This is defensible empirically. |
| 4. OTel + OpenInference + OpenLLMetry | FAIL (wrong namespace) | **STILL PARTIALLY WRONG — see N1** | The amendment spec does not touch Insight 4. The brief's Insight 4 still says "OpenInference + OpenLLMetry are winning the LLM-shaped extensions" and asks the implementer to confirm W6 emits "OpenInference-shaped attributes" — but W6 emits OTel GenAI semconv (`gen_ai.*`), not OpenInference (`llm.*`). The F2 inline fix only patched §4.2 attribute names; Insight 4 was not rewritten. |
| 5. deletion-default structurally rare | PARTIAL (aspirational) | **DEFENSIBLE** — F7 added the 90-day measurement protocol + downgrade condition. The insight is now load-bearing iff the measurement happens; the spec routes the measurement to tracking issue (4). If the issue never gets run, the insight degrades to "asserted; not yet falsified" — which the spec explicitly admits. Honest framing. |

**Lens 3 net:** 4 of 5 insights defensible post-amendment. Insight 4 remains the open BLOCKER (N1).

### Lens 4 — Borrow/reject calls updated per amendments?

Spec §2 lists every §-level knock-on. Spot-checks:

- **§4.2 first bullet:** Confirmed — F8 rewrites the lead from Braintrust to DeepEval+promptfoo+ragas; Braintrust demoted to "SaaS variant."
- **§4.3 second bullet:** Confirmed — knock-on table says "aligned with F8's OSS-precedent reframe; same conclusion." Reject conclusion (no closed-source dependency in MVR-1) preserved.
- **§7 Insight 1, 2, 3, 5:** All four updated per F3/F6/F5/F7.
- **§5.1 matrix:** Confirmed — F9 adds adjacent-rows footnote; primary matrix unchanged. No surveyed-system row is altered.
- **§9 sources:** Confirmed — F10 annotates + adds neutral substitutes; no source deleted, preserving the audit trail.

**Lens 4 verdict:** No borrow/reject reverses direction; every amendment is a narrowing / evidence-anchoring / source-bias annotation. The spec's claim "No borrow/reject calls reverse direction" is true.

### Lens 5 — New findings introduced by amendments?

Six new findings raised by this second-tier review:

**N1 — BLOCKER. Spec §0 falsely claims F2 was fully discharged inline.** The inline commit `f4b8f9a` only changed three attribute-name tokens in §4.2 (line 120). The brief's:
1. §4.2 prose still says "OpenInference schema" / "OpenInference-shaped attributes"
2. Insight 4 (line 197) still says "OpenInference + OpenLLMetry are winning the LLM-shaped extensions"
3. Load-bearing follow-up (a) (line 212) still says "W6 OpenInference attribute emission"

Per #409 F2 recommendation verbatim: "rewrite §4.2 OpenLLMetry bullet **+ Insight 4 + load-bearing follow-up (a)** to name OTel GenAI semconv (`gen_ai.*`) as the schema W6 emits; reframe open question as cross-namespace ingestion compatibility." Two of three sites were not rewritten. The amendment spec self-attests F2 is closed; the brief evidence contradicts the self-attestation.

**Fix (cheapest):** Add a new amendment entry `F2-residual` to spec §1 that rewrites Insight 4 + load-bearing follow-up (a) to name OTel GenAI semconv as the wire format W6 emits, and reframes the open question as cross-namespace ingestion compatibility (Phoenix/Arize/Langfuse ingestion of GenAI-semconv-shaped spans). Tracking issue (1) in §3 already covers the ingestion-compatibility question; the brief framing change is the missing piece.

**N2 — Risk. F6's matrix-row-vs-footnote choice may underweight the OSS precedent.** The spec §5 attestation flags this risk and locks the footnote choice under `feedback_spec_pattern_authority`. Defensible. Counter-risk: a downstream reader scanning only the §4.1 primary matrix sees no OSS methodology-gate row and concludes Braintrust is the only precedent — the footnote may not be load-bearing enough. Mitigation: spec §4 A+ scorecard could add criterion: "F6 footnote is rendered above-the-fold in any §4 PR summary." Soft finding; not a merge-blocker.

**N3 — Risk. F7 measurement deferral has no owner or due date.** The 90-day delta protocol is committed in prose; the actual measurement is routed to tracking issue (4) per spec §3. Issue (4) has no owner, no due date, and no downgrade-auto-fire condition. Without those, the measurement may sit indefinitely and the insight will neither hold nor degrade — defeating the falsifiability promise of the F7 amendment. **Fix:** spec §3 issue (4) should specify (a) owner = autonomous-session-prompt operator; (b) due date = 90 days from spec-merge; (c) auto-downgrade prose = "if issue (4) closes with `delta=+N/-0`, the brief's Insight 5 framing automatically downgrades to 'asserted, not yet falsified' via a one-line PR."

**N4 — Load-bearing. F11 closure depends on tracking issues being filed; none are filed at review time.** `gh issue list` against the four search terms in spec §3 returns zero matches. Per `feedback_unaddressed_load_bearing` (file tracking issue for every load-bearing leftover) and the spec's own §3 commitment ("filed before merge, not after"), the four issues should be visible by the time PR #415 enters automerge. **Fix:** file all four issues before PR #415 merges; paste the URLs into the release-notes fence (currently `none`). If the four issues are deferred to a follow-up PR, the F11 discharge is itself deferred — that defeats the point of the amendment.

**N5 — Edge case. F4 §-renumbering cascade has correct mitigation but tests no anchor.** Spec §5 names the §7→§8, §8→§9, §9→§10 cascade and says "All cross-references inside the brief use named anchors (§7 Insight 3, §9 Sources) — the renumbering is a search-and-replace of three section headers and any in-brief back-references." Spot-checked: the brief contains seven cross-refs to §7/§8/§9 that need updating (six are named-anchor-form like `§7 Insight 3`, one — line 161 — is bare numeric `§8`). The implementer prompt should explicitly say "grep `§[0-9]` after insertion, hand-update the bare numeric." Without that, the bare numeric will silently misroute.

**N6 — Refactor. Spec §1 F3 "Bet-against row" wording uses parenthetical (a)(b)(c) for three falsifiers but no list-marker discipline.** Reader has to parse mid-sentence boundaries to count the three triggers. Cheaper: render as a 3-bullet sub-list under "**Bet-against row**". Cosmetic; not a merge-blocker. Cited because Lens 3 of `feedback_adversarial_review` requires proposing at least one concrete tightening even when approving.

---

## 2. Cross-check: spec scorecard self-grade vs. reviewer scorecard

| Tier | Spec self-grade | Reviewer counter-grade | Delta |
|---|---|---|---|
| B (floor) | PASS — 8 findings, 8 diffs | **PARTIAL** — 8 of 8 amendment diffs present but N1 (F2-residual) demonstrates the spec's "BLOCKERS already discharged inline" claim is false; B floor implicitly assumes BLOCKERs closed | One-line delta: F2 not actually closed |
| A (target) | PASS — every amendment cites the feedback + names a deadline/observable/substitute | **PASS-WITH-NOTE** | A still holds for the eight amendments shipped; the gap is upstream of A (BLOCKER closure is a B-floor problem, not an A-target problem) |
| A+ (stretch) | PASS — 3/3 cleared (Insight 2 narrowed; Insight 3 honors locked W9; F4 applies deletion-default recursively) | **PASS conditional on N1 fix** | A+ stretch is the cleanest of the three — the surgical Finding→Amendment-diff→Why-this-discharges format is reusable |

**Reviewer scorecard verdict: B currently FAILS; A+ achievable after N1 fix + N4 follow-through.**

---

## 3. Recommendation

Do not merge as-is. Fix in-place on `spec/wave-3-amendments`:

1. **N1 (BLOCKER)** — add a new F2-residual amendment that rewrites Insight 4 + load-bearing follow-up (a) on `research/wedge-wave-3` to name OTel GenAI semconv as the wire format W6 emits; reframe the open question as cross-namespace ingestion compatibility.
2. **N4 (load-bearing)** — file the four tracking issues named in spec §3 *before* PR #415 merges; paste URLs into the release-notes fence (currently `none`).
3. **N3 (risk)** — annotate tracking issue (4) with owner + due-date + auto-downgrade condition.
4. **N5 (edge)** — add to spec §5 attestation: "implementer must grep `§[0-9]` and hand-update bare numerics after insertion."
5. **N2, N6** — defer or address in-place; neither blocks merge.

The amendment spec is otherwise the cleanest discharge of a multi-finding review I have seen in this repo's research-mode cycle. The Finding→Amendment-diff→Why-this-discharges shape is genuinely reusable; F4's recursive application of `feedback_deletion_default` to the survey scope itself is a defensible A+ move. The blocker is one false self-attestation (N1) and one missing follow-through (N4).

```release-notes
none
```
