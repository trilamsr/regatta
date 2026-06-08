---
status: draft
date: 2026-06-07
supersedes: none
extends:
  - docs/engineer/briefs/2026-06-01-regatta-research-vision.md
  - docs/engineer/specs/2026-06-01-research-mode-extension-design.md
---

# Research-loop gap-bridge — delta over the locked research-mode spec

## 1. Why this brief exists

The locked research-mode spec (`docs/engineer/specs/2026-06-01-research-mode-extension-design.md`) covers the falsifiable-contract shape, four methodology gates, K=10 reproducibility cron, four Rego rules, and Pandoc renderer. It is Phase-X-gated; will not dispatch until substrate Wave 1 + W8 slim + W9 substrate-default + arch-simplification land AND the Phase-X trigger fires.

A four-agent prior-art survey (thesis-generation, evidence-DAG/provenance, validation/adversarial-LLM, repo-internal audit) was run 2026-06-07 to test whether the locked spec covered every primitive the research loop needs. **It does not.** Seven primitives surfaced as genuine gaps; one is wedge-shaped, six are field-additions or hook-points that should land alongside (not after) the existing Task 0 schema extensions.

This brief enumerates the deltas, names the OSS to adopt, scopes the cost, and proposes whether each delta amends Task 0, lands as a new Task 7, or stays out-of-scope.

## 2. What the locked spec already covers (do NOT re-litigate)

| Locked primitive | Source |
|---|---|
| `WorkItem.kind="research"` discriminator | spec §2.1 |
| `prereg` sub-block (claim_direction, primary_metric, dataset.sha256, baselines[].artifact_sha256, statistical_test, n_planned, stop_rule) | spec §2.2 |
| `KindReproVerdict` event kind | spec §2.3 |
| `CriterionStateRefuted` + `CriterionKind` (confirmatory/exploratory) | spec §2.4 |
| Four PR-blocking gates: p-hack, power, leakage, stattest | spec §3 |
| Nightly K=10 repro cron + cost-gov wiring | spec §4 |
| Four Rego rules (promote / override / publish / retract) | spec §5 |
| Pandoc-renderer (NOT generator) for publication | spec §6 |
| Trap corpus (capture-the-flag fixture set) | spec §10 |
| HMAC signing via `contracts/schemas/sign.go` | spec §0 + §2.3 |

Net: the spec is comprehensive on the *first-run* of a thesis. Where it is thin: what happens between thesis #1 and thesis #N.

## 3. Seven gaps — delta over locked spec

### 3.1 Thesis-graveyard query primitive (NEW)

**Gap.** Spec covers `CriterionStateRefuted` as a terminal state. No primitive lets a new thesis query "has this hypothesis shape been tried + refuted before?" before dispatch. Without it, the loop re-walks dead branches.

**Adopt.** `claude-mem` corpus is already in the repo plugin set (`.claude/settings.json` enables `claude-mem@thedotmack`); FTS5-backed; supports `memory_search` / `observation_search`. No new dep.

**Shape.** One subcommand: `regatta research graveyard --query "<claim text>"` → returns matching prior `WorkItem.kind=research` rows where `Criterion.State=refuted` OR latest `KindReproVerdict.reproduced=false`. Plain SQL over substrate + claude-mem; ~80 LoC Go.

**Where it lands.** New Task 7 (sequential after Task 0). File-disjoint from Tasks 1-6.

**Effort.** S (3-5d).

### 3.2 Base-rate prior consultation gate (NEW)

**Gap.** Spec has no rule requiring a thesis to consult prior runs before dispatch. Adversarial-review-every-step rule in CLAUDE.md applies post-dispatch; base-rate is a pre-dispatch hook.

**Adopt.** Same `claude-mem` substrate; same query as graveyard. Pattern from LangGraph checkpointers (https://github.com/langchain-ai/langgraph) — reference only.

**Shape.** Implementer dispatch brief for `kind=research` gains one required step: "before forming hypothesis, query graveyard + cite N prior attempts (success / refuted / inconclusive) in the brief". Enforced via amendment to `docs/engineer/dispatch-templates/implementer.md` (worker-prompt parity gate at `scripts/check-prompt-parity.sh` propagates to `internal/orchestrator/spawner/claude.go::defaultPromptBuilder`) plus a check in the p-hack gate: missing base-rate citation → WARN; missing for confirmatory criteria → FAIL.

**Where it lands.** Amends Task 1 (`phack` gate gains one citation check) + amends `docs/engineer/dispatch-templates/implementer.md` (worker-prompt parity gate auto-propagates).

**Effort.** XS (1-2d).

### 3.3 OTel `traceparent` seed propagation (AMEND Task 5)

**Gap.** Spec §4.1 step 3 says "Run K=10 fresh seeds in parallel via existing `Spawner.Spawn`". No mention of how `{trace_id, rng_seed, model_sha, prompt_sha}` propagates into each subagent invocation. Without it, the K=10 cron cannot be replayed bit-exact.

**Adopt.** OpenTelemetry Go SDK (https://github.com/open-telemetry/opentelemetry-go) v1.30.0, Apache-2.0. NoopExporter satisfies self-host scope (no external collector required). Pattern: W3C `traceparent` header carries trace+span IDs; extend with `regatta-research/v1` baggage entries for seed/model/prompt SHAs.

**Shape.** `Spawner.Spawn` extended to accept a `ResearchContext{TraceID, SpanID, RNGSeed, ModelSHA, PromptSHA}` struct; values render into the subagent invocation env as `REGATTA_RESEARCH_*` vars. Existing OBS-C dispatch-span work (`internal/obs/dispatch/spans.go`) already carries trace context; this extends it.

**Where it lands.** Amends Task 5 contract.

**Effort.** S (3-5d, file-disjoint with Tasks 1-4).

### 3.4 Content-addressed LLM memo cache (NEW Task 8)

**Gap.** Spec has no memoization layer. Identical sub-queries (e.g. canary-extraction prompts repeated across K=10 seeds, repeated leakage scans against the same `train_manifest_sha`) re-spend LLM tokens.

**Adopt.** Bazel REAPI CAS digest shape (`sha256:<hex>/<size>`) over substrate row; impl trivial in SQLite. No new dep.

**Shape.** New helper `internal/cache/llmmemo/`: `Key = sha256(canonical(prompt + model + seed + tool_set))`. Hit → return cached `tool_call` event payload. Miss → invoke + write. Cost-gov wins twice: budget+latency.

**Where it lands.** New Task 8 (parallel with Task 5; gated by Task 0).

**Effort.** S (3-5d).

### 3.5 in-toto / SLSA envelope shape for evidence events (AMEND Task 0)

**Gap.** Spec §2.3 defines `KindReproVerdict` payload as a flat JSON object signed by HMAC. Verifiable-evidence consumers (external auditors, future supply-chain checks) expect a predicate-shaped envelope.

**Adopt.** in-toto Attestation Framework v1.0 (https://github.com/in-toto/attestation/blob/v1.0/LICENSE) Apache-2.0; envelope is plain JSON. SLSA v1.0 predicate schema (Community Specification License 1.0: https://github.com/slsa-framework/slsa/blob/main/LICENSE.md — schema-only use; no code vendored). Skip DSSE signing (defer to Phase X Sigstore swap; HMAC stays for now).

**Shape.** `KindReproVerdict` payload gains three additive fields: `predicateType: "https://regatta.io/research/repro-verdict/v1"`, `subject: [{name, digest: {sha256}}]`, `predicate: {<existing payload>}`. Existing fields unchanged. Migration: writer canonicalization unchanged; readers ignore unknown fields.

**Where it lands.** Amends Task 0 schema extension (one row in payload schema).

**Effort.** XS (1d).

### 3.6 Adversarial-thesis subagent role (AMEND dispatch templates)

**Gap.** CLAUDE.md `feedback_adversarial_review_every_step` says every load-bearing artifact gets an adversarial pass. Spec §3 has four gate runners but no role for an adversarial subagent that tries to *break* the thesis itself (find counter-examples, propose null-hypothesis interpretations, hunt unstated assumptions).

**Adopt.** Anthropic debate work (arXiv:1805.00899) — pattern only. Existing dispatch-template pattern (`docs/engineer/dispatch-templates/reviewer.md`) extends with a `--mode=antithesis` flag. No new framework dep.

**Shape.** New dispatch-template section in `reviewer.md`: when invoked with `--mode=antithesis` against a `kind=research` work-item, the reviewer subagent's prompt scope flips from "is the PR safe to merge" to "what would falsify this thesis if we ran the experiment differently". Output is a `kind=gate_verdict` row with verdict=`antithesis_pass` or `antithesis_fail`. Required before publish (joins the four spec §3 gates as gate #5).

**Where it lands.** New Task 9 (after Tasks 1-4 ship). Pure dispatch-template + one gate shim.

**Effort.** S (3-5d).

### 3.7 SPRT / Bayesian early-stop on K=10 repro (AMEND Task 5)

**Gap.** Spec §4.1 hardcodes K=10 seeds. If the first 3 seeds wildly confirm or wildly refute, the remaining 7 are wasted budget.

**Adopt.** SPRT (Wald 1945); impl over gonum/stat distuv (https://github.com/gonum/gonum) v0.15.1, BSD-3. ~50 LoC. gonum is NOT in `go.mod` today; SPRT lands as new dep at this delta's filing time. Bayesian early-stop is the more ambitious form; defer.

**Shape.** `regatta research repro` gains `--early-stop sprt|none` flag (default `sprt`). After each seed, run SPRT step against null `mean = baseline`; stop when log-likelihood-ratio crosses threshold. Always run minimum N=3 (catch CUDA-nondet leak); ceiling stays K=10. Cost-gov sees the win.

**Where it lands.** Amends Task 5.

**Effort.** S (3-5d).

## 4. Net delta table

| # | Gap | Disposition | Effort | New code | New dep |
|---|---|---|---|---|---|
| 3.1 | Thesis-graveyard query | Task 7 (new) | S | ~80 LoC | none |
| 3.2 | Base-rate prior gate | Amend Task 1 + Task 0 prompt-parity | XS | ~30 LoC + prompt entry | none |
| 3.3 | OTel traceparent seed propagation | Amend Task 5 | S | ~150 LoC | OTel Go SDK (additive) |
| 3.4 | LLM memo cache | Task 8 (new) | S | ~120 LoC | none |
| 3.5 | in-toto envelope shape | Amend Task 0 | XS | ~20 LoC | none (schema-only) |
| 3.6 | Antithesis subagent role | Task 9 (new) | S | ~80 LoC + template | none |
| 3.7 | SPRT early-stop on K=10 | Amend Task 5 | S | ~50 LoC | gonum BSD-3 (NOT in go.mod) |

**Net:** ~530 LoC additive, two new external deps (OTel Go SDK Apache-2.0, gonum BSD-3 — both NoopExporter / no-server-required), three new tasks (7, 8, 9), three amendments (Tasks 0, 1, 5). All deltas inherit the locked spec's Phase-X gate; nothing dispatches before substrate Wave 1 + W8 slim + W9 substrate-default + arch-simplification land.

## 5. Out of scope (cut by this brief)

These surfaced in prior-art survey but FAIL the self-host filter or duplicate existing primitives:

- **Multi-agent debate framework (AutoGen / MetaGPT / CrewAI)** — Python; orchestration-bloated; existing spawner + dispatch-template pattern covers the workload at ~5% of the runtime cost.
- **EventStoreDB / XTDB / Materialize** — server daemons; substrate already covers append-only role.
- **PROV-DM RDF serialization** — borrow the three-noun model (Entity / Activity / Agent) as event-row fields; skip Turtle/RDF.
- **MLflow / Weights+Biases / DVC** — over-built for single-operator; substrate + claude-mem corpus + RO-Crate export already cover the workload.
- **Bayesian early-stop (full)** — SPRT is the cheap form; full Bayesian deferred until N>20 dispatches/arm of cold-start data exist.
- **Reproducible-Builds rebuilderd architecture** — GPL-3; reference only; pattern already in spec via K=10 cron.
- **OpenInference / Phoenix span clustering** — deferred per `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md`; revisit when self-improve detector rules R6-R11 fire baseline data.
- **Bandit-scheduled hypothesis dispatch (Thompson sampling, UCB1)** — needs N>=20 dispatches/arm for signal; defer to post-MVR-1 when corpus exists.
- **Trap-projection / inefficiency-detector pattern mining** — already covered by R6-R11 self-improve detector spec (`docs/engineer/specs/2026-06-02-phase-autonomy-w4-self-improvement-detector.md`); not a research-mode concern.
- **Property-based (Hypothesis-style shrinking)** — Go 1.18+ native `go test -fuzz` covers code-level falsifier execution; no new primitive needed.

## 6. Docs that need updating

Audit verdict after the four-agent survey:

| Doc | Current state | Update needed |
|---|---|---|
| `docs/engineer/specs/2026-06-01-research-mode-extension-design.md` | Locked, comprehensive on first-run | Append §16 cross-referencing this brief's seven deltas; no body changes (spec stays locked) |
| `docs/engineer/briefs/2026-06-01-regatta-research-vision.md` | Vision; pre-cut by adversarial review | No change — vision is intact; deltas are implementation-layer |
| `docs/engineer/specs/2026-06-02-phase-autonomy-w4-self-improvement-detector.md` | W4 detector + R6-R11 surface | No change — meta-improvement loop is separate from research-mode loop; do not conflate |
| `docs/engineer/dispatch-templates/reviewer.md` | Reviewer subagent prompt | Add `--mode=antithesis` stanza per delta 3.6 (deferred until research-mode dispatches) |
| `docs/engineer/dispatch-templates/implementer.md` | Implementer prompt | Add base-rate-citation rule for `kind=research` items per delta 3.2 (deferred until research-mode dispatches) |
| `CLAUDE.md` | Universal agent rules | No change — research-mode rules stay scoped to research-mode docs until dispatch; do not pollute universal surface |
| `contracts/schemas/work_item.schema.json` | WorkItem schema | No change yet — Task 0 amendment per delta 3.5 lands when research-mode dispatches |
| `internal/orchestrator/state/substrate/event.go` | Event-kind enum | No change yet — `KindReproVerdict` lands when research-mode dispatches |

**Net docs update in this PR:** ONE file (this brief). Two dispatch-template amendments (per deltas 3.2 + 3.6) queued for the research-mode dispatch window — they do NOT land in this PR. Spec amendment (§16 cross-ref) optional; deferred.

## 7. Filing plan

Per `feedback_unaddressed_load_bearing`, every load-bearing leftover gets a tracking issue. For each delta:

| # | Delta | Issue title |
|---|---|---|
| 3.1 | Thesis-graveyard | `[RESEARCH-DELTA] thesis-graveyard query primitive (Task 7)` |
| 3.2 | Base-rate prior | `[RESEARCH-DELTA] base-rate prior consultation gate (Task 1 amend)` |
| 3.3 | OTel seed propagation | `[RESEARCH-DELTA] OTel traceparent + seed propagation through Spawner (Task 5 amend)` |
| 3.4 | LLM memo cache | `[RESEARCH-DELTA] content-addressed LLM memo cache (Task 8)` |
| 3.5 | in-toto envelope | `[RESEARCH-DELTA] in-toto/SLSA envelope on KindReproVerdict (Task 0 amend)` |
| 3.6 | Antithesis subagent | `[RESEARCH-DELTA] adversarial-thesis subagent role (Task 9)` |
| 3.7 | SPRT early-stop | `[RESEARCH-DELTA] SPRT early-stop on K=10 repro cron (Task 5 amend)` |

Each issue body: cite this brief, name the trigger predicate (= research-mode dispatch + Phase-X trigger), name the OSS adopted with version+license. Label `research-delta` (new label; create on first filing) + `phase-x` (inherits research-mode gate).

## 8. Open questions for operator

1. **Q1.** File the seven `[RESEARCH-DELTA]` tracking issues now (against Phase-X gate), or defer until research-mode actually opens scope? Default if no answer: file now — durability rule per `feedback_unaddressed_load_bearing`.
2. **Q2.** Add §16 cross-reference to the locked research-mode spec, or leave the spec untouched? Default if no answer: leave untouched — the spec is locked and this brief carries the cross-reference instead.
3. **Q3.** Promote any delta (most-likely candidate: 3.1 thesis-graveyard) ahead of the Phase-X gate, since claude-mem corpus already exists today? Default if no answer: no — Phase-X gate is structural; do not relitigate.

## 9. References

- Locked research-mode spec: `docs/engineer/specs/2026-06-01-research-mode-extension-design.md`
- Research vision: `docs/engineer/briefs/2026-06-01-regatta-research-vision.md`
- Self-host filter: `docs/engineer/briefs/2026-06-01-self-host-first.md`
- in-toto attestation v1.0: https://github.com/in-toto/attestation
- SLSA v1.0: https://github.com/slsa-framework/slsa, LICENSE: https://github.com/slsa-framework/slsa/blob/main/LICENSE.md (Community Specification License 1.0; schema-only use)
- OpenTelemetry Go SDK v1.30.0: https://github.com/open-telemetry/opentelemetry-go
- Bazel REAPI digest shape: https://github.com/bazelbuild/remote-apis
- gonum v0.15.1: https://github.com/gonum/gonum
- Anthropic debate (arXiv:1805.00899): https://arxiv.org/abs/1805.00899
- Wald, "Sequential Tests of Statistical Hypotheses" (1945)
- LangGraph checkpointers (reference pattern): https://github.com/langchain-ai/langgraph
