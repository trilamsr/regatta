# regatta — research-mode vision brief (post MVP-4 candidate)

_Author: design subagent, 2026-06-01. Source-of-truth: `docs/wedges/research-mode.md` (thesis) + `docs/engineer/specs/2026-06-01-research-mode-extension-design.md` (locked design) + adversarial review of original 6-component synthesis (70% cut by reviewer subagent)._

## 1. Where regatta IS, post-MVP-4 (the assumed pre-condition)

- Substrate Wave 1 shipped: `substrate_events` is the single signed audit-replay journal for every wedge.
- W6 OTel backbone: traces correlate across LLM spans, gate decisions, scheduler ticks.
- W7 operator web UI: approvers click links, see DAG context + cost + Approve/Reject.
- W8 OPA RBAC: `Authorizer` interface is the policy hook; Rego rules ship per surface.
- W9 replay/diff: deferred via Option D unless substrate proves insufficient.
- W10 Sigstore: signed prompts + signed events with Rekor transparency log.
- W11 blackboard: `substrate_blobs` content-addressed store with `BlobDigest`.
- W12 billing: per-tenant USD rollup + Stripe metered-usage export.

Today the primitives that drive code work (deterministic test as success signal, immutable acceptance criteria, signed gate verdicts) sit unused for any non-code workflow.

## 2. Gap between MVP-4 and "regatta runs research"

- **No falsifiable-contract shape for empirical claims.** `WorkItem` accepts an acceptance criterion as free text. A research claim needs a structured prereg: claim direction, metric, dataset version + sha256, baselines with checksums, statistical test, N planned, stop rule.
- **No methodology gates.** The L0-L5 stack (L0 shipped today; L1-L5 deferred per `docs/design.md`) targets spec-immutability, repo CI, PR-body conformance, spec-conformance (Opus), adversarial review (Sonnet), drift (Haiku). None of them, even once L1-L5 ship, catch p-hacking, train/test leakage, underpowered N, or wrong statistical test selection.
- **No reproducibility lifecycle.** A code PR merges when CI is green. A research result needs K=10 fresh-seed re-runs to bound variance — a process that takes hours and cannot block PR merge.
- **No refutation-as-deliverable.** `Criterion.State` today is `{planned, in_progress, done}`. A confirmatory study that fails is the most important thing research mode produces; it has no representation.
- **No content-addressed dataset pin.** Datasets exist outside the repo. A leakage gate without a dataset CID is security theater. W11's `BlobDigest` solves this; research-mode is one of its first consumers.
- **No methodology-trap catalog.** `docs/incidents.md` enumerates AI-agent code incidents. Research-mode needs the methodology analog: HARKing, garden-of-forking-paths, optional stopping, base-rate neglect, Simpson's paradox, dataset overfit, prompt-sensitivity, eval-leakage, citation laundering.

## 3. Next-level vision

**Tagline: regatta = the only fleet orchestrator whose primitives map 1:1 to preregistered empirical research.** Control-plane-for-AI-labor framing holds. The moat sharpens: the same falsifiability discipline that makes regatta operator-credible for code (mandatory L0 + mandatory gates + signed audit + cost cap + human merge) is structurally identical to what makes a research orchestrator publication-credible. AsPredicted preregistration + ICH E9 confirmatory/exploratory split + Registered Reports in-principle acceptance map onto L0 immutability + `Criterion.Kind` + the existing gate stack. A research result becomes a publishable artifact when the same primitives that gate a code PR have all cleared — no parallel ceremony.

This is NOT a pivot from code orchestration. Code work and research work share the same fleet primitives because they share the same epistemic shape: a falsifiable contract committed before the work, mechanical verdicts at the end, an immutable audit trail in between. The 70% of the original research-mode proposal that the adversarial review cut was the parallel-architecture portion: separate HypothesisAdapter, separate Workspace abstraction, separate Claim DSL, separate ResultBundle storage. None of that earned its place. The 30% that survived is the discriminator + four gate packages + one cron + four Rego rules. Everything else is existing regatta.

## 4. Ranked wedge sequence for MVR-1 (post-MVP-4)

### Task 0 — Schema extensions (single-file, gate-keeping)  _(MVR-1, rank #1)_
- **Elevator**: one CUE enum value (`KindResearch`), one optional `prereg` sub-block on `work_item.schema.json` (seven or fewer fields), one event-kind value (`KindReproVerdict`), one `Criterion.State` value (`refuted`), one optional `Criterion.Kind` field. No new schema files.
- **Trap fit**: P3 (trusted instructions from `main` only — the prereg is the contract).
- **Prior art adopted**: AsPredicted 9-question template (smallest validated working set); ICH E9 confirmatory/exploratory split (30 years of regulatory practice). Rejected OSF 30-field template — ceremony kills adoption per `feedback_drop_ceremony`.
- **Why first**: every downstream gate reads the prereg sub-block; nothing else dispatches until this lands.
- **UX-first**: operator adds `kind: research` + a `prereg:` block to their existing `.regatta/items/*.md`. No new file shape, no new directory.
- **Reference bar**: matches AsPredicted floor; deviation tracked per `feedback_spec_pattern_authority`.
- **Dependencies**: substrate Wave 1, W10 Sigstore (for signing the locked prereg).
- **Slot**: MVR-1 Wave 1.
- **Effort**: small — single PR, file-disjoint.

### Tasks 1-4 — Four methodology gates (parallel)  _(MVR-1, rank #2-#5)_
- **Elevator**: four PR-blocking gates under `internal/gates/research/`: p-hack (prereg diff), statistical-power (statsmodels.solve_power), leakage (MinHash-LSH + suffix-array + canary), statistical-test (paired bootstrap + BCa CI / McNemar / Wilcoxon / Friedman+Nemenyi).
- **Trap fit**: P6 (verified grounding for every published claim).
- **Prior art adopted**: Gelman & Loken garden-of-forking-paths (p-hack); Cohen power framework via `statsmodels.stats.power` (power); Lee et al. + Sainz et al. + Carlini et al. dedup + contamination + canary extraction (leakage); Dror et al. ACL P18-1128 decision tree (stat-test). All four gates ship Python sidecars under the existing L3/L4/L5 sidecar pattern; Go shims write `kind=gate_verdict` events.
- **Why now**: gates are the load-bearing novel value. p-hack + statistical-power are Tier-1 (ms, PR-blocking). Leakage + stat-test are Tier-2 (minutes, PR-blocking).
- **UX-first**: red gate verdict shows up in the existing W7 operator UI panel; root cause cited in the verdict payload; reviewer overrides via OPA-gated Rego rule.
- **Reference bar**: matches statcheck auditor model in psychology; no equivalent exists in ML upstream — meaningful contribution.
- **Dependencies**: Task 0 schema extensions.
- **Slot**: MVR-1 Wave 2 (3 parallel — file-disjoint per `feedback_dispatch_strategy`). Wave 3 ships the fourth gate.
- **Effort**: medium per gate (400-800 LoC each: Go shim + Python module + fixture corpus + mutation testing).

### Task 5 — Reproducibility cron + cost-gov wiring  _(MVR-1, rank #6)_
- **Elevator**: `regatta research repro --work-item <id> --k 10` runs K=10 fresh seeds via existing `Spawner`, computes sigma + 95% CI, writes a signed `kind=repro_verdict` event. Cost-governor reads the `usd_cents` payload; per-item daily cap defaults `$10/day`.
- **Trap fit**: P8 (spend brakes with mandatory re-approval).
- **Prior art adopted**: Bouthillier et al. arXiv:2103.03098 — K=10 seed variance accounting is the canonical ML reproducibility floor. `torch.use_deterministic_algorithms(True)` + `CUBLAS_WORKSPACE_CONFIG=:4096:8` + `cudnn.benchmark=False` catch CUDA-nondeterminism leaks.
- **Why now**: research-mode WorkItem becomes publishable iff (a) all four gates PASS at PR-merge AND (b) latest `kind=repro_verdict` has `reproduced=true`. The cron decouples slow K=10 confirmation from fast PR merge.
- **UX-first**: publishability is a binary signal in the operator UI; the K=10 latency is invisible to PR review.
- **Reference bar**: matches Bouthillier's variance accounting; cost-gov ties reproducibility spend to the same budget surface as code work.
- **Dependencies**: Task 0; at least 2 of Tasks 1-4 merged; W11 CAS (dataset pins).
- **Slot**: MVR-1 Wave 3.
- **Effort**: small — single subcommand + cost-gov reader registration + one substrate writer.

### Task 6 — Four OPA Rego rules  _(MVR-1, rank #7)_
- **Elevator**: `policies/research/*.rego` ships four rules — `promote_criterion`, `override_gate`, `publish_bundle`, `retract_claim` — loaded via the W8 Authorizer adapter.
- **Trap fit**: P2 (HITL ergonomics) + P3 (trusted-main only).
- **Prior art adopted**: W8 OPA RBAC adapter contract (one-file impl swap per W7 spec §3.6.4). Rejected bespoke ACL — OPA already shipping for code-side surfaces.
- **Why now**: confirmatory→exploratory promotion is the highest-risk operator action; two-key retraction is mandatory for published claims.
- **UX-first**: operator overrides a red verdict with a tracking-issue URL; Rego rule rejects on missing URL.
- **Reference bar**: matches GitHub branch protection 2-reviewer requirement for sensitive actions.
- **Dependencies**: W8 OPA Authorizer; Task 0; Tasks 1-5.
- **Slot**: MVR-1 Wave 4.
- **Effort**: small — four Rego files + property tests.

## 5. Renderer (NOT generator) for publication

The publication path is **never** an LLM. Pandoc-template substitution over the substrate event fold: `regatta export-bundle <work_item_id>` emits RO-Crate (`ro-crate-metadata.json`) + BagIt sha256 manifest + LaTeX rendered from `templates/paper.tex.j2`. Pandoc render fails on any unsubstituted `{{claim_id}}` placeholder — guarantees every published sentence binds to a `Criterion` (state=done or state=refuted) citing a signed `gate_verdict` event. Galactica's failure mode is structurally precluded.

Auto-LLM Methods, related-work, discussion: out of scope, never.

## 6. What this brief is NOT

- A pivot. Code orchestration remains the primary surface. Research-mode is a thin overlay.
- A commitment. Research-mode dispatches ONLY after substrate Wave 1 + W8 + W10 + W11 + the architecture-simplification pass all land. If a wedge needs to ship sooner, re-litigate the spec.
- A claim that LLMs do science. The orchestrator enforces methodology. Whether a hypothesis is worth running is a human call.
- A bootstrap roadmap. Stages 3-6 of the original synthesis (spec-drafter, roadmap-proposer, self-modifying scheduler) violate Trap P11 (agent artifact pipelines as attack surface) and are deleted. Stages 0-2 (reactive janitor, doc/test surgeon, spec-implementer) fit inside the existing autonomous-session-prompt flow and do not need a separate roadmap.

## 7. Roadmap impact (summary; details in `2026-06-01-arch-simplification-pass.md`)

The research-mode wedge surfaces a deeper observation: regatta has accumulated parallel hierarchies (sibling-package `Estimator` declarations whose seam-vs-impl split needs an audit, three-way `planner.go`/`planner_v2.go`/`planner_stub.go` fork, `cost/{5 sub-packages}` + `orchestrator/{adapter,adaptersync,lockfile}` splittable trees, ~12 spec files in `docs/engineer/specs/` where 3 should be live). The substrate spec catches this for `kind=*` events. The adapter-contracts spec exhibits it (5 adapters, 0 second consumers). Research-mode would exhibit it again if shipped naively.

The companion brief `2026-06-01-arch-simplification-pass.md` documents the collapse pass that must precede MVR-1: re-sequence MVP-3 behind substrate Wave 1; delete the adapter-contracts spec; flatten the splittable single-consumer sub-package trees; pick-one between the three-way `planner.go`/`planner_v2.go`/`planner_stub.go` fork; audit the `Estimator` seam-vs-impl split during the cost-package flatten. Until that pass lands, research-mode is forbidden to dispatch.

## 8. Grade rubric reference

The locked spec (`docs/engineer/specs/2026-06-01-research-mode-extension-design.md`) carries the B/A/A+ rubric per `feedback_grade_rubric`. Implementer PRs MUST post the scorecard verbatim. Reviewer subagent re-scores independently per `feedback_adversarial_review`. Risk-tier findings filed as tracking issues per `feedback_unaddressed_load_bearing` before automerge per `feedback_review_before_automerge`.

## 9. References

- Wedge: `docs/wedges/research-mode.md`
- Spec: `docs/engineer/specs/2026-06-01-research-mode-extension-design.md`
- Companion brief: `docs/engineer/briefs/2026-06-01-arch-simplification-pass.md`
- AsPredicted (https://aspredicted.org/) — prereg field floor
- ICH E9 R1 (https://www.ema.europa.eu/en/documents/scientific-guideline/ich-e9-r1-addendum-estimands-and-sensitivity-analysis-clinical-trials-guideline-statistical-principles-clinical-trials-step-5_en.pdf) — confirmatory/exploratory split
- Bouthillier et al., arXiv:2103.03098 — K=10 seed variance accounting
- Lee et al., arXiv:2107.06499 — MinHash + ExactSubstr dedup reference impl
- Sainz et al., arXiv:2310.18018 — per-benchmark contamination measurement
- Carlini et al., arXiv:2202.07646 — canary extraction protocol
- Dror et al., ACL P18-1128 — significance-test decision tree
- Gelman & Loken — garden of forking paths
