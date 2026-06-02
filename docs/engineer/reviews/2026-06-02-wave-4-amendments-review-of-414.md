# Second-tier adversarial review of PR #414 — wave-4 amendments spec

**Reviewer:** adversarial subagent (second tier)
**Date:** 2026-06-02
**Target PR:** https://github.com/trilamsr/regatta/pull/414 (branch `spec/wave-4-amendments`)
**Spec reviewed:** `docs/engineer/specs/2026-06-02-wave-4-amendments.md` (194 lines, 5 sections + 10 amendments)
**Prior review applied:** `docs/engineer/reviews/2026-06-02-wave-4-review-of-401.md` (branch `review/401-wave-4`, PR #406)
**Brief under amendment:** `docs/engineer/research/2026-06-02-wedge-wave-4-emerging-tech.md` (branch `research/wedge-wave-4`, PR #401)
**Memory cites:** `feedback_adversarial_review` (edge cases + refactor + risk + simplification; never auto-approve) · `feedback_pr_body_file_only` (`--body-file` only) · `feedback_pr_body_release_notes_mandatory` (release-notes fence) · `feedback_unaddressed_load_bearing` (tracking issues for load-bearing leftovers).

> Verdict: **APPROVE WITH NITS.** Spec closes 10/10 review findings with concrete, falsifiable amendments. Apply order is numbered + bounded. Three structural nits remain — none merge-blocking — plus one new finding the second-tier scan surfaced (amendments-vs-implementation gap: spec is a contract, not the brief edit; verification only happens after the brief PR re-runs).

---

## 0. Findings ledger (priority order)

| # | Severity | Lens | Finding | Action |
|---|---|---|---|---|
| G1 | Nit | Lens 6 (new finding) | Spec is a contract for an edit that lands on another branch. Nothing in this PR proves the brief edits will match the spec — there's no diff yet. Verification gate (`doc-check` on amended brief) happens **after** this spec merges + brief PR follows up. | Acceptable for spec-PR pattern; record as expected risk. Brief PR #401 author must run `bash scripts/doc-check.sh` post-edit and post the diff in #401 before requesting merge. |
| G2 | Nit | Lens 3 (Operator dating) | Amendment F4 pins OpenAI Operator GA to **Jan 2025**. Operator was launched as research-preview Jan 2025 (Pro-tier only), not full GA. Public/multi-tier GA shifted to later in 2025. The "GA" label is slightly loose. | Reword F4 row to "Operator preview Jan 2025 → broader availability through 2025" or simply "Jan 2025 launch (Pro preview)". One-word fix. |
| G3 | Nit | Lens 5 (differentiation defensibility) | F2's "regatta wedge they miss" column reads honestly for Sweep/Aider/Copilot Workspace but is **softer for Devin** ("no HITL approval gate · no CUE-validated plans · no cross-family adversarial judge · closed-source loop"). Devin's re-plan loop is the actual hard-to-copy primitive; the brief's own §1 calls it "the pattern to copy". The "differentiation" against Devin should also name what regatta concedes — Devin ships the re-plan loop; regatta doesn't (yet). | Add one half-sentence to the Devin row: "regatta concedes Devin's autonomous re-plan loop; recovers via P2 HITL + CUE plans". Honest framing. |
| G4 | Nit | Lens 4 (semantic-merge gap framing) | The Lens-8 follow-up amendment is correct (CRDTs merge syntactically; agent disagreement is semantic) and the addition to §6 ("regatta's contribution is the semantic layer above the CRDT, not the substrate") is the right move. **But** the 24-mo prediction softening ("substrate; regatta's wedge becomes the semantic layer above it") still leaves the *original strong claim* ("collapse two wedges into one library") visible in the §0 TL;DR predicted-impact cell — the rewrite weakens the wording without naming what regatta **adds**. | Tighten §0 24-mo row's `Predicted impact` cell: replace "regatta's contribution is the **semantic-merge layer** above the CRDT (judge-arbitration on intent conflict), not the CRDT itself" with the same phrasing already used in the §6 amendment ("Yjs is the floor, the L4 adversarial judge is the ceiling"). Parallels the §6 wording. |
| G5 | Comment-noise dodge | Lens 1 (falsifiability) | The bet-against rows are pinned to dates (**2026-12-01**, **2027-06-01**, **2027-12-01**) — strong. **But** the failure signals are themselves measured against partly-vendor-controlled surfaces: "Anthropic announces Skills first-party-only" is a vendor announcement, not a metric the regatta team can observe independently. | Acceptable as written — vendor announcements ARE observable signals. Record as known-tier-2 evidence (lower confidence than the install-count tier-1 signals in the same row). One-line caveat in the spec body would be cleaner but not required. |

**Counts:** 0 blockers, 0 load-bearing, 5 nits (all in the "approve" tier).

---

## 1. Per-lens verdicts (against the 6 lenses in the review brief)

### Lens 1 — Does each load-bearing + risk finding from #406 close?

| #406 finding | Severity | Amendment in #414 | Closes? |
|---|---|---|---|
| F1 banned-phrase | BLOCKER | F1 — already inline-fixed on `research/wedge-wave-4`; audited via `doc-check` exit 0 on the branch tip. | **Yes** (and verified). |
| F2 competitor differentiation | Load-bearing | F2 — adds 9th column `regatta wedge they miss` with per-row deltas for all 6 competitors. | **Yes.** |
| F3 horizon falsifiability | Load-bearing | F3 — adds `Bet-against case` + `Failure signal by deadline` columns; each pinned to a date (2026-12-01 / 2027-06-01 / 2027-12-01). | **Yes.** |
| F4 missing Operator / Computer Use / CUA / Cursor BG v2 | Load-bearing | F4 — inserts 4 rows into §7 (Operator, Computer Use API, Gemini Computer Use, Cursor BG v2). | **Yes** (modulo G2 dating nit). |
| F5 sandbox sourcing | Risk | F5 — adds `Source neutrality` column; downgrades §3 summary from `Adopt` to `Adopt — pending validation spike`; files tracking issue. | **Yes.** |
| F6 G-Eval defensibility | Risk | F6 — promotes cross-family-judge rule into the row's Decision cell; splits DeepEval + G-Eval rows. | **Yes.** |
| F7 dual-publish maintenance tax | Risk | F7 — adds maintenance-tax paragraph with ≈4 hr/mo estimate + gate-on-signal rule ("≥10 installs/mo for two months before second channel"). | **Yes.** |
| F8 Devin 3.0 single date + dedupe | Simplification | F8 — pins to `Mar 2026 Devin 3.0 GA`; §7 row collapses to one-liner pointer. | **Yes.** |
| F9 §5 framing inversion | Edge case | F9 — rewrites §5 lead paragraph to "stay / steal / reject" framing. | **Yes.** |
| F10 Modal `3× normal` baseline | Comment-noise dodge | F10 — rewrites cell to `$0.142/CPU-hr (≈3× the E2B $0.05 reference; vendor blog source)`. | **Yes** (multiplier is now falsifiable). |
| Lens-8 semantic-merge gap | Falsifiability note | Lens-8 amendment — adds open-problem row to §6 + softens §0 24-mo prediction. | **Yes** (modulo G4 wording-parallel nit). |

**10/10 closed.** Verdict on Lens 1: **A+**.

### Lens 2 — Per-horizon bet-against rows: concrete + dated failure signals?

All three horizons now carry:
- A named **bet-against case** (not just "could fail" — a specific causal hypothesis: MCP fatigue / judge cost-per-PR / agent-state-isn't-text).
- A **failure signal pinned to a date** (2026-12-01 / 2027-06-01 / 2027-12-01).
- A **measurable threshold** (<50 installs/mo · <5k cumulative stars · zero production examples).

The 12-mo signal ("<30% of new OSS Python repos with CI include an LLM-judge step") is the cleanest — it's a population-level measurement with a clear methodology. The 6-mo install-count is reasonable. The 24-mo "zero production examples on Yjs issue trackers" is observationally cheap (`gh search`).

**Verdict on Lens 2: A.** One tier-2 caveat (G5): vendor announcements ("Anthropic narrows Skills to first-party") are softer than metrics. Not blocking.

### Lens 3 — OpenAI Operator + Gemini Computer Use addition: accurate state-of-the-world?

F4 amendment adds 4 rows to §7. Cross-checked against publicly known dates:

| Claim in amendment | State of the world (2026-06-02) | Verdict |
|---|---|---|
| `Jan 2025 OpenAI Operator GA` | Operator preview launched Jan 2025 as Pro-tier feature; broader availability rolled out through 2025. "GA" is slightly loose for Jan 2025 — that was preview. | **Slightly loose** (G2). One-word fix. |
| `Apr 2026 Anthropic Computer Use API (Sonnet, refreshed Claude 4.6 era)` | Computer Use was first released Oct 2024 as a beta API on Claude 3.5 Sonnet. The amendment's "refreshed Claude 4.6 era" language is reasonable for the §7 era-window. | **Accurate** for the relevant 6-mo window. |
| `Apr 2026 Google Gemini Computer Use` | Gemini's CUA-equivalent has been referenced via Antigravity 2.0 (Apr 2026 per the brief's own §7 row). The standalone CUA primitive is real. | **Accurate.** |
| `late 2025 – early 2026 Cursor Background Agents v2 (PR-creating mode)` | Amendment **explicitly marks the date "unconfirmed at publish"** — that's the right move per `feedback_research_design_principles`. | **Accurate + honest about uncertainty.** |

**Verdict on Lens 3: A−** (one dating loosening on Operator; everything else accurate and properly hedged where unsure).

### Lens 4 — Semantic-merge gap: does the amendment name what regatta does differently?

The Lens-8 amendment adds a new row to §6:

> regatta's contribution to the multi-agent-on-CRDT pattern is the semantic layer, not the substrate — Yjs is the floor, the L4 adversarial judge is the ceiling.

This is the correct framing. CRDTs (Yjs/Automerge) merge text/JSON syntactically; agent disagreement is *semantic* (two refactors that touch disjoint lines but contradict in intent). The amendment names the layer regatta adds: **L4 cross-family judge gate on merge-conflict-detected**.

One refinement (G4): the §0 24-mo prediction's `Predicted impact` cell uses the wording "regatta's contribution is the semantic-merge layer above the CRDT (judge-arbitration on intent conflict), not the CRDT itself", while §6 uses "Yjs is the floor, the L4 adversarial judge is the ceiling". Parallel wording would tighten the brief.

**Verdict on Lens 4: A.**

### Lens 5 — Differentiation-column per competitor: defensible?

F2 amendment adds the 9th column with per-competitor deltas. Checked each row for defensibility:

| Competitor | Amendment's claim about what regatta has | Defensible? |
|---|---|---|
| PR-Agent (Qodo) | No cost-governor (P8) · no plan-as-code (P4) · no DAG conditional edges (P1) | **Yes.** PR-Agent is review-only; cost/plan/DAG features are regatta-specific wedges. |
| Cursor Bugbot / Background Agents | Vendor + model lock-in · no self-host · no CRDT blackboard · no rubric-as-prompt audit trail | **Yes.** Closed-source + closed-runtime is the structural difference. |
| Devin (Cognition) | No HITL approval gate · no CUE-validated plans · no cross-family adversarial judge · closed-source loop | **Mostly.** G3 nit: should also concede what Devin has that regatta doesn't (re-plan loop). |
| GitHub Copilot Workspace | Model-locked (Copilot family) · no L4 adversarial judge · GitHub-only distribution surface | **Yes.** |
| Sweep AI | Single-agent · no multi-agent blackboard · no judge gate · post-pivot scope narrowed to enterprise SaaS | **Yes.** Sweep's enterprise pivot is the structural break. |
| Aider | Single-agent · no DAG · no conditional edges (P1) · no cost-governor (P8) | **Yes.** Aider is the right comparison since the brief also says "adopt Aider's Git-native pattern" — naming what's missing makes the steal honest. |

**Verdict on Lens 5: A−** (Devin row needs the concession; everything else is defensible).

### Lens 6 — New findings introduced by amendments?

Second-tier scan of the spec itself for new issues not in #406:

1. **G1 (Nit): Spec-vs-implementation gap.** The spec defines the *contract* for amendments to the brief; the brief edits land in a separate follow-up commit. This PR's CI gate is `doc-check` on the spec file only. The amended brief's `doc-check` happens on PR #401's next push. **Acceptable for the spec-PR pattern**, but worth recording as expected risk so the #401 author re-runs `doc-check` post-edit and posts the doc-check exit code in the #401 thread.
2. **G2 (Nit): Operator dating loose.** Jan 2025 was preview, not GA.
3. **G3 (Nit): Devin differentiation one-sided.** Doesn't name what Devin has that regatta concedes.
4. **G4 (Nit): §0/§6 wording parallel.** Two amendments use different phrasings for the same gap; should match.
5. **G5 (Comment-noise dodge): Tier-2 evidence in 6-mo failure signal.** Vendor announcements are observable but softer than metrics — fine as written, would be tighter with one-line caveat.

No load-bearing surprises. No blockers. No security/compliance concerns. No banned-phrase trip (verified: `bash scripts/doc-check.sh` exits 0 with this spec file present).

**Verdict on Lens 6: A−.**

---

## 2. Risk-tier summary (per `feedback_adversarial_review`)

- **Edge cases hunt:** G1 (spec-vs-implementation timing), G5 (tier-2 evidence in failure signals) — two edge cases worth recording.
- **Refactor:** G3 (Devin row concession), G4 (§0/§6 wording parallel) — minor.
- **Risk:** G1 (verification deferred to brief-PR) — acceptable for spec-PR pattern; brief-PR author owns the re-check.
- **Simplification:** none — spec is already lean (194 lines, no padding, all tables).

No risk-tier item is merge-blocking. All five fit on a single line of follow-up comment.

---

## 3. Decision

**APPROVE WITH NITS.** Merge-able as-is. The spec closes 10/10 findings with concrete, dated, falsifiable amendments. Five nits, all addressable in a one-line PR-comment cleanup pass or rolled into the brief-PR follow-up commit:

1. (G2) Operator: "Jan 2025 launch (Pro preview)" instead of `Jan 2025 OpenAI Operator GA`.
2. (G3) Devin row: add "regatta concedes Devin's autonomous re-plan loop; recovers via P2 HITL + CUE plans".
3. (G4) §0 24-mo row uses §6's "Yjs is the floor, L4 judge is the ceiling" wording for parallelism.
4. (G5) Optional one-line caveat on tier-2 vendor-announcement signals in the 6-mo failure-signal cell.
5. (G1) Brief-PR #401 author runs `bash scripts/doc-check.sh` post-amendment-commit and posts the exit code in #401.

---

## 4. Recommended follow-ups (if merged with nits)

Already filed in the spec (per `feedback_unaddressed_load_bearing`):
- Tracking issue: "Brief §7 — Operator + CUA continued tracking" (F4 leftover).
- Tracking issue: "Validation spike — E2B/Daytona cold-start + cost numbers independently benchmarked" (F5).
- Tracking issue: "Wedge P6+P9 — semantic-merge layer above CRDT" (Lens 8).

Additional from this second-tier review (optional, all roll into brief-PR follow-up):
- One-line wording-parallel cleanup (G4).
- Operator dating fix (G2).
- Devin concession half-sentence (G3).

---

## 5. Scorecard

```
Grade target: A+
Axes:
  Closure of #406 findings ............ A+  (10/10 findings amended with concrete + dated + falsifiable edits)
  Falsifiability of predictions ....... A+  (bet-against rows pinned to 2026-12-01 / 2027-06-01 / 2027-12-01 with observable thresholds)
  State-of-the-world accuracy ......... A   (Operator GA dating slightly loose; all other release rows accurate or properly hedged)
  Differentiation defensibility ....... A-  (Devin row missing the concession; other 5 rows defensible)
  Semantic-merge framing .............. A   (right framing; §0/§6 wording could parallel tighter)
  Apply-order clarity ................. A+  (8 numbered edits, single commit, gate on doc-check exit 0)
  Banned-phrase + link cleanliness .... A+  (doc-check exits 0 on the spec file; quoted tokens in backticks)
  New findings introduced ............. A   (5 nits surfaced, 0 blockers, 0 load-bearing)
Overall: A
```

**Verdict: A (approve with 5 nits, none blocking).**
