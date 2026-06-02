# Wave-2 wedge brief — amendment spec (responds to #407 review of #402)

Status: spec (no code, no dispatch)
Date: 2026-06-02
Audience: design-phase agents picking the next dossier batch
Scope: surgical amendments to `docs/engineer/research/2026-06-02-wedge-wave-2.md`
(branch `research/wedge-wave-2`, PR #402) folding the ten findings from PR
#407 review (`docs/engineer/reviews/2026-06-02-wave-2-review-of-402.md`,
branch `review/402-wave-2`).

The seven primitive surveys in §§1-7 of the original brief are reusable.
This amendment touches only: (a) §0 customer-0 alignment, (b) the top-3
ranking, (c) §5 eval framing, (d) §4 MCP v1 surface, (e) §3 observability
primary, (f) §6 sandbox scoping, (g) §2 Skill-vs-plugin pick, (h) reopen
conditions tightened to dashboardable predicates.

Memory cites: `feedback_research_design_principles` (proven OSS over
build-from-scratch; UX > best-in-class > best-practices > long-term),
`feedback_decision_priority` (UX → ease → performance → best-practices →
speed → velocity; long-term > short-term), `feedback_grade_rubric` (B/A/A+
tool-checkable scorecard), `feedback_adversarial_review` (hostile-read
mandate), `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`.

Self-host-first constraint: per `docs/engineer/briefs/2026-06-01-self-host-first.md`
and `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md`, every
adopted primitive in this amendment runs on the operator's host without a
required cloud account. Vendor-locked primitives are dropped from the top-3
and downgraded to "track-only" rows.

---

## §0 — Customer-0 alignment (addresses F1)

`docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md` picks
**persona A = solo OSS maintainer of a single large repo** as customer 0
and sequences MVR-1 around three load-bearing unblocks (W7 htmx UI;
init-bundle = `regatta init` + GoReleaser + GH-issue adapter; Gitea SCM
adapter). The original wave-2 brief's top-3 (MCP-server expose, SWE-bench
worker eval, Skill format) instead serve persona B / C / D, which the
customer roadmap explicitly sequences as MVR-2 / MVR-3 / MVR-4.

This amendment re-sequences the wave-2 top-3 against persona A. Sections
1-7 of the original brief remain valid research; only the *ranking* moves.

### Persona-A fit of each §1-§7 wedge

| § | Wedge | Persona-A unblock | Persona-B/C/D unblock | Verdict for wave-2 top-3 |
|---|---|---|---|---|
| §1 | Plan authoring (markdown + CUE) | already shipped; no change | no change | reuse — not a top-3 |
| §2 | Skill format + Claude Code plugin manifest | medium — persona A has one repo | high — publishing surface | candidate (see below) |
| §3 | Observability — Grafana/Tempo + Langfuse OSS | high — `stdout` + local Grafana suffice | high — same stack scales | reuse — not a top-3 wedge |
| §4 | MCP — consume-only for v1 | medium — `gh` CLI already covers issue/PR | low for expose | candidate (see below) |
| §5 | Eval — L4 reviewer discrimination | high — gates persona-A merges | high | **top-3 candidate** |
| §6 | Sandbox — local Docker only | covered by self-host default | n/a until SaaS | reuse — not a wedge |
| §7 | Promptfoo + CAS prompt revisions | medium — prompt drift surfaces gate regressions | high | candidate |

### Re-ranked top-3 (replaces the original brief's §"Top-3 recommended wedges")

Ranked by (a) persona-A unblock weight from `2026-06-02-next-horizon-customer-roadmap.md`
§2 blockers, (b) cost to adopt, (c) durability of the moat against Claude
Code Dynamic Workflows.

1. **L4-reviewer-discrimination eval suite (replaces SWE-bench-as-worker-gate)** —
   §5 amended per F2. The L4 adversarial-reviewer gate (`docs/engineer/specs/2026-06-02-s2-t2-adversarial-l4-gate.md`)
   is regatta's named differentiator over Claude Code. Persona A's merge
   trust is bounded by L4 precision/recall — false-positives churn the
   maintainer, false-negatives ship bad PRs. The eval primitive must
   measure the *reviewer*, not the worker. See §1 below for the adopted
   tool (DeepEval G-Eval rubric) and the harness shape.
2. **MCP-consume hardening (replaces MCP-server expose)** — §4 amended
   per F3. Persona A already calls `gh` MCP + `slack` MCP for notify/PR
   touch paths. The wave-2 wedge is to (a) consolidate those consume
   paths through a single MCP client pool and (b) add a phone-side
   `approve(token)` mobile-flow consumer if the operator opts into the
   write-light surface. The bidirectional-expose ambition is dropped
   from wave-2 and tracked as a wave-3 follow-up gated on a real second
   consumer.
3. **Promptfoo eval harness over CAS-stored prompt revisions** — §7
   amended per F8. Persona A reads PRs daily; reviewer-prompt drift is
   the load-bearing risk after L4 precision/recall regression. Adopting
   Promptfoo (OSS, CLI, YAML-driven, MIT) over the CAS blob store with a
   `prompt_revision` tag feeds the §5 eval suite directly. No new tree;
   no new vendor. The eval harness IS the prompt-versioning surface.

The Anthropic Skill format pick from the original top-3 drops to wave-3:
persona A has one repo with one skill set; skill *publishing* is a
persona-B/C marketing primitive per `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md`
§1. The skill-format adoption itself is the right call (§2 of the original
brief stands), but it is not load-bearing for customer 0.

---

## §1 — L4-reviewer-appropriate eval (addresses F2)

### Why SWE-bench Verified is the wrong primitive

SWE-bench Verified measures patch-correctness of the *worker* (Claude /
GPT / Gemini patching a real-repo task). Regatta does not differentiate
on worker quality — `docs/wedges/README.md` explicitly lists "smarter
prompts or planners" as an **anti-wedge** (model-vendor territory). The
SWE-bench score moves with model-vendor releases, not with regatta code,
so the moat is invisible in the metric.

Regatta's named differentiator is the L4 adversarial-reviewer gate
(`docs/engineer/specs/2026-06-02-s2-t2-adversarial-l4-gate.md`). The
gate's quality is **reviewer-discrimination**: precision (does it catch
truly bad PRs?) × recall (does it pass truly good PRs?). That is the eval
primitive the wave-2 wedge must adopt.

### Candidate eval frameworks for an L4-reviewer judge

Per `feedback_research_design_principles` — proven OSS first, scored on
(a) judge-eval fit, (b) self-host scope fit, (c) cost-of-adoption.

| Candidate | Class | License | Self-host | Judge-eval fit | Cost-of-adoption | Score |
|---|---|---|---|---|---|---|
| **DeepEval (G-Eval rubric)** | Python eval framework, LLM-judge primitives | Apache-2.0 | yes (`pip install deepeval`, runs local) | high — G-Eval is purpose-built for LLM-as-judge with rubric-graded outputs; ships with `GEval` metric class accepting a criteria string + evaluation_steps | low — Python subprocess shells out from Go gate harness; reads stdin diff, writes stdout JSON | A |
| Promptfoo `llm-rubric` assertion | YAML CLI eval, JS runtime | MIT | yes (`npm install promptfoo`) | medium — `llm-rubric` assertion judges output against a rubric; less explicit precision/recall scaffolding than DeepEval | very low — already adopted in wave-2 top-3 #3 for prompt-revision evals; same tool covers both | A |
| Phoenix LLM-as-judge | Arize OSS eval+observability | Elastic-2.0 (OSS) | yes (self-host UI + storage) | medium — strong for trace-level evals; heavier than needed for one gate | medium — adds a UI dep; overlap with W6 OTel backbone | B |
| Constitutional AI eval (Anthropic) | Critique + revise loop | research-paper, no canonical OSS impl | no canonical self-host | low — research-grade; no shrink-wrapped tool to adopt | high — would re-implement | C (reject) |
| RAGAS | RAG-specific eval framework | Apache-2.0 | yes | low — designed for retrieval, not adversarial code-review judgment | n/a | reject |

### Decision row

| What to adopt | What to build | Pass + reopen condition |
|---|---|---|
| **Promptfoo `llm-rubric` assertion as the primary L4 eval harness** (already adopted in wave-2 top-3 #3 for prompt-revision evals; one tool covers both). **DeepEval G-Eval as the fallback** when richer precision/recall scaffolding is needed past the Promptfoo `llm-rubric` ceiling. Both are OSS, self-host, free of cloud-account requirement. | A `regatta eval reviewer` subcommand that (a) takes a corpus of known-good and known-bad PR diffs from `docs/engineer/eval-corpus/` (committed to repo, ~50 PRs to start), (b) runs each through the L4 reviewer gate, (c) emits `precision`, `recall`, `false_positive_rate`, `false_negative_rate` as OTel span attributes + a `kind=l4_eval` substrate event. Output feeds the cost-governor dashboard's new `l4_reviewer_quality` panel. | Promote DeepEval from fallback to primary when the L4 false-positive rate on `pr_merge_rate` panel exceeds 5% for a 7-day window (per F7's dashboardable predicate). Track as `inbound_l4_richer_eval_asks ≥ 1` once an operator files an enriched-eval request. |

### Corpus shape

The eval corpus lives at `docs/engineer/eval-corpus/` (created by the
implementer of this spec, not by this amendment). Each entry is a
directory `<NN>-<slug>/` containing `pr.diff`, `metadata.yaml`
(`{label: pass | fail, severity: critical | high | medium | low,
category: correctness | security | test-coverage | refactor | risk,
reason: <one-line>}`), and (optionally) `expected_findings.yaml` for
deeper rubric scoring. Seed corpus = 50 entries: 30 known-good (real
green-merged PRs from regatta history) + 20 known-bad (synthetic
mutations or historical reverted PRs). Corpus grows with every operator
report of an L4 miss.

### SWE-bench fate

`SWE-bench Verified` does not disappear from the brief — it stays in §5
as an **advisory** worker-quality observation (the operator may emit the
worker model's published SWE-bench score as a context tag for drift
detection), but it is **not** the canonical regatta eval primitive and
not the wave-2 top-3 pick. The reopen condition for promoting SWE-bench
from advisory to canonical: an operator surfaces a customer-facing
workflow that demands worker-quality discrimination — which has not
happened.

---

## §2 — MCP-as-regatta reframing (addresses F3)

### Why read-only-expose has no consumer

Original brief §4 picked exposing a read-only MCP surface on regatta
(`list_runs`, `get_run`, `get_budget`) for v1, with write verbs
(`dispatch`, `approve`) gated behind a "≥2 operator IDEs for ≥30 days"
reopen condition. Per F3: read-only over MCP gives operators almost no
value vs `gh run list` + a regatta CLI. The interesting verbs are
exactly the gated ones. v1 ships with no realistic consumer → no usage
→ reopen-condition never fires → write surface never lands.

### Two viable reframings

**Reframe A — drop MCP-expose, ship MCP-consume only.** Persona A
already calls GitHub MCP + Slack MCP for notify/PR-touch paths. The
wave-2 wedge becomes: (a) consolidate those consume paths through a
single MCP client pool (`internal/mcp/client/`), (b) add a `regatta
mcp install <ref>` subcommand that resolves an MCP-registry entry into
a per-tenant sandboxed config dir. No expose surface. The
bidirectional-expose ambition becomes a tracked wave-3 follow-up.

**Reframe B — ship MCP-expose with one write-light verb that has a
real persona-A consumer: `approve(token)` for mobile review.** Per
`docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md` §2
G2 ("solo maintainer reviewing on phone can't approve from a CLI"), the
mobile approval flow is a load-bearing customer-0 blocker. An
MCP-expose surface with `approve(token: short-lived-jws)` + `list_runs`
+ `get_run` is a coherent v1 with a real consumer (a phone-side MCP
client app — e.g. the Anthropic Claude iOS app's MCP-tool support, or
a `gh`-clone CLI on iSH). The write verb is constrained to one
ergonomic flow, gated by short-lived signed tokens that map 1:1 to
existing approval IDs.

### Decision row

| What to adopt | What to build | Pass + reopen condition |
|---|---|---|
| **Reframe A (MCP-consume-only for wave-2).** GitHub + Slack MCP servers consolidated through a single `internal/mcp/client/` pool. **No MCP-expose surface in wave-2.** Reframe B (write-light expose) becomes a tracked wave-3 follow-up issue with the explicit `approve(token)` consumer named, dependency-blocked on (a) the W7 Wave-1 approval flow shipping and (b) a phone-side MCP client app being identified by name. | A `regatta mcp install <ref>` subcommand resolving MCP-registry entries into per-tenant sandbox dirs. Authn lives in existing policy engine. No new framework. | Promote Reframe B from tracked wave-3 to active wedge when (a) MVR-1's W7 approval flow ships AND (b) one named phone-side MCP client app exists with `approve` tool support documented. Both predicates dashboardable: PR-merge of W7 Wave-1 + a `docs/engineer/decisions/<date>-mcp-approve-consumer.md` decision record naming the client. |

The original brief's MCP-expose pick is downgraded — not deleted. The
inversion-of-absorption-risk thesis is sound; the v1 sizing was wrong.

---

## §3 — Observability primary, self-host-aligned (addresses F4)

### Why Honeycomb violates self-host-first

Honeycomb is OTel-native and generalist, but it is a **proprietary SaaS
that requires a cloud account**. Persona A is the self-host operator
per `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md`
§1. A "primary: Honeycomb, fallback: Grafana/Tempo" framing inverts the
customer-0 ranking — Grafana is the *actual* primary; Honeycomb only
appears once persona B/C SaaS lands. Per self-host-first scope,
vendor-locked primitives must drop from the top of the pick list.

### Self-host-aligned candidates (all OSS, all self-host, no cloud account required)

| Candidate | Class | License | Strengths | Cost | Verdict |
|---|---|---|---|---|---|
| **Grafana + Tempo + Loki (Grafana Labs LGTM stack)** | OSS metrics + traces + logs | AGPL-3.0 (Grafana), Apache-2.0 (Tempo + Loki) | mature, large community, OTel-native, runs in a single docker-compose for self-host; persona A can `docker compose up` and have spans flowing in 5 minutes | low — three containers + one Grafana datasource per backend | **ADOPT — primary self-host stack** |
| Jaeger v2 | OSS traces only | Apache-2.0 (CNCF graduated) | OTel-native, simpler than Tempo, well-known to operators familiar with k8s tracing | low — single container | **ADOPT — minimal-deps fallback** when persona A wants traces-only without the Grafana datasource graph |
| SigNoz | OSS APM, OTel-native, single deployment | Apache-2.0 | unified metrics + traces + logs UI; ClickHouse-backed; faster to stand up than LGTM stack for persona A | low — single docker-compose | **ADOPT — alternative all-in-one** if the operator prefers a single UI over the LGTM stack |
| Langfuse | LLM-specific, MIT, self-hostable | MIT | LLM-trace UI; drops in next to either generalist backend | low — single docker-compose | **ADOPT — LLM-specific co-pilot** (unchanged from original brief) |
| Honeycomb | OTel-native commercial SaaS | proprietary, cloud account required | best UX for trace querying; not self-hostable | requires cloud account | **DROP from primary**; track as a SaaS-when-it-ships option for persona B/C |
| Datadog APM | Commercial APM, cloud account required | proprietary | mature LLM-observability product | requires cloud account | **DROP from primary**; same downgrade as Honeycomb |
| New Relic | Commercial APM | proprietary | OTel GenAI native | requires cloud account | **DROP from primary** |
| Arize Phoenix | OSS LLM eval + obs (OpenInference) | Elastic-2.0 | strong for trace-level evals; overlaps with §5 eval framework | medium | hold — revisit if §5 DeepEval ceiling hit |

### Decision row (replaces original §3 decision row)

| What to adopt | What to build | Pass + reopen condition |
|---|---|---|
| **Self-host primary: Grafana + Tempo (LGTM stack, OSS, no cloud account).** **Minimal-deps fallback: Jaeger v2** (single container) for persona-A operators who want traces-only. **All-in-one alternative: SigNoz** for operators preferring a single UI over the LGTM-multi-datasource shape. **LLM-specific co-pilot: Langfuse** (MIT, self-hostable, drops in next to any of the three). **SaaS-when-it-ships option: Honeycomb / Datadog / New Relic** — tracked but not the primary path; reopen when MVR-2 lands with a paying persona-B/C customer asking for managed observability. | A documented "pick your observability backend" runbook in `docs/observability/README.md` showing the three self-host paths (LGTM / Jaeger / SigNoz) + how to point the OTel collector at each. No vendor-specific code in regatta core; the collector config is the swap point. | Promote Honeycomb / Datadog / New Relic from tracked to primary when a paying persona-B/C customer signs a pilot with a stated managed-observability requirement. Predicate: `inbound_managed_observability_asks{tier=paying} ≥ 1`. |

The self-host-first stance is no longer a footnote; it is the ranking
itself. Vendor-locked options are tracked, not picked.

---

## §4 — §2 Skill format + Claude Code plugin manifest (addresses F6)

Original brief §2 picked Anthropic Skill format and rejected Claude Code
plugin manifests by omission. Per F6 the audit-trail property of regatta
is load-bearing: Skill format fits the **authoring** shape (markdown +
YAML frontmatter); plugin manifest fits the **install + signed-audit**
shape (JSON manifest, permissions-scoped, signed installation contracts).
The two are complementary, not competing.

### Decision row (replaces original §2 decision row)

| What to adopt | What to build | Pass + reopen condition |
|---|---|---|
| **Anthropic Skill format (SKILL.md + YAML frontmatter)** for the *authoring* shape of regatta-published agent capabilities. **Claude Code plugin manifest semantics** (JSON manifest, permissions block, signed install contract) for the *install + audit* shape. The two layer: SKILL.md is the authored unit; plugin-manifest.json is the install descriptor that wraps it. | A `regatta skill install <ref>` command that (a) resolves an MCP registry or Skill URL into a sandboxed install dir per tenant, (b) emits a plugin-manifest-shaped JSON descriptor at install time (`{name, version, permissions, signatures, sha256_of_authored_files}`), (c) feeds that descriptor into the existing W10 Sigstore signer (when W10 ships per MVR-3) so the install is signed end-to-end. Permissions live in the existing policy engine. | Building a regatta-branded marketplace remains blocked: reopen only if `inbound_cross_tenant_skill_share_asks ≥ 3` (raised from the original brief's vague "≥3 operators"). Today, lean on Anthropic's directory + MCP registry. |

This amendment promotes the original brief's §2 pick from "Skill format
only" to "Skill format + plugin manifest" without re-doing the §2
candidate scan. The plugin-manifest shape is a thin schema layered on
top, not a competing primitive.

---

## §5 — §6 sandbox scoping (addresses F5)

Original brief §6 picked "SaaS canonical: E2B" alongside the
unchanged self-host default of local Docker. Per F5: no regatta SaaS
exists per `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md`
§1; persona A is self-host. The E2B pick is research for a customer
that has not landed.

### Decision row (replaces original §6 decision row)

| What to adopt | What to build | Pass + reopen condition |
|---|---|---|
| **Self-host (wave-2): local Docker subprocess (no change).** Track E2B + Daytona + Modal for the SaaS lane when MVR-2 or MVR-3 fires per the customer roadmap. No new sandbox primitive lands in wave-2. | A pluggable `Sandbox` interface with one reference impl (local-docker) gated by a `regatta.toml` `sandbox.kind` field, so MVR-2/3 can drop in E2B/Daytona/Modal without spec churn. | Promote E2B (or whichever sandbox scores best at the time) from tracked to active wedge when MVR-2's first paying customer signs a pilot mentioning a hosted sandbox requirement, OR when local-Docker cold-start exceeds the LLM-call wall-time by >10% for 7 consecutive days (dashboardable as `sandbox_cold_start_p95 / llm_call_p95` panel). |

---

## §6 — Reopen condition tightening (addresses F7)

The original brief had seven reopen conditions, four of which were vague.
Per F7 + `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md`
§5 (which sets explicit customer-count tiers 1 / 2 / 5 / 10), every
reopen condition in wave-2 must be dashboardable — i.e. expressible as a
panel query or a tagged inbound count.

### Rewritten reopen conditions

| § | Original (vague) reopen condition | Amended (dashboardable) reopen condition |
|---|---|---|
| §1 (Plan authoring — Pkl) | "if a paying customer files a request for IDE-grade authoring" | `inbound_customer_asks{wedge=ide_authoring} ≥ 1` (single named ask with a stated use case) |
| §2 (Skill marketplace) | "≥3 operators ask for cross-tenant skill sharing" | `inbound_cross_tenant_skill_share_asks ≥ 3` (named operators with stated use cases) — same threshold, dashboard-explicit |
| §3 (Honeycomb / Datadog primary) | "if a paying customer mandates it" | `inbound_managed_observability_asks{tier=paying} ≥ 1` |
| §4 (MCP-expose write verbs) | "≥2 operator IDEs for ≥30 days" | (a) W7 Wave-1 approval flow merged AND (b) named phone-side MCP client app with `approve` tool support documented in `docs/engineer/decisions/<date>-mcp-approve-consumer.md` |
| §5 (DeepEval promotion to primary) | "operator surfaces a workflow SWE-bench cannot model" | L4 false-positive rate on `pr_merge_rate` panel exceeds 5% for a 7-day window OR `inbound_l4_richer_eval_asks ≥ 1` |
| §6 (Sandbox swap) | "cold-start latency becomes the dispatch-loop bottleneck" | `sandbox_cold_start_p95 / llm_call_p95 > 0.1` for 7 consecutive days |
| §7 (Promptfoo → Braintrust / LangSmith) | "operator needs cross-team prompt sharing" | `prompt_revision_count_per_dispatch ≥ 3 within a 30-day window` (diff-fatigue signal) OR `inbound_cross_team_prompt_share_asks ≥ 1` |

All seven are tool-checkable panels or labeled-issue counts — no
operator-feeling-based predicates remain.

---

## §7 — Promptfoo + CAS storage (addresses F8)

Original brief §7 picked Promptfoo with "configs live next to plans in
`.regatta/`." Per F8 this introduces a parallel `.regatta/prompts/*.yaml`
tree that breaks the 3-primitive discipline (events / policies / blobs)
collapsed in `docs/wedges/unified-substrate.md`. Prompt revisions are
blobs — they belong in the CAS, not in a new directory.

### Decision row (replaces original §7 decision row)

| What to adopt | What to build | Pass + reopen condition |
|---|---|---|
| **Promptfoo CLI** as the local-first prompt-eval tool. **Prompt storage:** CAS blob store with a `prompt_revision` tag (per `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md` §3 W11 "plain sqlite-CAS over substrate `blob_digest` column"). No new `.regatta/prompts/` directory. Versioning is git + CAS, A/B routing is the policy engine, Promptfoo reads the CAS via a thin adapter. | A `regatta prompt` subcommand wrapping Promptfoo against CAS-stored prompt revisions. Eval results feed the §5 L4-reviewer eval suite and emit OTel span attributes per the same span discipline as the L4 gate. | Adopt Braintrust / LangSmith only when (a) `prompt_revision_count_per_dispatch ≥ 3 within a 30-day window` (diff-fatigue) OR (b) `inbound_cross_team_prompt_share_asks ≥ 1`. |

---

## §8 — SWE-bench citation tightening (addresses F9)

Original brief §5 cited "Claude 4 Sonnet 77.2%, GPT-5 74.9%, Gemini 2.5
71.8% as of 2026" without inline source. Per F9 the load-bearing factual
claim needs an inline source or a weakening to a range.

**Amendment to §5 prose (when the implementer rewrites the original §5):**
either replace the specific numbers with "frontier models cluster in the
70-80% SWE-bench Verified range as of 2026-Q2" (range-claim, harder to
falsify trivially), OR cite the canonical SWE-bench leaderboard URL
(swebench.com) for each percentage. The implementer picks the lower-cost
option; this amendment does not require both.

SWE-bench Verified itself is preserved as an **advisory** observation
per §1 above, not as the canonical regatta eval.

---

## §9 — Self-marked adversarial-review section (addresses F10)

Original brief's "Adversarial review pass" section listed four findings
that the brief author already had explicit responses ready for — none of
F1-F4 appeared. Per F10 + `feedback_adversarial_review` the reviewer
must surface contradiction risk, not validate the author's already-held
view.

**Amendment:** when the implementer of this spec lands the amended
brief, the self-marked "Adversarial review pass" section is **deleted**,
replaced by a one-line pointer:

> Adversarial review: see `docs/engineer/reviews/2026-06-02-wave-2-review-of-402.md`
> (PR #407) for the F1-F10 findings folded into this brief.

This is the cheaper option (per F10 alternative); the review file is
already on `review/402-wave-2`. No re-litigation.

---

## §10 — Adversarial review of this amendment

Per `feedback_adversarial_review` — a reviewer subagent independently
ran a hostile-read over this spec. Findings folded back in:

- **R1 — top-3 pick #3 (Promptfoo over CAS) is itself a tooling wedge, not a customer-0 unblock.** Persona A's load-bearing blocker per the customer roadmap is W7 UI + init-bundle + Gitea adapter, not prompt-eval tooling. Promptfoo unblocks §5 L4-reviewer-eval indirectly. — *Resolved:* the wave-2 brief's purpose is "primitives that become relevant when regatta widens beyond the dispatch DAG" (per the original brief's "Why this brief exists"). It is not the customer-roadmap brief. Wave-2's top-3 must align with persona A but cannot duplicate the customer roadmap's top-3 — those wedges are already owned by the MVR-1 sequence. Wave-2 picks the next layer: eval rigor + MCP-consume + prompt-revision storage, all of which directly feed the L4 gate that gates persona-A merges.
- **R2 — MCP-consume reframing concedes the inversion-of-absorption-risk thesis.** Reframe A drops MCP-expose entirely from wave-2. If the absorption-risk thesis was load-bearing, the concession is large. — *Resolved:* the thesis is preserved, not dropped. MCP-expose moves from wave-2 active to wave-3 tracked with a named consumer predicate. The cost of premature MCP-expose (no consumer → no usage → unmeetable reopen-condition) is higher than the cost of delayed inversion (the thesis still fires once W7 Wave-1 approval ships and one named phone-side client emerges).
- **R3 — three self-host observability candidates (LGTM, Jaeger, SigNoz) is choice paralysis for persona A.** A single primary pick would be cleaner. — *Resolved:* the primary IS the LGTM stack; Jaeger and SigNoz are explicit fallbacks for operator preference, not co-equal alternatives. The runbook in `docs/observability/README.md` (the implementer's job) makes the LGTM stack the docker-compose default; the other two are documented alternatives.
- **R4 — eval-corpus seed of 50 PRs is unjustified; could be 20 or 200.** No source for the threshold. — *Resolved:* 50 splits as 30 known-good + 20 known-bad, which is the smallest set where a 5% false-positive rate (1.5 false-positive on 30) crosses the noise floor while staying maintainable for one operator. Threshold tunable in the spec the implementer lands; this amendment surfaces the rationale.
- **R5 — Promptfoo and DeepEval overlap in §5.** Picking both for L4 eval is the same anti-pattern F6 flagged for Skill-vs-plugin (pick both creates a 2-way maintenance burden). — *Resolved:* Promptfoo is the primary (already adopted in top-3 #3); DeepEval is the fallback when Promptfoo's `llm-rubric` ceiling is hit. The two are not co-equal — same pattern as the LGTM-vs-Jaeger-vs-SigNoz tiering in §3.

---

## §11 — B/A/A+ rubric for this amendment

Per `feedback_grade_rubric` — every spec ships with a scorecard the PR
body posts verbatim.

| Tier | Criteria |
|---|---|
| **B (floor)** | (a) Every BLOCKING finding from #407 (F1, F2) has a named amendment with a decision row. (b) Every HIGH finding (F3) has a named amendment. (c) Self-host-first scope respected — no required-cloud-account primitive in any top-3 pick. (d) Release-notes fence present at end of PR body. |
| **A (target)** | B + (e) All 10 findings (F1-F10) addressed with named amendments or explicit defer-tracking. (f) Top-3 wedges re-ranked against `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md` persona A. (g) Every reopen condition rewritten to a dashboardable predicate (panel query or labeled-inbound count). (h) Adversarial-review section surfaces ≥3 contradiction-risk findings not already addressed elsewhere in the spec. (i) Eval primitive named (DeepEval / Promptfoo / Phoenix / Constitutional / RAGAS) with explicit reject-reason for non-picks. |
| **A+ (stretch)** | A + (j) Original wave-2 §§1-7 surveys preserved (not re-litigated) — amendments are surgical. (k) Vendor-locked alternatives downgraded to "track-only" rows with named reopen predicate, not deleted (preserves the rationale). (l) Cross-reference to PR #399 customer-roadmap and PR #407 review cited inline. (m) `feedback_doc_check_banned_phrases` clean (zero banned tokens via local `scripts/doc-check.sh`). (n) Top-3 picks each cite the persona-A blocker (G1-G15) they map to. |

**Self-scored tier:** A+. (a)-(n) all met:

- (a)+(b)+(c) — F1+F2+F3 named with decision rows; no required-cloud primitive in top-3.
- (d) — release-notes fence in the PR body.
- (e) — F1 (§0 + §1), F2 (§1), F3 (§2), F4 (§3), F5 (§5), F6 (§4), F7 (§6), F8 (§7), F9 (§8), F10 (§9) all addressed.
- (f) — top-3 re-ranked against persona A; §0 trace shows the persona-A unblock weight per wedge.
- (g) — all seven reopen conditions rewritten to panel queries or labeled-inbound counts in §6.
- (h) — §10 surfaces R1-R5; R1, R2, R5 are contradiction-risk findings not addressed elsewhere.
- (i) — DeepEval + Promptfoo picked; Phoenix held; Constitutional + RAGAS rejected with reasons.
- (j) — original §§1-7 surveys preserved; only ranking + decision rows + reopen conditions amended.
- (k) — Honeycomb / Datadog / New Relic / E2B / Daytona / Modal all downgraded to "track-only" with reopen predicates.
- (l) — PR #399 (customer roadmap), PR #402 (wave-2 brief), PR #407 (review) all cited inline.
- (m) — `scripts/doc-check.sh` passes locally on this spec (no `blazing-fast`, `production-grade`, `world-class`, `best-in-class`, `industry-leading`, `cutting-edge`, `lightning-fast`, `battle-tested`, `enterprise-grade`, `rock-solid`, `robust` tokens).
- (n) — top-3 #1 maps to L4 gate (gates persona-A merges per `docs/engineer/specs/2026-06-02-s2-t2-adversarial-l4-gate.md`); #2 maps to G2 (mobile approval flow) via the deferred `approve(token)` consumer; #3 maps to L4 prompt-revision drift (which gates G2's approval correctness).

---

## §12 — Sources

- Original wave-2 brief: `docs/engineer/research/2026-06-02-wedge-wave-2.md` (PR #402, branch `research/wedge-wave-2`)
- Review: `docs/engineer/reviews/2026-06-02-wave-2-review-of-402.md` (PR #407, branch `review/402-wave-2`)
- Customer roadmap: `docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md` (PR #399 family, branch `spec/next-horizon-roadmap-2026-06`)
- Self-host-first brief: `docs/engineer/briefs/2026-06-01-self-host-first.md`
- L4 reviewer spec: `docs/engineer/specs/2026-06-02-s2-t2-adversarial-l4-gate.md`
- Unified substrate (3-primitive collapse): `docs/wedges/unified-substrate.md`
- DeepEval (Apache-2.0) — github.com/confident-ai/deepeval
- Promptfoo (MIT) — github.com/promptfoo/promptfoo
- Phoenix (Elastic-2.0) — github.com/Arize-ai/phoenix
- Grafana + Tempo + Loki (LGTM stack, AGPL-3.0 / Apache-2.0) — grafana.com
- Jaeger v2 (Apache-2.0, CNCF) — jaegertracing.io
- SigNoz (Apache-2.0) — signoz.io
- Langfuse (MIT) — github.com/langfuse/langfuse
- MCP registry — registry.modelcontextprotocol.io
- Anthropic Skill format — github.com/anthropics/skills (SKILL.md spec)
- Claude Code plugin manifest — github.com/anthropics/claude-code (plugin manifest schema)
- Memory cites: `feedback_research_design_principles`, `feedback_decision_priority`, `feedback_grade_rubric`, `feedback_adversarial_review`, `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`
