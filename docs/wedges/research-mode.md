# Wedge: research-mode (autonomous empirical AI/CS research)

Prospective. **Phase X wedge** per
[`docs/engineer/briefs/2026-06-01-self-host-first.md`](../engineer/briefs/2026-06-01-self-host-first.md).
See [`README.md`](./README.md) for ranking and the
adopt-when-needed gate. Adopt-when-needed trigger: the 30-day-
self-host-green trigger fires (≥10 PRs/day green-merge for ≥30
days unattended at this repo) OR the first external user with a
machine-readable research backlog asks AND substrate Wave 1 + W8
slim authorizer + Phase S2 W9 substrate-default `DurableHistory`
impl all landed. Phase X wedges (W10 Sigstore, W11 blackboard
CAS, W12 billing) are NOT in the dependency chain — research-
mode rides existing primitives (`contracts/schemas/sign.go`
HMAC, the `prereg.dataset.sha256` field declared on `WorkItem`,
local publish). Migration shapes documented in the spec.

## Thesis

Regatta's primitives — `SpecAdapter`, `WorkItem` with immutable
acceptance criteria, deterministic gate stack, signed
substrate-event log, content-addressed blob store — are
methodology-agnostic. They were built to drive code work, but
their shape (falsifiable contract + mechanical enforcement + audit
trail) is the same shape any empirical methodology demands. Where
the code-work pipeline has `make test` as the deterministic
success signal, an empirical research pipeline has a
preregistered statistical claim with the same property: the
verdict is mechanical given a fixed protocol + fixed dataset +
fixed analysis plan.

The wedge is to extend two existing schema files (`spec_adapter.go`
+ `work_item.schema.json`) with a `kind="research"` discriminator,
an optional `prereg` sub-block, a new `repro_verdict` event kind,
a `refuted` criterion state, and an optional `confirmatory`/
`exploratory` criterion kind — plus four gate runners (p-hack,
statistical-power, leakage, statistical-test), one nightly
reproducibility cron, and four OPA Rego rules. No new schema files,
no new adapters, no new storage primitives, no new tables.
Research-mode is a thin overlay on the existing fleet primitives,
NOT a parallel architecture.

Maps to **Trap Catalog P3** (trusted instructions from `main`
only — applied to the preregistered hypothesis), **P6** (verified
grounding — applied to every claim citation), and **P11** (agent
artifact pipelines as attack surface — applied to the
publication path, which is renderer-only, never generator).

### Defensibility under Dynamic Workflows

Claude Code Dynamic Workflows can run an ablation sweep in a
single session. They cannot:

- preregister a hypothesis at commit-time and prove later that
  the experiment matched the prereg (no immutable, signed
  acceptance contract);
- mechanically diff a prereg against final results for
  p-hacking categories (no auditor primitive);
- gate publication on K=10 fresh-seed reproducibility (no fleet
  scheduler, no out-of-PR cron with cost attribution);
- carry a refutation as a first-class publishable outcome (no
  `Criterion.state="refuted"` lifecycle).

The wedge widens the gap between "ran an experiment in a session"
and "published a methodologically-defensible result".

## Prior art

| Source | Pattern worth stealing |
|---|---|
| [AsPredicted 9-question template](https://aspredicted.org/) | Minimum-viable preregistration. Smallest validated working set; OSF-30 fails adoption. Floor for the `WorkItem.prereg` sub-block. |
| [ICH E9 R1 — confirmatory vs exploratory split](https://www.ema.europa.eu/en/documents/scientific-guideline/ich-e9-r1-addendum-estimands-and-sensitivity-analysis-clinical-trials-guideline-statistical-principles-clinical-trials-step-5_en.pdf) | 30 years of regulatory practice. Confirmatory criteria byte-lock at L0; exploratory criteria stay open. Maps onto `Criterion.kind` enum. |
| [Center for Open Science — Registered Reports](https://www.cos.io/initiatives/prereg) | In-principle acceptance: review the brief, run the experiment, publish whatever the result says. The same flow regatta already runs for code work (L0 approves the contract; CI exits non-zero or zero; the verdict is mechanical). |
| [Gelman & Loken — Garden of Forking Paths](http://stat.columbia.edu/~gelman/research/unpublished/p_hacking.pdf) | Single analysis path with data-dependent choices behaves like a multiple-comparisons problem. The mechanical defense: structured diff between prereg and final results, by category (metric swap, exclusion added, N changed, seed cherry-pick, baseline swap, test swap, covariate added). |
| [statcheck (Nuijten et al.)](https://www.nuijten.io/statcheck/) | Recomputes p-values from reported test statistics + df, flags inconsistencies. Proves the auditor-tool model in psych. No ML-equivalent exists upstream. |
| [Bouthillier et al., arXiv:2103.03098](https://arxiv.org/abs/2103.03098) | Decomposes ML benchmark variance into seven sources (init, data order, augmentation RNG, dropout RNG, CUDA, hardware, hyperparam tuning) and recommends ten or more fully-random replications. Single-seed deltas are unreliable below rank-correlation 0.7. K=10 is the floor for the reproducibility cron. |
| [Lee et al., arXiv:2107.06499](https://arxiv.org/abs/2107.06499) | Deduplicating training data — MinHash-LSH (Jaccard >= 0.8 on 13-gram shingles) + suffix-array ExactSubstr (>= 50-token contiguous match) catches the bulk of train/eval contamination. Reference implementation in `text-dedup`. |
| [Sainz et al., arXiv:2310.18018](https://arxiv.org/abs/2310.18018) | Per-benchmark contamination as a publication requirement. Loss-gap probe + canary extraction protocol fill the gaps MinHash misses. |
| [Carlini et al., arXiv:2202.07646](https://arxiv.org/abs/2202.07646) | Canary extraction protocol: seed unique strings into training data, probe model with prefixes, measure verbatim continuation. >10% extraction is the FAIL threshold. |
| [Dror et al., ACL P18-1128](https://aclanthology.org/P18-1128/) — Hitchhiker's Guide to Testing Statistical Significance in NLP | Decision tree: paired metric → paired bootstrap + BCa CI; binary outcome → McNemar; multi-system → Friedman + Nemenyi; distributional → Wilcoxon. The minimum stat-test menu. |

## Why this is regatta-shaped, not a separate product

A research-mode adapter sitting outside regatta loses the load-bearing
properties:

- **Immutability**: a research orchestrator without an L0-equivalent
  cannot prove the preregistered claim existed before the experiment
  ran. Regatta's L0 + signed substrate events solves this for free.
- **Audit replay**: a research orchestrator without an append-only
  signed journal cannot reconstruct what was claimed vs what was run.
  Regatta's substrate already journals every gate verdict and node
  output.
- **Cost attribution**: K=10 reproducibility runs are LLM-expensive.
  Without a per-work-item cost cap, the budget evaporates.
  Regatta's cost governor (Wedge: cost-governor) already attributes
  spend per DAG node and denies pre-call when caps are hit.
- **Renderer integrity**: publication needs every sentence in the
  paper to bind back to a substrate event. A standalone tool
  publishing LaTeX has no such constraint and inherits the
  Galactica failure mode (auto-generated citations).

The wedge is a thin overlay specifically because everything it needs
exists in regatta's fleet primitives.

## Data model

**No new tables.** No new schema files. The wedge adds:

- One value to `WorkItemKind` enum (`KindResearch`).
- One optional `prereg` sub-block on `work_item.schema.json`
  (seven or fewer fields: claim_direction, primary_metric, dataset,
  baselines, statistical_test, n_planned, stop_rule).
- One value to `EventKind` enum (`KindReproVerdict`) carrying a
  small JSON payload (<= 1 KiB per Wave 1 substrate budget).
- One value to `CriterionState` enum (`refuted`) and one optional
  `Criterion.Kind` field (`confirmatory | exploratory`).

The four research gates emit the existing `gate_result.schema.json`
shape; their verdicts flow through the existing
`internal/gates/<name>/` package convention.

## Mapped to the trap catalog

| Trap | Hook |
|---|---|
| P3 (trusted instructions from `main` only) | `WorkItem.prereg` is byte-locked at L0 publication time. Confirmatory criteria cannot mutate post-lock. Amendments require a signed pre-results commit reference. |
| P6 (verified grounding for any outward-facing claim) | The publication renderer fails Pandoc substitution on any unbound `{{claim_id}}` placeholder. Every published sentence binds to a `Criterion` (state=done or state=refuted) citing a signed `gate_verdict` event. |
| P11 (agent artifact pipelines as attack surface) | No LLM in the publication path. The renderer is pure Pandoc-template substitution over a substrate event fold. Galactica's failure mode is structurally precluded. |
| P8 (spend / iteration brakes with mandatory re-approval) | Reproducibility cron emits `kind=repro_verdict` with `usd_cents`; cost-governor denies further runs once per-item-per-day cap trips. |

## What this wedge is NOT

- A novelty filter. Whether a hypothesis is worth running is a human
  call; regatta enforces methodology, not significance.
- A theorem prover. Theory + position papers + qualitative work have
  no falsifiable target and are out of scope.
- A wet-lab orchestrator. IRB, lab automation, and human-subjects
  research are out of scope; the Workspace primitive is `Spawner`
  (single-host, MVP-3 scope) until a customer ask justifies more.
- A self-modifying agent. Stages 3-6 of the original bootstrap proposal
  (spec-drafter, roadmap-proposer, self-modifying scheduler) violate
  Trap P11 and are excluded.
- A paper-generator. The renderer never invokes an LLM. Auto-generated
  Methods or Discussion sections are structurally precluded.

## Adoption gate

Research-mode is a Phase X wedge per `2026-06-01-self-host-first.md`.
It does NOT ship until ALL of the following land:

1. Substrate Wave 1 (`substrate_events` + reducers) — Phase S3-T2.
2. W8 slim authorizer (`Authorizer` interface + Rego hot-reload,
   single-tenant default) — Phase S3-T1. Research-mode adds four
   Rego rules to its policy bundle. Multi-tenant `tenant_id`
   propagation is itself Phase X and not in the dependency chain.
3. Phase S2-T1 W9 substrate-default `DurableHistory` impl —
   research-mode reuses this primitive for the K=10 reproducibility
   cron's fresh-seed sweeps. The Temporal-backed variant remains
   Phase X.
4. The 30-day-self-host-green trigger fires (≥10 PRs/day green-merge
   ≥30 days unattended) OR a first external research-customer ask is
   on file (per self-host-first §7).
5. The repo-wide simplification pass (audit the sibling-package
   `Estimator` seam-vs-impl split during the cost-package flatten;
   flatten `orchestrator/{adapter,adaptersync,lockfile}` and
   `cost/{estimate,gate,pricing,reconcile,spend}`; pick-one between
   `planner.go` / `planner_v2.go` / `planner_stub.go`).

**Phase X wedges NOT in the dependency chain:** W10 Sigstore, W11
blackboard CAS, W12 billing. Research-mode rides existing primitives
instead; the migration shape for each Phase X swap is documented in
the spec:

- Signing: HMAC canonicalization in `contracts/schemas/sign.go`. If
  W10 ships, writer-side canonical bytes stay the same; verifier-side
  gains a Rekor transparency-log lookup + an OIDC trust root.
  Different verification contract; spec amendment required.
- Dataset / model / canary pins: the `prereg.dataset.sha256` and
  `prereg.baselines[].artifact_sha256` fields declared on `WorkItem`
  (NOT `SourceRef.SHA`, which is the brief-pointer commit-SHA per
  `contracts/schemas/spec_adapter.go:114-122`). If W11 ships, those
  prereg fields migrate to `BlobDigest` references. Storage backend
  changes; equality-check shape preserved.
- Publication: local `regatta research publish` writes the rendered
  bundle to disk. If W12 ships, the verb gains a `--metered` flag
  that emits a Stripe usage event alongside the local write.

Until the gating items land, research-mode is forbidden to dispatch.
If a wedge needs to ship sooner because of an external research-
customer ask, re-litigate the wedge; do not ship around the
blockers.

## References

- Spec: `docs/engineer/specs/2026-06-01-research-mode-extension-design.md`
- Strategic brief: `docs/engineer/briefs/2026-06-01-regatta-research-vision.md`
- Related wedge: `docs/wedges/cost-governor.md` (cost attribution for
  reproducibility runs)
- Related wedge: `docs/wedges/blackboard.md` (CAS for dataset + model
  + canary digests)
- Related wedge: `docs/wedges/approval-gates.md` (HITL approval for
  confirmatory→exploratory criterion promotion, deferred)
