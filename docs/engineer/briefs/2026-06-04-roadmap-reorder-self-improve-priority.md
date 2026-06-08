---
status: draft
date: 2026-06-04
supersedes: (advisory) docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md §4 ordering only
---

# Roadmap reorder — self-improvement + meta-improvement promotion

## 1. Operator ask

Restated: the sole operator (persona-A self-host per `docs/engineer/briefs/2026-06-01-self-host-first.md:7-12`) wants the orchestrator to **observe its own + subagent action, find inefficiency, and improve**. Today the autonomous loop ships failure-detection only — W4 detector emits 5 reactive failure rules (`internal/selfimprove/rules.go:1-` lists `same-gate-fail-repeats`, `banned-phrase-recurrence`, `subagent-claim-vs-CI-failed`, `load-bearing-leftover`, `reaper-kills-same-agent` per `docs/engineer/specs/2026-06-02-phase-autonomy-w4-self-improvement-detector.md:153-157`). **Inefficiency, success-pattern, and self-tuning are absent.** Issue #832 filed R6-R11 + autotuner as a single tracking issue, deferred behind baseline-data + closed-loop-consumer gates.

The decision needed: do R6-R11 + autotuner stay deferred per #832 framing, or do they promote ahead of one or more MVR-1 wedges? Decision-priority spine (CLAUDE.md "Decision priority": UX > ease > performance > best-practices > speed > velocity; long-term > short-term) governs the answer.

## 2. Current sequence (verbatim from next-horizon-roadmap §4)

From `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md:131-260` (MVR-1 through MVR-4 tables). Reproduced compactly here; rows that move in §3 are tagged `[MOVES]`:

### Phase MVR-1 — adoption-cost collapse (single tenant)

| # | Task | Effort | Adopt |
|---|---|---|---|
| MVR-1-T1 | operator-console v5.1 — SvelteKit dual-principal console + S0 substrate prereqs | XL (~26 wks v1) | SvelteKit 2 + Svelte 5 + Tailwind v4 |
| MVR-1-T2 | `regatta init` wizard | S (3-5d) | AlecAivazis/survey |
| MVR-1-T3 | GoReleaser release pipeline | XS (1-2d) | GoReleaser |
| MVR-1-T4 | GH-issue adapter (`[autonomous]` label) | S (3-5d) | go-github |
| MVR-1-T5 | P3.8 SCM-adapter contract + Gitea second consumer | M (1-2 wks) | go-gitea/sdk |
| MVR-1-T7 | Strategy interface + concurrency-policy unify (DW-superset Wave A piece 1+5) | S (1 wk) | refactor |

Source row: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md:140-145`.

### Phase MVR-2 — first external paying customer

MVR-2-T1 W7 Wave 2 htmx, MVR-2-T2 W8 multi-tenant, MVR-2-T3 retract primitive, MVR-2-T4 LLM-gateway adapter (LiteLLM), MVR-2-T5 Wave-3 polish, MVR-2-T6 substrate-bridge for script-runs, MVR-2-T7 `/workflows` progress UI (rows at `2026-06-02-next-horizon-roadmap.md:183-189`).

### Phase MVR-3 — 5+ paying customers

MVR-3-T1 W10 Sigstore, MVR-3-T2 W12 Stripe metering, MVR-3-T3 W11 blackboard CAS, **MVR-3-T4 research-mode overlay** (`2026-06-01-regatta-research-vision.md` — rides `internal/selfimprove/` infra per §3 below), MVR-3-T5 script-plan gate adapter, MVR-3-T6 LLM-authored JS runtime (rows at `2026-06-02-next-horizon-roadmap.md:219-224`).

### Phase MVR-4 — 10+ paying customers OR perf trigger

MVR-4-T1 W9 Temporal-backed `DurableHistory`, MVR-4-T2 Postgres HA (rows at `2026-06-02-next-horizon-roadmap.md:248-249`).

### What is missing today

`docs/engineer/specs/2026-06-02-phase-autonomy-w4-self-improvement-detector.md:153-157` ships the 5 W4 rules. `docs/engineer/specs/2026-06-02-obs-wave-c-agent-loop-telemetry.md:27-101` ships the dispatch-span + outcome-counter + duration-histogram. **Issue #832 R6-R11 + autotuner have no slot in §4 today.** The roadmap is silent on closed-loop self-tuning.

## 3. Proposed sequence

Two reorders. Both small. Both defensible under decision-priority. Both keep MVR-1-T1 dominant (its UX delta is irreplaceable for persona-A) but unlock self-improve in the time MVR-1-T1 burns.

### Phase MVR-1 — reorder (no scope change, sequence change)

| # | Task | Effort | Change | Justification |
|---|---|---|---|---|
| MVR-1-T4 | GH-issue adapter | S (3-5d) | **PROMOTE — dispatch first of MVR-1** | T4 is the *consumer side* of every self-improve issue R6-R11 will file (per #832 reopen-trigger: "MVR-1-T4 GH-issue adapter PR merges AND substrate Wave 1 ships AND 30 autonomous PRs accumulate"). Without T4, every detector improvement is open-loop. T4 is also a pre-req for the operator-ask itself: regatta cannot observe its own loop usefully if it cannot read+write its own findings as issues. Effort 3-5d. |
| MVR-1-T2 | `regatta init` wizard | S (3-5d) | unchanged | Adoption-cost collapse still load-bearing for persona-A funnel. |
| MVR-1-T3 | GoReleaser release pipeline | XS (1-2d) | unchanged | Smallest task; parallel. |
| MVR-1-T5 | P3.8 SCM-adapter + Gitea | M (1-2 wks) | unchanged | Second-consumer proof per `feedback_research_design_principles`. |
| MVR-1-T7 | Strategy interface unify | S (1 wk) | unchanged | DW-superset Wave A refactor. |
| MVR-1-T1 | operator-console v5.1 | XL (~26 wks) | unchanged in dominance | Still the UX wedge. Burns calendar; that calendar is the *budget* for the new MVR-1.5 phase below. |

### Phase MVR-1.5 — meta-improvement closed-loop (NEW)

Dispatched in parallel with the back-half of MVR-1-T1 (which is 26 weeks alone — calendar headroom is real). File-disjoint from MVR-1-T1 (the operator-console touches SvelteKit + `internal/console/`; selfimprove touches `internal/selfimprove/` + `slo/`). Strict gating per #832: each sub-phase fires only when its trigger fires.

| # | Task | Effort | Trigger | Adopt |
|---|---|---|---|---|
| MVR-1.5-A1 | **SLO-first audit** of R6-R11 candidates against `slo/*.yaml` + `internal/obs/dispatch/spans.go` + `internal/obs/failtaxonomy/classifier.go` | XS (1-2d, designer subagent) | MVR-1-T4 merged | `feedback_research_design_principles` |
| MVR-1.5-A2 | **R6 latency-outlier + R7 cost-outlier** — Prometheus `histogram_quantile` recording rules + Sloth SLO files; **only** if A1 says no upstream primitive covers | S (1 wk if SLO-only; M if Go required) | A1 + ≥30 autonomous PRs merged | Sloth, Prometheus |
| MVR-1.5-A3 | **R8 rework-cycle** — substrate event consumer (`pr_force_push` event already in OBS-C T2 lifecycle per `2026-06-02-obs-wave-c-agent-loop-telemetry.md:125`) | S (1 wk) | A2 + R6/R7 firing >0 times | none — extends W4 rule registry |
| MVR-1.5-B | **R9 success-pattern-extract + R10 priority-thrash + R11 cap-thrash** | M (2 wk) | R6-R8 fire 10+ times AND ≥60 autonomous PRs in substrate | none — extends W4 rule registry |
| MVR-1.5-C | **Autotuner** — closed-loop write-back, see §5 | M (2-3 wk) | R9-R11 fire 5+ times AND operator approves §5 trust-boundary design | none — bespoke glue + go-git |

**Net MVR-1 calendar delta: 0**. Each MVR-1.5 wave fits inside MVR-1-T1's existing 26-week burn. MVR-1.5 dispatches under the same 3-4 parallel-implementer cap (CLAUDE.md Dispatch §"Cap parallel implementers at 3-4"); A2+A3 are file-disjoint from T1 by package owner (`internal/selfimprove/` vs `internal/console/`).

### Phase MVR-3 — research-mode now reuses MVR-1.5 infra (advisory)

MVR-3-T4 research-mode (`docs/engineer/briefs/2026-06-01-regatta-research-vision.md:42-`) shares the `internal/selfimprove/` host package surface (issue #832 cross-refs: "MVR-3-T4 research-mode ships methodology gates on same `selfimprove` infra"). Promoting MVR-1.5 *ahead of* MVR-3-T4 means the rule-registry + dedup + LLM-nightly scaffolding is hardened before research-mode lands on top — net **−1 week of migration cost** for MVR-3-T4 (rough; cite: `2026-06-01-regatta-research-vision.md:62-78` lists 4 methodology gates that ride `internal/selfimprove/llm.go` + the rule registry).

### MVR-2 / MVR-3 / MVR-4 — unchanged

No row moves. The advisory above is the only knock-on.

## 4. Re-ranking matrix (UX × long-term × ease × adoption-first × bespoke-tax)

Scoring scale per cell: **++** strong win, **+** mild win, **0** neutral, **-** mild cost, **--** strong cost. Decision-priority spine: UX > ease > performance > best-practices > speed > velocity; long-term > short-term.

| Wedge | UX (operator) | Long-term | Ease | Adoption-first | Bespoke-tax | Net |
|---|---|---|---|---|---|---|
| **MVR-1-T4 promote** | + (invisible until R8 fires; UX-neutral by itself, but unblocks every downstream meta-rule) | ++ (consumer side of every selfimprove issue; closes the loop the operator asked for) | ++ (3-5d, go-github already known dep) | ++ (go-github is the proven library; cited at `2026-06-02-next-horizon-roadmap.md:143`) | 0 | **PROMOTE** |
| **MVR-1.5-A1 SLO audit** | 0 | ++ (audit-before-build per `feedback_research_design_principles`; risk of duplicating Sloth recording rules is real per #832 "Best-practices audit BEFORE dispatch") | ++ (designer subagent, 1-2d) | ++ (audit IS the adoption-first ritual) | 0 | **DO FIRST** |
| **MVR-1.5-A2 R6+R7** | + (operator sees outlier alerts in same Grafana dashboard as `cost-governor.md` per `2026-06-02-obs-wave-c-agent-loop-telemetry.md:23`) | + (baseline data starts accumulating) | + (1 wk if SLO-only) | ++ (Prometheus `histogram_quantile` + Sloth = adopt) | 0 if SLO-only; − if Go-side rule reinvents Sloth | **GO if A1 clears it** |
| **MVR-1.5-A3 R8 rework** | + (operator sees rework cycles called out) | + (catches the failure mode "agent loops on same PR") | + (extends W4 registry, +1 rule) | + (rides OBS-C T2 pr-lifecycle events per `2026-06-02-obs-wave-c-agent-loop-telemetry.md:125`) | 0 | **GO after A2** |
| **MVR-1.5-B R9/R10/R11** | + (success-pattern surfacing is the operator-ask payoff) | ++ (R9 success-pattern is the input for autotuner; can't tune without it) | 0 (2 wks; rule logic non-trivial for R9 cosine-similar dispatch features) | + (no obvious OSS primitive for "success-pattern extraction across dispatch prompts" — closest is Phoenix span clustering, deferred per `2026-06-02-next-horizon-roadmap.md:308`) | − (R9 has measurable bespoke surface — see §7) | **GO after R6-R8 baseline + 60 PRs** |
| **MVR-1.5-C autotuner** | ++ (THIS is "regatta improves itself" — the operator ask) | ++ (long-term destination; every wedge that defers this is a detour per CLAUDE.md long-term>short-term) | -- (3 wks; trust-boundary design is load-bearing — see §5) | + (HashiCorp Vault transit-engine multi-key shape + Renovate `automergeStrategy` pattern adoptable) | − (~600 LoC bespoke writer + 2-key gate; see §5) | **GO last, gated by §5 design** |
| MVR-1-T1 demote? | -- (T1 IS the persona-A UX wedge — `2026-06-02-next-horizon-roadmap.md:119` says "highest UX delta for persona A") | -- (deferring T1 delays persona-A install funnel — funnel decay is asymmetric per `2026-06-02-next-horizon-roadmap.md:170-173` 60-day abandon criterion) | + (T1 is 26 wks XL — deferring saves XL effort upfront) | 0 | 0 | **DO NOT DEMOTE** |
| MVR-1-T2/T3 demote? | -- (init+release are adoption-cost collapse for persona-A) | - | - (already small; no saving) | 0 | 0 | **DO NOT DEMOTE** |
| MVR-1-T5 demote? | - (SCM-adapter unblocks Gitea second-consumer per `feedback_research_design_principles`) | - (defers second-consumer proof) | + (1-2 wks free) | 0 | 0 | **DO NOT DEMOTE; just sequence after T4** |

**Conclusion**: promote T4 inside MVR-1 (3-5d sequence shift, zero new wedges). Insert MVR-1.5 as parallel-with-T1 phase (zero calendar delta given T1's 26-wk duration). Do not demote T1 — its UX delta is structural and `feedback_decision_priority` puts UX above all other lenses.

## 5. Autotuner — long-term destination

The closed-loop destination per #832 ("Long-term destination: `internal/selfimprove/autotuner.go` reads finding stream + writes back to `regatta.yaml` cost caps + dispatch templates"). The shape below derives from the operator ask + CLAUDE.md trust rules + `2026-06-01-self-host-first.md:9.4` (secret rotation) precedent for multi-key write-windows.

### 5.1 Architecture sketch

```
  W4 detector (rules.go) + R6-R11 fired-issues
            |
            v
   substrate_events kind=selfimprove_finding (signed)
            |
            v
  +---------------------+
  |  autotuner          |  internal/selfimprove/autotuner.go
  |  - read finding feed|
  |  - propose patch    |  PROPOSE-ONLY; never direct-write
  |  - mint PR via T4   |
  +---------------------+
            |
            +---> regatta-bot opens PR titled `auto-tune: <rule-id> <target>`
            |     body = finding ID + proposed YAML diff + scorecard
            |
            +---> standard L0-L4 gate stack runs on the auto-tune PR
            |
            +---> operator (human) merges OR rejects (#783-style branch protection)
```

### 5.2 What the autotuner writes back

Three write targets, ranked by reversibility (lowest risk first):

| Target | What gets tuned | Reversibility | Trust gate |
|---|---|---|---|
| `regatta.yaml` cost caps | `cost.daily_usd_cap`, `cost.per_item_usd_cap` (raise/lower by 10% bounded per tune cycle) | High — single YAML field, `git revert` is 1 click | 2-key (cited §5.4) |
| `slo/*.yaml` SLO thresholds | latency p99 ceiling, error-rate budget | High — recording-rule reload, no code | 2-key |
| Dispatch templates (`docs/engineer/dispatch-templates/*.md`) | append "WHY not WHAT" reminder when R9 surfaces a high-rework template; never delete operator content | Medium — markdown diff is reviewable | 2-key + operator inline-confirm |
| **Spec frontmatter** | NEVER — out of scope; spec authority is human per `feedback_spec_pattern_authority` | N/A | banned |
| **CLAUDE.md** | NEVER — universal rules surface | N/A | banned |
| **Memory feedback files** | NEVER — operator-only writer per CLAUDE.md memory rules | N/A | banned |

### 5.3 Bounded-tune envelope

Per-tune-cycle write envelope (hard caps; refuse to mint PR otherwise):
- ≤1 file
- ≤10 LoC
- ≤±10% drift on any numeric value
- ≤1 autotune PR open at any time per target (queue, don't fan out)
- 7-day cool-off per target after merge (no re-tune within window — matches HMAC keyring rotation window per `2026-06-01-self-host-first.md:723`)

### 5.4 Trust-boundary design

The risk is supply-chain: orchestrator writing its own config = self-modification attack vector. Three defenses, all required:

1. **HMAC-signed finding feed**. Autotuner reads only `substrate_events kind=selfimprove_finding` rows whose HMAC chain verifies (`internal/canon/approval_token.go` keyring, per `2026-06-01-self-host-first.md:721-722`). Forged finding → no patch.
2. **Two-key approval**. The autotuner mints the PR; the **L4 adversarial reviewer subagent + operator-merge** are the two keys. Operator-merge already gated by GitHub branch protection on `main` (CLAUDE.md "Branch protection state"). Net: zero force-push, zero direct-write, every change goes through `make ci-check` + reviewer + human merge — identical trust surface to any other PR.
3. **Append-only audit**. Every autotune PR mints a `substrate_events kind=autotune_proposal` event before opening the PR (signed). Rejected/closed PRs leave the event in place. Future audit can replay-diff "what did the autotuner try across the year?" cheap.

`feedback_root_cause` framing: the root cause of "orchestrator modifies own config = attack surface" is *unmediated write*. The defense is to keep every change *mediated by the same PR pipeline that gates any other change*. No new trust boundary added — the autotuner is a PR author with reduced powers (envelope §5.3 above), not a privileged writer.

### 5.5 What the autotuner does NOT do

Reject set (deletion-default per CLAUDE.md):
- No model-pick (which Claude variant runs the next subagent). Operator picks via `regatta.yaml`.
- No prompt rewriting in `docs/engineer/dispatch-templates/*.md` body — only header-flag toggles via a structured YAML side-car (`docs/engineer/dispatch-templates/_tunables.yaml`, new file, single source of tunable knobs). Template prose stays human-authored per `feedback_spec_pattern_authority`.
- No persona-A-facing config (UI strings, CLI help). Operator owns persona-A UX.
- No CI-gate disable. Banned-target list above.

## 6. Adversarial cuts — what drops or moves out

Per CLAUDE.md "Deletion default", every promotion must answer "what got smaller?". Cuts taken:

1. **MVR-1-T1 demotion considered + REJECTED.** Cited matrix §4 row. Promoting MVR-1.5 ahead of T1 was the obvious adversarial move; it fails the UX-first lens (T1 IS persona-A's UX), and T1's 26-wk burn provides the calendar headroom MVR-1.5 needs. No T1 cut.
2. **MVR-1.5-A2 Go-side rule writer — CUT pending A1 audit.** If `slo/*.yaml` + Prometheus `histogram_quantile` covers R6+R7 (likely per #832 best-practices audit), the Go-side rule code does not ship — only YAML files. Saves ~200 LoC. The cut is conditional on A1's verdict; designer subagent decides per `feedback_spec_pattern_authority`.
3. **R9 success-pattern bespoke clustering — DEFERRED unless rule shape proves out.** R9's "cosine-similar dispatch features in p10 fast+cheap PRs" is the most bespoke surface in MVR-1.5. Cut path: if R6-R8 firing data shows that latency+cost outliers correlate cleanly with template choice already, R9 collapses into "R8 + a `group_by template` clause" — zero new clustering code. Designer subagent re-checks at MVR-1.5-B trigger.
4. **Autotuner first-target = cost caps only, not dispatch templates.** Smaller blast radius. Dispatch-template tunables move to MVR-1.5-C-v2 (separate follow-up wedge) once cost-cap autotune has been merged + reverted >=1 time without drama. Single-target first.
5. **Cuts to existing MVR-3-T4 research-mode dependency chain.** Research-mode currently lists `internal/selfimprove/llm.go` as the LLM-nightly host (`2026-06-01-regatta-research-vision.md:62-78`). With MVR-1.5 hardening that surface ahead of MVR-3, the research-mode spec can DROP its own "self-host the nightly scan" task — that ~1 wk of MVR-3-T4 effort gets reclaimed.

Net cuts: ~200 LoC conditional R6/R7 Go-side + ~1 wk MVR-3-T4 effort + R9 deferred-if-collapsible. Deletion-default satisfied.

## 7. Adoption-first audit (per new rule)

Per `feedback_research_design_principles` cited at `CLAUDE.md` "Cross-cutting design / research". For each new rule + autotuner: name the existing OSS to ADOPT before any bespoke implementation lands. Designer subagent re-validates at A1 audit step.

| Rule / wedge | Adopt-first candidate | Source | Bespoke risk |
|---|---|---|---|
| **R6 latency-outlier** | Sloth (https://github.com/slok/sloth) — Prometheus SLO generator; `histogram_quantile(0.99, ...) > 3σ` shape is a 5-line YAML rule. Alternative: Prometheus `predict_linear` or `holt_winters`. | Sloth v0.11 (2024) MIT; matches OBS-C dispatch histogram per `2026-06-02-obs-wave-c-agent-loop-telemetry.md:55-63` | LOW — pure YAML if A1 clears |
| **R7 cost-outlier** | Same Sloth shape against `regatta.cost.spend_usd` counter from `internal/cost/spend/writer.go` (per `2026-06-02-obs-wave-c-agent-loop-telemetry.md:166`). | Sloth + Grafana Mimir alerts | LOW — pure YAML if A1 clears |
| **R8 rework-cycle** | Renovate's `prCreation: "approval"` + `prConcurrentLimit` pattern for "stop firing on stuck PR" semantics (cited `2026-06-02-next-horizon-roadmap.md:411-412`). Implementation: substrate event consumer over `pr_force_push` events (OBS-C T2 already lands the event). | Renovate v37+ (MIT-like / Mend); existing event shape | LOW — extends W4 registry |
| **R9 success-pattern-extract** | OpenInference span-clustering via Phoenix (Arize) — clusters spans by attribute similarity. `2026-06-02-next-horizon-roadmap.md:308` already names Phoenix as "track-only". Adopt-trigger fires now: phase-X reopen. Alternative: simple `group_by template + sort_by p10_latency` over Prometheus histograms — zero new dep. | Phoenix 4.x Apache-2 OR pure Prometheus query | MED — if Phoenix-route; LOW if Prometheus-route. Designer subagent picks |
| **R10 priority-thrash** | Inngest `step.run` memoization pattern (cited `2026-06-02-next-horizon-roadmap.md:401-402`) for "same item picked N times without progress". Implementation: scheduler event consumer over existing `scheduler_pick` events. | Inngest pattern, not lib | LOW — extends W4 registry |
| **R11 cap-thrash** | Renovate `prHourlyLimit` shape for "N items same day hit cap" semantics. Pure substrate event count. | Renovate pattern, not lib | LOW |
| **Autotuner — finding read path** | HashiCorp Vault `transit/keys/<name>/config` multi-key window (cited `2026-06-01-self-host-first.md:730-733`) — already-known precedent for "old + new keys both valid during cutover", maps to autotuner's PR-overlap window. | Vault Transit API shape; existing HMAC keyring | LOW — pattern adoption |
| **Autotuner — PR mint path** | go-github (already MVR-1-T4 dep). Reuse, do not re-author. | go-github | NONE — re-uses MVR-1-T4 |
| **Autotuner — YAML write path** | go-yaml + CUE validation (existing). Patches the file then runs `cue vet` before commit. | yaml.v3 + cue | LOW |
| **Autotuner — two-key gate** | GitHub branch protection (already in place per CLAUDE.md "Branch protection state") + L4 reviewer (PR pipeline). No new infra. | Existing | NONE |

**Adoption-first verdict**: 8 of 10 candidate surfaces have a proven OSS / proven-pattern primitive. R9 is the only meaningful bespoke risk; the designer subagent chooses Phoenix-clustering vs Prometheus `group_by` at the MVR-1.5-B trigger. R6+R7 land as Sloth YAML if A1 clears (likely).

## 8. Gate triggers — what fires each wedge

Per `feedback_research_design_principles` + #832 deferred-until-ready framing. Each row has *one* trigger predicate.

| Wedge | Trigger predicate | Source signal |
|---|---|---|
| MVR-1-T4 promote | dispatched immediately on roadmap accept | this brief merge |
| MVR-1.5-A1 SLO audit | MVR-1-T4 PR merges | substrate `pr_merge` event w/ pr_number == T4 |
| MVR-1.5-A2 R6+R7 | A1 audit verdict says "no Sloth coverage" AND substrate `kind=pr_merge` count ≥30 with `autonomous=true` (per #832 reopen-trigger) | substrate query (matches #832 acceptance) |
| MVR-1.5-A3 R8 rework | A2 lands AND R6/R7 fire ≥1 time | substrate `kind=selfimprove_finding` |
| MVR-1.5-B R9/R10/R11 | R6-R8 fire ≥10 times AND ≥60 autonomous PRs in substrate | substrate event count |
| MVR-1.5-C autotuner | R9-R11 fire ≥5 times AND operator inline-approves §5 trust-boundary design | operator decision in §10 Q3 |
| MVR-3-T4 research-mode | unchanged from `2026-06-02-next-horizon-roadmap.md:222`; advisory cite of MVR-1.5 reuse | operator-paying-customer count |

The shared shape: every wedge has a measurable substrate-event predicate. Operator never asks "should I dispatch this?" — the dispatch fires when substrate query matches, per CLAUDE.md "Tool-checkable facts: verify, never ask".

## 9. Open questions for operator

Two require human decision (per CLAUDE.md "NEVER ask user — spawn review subagent + decide via these rules" exception clause: tool-checkable facts only, these are not tool-checkable):

1. **Q1 — Promote MVR-1-T4 to dispatch-first of MVR-1? Yes/no.** Default if no answer in 7d: yes (decision-priority spine produces yes; matrix §4 supports). Operator can veto by editing this brief.
2. **Q2 — Autotuner trust-boundary design (§5) approved as-is, or require additional gate?** Default if no answer: required additional gate = "operator must inline-comment `@regatta autotune:approve` on every autotune PR for the first 10 PRs, after which the gate relaxes to L4-reviewer-only". This shapes #832 Phase C dispatch.
3. **Q3 — R9 implementation choice: Phoenix-clustering (3 wk, new dep) vs Prometheus `group_by` (1 wk, no new dep)?** Default if no answer: Prometheus first, Phoenix only if Prometheus shape fails to surface real patterns after 30 autonomous PRs. Designer subagent re-decides at MVR-1.5-B dispatch.

Three open questions; all have defaults; none block this brief's merge.

## 10. A+ rubric self-score

Per CLAUDE.md "Per-criterion citation gate" — every `[x]` cites file:line OR `Test*`-name OR `#issue` on the same line.

| Criterion | B | A | A+ | Tier claimed | Evidence (on same line) |
|---|---|---|---|---|---|
| Operator-ask restated | [x] | [x] | [x] | A+ | `§1` cites `docs/engineer/briefs/2026-06-01-self-host-first.md:7-12` + `internal/selfimprove/rules.go` + `#832` |
| Decision-priority spine applied | [x] | [x] | [x] | A+ | §1 + §4 matrix headings cite CLAUDE.md "Decision priority" verbatim |
| Source roadmap quoted | [x] | [x] | [x] | A+ | §2 cites `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md:131-260` + per-phase line ranges |
| Proposal preserves UX-first | [x] | [x] | [x] | A+ | §3 matrix row "MVR-1-T1 demote? — DO NOT DEMOTE" cites `2026-06-02-next-horizon-roadmap.md:119` UX-delta claim |
| Re-rank matrix per-lens | [x] | [x] | [x] | A+ | §4 table — 5 lenses (UX/long-term/ease/adoption-first/bespoke-tax) × 9 rows |
| Autotuner trust-boundary design | [x] | [x] | [x] | A+ | §5.4 — three named defenses (HMAC chain + 2-key approval + append-only audit) cited to `internal/canon/approval_token.go` + CLAUDE.md branch-protection |
| Adversarial cuts taken | [x] | [x] | [x] | A+ | §6 — 5 named cuts, each with reopen condition; satisfies CLAUDE.md "Deletion default" |
| Adoption-first audit | [x] | [x] | [x] | A+ | §7 — 10 surfaces, each with named OSS candidate + license/version pin |
| Gate triggers measurable | [x] | [x] | [x] | A+ | §8 — every trigger has a substrate-event predicate; CLAUDE.md "Tool-checkable facts" |
| Open questions ≤3 with defaults | [x] | [x] | [x] | A+ | §9 — 3 questions, each with default-if-no-answer |
| Independent reviewer dispatched | [x] | [x] | [ ] | A | N/A — solo-author shipped at A per `feedback_review_proportional` (docs-only, no code change; per CLAUDE.md "Skip reviewer when proportional" — `git diff --name-only origin/main...HEAD \| grep -vE '^(docs/|\.github/|scripts/|.*\.md$)'` is empty) |
| No AI signatures | [x] | [x] | [x] | A+ | grep `Co-Authored-By` / `Generated with` / `written by Claude` against §1-§9 returns 0 — per CLAUDE.md "Identity / output" |
| WHY-not-WHAT prose | [x] | [x] | [x] | A+ | §5.4 root-cause framing cited to `feedback_root_cause` |
| Banned-phrase clean | [x] | [x] | [x] | A+ | literal tokens `blazing-fast` / `production-grade` / `world-class` / `seamless` / `cutting-edge` / `state-of-the-art` absent from §1-§9 prose — per CLAUDE.md "Banned-phrase gate" (this rubric row uses backticks per the gate's literal-token allowance) |

**Claimed tier**: A (12 of 13 criteria at A+; "independent reviewer dispatched" rated A per proportionality exemption). Operator review pulls to A+ on Q1-Q3 resolution.

---

## Pointers

- Source #832: `gh issue view 832` — single tracking issue for R6-R11 + autotuner.
- Source roadmap superseded (advisory): `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4.
- Source W4 detector spec: `docs/engineer/specs/2026-06-02-phase-autonomy-w4-self-improvement-detector.md`.
- Source OBS-C spec (subagent telemetry surface this brief consumes): `docs/engineer/specs/2026-06-02-obs-wave-c-agent-loop-telemetry.md`.
- Source self-host filter: `docs/engineer/briefs/2026-06-01-self-host-first.md`.
- Source arch-simplification gates: `docs/engineer/briefs/2026-06-01-arch-simplification-pass.md`.
- Source research-mode (downstream consumer of `internal/selfimprove/`): `docs/engineer/briefs/2026-06-01-regatta-research-vision.md`.
- Host package: `internal/selfimprove/` (detector.go, rule.go, rules.go, llm.go, source.go).
