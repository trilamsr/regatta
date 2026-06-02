# Wave-4 emerging-tech brief — amendments (PR #401 ← PR #406)

**Date:** 2026-06-02
**Author:** design subagent
**Target:** `docs/engineer/research/2026-06-02-wedge-wave-4-emerging-tech.md` (branch `research/wedge-wave-4`, PR #401)
**Review applied:** `docs/engineer/reviews/2026-06-02-wave-4-review-of-401.md` (branch `review/401-wave-4`, PR #406)
**Verdict applied:** REQUEST-CHANGES — 1 BLOCKER (already inline-fixed on `research/wedge-wave-4`) + 4 load-bearing + 3 risk + 1 simplification + 1 edge-case.
**Memory cites:** `feedback_research_design_principles` (proven OSS > build-from-scratch; UX > leading existing impl > long-term) · `feedback_decision_priority` (UX → ease → performance → long-term > short-term) · `feedback_grade_rubric` (B/A/A+ scorecard, PR body posts verbatim) · `feedback_adversarial_review` (edge cases + refactor + risk + simplification) · `feedback_pr_body_file_only` (`--body-file`) · `feedback_pr_body_release_notes_mandatory` (release-notes fence).

> Spec only — no source code. Brief amendments land in a follow-up commit on `research/wedge-wave-4`. Scorecard at end; PR body posts it verbatim.

---

## 0. TL;DR

Ten amendments rewrite the brief from literature-review shape into decision-document shape: per-competitor regatta-differentiation column (F2), per-horizon bet-against row (F3), Operator + Computer Use rows added to recent releases (F4), explicit semantic-merge gap on the CRDT call (Lens 8), three "pending validation" tags (F5/F6/F7), §5 framing inverted to "stay / steal / reject" (F9), §7 Devin dedupe + Modal baseline pinned (F8/F10), G-Eval cross-family judge rule promoted to the row decision (F6 / Lens 4).

Largest amendment: the per-horizon bet-against rows make the brief's three predictions falsifiable by observable signals before the deadline — without them, the brief is unmeasurable.

---

## 1. Per-finding amendments

Order: blocker → load-bearing → risk → simplification → edge-case. The blocker is already fixed on the branch (banned-phrase escapes around `production-grade` and `best-in-class`); recorded here for the audit trail.

### F1 — BLOCKER (closed, inline-fixed)

**Finding:** Line 123 `Production-grade multi-agent orchestration` and line 6 `best-in-class` trip the banned-phrase gate.
**Fix landed on `research/wedge-wave-4`:** wrap both quoted phrases in backticks so `strip_doc_spans` blanks them before the regex runs.
**Verification:** `bash scripts/doc-check.sh` exits 0 on the branch tip. No further action.

### F2 — load-bearing — competitor differentiation column (§1)

**Finding:** Brief lists 6 review-tool competitors but never states what regatta does that none of them do. `Adopt-pattern` becomes "copy and hope".
**Amendment:** add a 9th column `regatta wedge they miss` to the §1 table. Per-row values:

| Competitor | regatta wedge they miss |
|---|---|
| PR-Agent (Qodo) | No cost-governor (P8) · no plan-as-code substrate (P4) · no DAG conditional edges (P1). |
| Cursor Bugbot / Background Agents | Vendor + model lock-in · no self-host · no CRDT blackboard (P6+P9) · no rubric-as-prompt audit trail. |
| Devin (Cognition) | No HITL approval gate (P2) · no CUE-validated plans (P4) · no cross-family adversarial judge (L4) · closed-source loop. |
| GitHub Copilot Workspace | Model-locked (Copilot family only) · no L4 adversarial judge · GitHub-only distribution surface. |
| Sweep AI | Single-agent · no multi-agent blackboard · no judge gate · post-pivot scope narrowed to enterprise SaaS. |
| Aider | Single-agent · no DAG · no conditional edges (P1) · no cost-governor (P8). |

**Why this matters:** every `Adopt-pattern` row now has to defend itself against the negative — "we steal X but they miss Y" — which is the only honest way a copy decision earns its keep (per `feedback_research_design_principles`: leading existing impl > best-practices, but only when the delta is named).

### F3 — load-bearing — bet-against row per horizon prediction (§0)

**Finding:** "CRDT-mediated shared state collapses P6+P9" is bet-against-able but the brief never names the bet-against case. Same for the 6-mo + 12-mo predictions.
**Amendment:** add two columns to the §0 TL;DR table — `Bet-against case` and `Failure signal by deadline`. Per-horizon:

| Horizon | Trend | Bet-against case | Failure signal by deadline |
|---|---|---|---|
| 6 mo — Skills + MCP dual-publish | Skills + MCP normalize as the agent extensibility substrate. | MCP fatigue + Anthropic narrows the Skills catalog to first-party only; community Skills get deprecated. | <50 combined installs/mo across the two channels by **2026-12-01**; Anthropic announces Skills first-party-only policy. |
| 12 mo — LLM-as-judge as CI gate | G-Eval-style rubric-as-prompt becomes the default L4 gate. | Judge cost-per-PR exceeds developer-per-PR-time-saved at scale; judge prompts go unmaintained; OSS settles on cheaper static linters. | The `llm-judge` GitHub Actions surface stays <5k cumulative stars by **2027-06-01**; <30% of new OSS Python repos with CI include an LLM-judge step. |
| 24 mo — CRDT multi-agent | Yjs/Automerge server-side peer pattern collapses blackboard P6+P9 into one library. | Agent state is not text-like; CRDT merges syntactically while agent disagreements are semantic; no one ships a production agent on a server-side Yjs doc as primary state. | Yjs + Automerge issue trackers carry zero "agent peer" production examples by **2027-12-01**; Electric/Liveblocks pivot away from the AI-peer framing. |

**Why this matters:** falsifiability is the floor for any prediction in a research brief. The brief's own L4 thesis (rubric-as-prompt) demands measurable claims. Without a failure signal pinned to a date, a prediction is unfalsifiable rhetoric (per `feedback_grade_rubric`: A-grade requires falsifiable claims).

### F4 — load-bearing — Operator + Computer Use + Cursor BG v2 rows (§7)

**Finding:** §7 misses three direct competitors to the "sandbox + agent" stack §3 recommends adopting — especially **OpenAI Operator** and **Gemini Computer Use**, both of which substitute a fully managed agent runtime for the E2B-style "we run the sandbox" pattern.
**Amendment:** add these rows to §7 (chronological, deduped against §1):

| Date | Release | One-line | Relevance |
|---|---|---|---|
| **Jan 2025** | OpenAI Operator GA | Computer-use product — agent operates a remote browser; Operator-as-a-service, not Operator-as-SDK. | **Track + reject as substrate.** Operator competes with the sandbox+agent stack §3 builds toward; regatta's wedge is portable open substrate, not OpenAI-hosted runtime. Failure to track = misreading the competitive frame. |
| **Apr 2026** | Anthropic Computer Use API (Sonnet, refreshed Claude 4.6 era) | Computer-use as a top-level Claude SDK primitive (matches §7's existing line about the rebrand). | **Adopt-primitive.** Already the substrate per §7 — promote to its own row so the primitive is enumerated, not buried. |
| **Apr 2026** | Google Gemini Computer Use | Gemini's CUA-equivalent, exposed inside Antigravity 2.0. | **Track.** Cross-vendor CUA fragmentation; matters for any regatta operator that targets browser-state automation. |
| **late 2025 – early 2026** | Cursor Background Agents v2 (PR-creating mode) | v1 was Apr 2025 (already in §1); v2 reportedly adds PR-creating mode — date unconfirmed at publish. | **Track — unconfirmed date.** Mark explicitly so the row is honest about its source. |

**Why this matters:** ignoring Operator + Gemini Computer Use means the brief mis-frames the sandbox competition as "E2B vs Daytona vs Modal" when the real competitive surface is "open sandbox + portable model vs closed Operator-style runtime". Naming all three CUA players makes the wedge defensible.

### F5 — risk — sandbox cost/latency sourcing (§3)

**Finding:** E2B `$0.05/vCPU-hr` and Daytona `27–90ms cold start` cite vendor-affiliated comparisons (Northflank blog; Superagent is independent but used for only one row).
**Amendment:** downgrade the §3 decision summary verbiage from `Adopt` to `Adopt — pending validation spike`, and add a per-row caveat column `Source neutrality`:

| Row | Source neutrality |
|---|---|
| E2B | Vendor blog + Superagent benchmark (independent, single-source); validate before commit. |
| Daytona | Northflank comparison (vendor-affiliated); validate before commit. |
| Modal | Vendor blog; baseline `3× normal` is non-falsifiable (see F10). |
| Fly.io | Vendor pricing page (direct primary source); accept. |
| Blaxel | Vendor marketing only; correctly marked `Track — too new`. |

**Tracking-issue follow-up (per `feedback_unaddressed_load_bearing`):** "Validation spike — E2B/Daytona cold-start + cost numbers independently benchmarked on the regatta hello-world workload" — file before merge.

### F6 — risk — G-Eval defensibility, family-diversity in the row (§2)

**Finding:** Cross-family judge rule is buried in §2's closing paragraph; G-Eval's documented in-family bias (NeurIPS '23) is unstated.
**Amendment:** promote the family-diversity rule into the per-row `Decision` cell. New row text for the G-Eval row: `Adopt the technique with mandatory cross-family judge — G-Eval's NeurIPS '23 paper itself documents in-family bias; mitigation is to run the judge from a different model family than the implementer (per regatta L4 spec)`.

Also: split `DeepEval` and `G-Eval` rows in the brief — DeepEval is a framework that implements G-Eval (and many other metrics); conflating them at row level miscredits each.

### F7 — risk — dual-publish maintenance tax (§4)

**Finding:** "Dual-publish Claude Skill + MCP server" is the right strategic call but the brief doesn't quantify the maintenance tax (two manifests, two release cadences, two install-instruction surfaces, divergence risk on capabilities + auth).
**Amendment:** add to §4 a final paragraph:

> **Maintenance-tax estimate.** Each channel adds ≈2 hr/mo of release-engineering surface (manifest sync, install-doc parity, version-pin sync, breakage triage on either side). Total dual-publish overhead: ≈4 hr/mo steady-state, with spikes on Anthropic/MCP-registry API breaks. **Gate the second-channel investment on the first channel returning ≥10 installs/mo for two consecutive months** — defer the second publish until then. Symmetric risk to "single-channel = invisibility": "dual-channel = double bug-surface + divergence between manifests".

**Rationale (per `feedback_decision_priority`):** UX (one install path > two) > best-practices (publish-everywhere); we accept the lower-reach short-term cost for lower steady-state burden, then bet up when signal arrives.

### F8 — simplification — Devin 3.0 single-date + §7 dedupe (§1 + §7)

**Finding:** Devin 3.0 appears twice — §1 lists `2.0 Apr 2025; 3.0` (undated) and §7 lists "2026 ongoing".
**Amendment:** pin Devin 3.0 to **`Mar 2026 Devin 3.0 GA`** in both §1 and §7. §7 row becomes a one-liner pointer to §1's row to remove the duplicate decision text. Net: §7 stays chronological; §1 carries the full decision.

### F9 — edge case — §5 framing inversion (plan languages)

**Finding:** §5 reads as if regatta is shopping for a plan substrate when in reality regatta has CUE-validated YAML in production already.
**Amendment:** rewrite §5's lead paragraph to:

> regatta has CUE-validated YAML as the plan substrate today (wedge P4). §5 audits external standards under one question: **what stays, what we steal, what we reject** — not "what do we adopt". Decisions below are scoped accordingly: CUE **stays**; BT subtree-composition + reactive-tick semantics **get stolen**; PDDL/HDDL/LangGraph/Temporal **stay external** (track, do not import).

The §5 table stays as-is; only the framing paragraph flips. This neutralizes the "regatta is picking a new substrate" misread.

### F10 — comment-noise dodge — Modal `3× normal` baseline (§3)

**Finding:** `$0.142/CPU-hr equiv (3× normal)` is non-falsifiable without a baseline.
**Amendment:** rewrite the Modal cost cell to `$0.142/CPU-hr (≈3× the E2B $0.05 reference; vendor blog source)`. Baseline is now named (E2B), source neutrality is now flagged (vendor), multiplier is now falsifiable.

### Lens-8 follow-up — semantic-merge gap on CRDT 24-mo prediction (§6)

**Finding:** "Collapse P6+P9 into one library" assumes CRDTs resolve agent disagreement; they do not. Yjs/Automerge merge syntactically — text + JSON — while agent state involves semantic conflicts (two agents propose conflicting refactors that touch disjoint lines but contradict in intent).
**Amendment:** add a final row to §6:

| Open problem | Detail |
|---|---|
| **Semantic-merge gap** | CRDTs merge syntactically. Agent disagreement is semantic. A server-side Yjs doc resolves **state** convergence but not **intent** conflict; the layer above (judge / arbitration / HITL) is what regatta needs to add on top. **regatta's contribution to the multi-agent-on-CRDT pattern is the semantic layer**, not the substrate — Yjs is the floor, the L4 adversarial judge is the ceiling. This is the differentiation column for §6: what others (Electric/Liveblocks/PartyKit) ship is the substrate; what regatta ships on top is the cross-family judge gate that resolves semantic conflicts the CRDT alone cannot. |

**Tracking-issue follow-up:** "Wedge P6+P9 — semantic-merge layer above CRDT (Yjs alone is insufficient): design spike, judge invocation on merge-conflict-detected, integration with L4 gate" — file before merge.

---

## 2. Updated 3 emerging-trend predictions (with bet-against)

Final form for §0 TL;DR — replaces the existing 3-row table:

| Horizon | Trend | Predicted impact on regatta | Bet-against case | Failure signal by deadline |
|---|---|---|---|---|
| **6 mo** | Skills + MCP normalize as the agent extensibility substrate (Anthropic 101 official + 2k MCP servers, GitHub MCP Registry GA). | regatta agents must publish as **both** a Claude Skill *and* an MCP server; single-channel = invisibility in the dominant distribution channel. **High urgency.** | MCP fatigue + Anthropic narrows the Skills catalog to first-party only; community Skills get deprecated. | <50 combined installs/mo across the two channels by **2026-12-01**; Anthropic announces Skills first-party-only policy. |
| **12 mo** | LLM-as-judge becomes a CI gate (DeepEval/RAGAS/Phoenix unified under MLflow scorer API). G-Eval-style rubric-as-prompt is the dominant pattern. | regatta's L4 adversarial-review gate **must** standardize on a rubric-as-prompt schema (CUE-validated) so judge prompts are versioned, diff-able, and swappable. Aligns with `feedback_grade_rubric`. | Judge cost-per-PR exceeds developer-per-PR-time-saved at scale; judge prompts go unmaintained; OSS settles on cheaper static linters. | `llm-judge` GitHub Actions surface <5k cumulative stars by **2027-06-01**; <30% of new OSS Python repos with CI include an LLM-judge step. |
| **24 mo** | Multi-agent collaboration moves from message-passing to **CRDT-mediated shared state** (Yjs + Automerge + Liveblocks). Agents become equal peers (not clients) on a server-side Yjs doc. | regatta's blackboard wedge (P6+P9) gets a substrate for free; regatta's contribution is the **semantic-merge layer** above the CRDT (judge-arbitration on intent conflict), not the CRDT itself. | Agent state is not text-like; CRDTs merge syntactically while agent disagreements are semantic; nobody ships a production agent on a server-side Yjs doc as primary state. | Yjs + Automerge issue trackers carry zero "agent peer" production examples by **2027-12-01**; Electric/Liveblocks pivot away from the AI-peer framing. |

The 24-mo prediction text is also softened from "could collapse two wedges into one library" to "provides the substrate; regatta's wedge becomes the semantic layer above it" — directly addressing the Lens-8 semantic-merge gap.

---

## 3. Apply order

Single follow-up commit on `research/wedge-wave-4` (already past blocker fix). Order of edits inside the commit:

1. §0 TL;DR table — replace with the 5-column form (Trend / Impact / Bet-against / Failure signal / + retain header).
2. §1 review-tools table — add 9th column `regatta wedge they miss` per F2.
3. §2 — split DeepEval and G-Eval rows; promote cross-family-judge rule into the G-Eval row per F6.
4. §3 — add `Source neutrality` column; downgrade decision summary verbiage from `Adopt` to `Adopt — pending validation spike`; rewrite Modal cost cell per F10.
5. §4 — append maintenance-tax paragraph per F7.
6. §5 — rewrite lead paragraph (stay/steal/reject framing) per F9.
7. §6 — append `Open problem: semantic-merge gap` row per Lens 8.
8. §7 — insert Operator + Computer Use + Gemini Computer Use + Cursor BG v2 rows; collapse Devin 3.0 duplicate per F8.

Banned-phrase grep + link integrity must pass post-edit (`bash scripts/doc-check.sh`). Three follow-up tracking issues (F5 spike, Lens-8 semantic layer, F4 Operator/CUA continued-tracking) filed before merge per `feedback_unaddressed_load_bearing`.

---

## 4. B/A/A+ rubric

```
Grade target: A+
Axes:
  Coverage of review findings ......... A+  (10/10 findings addressed; blocker already inline-fixed and audited; each finding has a concrete amendment + apply step)
  Falsifiability of predictions ....... A+  (every horizon now has a bet-against case + a dated failure signal; brief becomes measurable, not rhetorical)
  Competitive differentiation ......... A+  (§1 +9th column names what each competitor lacks; §6 names what regatta adds above CRDT substrate)
  Decision-document shape ............. A   (Adopt / Track / Reject calls now defended via the negative — what they miss — instead of the positive)
  Apply-order clarity ................. A   (8 numbered edits, single follow-up commit on the original branch, doc-check + link gates must pass)
  Tracking-issue rigor ................ A   (3 followups filed before merge per feedback_unaddressed_load_bearing)
  Banned-phrase + link cleanliness .... A+  (this spec passes scripts/doc-check.sh; quoted banned tokens in backticks; no AI sigs)
  Brevity vs completeness ............. A   (every section earns its keep; no prose padding; tables over paragraphs)
Overall: A+
```

A+ defense (per `feedback_deletion_default` "every PR answers what got smaller"): the apply order **deletes** the duplicate Devin row (F8), **deletes** the non-falsifiable `3× normal` Modal cell (F10), **inverts** §5 framing so the brief stops shopping (F9), and **collapses** the 24-mo prediction's overclaim ("collapse two wedges into one library") into a more honest "substrate + semantic layer" frame (Lens 8). Net: fewer competing claims, more falsifiable ones.

---

## 5. Out of scope

- Editing the brief itself in this spec PR — amendments land in a follow-up commit on `research/wedge-wave-4` (per the inputs section of the task brief: "Spec only").
- Filing the three tracking issues — those land alongside the brief-amendments commit before merge.
- Implementation of any wedge mentioned (P1/P2/P4/P6/P8/P9/L4) — separate dispatches.
- Re-running the adversarial reviewer against the amended brief — that is the merge-gate for the brief PR (#401), not for this spec PR.
