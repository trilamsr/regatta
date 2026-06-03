# Positioning: regatta vs. emergent multi-agent doctrine

Status: positioning doc, not a roadmap commitment. Authored from
the quarterly competitive scan logged in
[`docs/wedges/README.md`](../wedges/README.md#watch-item).
Updates land here as the agent-orchestration landscape evolves;
the wedges README "logged scans" list points back to this file.

## Why this doc exists

The closest single-vendor agent-orchestration competitor is also
the only one to have *publicly articulated and then walked back*
a doctrine on multi-agent architectures. That makes the
competitor stack the single most important external reference
point for regatta's design choices. This doc is the load-bearing
reconciliation between the public "don't build multi-agents"
argument and regatta's plan-as-code + substrate + DAG
orchestration posture.

The reconciliation is structural, not rhetorical. regatta's
unified-substrate primitives (events log, typed reducers, the
single `Decider` interface) directly satisfy the Principle 1
invariant the polemic demands, but at the orchestrator scope
rather than within a single agent context window. Detailed
argument in §3.

## 1. Competitor stack snapshot

Generic descriptors; vendor names withheld from this prose by
project policy. Specific blog URLs in §References are sufficient
for citation audit.

| Layer | Pattern | Notes |
|---|---|---|
| Cloud agent | Sandboxed Linux VM agent | shell + browser + computer-use; agent self-verifies via end-to-end testing. |
| IDE / fleet | Kanban surface over local + cloud agents | Shared-context "spaces" buckets between sessions; supports an open editor-agent JSON-RPC protocol so third-party agents plug in alongside. |
| Model | Co-trained base + harness | Hundreds-of-billions parameters, served on dedicated accelerator hardware at ~950 tok/s; trained end-to-end RL on the agent harness. |
| Retrieval | RL-trained context-distiller subagent | ~2,800 tok/s; distills context, never decides. Powers fast-context, codebase-understanding, and tab-complete surfaces. |
| Infra | Hypervisor + incremental-snapshot file system | Concurrent VM fleet for tens of thousands of agent sandboxes. |
| Business | ~$1B Series D @ ~$26B valuation (mid-2026); ~$492M ARR. "89% of own code committed by their own agent." | Pricing: $20 / $200 / $80+$40-seat tiers. Compute unit ≈ 15 min agent work. |

Adjacent OSS releases (bibliography, not load-bearing for §3–§6):
a multi-turn-RL CUDA-kernel writer (academic collab) and a
publicly-released SWE-bench eval-harness reproduction.

## 2. The doctrine pivot — 2025 polemic → 2026 walk-back

### 2.1 The polemic (2025)

Headline claim from the competitor's engineering blog:

> "Principle 1: Share context… Principle 2: Actions carry
> implicit decisions, and conflicting decisions carry bad
> results. I would argue that Principles 1 & 2 are so critical,
> and so rarely worth violating, that you should by default rule
> out any agent architectures that don't abide by them."

The illustrative failure mode: two subagents produce conflicting
game assets (one builds a Mario-style background, another builds
a non-game-asset bird) because the up-front decomposition didn't
prescribe shared assumptions.

Endorsed pattern: deliberately-narrow subagents where "the
subtask agent is usually only tasked with answering a question,
not writing any code." Named-as-wrong frameworks: emergent
multi-agent libraries such as the OpenAI `swarm` reference impl
and Microsoft `autogen` — both push *autonomous LLM task
decomposition* rather than *prescribed plan execution*.

### 2.2 The walk-back (2026)

Same blog, 2026-06-02 product-launch post:

> "The best engineers we work with are not just pair programming
> with one agent at a time. They are using agents to scope and
> plan work, delegating tasks to cloud agents, reviewing
> progress, and deciding what makes it to production… We built
> Spaces to enable related agents to share context, so they can
> collaborate effectively on tasks."

The new product ships a Kanban surface over fleets of
single-agent sessions, manually orchestrated by an operator.
ACP-style support lets multiple competing agents launch inside
the same surface. The 2025 polemic now reads as "don't do
*emergent* multi-agent." The competitor has implicitly adopted
the orchestration layer; they refuse to let the agents co-plan.

## 3. regatta's structural answer to Principle 1

The polemic's load-bearing claim: "every action [must be]
informed by the context of all relevant decisions made by other
parts of the system. Ideally, every action would just see
everything else." The fragility argument is real — emergent
multi-agent architectures with isolated contexts produce
conflicting implicit decisions.

regatta does not hand-wave this. The unified-substrate primitives
satisfy the invariant by construction:

- [`docs/wedges/unified-substrate.md`](../wedges/unified-substrate.md)
  §"The one decider" declares a single `Decider` interface with
  three impls (`CELDecider`, `HumanDecider`, `VerifierDecider`).
  Every decision in the system flows through this one interface
  and emits a signed `gate_verdict` event.
- The same dossier's §"Reducer contract" (the typed
  `fold(events WHERE kind=X) → Snapshot` discipline) is the
  mechanism by which every `Decide()` call sees every prior
  decision in its scope. The polemic's "ideally every action
  would just see everything else" is literally
  `events WHERE kind=*` over the substrate.
- [`docs/wedges/plan-as-code.md`](../wedges/plan-as-code.md)
  pattern #9 (file-set conflict compile-check) is the structural
  enforcement of Principle 2: regatta prescribes the
  file-ownership decision *upfront in CUE*, where emergent
  multi-agent architectures discover the conflict at runtime.
- Approval gates + conditional-DAG predicates compose with the
  Decider interface, so HITL reconciliation of conflicting agent
  outputs is a typed primitive, not an out-of-band human
  override.

The structural difference: regatta is **prescribed multi-agent**
(typed plans, shared event log, file-ownership constraints,
single Decider). The polemic targets **emergent multi-agent**
(autonomous LLM task decomposition, no shared event log, no
file-ownership contract). The competitor's 2026 product is now
*manually-orchestrated multi-agent* (Kanban + shared-context
buckets) — the delta is *who orchestrates*: regatta's substrate
vs. the operator's mouse cursor.

## 4. The model-agnostic vs. co-trained bet

| Axis | Competitor | regatta |
|---|---|---|
| Model | Co-trained end-to-end with the in-house harness; "our goal as an agent lab is not to train a model in isolation, but to build a complete agent" | Model-agnostic by design; swap upstream as frontier improves (Anthropic-direct today; LiteLLM proxy in Phase 2 P2.8 for cross-family). |
| Optimization | Re-train on the harness; iterate model + harness jointly. | Iterate substrate + gate stack; rely on vendor model improvement. |
| Risk | 6-month tech-debt cycle whenever frontier models leap. Public release admits some gains reflect harness fit, not pure model gains. | Frontier-model regressions on regatta-shaped tasks cannot be fixed in-house. |
| Reward | Tightly tuned tool-call success rate; predictable latency on dedicated infra. | Operator picks the model that wins the family-stratified catch-rate benchmark per gate; cross-vendor benchmarks (Phase 3 P3.3). |

Both bets are defensible. The regatta bet is grounded in
PRINCIPLES #4 (adopt-when-needed) and the cross-vendor stance —
co-training is a Phase X consideration at best, gated on a
30-day-self-host-green trigger AND evidence that
harness-specific gains exceed cross-family validator coverage.
For now: **capture canonical agent traces during MVP-3 → Phase 2
as cheap insurance**, but do not promote co-training to a wedge.

## 5. Competitor primitive map

| regatta primitive | Competitor equivalent | Verdict |
|---|---|---|
| DAG planning | None public — internal long-horizon LLM planning only | **Differentiator** |
| HITL approval gates | PR-review gate only; no in-DAG node-level approval | **Differentiator** (finer-grained) |
| Parallel agent dispatch | Kanban Command Center (operator-orchestrated; agents do not co-plan) | **Philosophical contrast** — regatta's prescribed-multi-agent vs. competitor's manually-orchestrated-multi-agent |
| Plan-as-code (CUE/YAML) | None public | **Differentiator** |
| Cost governor (per-DAG $/tok) | Compute-unit quotas at plan level; no per-task / per-DAG runtime cap exposed | **Differentiator** (finer-grained) |
| Blackboard (typed facts + reducers + CAS) | Unstructured shared-context "spaces" buckets | **Watch closely** — could evolve toward typed schemas |
| Retrieval primitive | RL-trained retrieval subagent (~2,800 tok/s) | **Pattern absorbed** — cost-governor model-pinning policy (§Sub-trigger in `wedges/cost-governor.md`) |
| IDE protocol | Open JSON-RPC editor-agent protocol (Apache 2.0, third-party origin) | **Pattern absorbed** — protocol target adapter as *client-only* shim; MCP stays primary server surface |

## 6. Absorbed into existing dossiers

§5 maps every competitor primitive to its absorbed location.
The merge-PR description for the doc carries the
files-not-created list and the rejection rationale (durable
home; not re-read after merge).

## 7. Update protocol

When a major competitor event fires (model release, doctrine
post, acquisition, OSS framework drop):

1. Run a competitive-scan agent against the competitor blog +
   adjacent press surface. URLs in §References below.
2. Synthesize into the existing dossier or this doc — do not
   create a new wedge file unless the validation checklist in
   `wedges/README.md` clears AND the anti-wedge filter ("does
   this fold into an existing dossier?") returns no.
3. Append the scan to the "Logged scans" list in
   `wedges/README.md` Watch item.
4. If a competitor primitive *converges* with a regatta
   primitive (e.g., shared-context buckets gain typed schemas),
   promote it from "Watch closely" to "Direct competitor
   primitive" in §5 above and update the affected dossier's
   Defensibility section (or add one).

## References (URLs only)

URLs are kept for citation audit. Vendor / product names absent
from prose by project policy; readers can resolve identity from
the URL host.

- https://cognition.ai/blog/dont-build-multi-agents — the 2025 polemic
- https://cognition.ai/blog/introducing-devin-desktop — the 2026 walk-back
- https://cognition.ai/blog/swe-1-5 — co-trained model + harness post
- https://cognition.ai/blog/swe-grep — retrieval-distiller subagent
- https://cognition.ai/blog/kevin-32b — multi-turn-RL CUDA-kernel writer
- https://cognition.ai/blog/swe-bench-technical-report — eval methodology
- https://github.com/agentclientprotocol/agent-client-protocol — third-party IDE protocol
