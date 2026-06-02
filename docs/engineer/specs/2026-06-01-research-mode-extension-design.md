# Spec: Research-Mode Extension — autonomous empirical AI/CS research on existing regatta primitives

_Author: design subagent, 2026-06-01 (v1). Source-of-truth: `docs/wedges/research-mode.md` (thesis) + `docs/engineer/briefs/2026-06-01-regatta-research-vision.md` (vision) + adversarial review of original 6-component synthesis (70% cut by reviewer subagent)._

This spec is an **additive Phase X wedge** per `docs/engineer/briefs/2026-06-01-self-host-first.md`, NOT a pivot. It sits alongside the self-host-first roadmap and ships AFTER Phase S1 → S2 → S3 land (substrate Wave 1 + W8 slim authorizer + W9 substrate-default `DurableHistory` impl + the Phase-X trigger), AND after the repo-wide architecture-simplification pass described in `2026-06-01-arch-simplification-pass.md`. Phase X wedges (W10 Sigstore, W11 blackboard CAS, W12 billing) are NOT in the dependency chain — research-mode rides existing primitives (HMAC via `contracts/schemas/sign.go`, the `prereg.dataset.sha256` + `prereg.baselines[].artifact_sha256` fields declared in §2.2, local publish) and the migration shape for each Phase X swap is documented in the relevant section (writer canonicalization stays for the HMAC→Sigstore swap; storage backend changes for the prereg-sha256→BlobDigest swap). Two parallel adversarial reviews (research-mode synthesis review + arch-simplification review) cut the original 6-component proposal to **one CUE enum extension + four gate packages + one cron + four Rego rules**. This spec locks the cut version. The deleted 70% of the original proposal is enumerated in §11 ("What got smaller").

This spec locks the schema discriminator, gate package layout, cron contract, RBAC rules, cost-governor integration, negative-result lifecycle, dataset-pin enforcement, performance budget, and grade rubric for **research-mode** — a `WorkItem.kind="research"` discriminator that routes through four new methodology gates (p-hack, statistical-power, leakage, statistical-test) plus a nightly reproducibility cron, all riding on existing primitives: SpecAdapter, substrate event log, whatever gates exist in the L0-L5 stack at dispatch time (L0 shipped today; L1-L5 deferred per `docs/design.md`), W8 OPA slim authorizer, `contracts/schemas/sign.go` HMAC primitive, `prereg.dataset.sha256` byte-pinning (declared in §2.2), cost-governor.

**Zero new schemas. Zero new adapters. Zero new tables. One new CLI subcommand.**

---

## 0. Status

**Pre-spec — Phase-X wedge per `docs/engineer/briefs/2026-06-01-self-host-first.md`.** Research-mode does NOT dispatch until ALL of the following land:

1. Substrate Wave 1 (`substrate_events` table) — currently scheduled for Phase S3-T2 (cost-gov + approvals cutover only; everything-else cutover deferred).
2. W8 OPA RBAC slim variant (`Authorizer` interface + policy hot-reload, single-tenant default) — Phase S3-T1. Multi-tenant `tenant_id` propagation is itself Phase X and NOT required.
3. Phase S2 W9 substrate-default `DurableHistory` impl — provides the K=10 reproducibility cron a replay-grade primitive (research-mode reuses this for fresh-seed sweeps).
4. The 30-day-self-host-green trigger fires (≥10 PRs/day green-merge ≥30 days unattended) OR an external research-customer ask is on file. Per self-host-first §7.

**No dependency on Phase X wedges** — research-mode does NOT block on W10 Sigstore, W11 blackboard CAS, or W12 billing. Each of those is deferred to Phase X by the self-host-first brief and research-mode rides existing primitives instead:

- Signing: existing HMAC canonicalization in `contracts/schemas/sign.go` (substrate Wave 1 uses this today). Sigstore upgrade lands in Phase X.
- Dataset / model / canary pins: the `prereg.dataset.sha256` and `prereg.baselines[].artifact_sha256` fields declared in §2.2 (locked at prereg time; byte-equality comparison). `SourceRef.SHA` on `WorkItem` is the brief-pointer commit-SHA / opaque ETag per `contracts/schemas/spec_adapter.go:114-122` and is NOT used as a dataset content-hash. When W11 blackboard CAS ships, the prereg sha256 fields migrate to `BlobDigest` references; storage shape changes, equality-check shape preserved.
- Publication: local `regatta export-bundle` to disk. W12 billing webhook lands in Phase X.

**Until the trigger fires, research-mode is forbidden to dispatch.** If a wedge needs to ship sooner because of an external research-customer ask (per self-host-first §7), re-litigate this spec; do not ship around it.

The repo-wide simplification pass per `2026-06-01-arch-simplification-pass.md` is independent of the Phase-X gate and can land at any time — recommended before Phase S2 dispatch so research-mode and its sibling wedges share collapsed primitives.

---

## 1. Prior art adopted (no bespoke invention)

Per `memory/feedback_research_design_principles`, every primitive cites a proven OSS source or a regatta primitive already shipped. Where two systems collide, the dossier's "why this one" is the tiebreaker.

| Primitive | Adopted from | What we take | Why not bespoke |
|---|---|---|---|
| Prereg field floor (9 questions) | [AsPredicted](https://aspredicted.org/) | Question / IV / DV / analysis / exclusions / N / stop-rule — minimum-viable prereg | Smallest validated working set; OSF-30 ceremony kills adoption per `feedback_drop_ceremony` |
| Confirmatory / exploratory split | [ICH E9 R1 §5](https://www.ema.europa.eu/en/documents/scientific-guideline/ich-e9-r1-addendum-estimands-and-sensitivity-analysis-clinical-trials-guideline-statistical-principles-clinical-trials-step-5_en.pdf) | `criterion.kind ∈ {confirmatory,exploratory}` discriminator | 30 years of regulatory practice; agent enforces what humans cannot (mechanical lock vs voluntary) |
| Registered Reports / in-principle acceptance | [COS RR](https://www.cos.io/initiatives/prereg) | L0 gate approves brief → run experiment → publish whatever the result says | NeurIPS Prereg Workshop failed due to human-volunteer bottleneck; agents do not get bored |
| p-hack detection via prereg diff | [Gelman & Loken, "Garden of Forking Paths"](http://stat.columbia.edu/~gelman/research/unpublished/p_hacking.pdf) + [statcheck](https://www.nuijten.io/statcheck/) | Diff prereg-locked CUE vs final-results CUE; flag each forking-path category | statcheck proves the auditor-tool model in psych; no ML-specific tool exists upstream — meaningful contribution |
| Dataset contamination detection | [Lee et al., arXiv:2107.06499](https://arxiv.org/abs/2107.06499) + [Sainz et al., arXiv:2310.18018](https://arxiv.org/abs/2310.18018) + [Carlini et al., arXiv:2202.07646](https://arxiv.org/abs/2202.07646) | MinHash-LSH 13-gram Jaccard + suffix-array ExactSubstr + canary extraction | Three independent empirical lines converged on this stack |
| Statistical-power | [`statsmodels.stats.power`](https://www.statsmodels.org/stable/stats.html#power-and-sample-size-calculations) | `solve_power(effect_size, alpha, power) → N_required` | Cohen framework is canonical; closed-form for standard tests |
| Statistical-test menu | [Dror et al., "Hitchhiker's Guide", ACL P18-1128](https://aclanthology.org/P18-1128/) + [Bouthillier et al., arXiv:2103.03098](https://arxiv.org/abs/2103.03098) | paired-bootstrap / McNemar / Wilcoxon / Friedman+Nemenyi dispatched by claim type | Canonical NLP-stats decision tree + variance-aware ML extension |
| Reproducibility variance bound | [Bouthillier et al.](https://arxiv.org/abs/2103.03098) | K=10 fresh seeds, sigma + 95% CI; single-seed claims unreliable below rank-correlation 0.7 | Canonical ML variance-decomposition result |
| Append-only signed research events | `internal/orchestrator/state/substrate/` (this repo) | Wave 1 substrate carries `kind=gate_verdict`, `kind=node_output`, `kind=token_spend`; research adds `kind=repro_verdict` only | Substrate spec §11 locks "every MVP-3+MVP-4 wedge consumes the substrate"; research is no exception |
| Signing primitive | `contracts/schemas/sign.go` (HMAC; substrate uses this today) | HMAC-SHA256 canonicalization on the `kind=repro_verdict` event row | Self-host scope = single operator trusts own git history per `2026-06-01-self-host-first.md` §1. Phase X swap policy lives in §0. |
| Authorization primitive | W8 OPA `Authorizer` slim variant (`docs/engineer/briefs/2026-06-01-self-host-first.md` §3 S3-T1) — interface + Rego policy hot-reload, single-tenant default | Rego rules for `promote_criterion`, `override_gate`, `publish_bundle`, `retract_claim` | W8 slim is in-scope for self-host. Multi-tenant `tenant_id` propagation is Phase X. Research-mode rules apply to single-tenant operator; tenant scoping ships if/when Phase X opens. |
| Cost enforcement | `internal/cost/spend/` writer + W2 cost-governor | `kind=repro_verdict` event carries `usd_cents`; cost-gov sums + denies | Repro K=10 is LLM-expensive; cost-gov is the existing budget primitive |
| Content-addressed evidence | `prereg.dataset.sha256` + `prereg.baselines[].artifact_sha256` fields declared in §2.2 (locked at prereg time) | Sha256 byte-equality against the prereg-locked value. NOT `SourceRef.SHA` (that field is the brief-pointer commit-SHA / opaque ETag per `spec_adapter.go:114-122`, not a content-hash). | W11 blackboard CAS is Phase X. Solo operator single-repo scope does not need a shared CAS table. When W11 ships, the sha256 fields migrate to `BlobDigest` references — storage shape changes, equality-check shape preserved. |

**Rejected alternatives (defended below):** parallel `HypothesisAdapter` (SpecAdapter is sufficient); parallel `Workspace`/`Driver`/`Job` interface refactor (out of scope — `Spawner` today is one focused package whose declared next step per `README.md` Next-steps §3 is a `claude --resume` worktree launcher, NOT Kubernetes); OpenTimestamps Bitcoin anchor (HMAC + git history are sufficient for self-host scope); nanopub-shaped JSON-LD Claim DSL (no consumer demands RDF reasoning); ResultBundle as a new storage primitive (substrate event fold renders RO-Crate); separate "Protocol DSL" YAML (`.regatta/items/*.md` + `kind="research"` + a small `prereg:` sub-block is the protocol).

**Deferred to later waves (NOT this spec):** k8s-job + slurm-rest workspace drivers (defer until a real research-customer ask); confirmatory→exploratory promotion workflow (defer until W7 panel scope is added); cross-paper citation graph (defer until 2nd research repo); auto-paper-generator (Galactica lesson — never; renderer-only when scoped); LiteLLM proxy for non-Claude validators (P2.8 — already deferred by the roadmap).

---

## 2. Locked schema additions

Research-mode adds **one CUE enum value**, **one optional sub-block on WorkItem**, **one new event kind**, and **one new Criterion state**. No new tables, no new schema files.

### 2.1 WorkItem discriminator (one enum value)

`contracts/schemas/spec_adapter.go` L77-79 today:

```go
const (
    KindFeature WorkItemKind = "feature"
    KindProgram WorkItemKind = "program"
)
```

Add:

```go
    KindResearch WorkItemKind = "research"
```

Routing: items with `kind="research"` route through whatever L0-L5 gates exist at dispatch time (L0 ships today; L1-L5 are deferred per `docs/design.md`) **plus** the four research gates (§3) **plus** the nightly repro cron's latest `kind=repro_verdict` event must satisfy `reproduced=true` before publication (NOT before merge — see §4).

### 2.2 WorkItem prereg sub-block (seven or fewer fields)

`contracts/schemas/work_item.schema.json` adds an optional top-level field `prereg`, required iff `kind="research"`:

```json
{
  "prereg": {
    "claim_direction": "positive | negative | null | equivalence",
    "primary_metric": {
      "name": "string",
      "direction": "higher_better | lower_better",
      "target": 0.0,
      "unit": "string"
    },
    "dataset": {
      "name": "string",
      "version": "string",
      "split": "train | dev | test",
      "sha256": "64-hex-chars"
    },
    "baselines": [
      { "name": "string", "artifact_sha256": "64-hex", "expected_score": 0.0 }
    ],
    "statistical_test": "t_test | bootstrap_ci | permutation | wilcoxon | mcnemar | none_descriptive",
    "n_planned": 0,
    "stop_rule": "string"
  }
}
```

Confirmatory vs exploratory lives on `Criterion`, not here — see §2.4.

### 2.3 New event kind: `repro_verdict` (one row in the enum)

`internal/orchestrator/state/substrate/event.go` `EventKind` enum gains:

```go
    KindReproVerdict EventKind = "repro_verdict"   // nightly K=10 cron output; payload below
```

Payload schema (JSON, <= 1 KiB per substrate Wave 1 budget):

```json
{
  "work_item_id": "string",
  "protocol_sha": "git-sha",
  "k_seeds": 10,
  "seed_metrics": [0.0, 0.0, "..."],
  "sigma_hat": 0.0,
  "ci_95": [0.0, 0.0],
  "reproduced": true,
  "usd_cents": 0,
  "wall_seconds": 0
}
```

Cost-gov reads `usd_cents` and sums per `work_item_id` per day. Row is HMAC-signed at write time using the existing `contracts/schemas/sign.go` canonicalization (substrate Wave 1 uses this primitive).

### 2.4 New Criterion state: `refuted` + criterion kind

`Criterion.State` today: `{planned, in_progress, done}`. Adds:

```go
    CriterionStateRefuted CriterionState = "refuted"
```

`Criterion.Kind` (new field, optional, applies only when parent `WorkItem.kind="research"`):

```go
type CriterionKind string

const (
    CriterionKindConfirmatory CriterionKind = "confirmatory" // locked at L0; deviation triggers p-hack gate
    CriterionKindExploratory  CriterionKind = "exploratory"  // open; flagged in final report
)
```

`refuted` is a publishable terminal state — negative results are first-class. `regatta publish --refutation` (§7) is the CLI verb.

---

## 3. Four research gates (`internal/gates/research/`)

Each gate ships as a standalone package under `internal/gates/research/`, emits a `gate_result.schema.json`-shaped verdict, and is signed by the existing canonicalization in `contracts/schemas/sign.go`. All four are **PR-blocking** for `kind="research"` items.

```
internal/gates/research/
  phack/         # diff prereg vs final results; flag forking-path categories
  power/         # statsmodels.solve_power; check N_declared >= N_required
  leakage/       # MinHash-LSH + suffix-array + canary scan
  stattest/      # dispatch test by claim type; emit p + BCa CI
```

Gate runners follow the existing Python-sidecar pattern (L3/L4/L5 use this today): each gate is a Go shim that shells out to a Python venv, parses stdout JSON into `GateResult`, writes the result to substrate as `kind=gate_verdict`. Library dependencies for the Python venv:

```
numpy
scipy>=1.7       # BCa bootstrap
statsmodels      # power; solve_power
datasketch       # MinHash-LSH
text-dedup       # Lee et al. reference impl
scikit-posthocs  # Nemenyi
torch            # deterministic flags only — no model loading in gates
```

### 3.1 `phack` — prereg-diff p-hack detector

**Input contract:** `{prereg_sha, final_results_path, criterion_kind}`.

**Algorithm:** Structured diff between `WorkItem.prereg` (locked at L0 publication time) and the final-results manifest. Flag categories:

| Category | Severity |
|---|---|
| `metric_swap` (declared metric does not match reported metric) | FAIL |
| `exclusion_added` (any new test-set filter not in prereg) | FAIL |
| `N_changed > 5%` | WARN |
| `N_changed > 20%` | FAIL |
| `seed_cherry_pick` (# seeds reported < # seeds run) | FAIL |
| `baseline_swap` (baseline `artifact_sha256` differs from prereg) | FAIL |
| `test_swap` (parametric to nonparametric after data inspection) | FAIL |
| `covariate_added` (regression covariate not in prereg) | WARN |
| GRIM (means impossible given integer N) | FAIL |

**False-positive mitigation:** legitimate amendments must use a signed `protocol_amendment` field referencing a pre-results commit; amendments before run start are permitted, amendments after run start are FAIL.

**Run time:** ms. Tier-1 PR-blocking.

### 3.2 `power` — preflight statistical-power check

**Input contract:** `{prereg.primary_metric.target, alpha=0.05, beta=0.20, variance_estimate?}`.

**Algorithm:**
1. If `variance_estimate` absent, derive sigma from a calibration run (>= 5 baseline seeds; cached if previous repro_verdict exists).
2. Compute Cohen d = `target / sigma`.
3. Call `statsmodels.solve_power(effect_size=d, alpha=alpha, power=1-beta)` → `N_required`.
4. PASS iff `prereg.n_planned >= N_required`. WARN at 60% of N_required. FAIL below.
5. Emit the **minimum detectable effect at the submitted N** as evidence (avoids "demand impossible N" false-positive mode).

**Run time:** ms. Tier-1 PR-blocking.

### 3.3 `leakage` — train/test contamination scan

**Input contract:** `{train_manifest_sha, eval_set_sha, model_artifact_sha?, canary_set_sha?}` — each is a sha256 byte-pinned at prereg time via the `prereg.dataset.sha256` + `prereg.baselines[].artifact_sha256` fields declared in §2.2 (NOT `SourceRef.SHA`, which is the brief-pointer commit-SHA / opaque ETag per `spec_adapter.go:114-122`).

**Algorithm:**
1. **MinHash-LSH dedup:** 13-gram shingles, Jaccard >= 0.8 → flag pair.
2. **ExactSubstr (suffix array):** >= 50-token contiguous matches eval-in-train.
3. **Canary extraction (if canary_set present):** prompt model with prefix, measure verbatim continuation; >10% extraction → FAIL.
4. **Loss-gap probe (if model API available):** paired loss on seen vs held-out shard, >2 sigma → WARN.

**False-positive mitigation:** common-crawl boilerplate corpus subtracted before scoring (license headers, Wikipedia intros, etc.).

**Run time:** minutes (LSH index amortized per `train_manifest_blob_digest`). PR-blocking but indexed offline.

### 3.4 `stattest` — statistical significance test

**Input contract:** `{system_a_scores, system_b_scores, claim_type, alpha=0.05, n_resamples=10000, pairing_key?}`.

**Dispatch by `claim_type`:**

| Claim type | Test | Library |
|---|---|---|
| `paired_metric` | paired bootstrap + BCa CI | `scipy.stats.bootstrap` |
| `binary_outcome` | McNemar (exact for n<25) | `statsmodels.stats.contingency_tables.mcnemar` |
| `multi_system` | Friedman + post-hoc Nemenyi | `scipy.stats.friedmanchisquare` + `scikit_posthocs.posthoc_nemenyi` |
| `distributional` | Wilcoxon signed-rank | `scipy.stats.wilcoxon` |

**Output:** `{test_selected, p_value, effect_size, ci_95}`. PASS iff alpha-corrected `p < alpha` AND CI excludes 0.

**False-positive mitigation:** abort if `claim_type=paired_metric` but `pairing_key` absent (unpaired scores → inflated power).

**Run time:** minutes. PR-blocking.

---

## 4. Nightly reproducibility cron (NOT a PR gate)

Per the adversarial review's hardest finding: **reproducibility K=10 is NOT a PR gate** — it would violate fleet-wide median PR latency. Ships as a scheduled cron, emits a substrate event, and publishable runs gate on the latest verdict.

### 4.1 Contract

New subcommand: `regatta research repro --work-item <id> --k 10`.

**Algorithm:**
1. Load `WorkItem.prereg` + `protocol_sha` from substrate (latest `kind=node_output` for this work_item_id).
2. Build sandbox from pinned env (existing W6 OCI digest path).
3. Run K=10 fresh seeds in parallel via existing `Spawner.Spawn` (no new workspace abstraction — `Spawner` is sufficient for the current single-host MVP-3 scope).
4. Compute sigma + 95% CI of mean across seeds.
5. CUDA-nondeterminism leak check: re-run seed 0 twice with `torch.use_deterministic_algorithms(True)`; delta > 0 → `reproduced=false`.
6. PASS iff `sigma <= prereg.declared_variance_bound` AND CI excludes the "no-improvement-over-baseline" point.
7. Write `kind=repro_verdict` event to substrate (HMAC-signed via `contracts/schemas/sign.go`). Payload per §2.3.
8. Cost-gov reads `usd_cents` from payload and sums per work-item per day; denies further repro runs that breach cap.

### 4.2 Publishability rule

A `kind="research"` WorkItem becomes **publishable** (eligible for `regatta publish` CLI) iff:

- All four research gates PASS at PR-merge time.
- The **latest** `kind=repro_verdict` event for this work_item_id has `reproduced=true`.
- All confirmatory criteria are `done` OR all confirmatory criteria are `refuted` (negative-result publish path is symmetric).

Until a `repro_verdict` exists, the WorkItem stays in `kind="research"` but `publishable=false`. This decouples PR merge (fast) from publishability (slow K=10 confirmation).

### 4.3 Cost-gov wiring

`internal/cost/spend/` extends to read `kind=repro_verdict` events:

```go
// existing pseudocode pattern
spend.RegisterEventReader(substrate.KindReproVerdict, func(e Event) USDCents {
    var p ReproVerdictPayload
    json.Unmarshal(e.PayloadJSON, &p)
    return USDCents(p.USDCents)
})
```

Per-work-item daily cap defaults to `cost.research_repro_daily_usd` (added to `regatta.v1.cue` as a single field; default `$10/day/item`). Override per-tenant via the existing W2 cost-gov config surface.

---

## 5. Four OPA Rego rules (W8 integration)

The W8 spec designs the `Authorizer` interface as a one-file impl swap. Research-mode adds four Rego rules to the policies bundle:

```rego
package regatta.research

# 1. Who can promote a criterion from exploratory to confirmatory?
#    Only the work-item author OR a CODEOWNER of the prereg path.
allow_promote_criterion[msg] {
    input.action == "promote_criterion"
    input.principal == input.work_item.author
    msg := "author can self-promote pre-publication"
}
allow_promote_criterion[msg] {
    input.action == "promote_criterion"
    input.principal in input.work_item.prereg_codeowners
    msg := "codeowner can promote"
}

# 2. Who can override a gate-red verdict (escape hatch)?
#    Only the human reviewer assigned by L6 + a tracking issue link.
allow_override_gate[msg] {
    input.action == "override_gate"
    input.principal in input.work_item.l6_reviewers
    input.justification.tracking_issue_url != ""
    msg := "L6 reviewer with tracking issue can override"
}

# 3. Who can publish a bundle (move from publishable=true to actually published)?
#    Author OR codeowner; cannot self-publish if confirmatory criterion is refuted
#    unless it is an explicit refutation publish.
allow_publish_bundle[msg] {
    input.action == "publish_bundle"
    publishable_authorship_ok
    not has_refuted_confirmatory_without_explicit_refutation_flag
    msg := "publishable + confirmatory state matches publication flag"
}

# 4. Who can retract a published claim?
#    Author + L6 reviewer concurrence (two-key rule).
allow_retract_claim[msg] {
    input.action == "retract_claim"
    input.principal == input.claim.author
    count(input.concurrence_signatures) >= 1
    input.concurrence_signatures[_] in input.work_item.l6_reviewers
    msg := "two-key retraction"
}
```

Rego rule sources live at `policies/research/*.rego` (deferred substrate-policies primitive; the W8 adapter loads from filesystem in the interim).

**Schema dependency note:** rules 2 and 4 reference `input.work_item.l6_reviewers` and `input.work_item.prereg_codeowners`. Neither field exists on `WorkItem` today. Both must be added to `contracts/schemas/work_item.schema.json` as part of Task 0 (or sourced from an existing approval-gates reviewer-set primitive if one exists at dispatch time). The Rego bodies above are illustrative; the binding to the actual reviewer-source surface is a Task 6 design-subagent decision per `feedback_spec_pattern_authority`, not a main-thread guess.

---

## 6. Renderer (NOT generator) for published bundles

"Renderer not generator" is operationalized as **Pandoc-template substitution over a substrate fold**, NOT an LLM. No LLM in the publication path.

`cmd/regatta export-bundle <work_item_id>` renders:

1. **RO-Crate `ro-crate-metadata.json`** — generated from the substrate fold (`kind=node_output` + `kind=gate_verdict` + `kind=repro_verdict`).
2. **BagIt sha256 manifest** — per-file checksums of all `prereg.dataset.sha256` + `prereg.baselines[].artifact_sha256`-pinned artifacts declared in §2.2.
3. **Pandoc template substitution** — `templates/paper.tex.j2` reads explicit `{{claim_id}}` placeholders; Pandoc render fails on any unsubstituted placeholder. No model-mediated sentence rejection.

Paper LaTeX template lives in `contracts/templates/`. Sample structure:

```jinja2
\section{Results}
Our experiment shows
  {{ claim_id_1.subject }}
  {{ claim_id_1.predicate }}
  {{ claim_id_1.object }}
  on {{ claim_id_1.dataset }}
  with Delta={{ claim_id_1.delta }}
  (95\% CI [{{ claim_id_1.ci_lo }}, {{ claim_id_1.ci_hi }}],
  p={{ claim_id_1.p_value }}).
```

A "claim" is a `Criterion` with `state=done` (or `state=refuted` for refutation papers), citing a `gate_verdict` event ID. No new claim schema.

Auto-LLM Methods sections, related-work generation, and discussion: **out of scope, never**. Galactica was withdrawn in three days for hallucinated citations. The renderer invariant is "every sentence binds to a `claim_id` from the substrate fold OR is in a static methods boilerplate that cites pinned environment digests."

---

## 7. New CLI surface (one subcommand)

```
regatta research repro --work-item <id> --k 10 [--budget-usd <cents>]
regatta research publish <work_item_id> [--refutation]
regatta export-bundle <work_item_id> [--output-dir <path>]
```

`regatta research` and `regatta export-bundle` are new top-level verbs. `regatta research publish` writes the rendered bundle to a local directory for the self-host scope (no remote upload, no billing webhook). Phase X swap policy lives in §0.

---

## 8. Out of scope (this spec is deliberately small)

These items appeared in the original synthesis proposal and were cut by the adversarial review. Re-litigating any of them requires a new spec.

- **HypothesisAdapter** — SpecAdapter + `kind="research"` is sufficient.
- **Workspace/Driver/Job 3-interface refactor** — `Spawner` is one focused package today; defer interface refactor until k8s/slurm customer ask.
- **Protocol DSL (`.regatta/protocols/*.yaml`)** — `.regatta/items/*.md` + `prereg` sub-block is the protocol.
- **Claim DSL (nanopub JSON-LD)** — `Criterion` + `gate_verdict` event is the claim.
- **ResultBundle as a storage primitive** — substrate event fold + RO-Crate renderer is sufficient.
- **Lineage DAG with typed edges (refines/contradicts/extends/replicates)** — `WorkItem.Dependencies` is acyclic + Kahn-checked; reuse with an edge-label field if needed.
- **OpenTimestamps Bitcoin anchor** — HMAC via `contracts/schemas/sign.go` + git history are sufficient for self-host scope.
- **25-field CUE schema** — seven-or-fewer prereg fields + Criterion kind/state extension is sufficient.
- **Auto-paper LLM generator** — Galactica was withdrawn in three days. Renderer-only, never generator.
- **Bootstrap stages 3-6** (spec-drafter, roadmap-proposer, self-modifying scheduler) — hypothetical-future scaffolding; forbidden by `feedback_drop_ceremony`. Stage 0-2 (reactive-janitor, doc/test surgeon, spec-implementer) are the only stages this spec entertains — and even those land **separately** under the existing autonomous-session-prompt flow, NOT in this spec.
- **TCB-enforcement L0 sub-gate** — without a written threat model that names a specific tamper path L0 misses, this is "police what you do not have" (`feedback_drop_ceremony` + design.md L545). Re-litigate with a threat model attached.

---

## 9. Implementation plan (Wave-aligned)

Six file-disjoint subagent tasks, dispatched in waves.

**Task 0 — Substrate event kind + Criterion state extensions (single-file, gate-keeping):**

- `internal/orchestrator/state/substrate/event.go` — add `KindReproVerdict` enum value.
- `contracts/schemas/spec_adapter.go` — add `KindResearch` constant.
- `contracts/schemas/work_item.schema.json` — add optional `prereg` sub-block + `kind="research"` enum value.
- `internal/orchestrator/state/criteria.go` (or current location) — add `CriterionStateRefuted` + `CriterionKind` type.
- Property test: round-trip a `WorkItem{kind="research"}` through `SpecAdapter.Open` + L0 enforcement; confirmatory criteria must be byte-locked after publication.

Ships first. Merge unlocks Tasks 1-4 (parallel) and Task 5 (sequential).

**Task 1 (parallel) — `internal/gates/research/phack/` + Python venv:**

- Go shim + `pkg/phack/phack.py` + Python venv + `requirements.txt` pinning numpy/statsmodels.
- Fixture corpus: `testdata/gates/research/phack/` with at least one positive case per forking-path category (9 categories per §3.1).
- Mutation testing >= 80% kill rate per `feedback_grade_rubric`.

**Task 2 (parallel) — `internal/gates/research/power/`:**

- Go shim + `pkg/power/power.py`.
- Fixture: known effect-size + variance, assert `solve_power` matches statsmodels reference within 1e-9.

**Task 3 (parallel) — `internal/gates/research/leakage/`:**

- Go shim + `pkg/leakage/leakage.py` (MinHash-LSH + suffix-array + canary).
- Fixture: contaminated train/eval pair from a public benchmark (likely a small GLUE subset shuffled into a synthetic train).
- Common-crawl boilerplate corpus subtracted (false-positive control).

**Task 4 (parallel) — `internal/gates/research/stattest/`:**

- Go shim + `pkg/stattest/stattest.py` dispatching by `claim_type`.
- Fixture: paired-bootstrap CI agrees with arch.bootstrap reference; McNemar matches statsmodels reference.

**Task 5 (sequential, after Task 0 + at least 2 of Tasks 1-4) — `cmd/regatta research repro` + cron + cost-gov wiring:**

- New subcommand under `cmd/regatta/research.go` (single file).
- Spawns K=10 via existing `Spawner` (no new abstraction).
- Emits `kind=repro_verdict` event HMAC-signed via existing `contracts/schemas/sign.go` primitive (matches the §0 self-host signing decision).
- Cost-gov registers a reader for the new event kind (single line in `internal/cost/spend/`).
- Repro CLI doc: `docs/operator/research-repro.md` (~50 lines, runbook style per `feedback_doc_check_banned_phrases`).

**Task 6 (sequential, after Task 5) — `policies/research/*.rego` + W8 wiring:**

- Four Rego rules per §5.
- Property tests: each rule has a positive + negative case.
- W8 Authorizer adapter loads from `policies/research/` glob.

Dispatch policy: cap parallel implementer subagents at 3-4 per session (shared API quota collapses at 5+ heavy-context sessions). Wave 1 = Task 0 only. Wave 2 = Tasks 1+2+3 (file-disjoint, three implementers max). Wave 3 = Task 4 + Task 5 (Task 4 file-disjoint with Task 5). Wave 4 = Task 6.

---

## 10. Adversarial benchmark — capture-the-flag for the framework

Before research-mode ships externally, build a **research-mode trap corpus** at `testdata/gates/research/traps/`. Each fixture is a complete (deliberately broken) `WorkItem{kind="research"}` + final-results manifest pair, annotated `{trap_kind, expected_gate, severity}`. Examples:

- `traps/phack-metric-swap/` — prereg declares F1; results report accuracy. `expected_gate=phack`, `severity=FAIL`.
- `traps/leakage-test-in-train/` — eval set contains exact substring matches in train shards. `expected_gate=leakage`, `severity=FAIL`.
- `traps/leakage-canary-extraction/` — model trained on a canary corpus; canary verbatim emission >10%. `expected_gate=leakage`, `severity=FAIL`.
- `traps/power-underpowered/` — N=10 declared, target effect-size detectable only at N>=200. `expected_gate=power`, `severity=FAIL`.
- `traps/phack-seed-cherry-pick/` — 10 seeds run, only top 3 reported. `expected_gate=phack`, `severity=FAIL`.
- `traps/phack-exclusion-added/` — post-hoc filter on test set not in prereg. `expected_gate=phack`, `severity=FAIL`.

Gate suite must hit **100% catch rate on this corpus** before MVR-1 research-mode lands (Phase X wedge per `2026-06-01-self-host-first.md`). This is the research analog of `docs/incidents.md` (the AI-agent incident catalog driving L0-L5 fixture design).

---

## 11. What got smaller (per `feedback_deletion_default`)

This spec is the deletion-default verdict applied to the original synthesis proposal:

| Original component | Status | Replacement |
|---|---|---|
| `HypothesisAdapter` (new Go iface + CUE schema) | **DELETED** | `WorkItem.kind="research"` discriminator on existing `SpecAdapter` |
| `Workspace`/`Driver`/`Job` 3-iface refactor | **DELETED** | `Spawner` is one focused package; next step is `claude --resume` worktree launcher per `README.md` Next-steps §3 |
| 25-field CUE `HypothesisBrief` schema | **DELETED** | seven-or-fewer-field `prereg` sub-block + `Criterion.Kind` enum |
| `.regatta/protocols/*.yaml` Protocol DSL | **DELETED** | `.regatta/items/*.md` + `prereg` sub-block IS the protocol |
| `ResultBundle` artifact primitive | **DELETED** | substrate event fold + `regatta export-bundle` RO-Crate renderer |
| Claim DSL (nanopub JSON-LD) | **DELETED** | `Criterion` (state=done\|refuted) + linked `gate_verdict` event |
| Typed lineage DAG (refines/contradicts/...) | **DELETED** | `WorkItem.Dependencies` + edge-label field if/when needed |
| 3-layer immutability (git + Sigstore + OpenTimestamps) | **CUT TO 1 LAYER** | HMAC via `contracts/schemas/sign.go` only (matches substrate Wave 1); git is implicit. W10 Sigstore is Phase X; swap policy in §0. |
| 7-stage bootstrap roadmap | **CUT TO 0-2** | Stages 0-2 live in the existing autonomous-session-prompt flow, NOT this spec; 3-6 deleted |
| TCB-enforcement L0 sub-gate prerequisite | **DELETED** | Re-litigate when a threat model names a specific tamper path L0 misses |
| Bootstrap stage 6 self-modifying scheduler | **DELETED** | Trap P11 (agent artifact pipelines are themselves attack surface) |
| LLM-based paper generator | **DELETED** | Pandoc template substitution; fail on unsubstituted `{{claim_id}}` |
| 17 workspace driver impls (k8s/slurm/modal/replicate/ray/...) | **CUT TO 0** | `Spawner` only; drivers added when a customer ask is on file |
| 5-tier methodology gate suite | **CUT TO 4 PR-gates + 1 cron** | Repro is a cron, not a gate (PR-latency invariant) |
| New schemas (HypothesisBrief, Protocol, ResultBundle, Claim) | **CUT TO 0** | One enum extension + seven-or-fewer-field sub-block |
| New tables | **CUT TO 0** | Substrate carries everything |
| New adapters | **CUT TO 0** | Reuse `contracts/schemas/sign.go` HMAC, W8 slim OPA, SpecAdapter, Spawner. W10 Sigstore is Phase X (swap policy in §0). |

**Net delta:** ~2,500-4,500 LoC added (4 gates × 400-800 LoC each per vision-brief budget = 1,600-3,200 for gates alone, plus Task 0 schema extensions, Task 5 cron + cost-gov wiring, Task 6 Rego rules, and the trap corpus fixtures — NOT the 15-25k a parallel-architecture build would require); zero new schemas, adapters, tables; one new CLI subcommand; one new event kind; one new CUE enum value; one new criterion state; one new optional criterion kind field.

---

## 12. Risk-tier table (per `feedback_adversarial_review`)

| Risk | Tier | Mitigation |
|---|---|---|
| Python venv drift across the four gate runners | Risk | Single shared `pkg/research/requirements.txt`; CI pins via lockfile; venv install gate in `make check` |
| Repro cron exceeds daily LLM budget | Risk | Cost-gov reads `kind=repro_verdict.usd_cents`; per-item daily cap defaults `$10/day`; configurable via existing W2 surface |
| Leakage gate false positive on common-crawl boilerplate | Important | Common-crawl boilerplate corpus subtracted before scoring; documented in §3.3 |
| p-hack gate false positive on legitimate bug-fix amendments | Important | `protocol_amendment` field with pre-results commit reference; amendments before run start permitted |
| Operator confusion: confirmatory vs exploratory promotion path | Important | W7 panel additions deferred per §8; named `[research-followup]` issue file at this spec land time |
| Refutation publishing flow surprises authors | Important | `regatta publish --refutation` is an explicit, separate CLI verb; never auto-promoted |
| Substrate not yet shipped → research-mode dispatches against legacy tables | **Risk (blocking)** | Status §0 explicitly forbids dispatch until the self-host-phase gates land (substrate Wave 1 + W8 slim + W9 substrate-default + arch-simp + Phase-X trigger). W10 / W11 / W12 are NOT in the dispatch gate. |
| Mutation-test corpus drift across the 4 gates | Important | Mutation testing >= 80% kill rate enforced per gate per `feedback_grade_rubric` |
| Pandoc template breakage in the renderer | Simplification | Pandoc render fails on unsubstituted placeholders; CI gate runs `regatta export-bundle` on a fixture work-item per release |

---

## 13. Grade rubric (B / A / A+ per `feedback_grade_rubric`)

**B (must-have):**

- All four research gates ship with Go shim + Python venv + fixture corpus + >= 80% mutation kill.
- `kind=repro_verdict` event lands in substrate; cost-gov reads `usd_cents`.
- Four Rego rules pass property tests.
- Trap corpus (§10) has >= 6 fixtures; all four gates achieve 100% catch on the corpus.
- Operator doc `docs/operator/research-repro.md` exists with banned-phrase pre-push grep clean (`feedback_doc_check_banned_phrases`).

**A (target):**

- Confirmatory→exploratory promotion path scoped (even if W7 panel deferred) with a written runbook.
- Renderer fixture: `regatta export-bundle` round-trips a fixture work-item → RO-Crate → Pandoc → PDF with all claim placeholders bound.
- Property test: a confirmatory `WorkItem.kind="research"` cannot have its prereg field byte-mutated after publication (L0 immutability extension test).
- Mutation kill rate >= 90% per gate.
- Cost-gov denial happens at the cron entry point, not mid-K=10-run.

**A+ (aspirational):**

- All B + A items plus:
- One real research wedge ships end-to-end (e.g. SWE-Bench Verified delta between two router variants) and produces a publishable RO-Crate.
- Trap corpus extends to >= 12 fixtures covering all forking-path categories.
- Renderer template ships for both confirmatory (positive-result) and refutation (negative-result) papers.
- Independent reviewer subagent re-scores the rubric and the score matches.
- Two production tenants (or wedges) consume research-mode without spec amendment.

---

## 14. Sequencing note

This spec **must not dispatch** until the Phase-X trigger fires per `docs/engineer/briefs/2026-06-01-self-host-first.md` §7 (30-day-self-host-green OR external research-customer ask), AND the following self-host-phase items land:

1. Substrate Wave 1 + Phase S3-T2 cutover (`substrate_events` + reducers; cost-gov + approvals on substrate).
2. Phase S3-T1 W8 OPA `Authorizer` slim impl + policy hot-reload (single-tenant default; multi-tenant scoping stays Phase X).
3. Phase S2-T1 W9 substrate-default `DurableHistory` impl (research-mode reuses this for K=10 reproducibility sweeps; the Temporal-backed variant remains Phase X).
4. Repo-wide simplification pass per `2026-06-01-arch-simplification-pass.md`: audit the sibling-package `Estimator` seam-vs-impl split during the cost-package flatten; flatten `orchestrator/{adapter,adaptersync,lockfile}` and `cost/{estimate,gate,pricing,reconcile,spend}`; pick-one between `planner.go` / `planner_v2.go` / `planner_stub.go`.

Phase X wedges (W10 Sigstore, W11 blackboard CAS, W12 billing) are NOT in the dependency chain — research-mode rides existing primitives (`contracts/schemas/sign.go` HMAC, `prereg.dataset.sha256` + `prereg.baselines[].artifact_sha256` byte-pinning, local `regatta research publish`). The migration shape for each Phase X swap is documented in the relevant section.

When the gating items land, re-validate this spec against the then-current state of the substrate, gate stack, and signing primitive. Spec amendments (per `feedback_spec_pattern_authority`) require a fresh design-subagent re-spawn, not main-thread guesswork.

---

## 15. References

- `docs/wedges/research-mode.md` — wedge thesis
- `docs/engineer/briefs/2026-06-01-regatta-research-vision.md` — strategic vision
- `docs/engineer/briefs/2026-06-01-arch-simplification-pass.md` — collapse-before-extend prerequisite
- `docs/engineer/briefs/2026-06-01-self-host-first.md` — Phase S1 → S2 → S3 → Phase X roadmap that places research-mode in Phase X
- `docs/engineer/specs/2026-06-01-unified-substrate-design.md` — substrate event log; this spec adds `KindReproVerdict` to its enum
- `docs/engineer/specs/2026-06-01-adapter-contracts-design.md` — Sigstore signer + OPA Authorizer adapter contracts (subject to deletion per simplification pass; signer Sigstore impl + multi-tenant OPA stay Phase X)
- `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` — Phase X host for the research-DAG view + claim-ledger panel (CLI is sufficient until then)
- `contracts/schemas/spec_adapter.go` — `WorkItemKind` enum gains `KindResearch`
- `contracts/schemas/work_item.schema.json` — gains optional `prereg` sub-block
- `contracts/schemas/gate_result.schema.json` — research gates emit this same shape; no new schema
- `internal/gates/l0/` — existing gate package; research gates follow the same `internal/gates/<name>/` layout
- AsPredicted (https://aspredicted.org/) — prereg field floor
- ICH E9 R1 (https://www.ema.europa.eu/en/documents/scientific-guideline/ich-e9-r1-addendum-estimands-and-sensitivity-analysis-clinical-trials-guideline-statistical-principles-clinical-trials-step-5_en.pdf) — confirmatory/exploratory split
- Bouthillier et al., arXiv:2103.03098 — K=10 seed variance accounting
- Lee et al., arXiv:2107.06499 — MinHash + ExactSubstr dedup reference impl
- Sainz et al., arXiv:2310.18018 — per-benchmark contamination measurement
- Carlini et al., arXiv:2202.07646 — canary extraction protocol
- Dror et al., ACL P18-1128 — significance-test decision tree
- Gelman & Loken — garden of forking paths
- Nuijten et al. — statcheck
- Brown & Heathers — GRIM test
- COS RR — registered reports / in-principle acceptance
