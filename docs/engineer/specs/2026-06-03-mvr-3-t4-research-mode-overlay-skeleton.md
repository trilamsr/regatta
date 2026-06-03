---
title: "MVR-3-T4 Research-mode overlay (skeleton-tier pre-fetch)"
status: active
summary: Pre-fetch skeleton for MVR-3-T4 research-mode overlay (WorkItem.kind=research discriminator + 4 methodology gates + nightly reproducibility cron); full spec re-spawns when MVR-3 trigger fires AND the publication-credible-audit-chain prerequisite (MVR-3-T1 Sigstore) has merged. Locks scope, prior-art, risks, test plan, dep-order.
---

# MVR-3-T4 Research-mode overlay (skeleton-tier pre-fetch)

_Author: design subagent, 2026-06-03. Skeleton-tier per `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 Phase MVR-3 row T4 (L, 6-8 wks). This spec is the pre-fetch contract; it does NOT dispatch implementer subagents._

Cites: `feedback_research_design_principles` (adopt prior-art over bespoke), `feedback_decision_priority` (UX > ease > performance > best-practices > velocity; long-term > short-term), `feedback_grade_rubric`, `feedback_deletion_default` (70% cut from original 6-component proposal already locked), `feedback_spec_pattern_authority`.

Prior-art baseline: `docs/engineer/specs/2026-06-01-research-mode-extension-design.md` (42 KB Wave 1 design, post-adversarial-cut) is the source-of-truth for the full surface. This skeleton inherits the cut version and re-litigates only the MVR-3 slice.

---

## 0. Scope (in / out)

### In scope (MVR-3-T4)

- **`WorkItem.kind="research"` discriminator** — one CUE enum extension; routes through new methodology gates.
- **Four methodology gate packages:** `internal/gate/phack` (multiple-comparison correction), `internal/gate/statpower` (power-analysis threshold), `internal/gate/leakage` (train/test bleed detection on dataset shards), `internal/gate/stattest` (test-statistic appropriateness check).
- **Nightly reproducibility cron** — K=10 fresh-seed replay sweep over completed research items; emits `research_reproducibility_drift` substrate event when result drifts beyond config-driven epsilon.
- **Four Rego rules** under W8 OPA (`policy/research/*.rego`): only `role=research-author` can dispatch `kind=research`; only `role=research-reviewer` can clear gate verdicts.
- **Prereg fields** — `prereg.dataset.sha256` + `prereg.baselines[].artifact_sha256` declared in WorkItem schema; locked at prereg time; byte-equality check on replay.
- **CLI** — `regatta research prereg` + `regatta research export-bundle` (local publish to disk).

### Out of scope (MVR-3-T4)

- New schemas / new adapters / new tables (zero per §11 of the parent spec — rides existing primitives).
- Built-in publication adapter (arxiv push, OSF integration — followup once a customer asks).
- Cross-org dataset sharing (W8 OPA tenant scope holds; research items inherit it).
- LLM-authored hypothesis generation (out of scope — overlay only audits human/agent-authored research).
- Dataset versioning beyond sha256 pin (Git-LFS / DVC integration deferred to W11 blackboard CAS once that ships).
- Cost-model overrides (cost-governor handles spend caps uniformly; research items inherit budget).

## 1. Prior art (cite version + license)

| Primitive | Adopted from | Version | License | What we take |
|---|---|---|---|---|
| Pre-registration | [OSF Registries](https://osf.io/registries/) + [AsPredicted](https://aspredicted.org/) | n/a | docs ref | YAML prereg shape; locked-at-time-of-dispatch contract; sha256 pin on dataset + baselines |
| p-hack gate | [Benjamini-Hochberg FDR](https://en.wikipedia.org/wiki/False_discovery_rate) | n/a | public algorithm | Multiple-comparison correction; threshold-driven verdict |
| Power-analysis gate | [G*Power 3.1](https://www.gpower.hhu.de/) + [statsmodels](https://www.statsmodels.org/stable/stats.html#power-and-sample-size-calculations) | n/a | docs ref | Power threshold (default 0.8) gated at prereg time |
| Leakage gate | [scikit-learn `GroupKFold` docs](https://scikit-learn.org/stable/modules/generated/sklearn.model_selection.GroupKFold.html) | n/a | BSD-3 docs ref | Train/test shard-overlap detector; sha256-on-dataset-rows comparison |
| Reproducibility cron K=10 | [NeurIPS reproducibility checklist](https://neuripsconf.medium.com/designing-the-reproducibility-program-for-neurips-2020-7fcccaa5c6ad) + [ML Reproducibility Challenge](https://reproml.org/) | n/a | docs ref | K=10 fresh-seed sweep; epsilon-bounded drift verdict |
| W9 replay primitive | `docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md` | n/a | repo-internal | DurableHistory interface; substrate-default impl is the K=10 cron's replay engine |

Rejected alternatives (per parent spec §11, 70% cut): bespoke prereg DSL (CUE already covers); custom causal-inference toolkit (out of scope for v1); LLM-authored gates (defer until methodology library matures); separate research-substrate (rides existing substrate per `feedback_deletion_default`).

## 2. Architecture (high-level)

```
contracts/schemas/
  spec_adapter.go      // +Kind=research enum; +prereg.dataset.sha256; +baselines[].artifact_sha256
internal/gate/
  phack/, statpower/, leakage/, stattest/   // four gate packages
policy/research/
  author.rego, reviewer.rego, prereg.rego, replay.rego
internal/research/
  cron.go              // nightly K=10 reproducibility sweep
cmd/regatta/
  research.go          // CLI: prereg + export-bundle
```

Gates plug into the existing L0-L6 gate stack (per `docs/design.md`). Cron rides W9 DurableHistory (substrate-default impl from Phase S2-T1).

## 3. Key risks (≥6 named)

| # | Risk | Mitigation |
|---|---|---|
| R1 | Premature dispatch before MVR-3-T1 (Sigstore) ships | Hard gate: `kind=research` items refuse to dispatch until `signer.kind != "local"` — assertion at admission |
| R2 | Methodology library drift (p-hack thresholds change in literature) | Threshold config-driven (`research.phack.fdr_target`); operator can override per-deployment; baseline locked at prereg time |
| R3 | Dataset sha256 pin breaks on file format upgrade (e.g. CSV → Parquet) | sha256 over canonical-bytes (operator-provided canonicalizer); migration path documented in operator runbook |
| R4 | K=10 reproducibility cron cost blowout | Cron runs against cost-governor; budget cap per-research-item; refuses K=10 if budget exhausted; partial-K result still emits drift event |
| R5 | Operator floods substrate with `research_reproducibility_drift` events on noisy datasets | Epsilon config-driven; drift events deduped via lww reducer keyed by (research_id, week); operator runbook covers tuning |
| R6 | Leakage gate false-positive on legitimately overlapping shards (e.g. time-series CV) | Operator can register custom shard-strategy via OPA rule; default GroupKFold is conservative |
| R7 | Prereg lock-in too rigid — author needs to amend after dispatch | Amendments allowed via `regatta research amend`; substrate event `prereg_amended` (lww); reviewer must re-clear |
| R8 | Cross-research-item dependency hell (item B reads item A's pinned dataset) | Dataset sha256 stored as opaque blob ref; W11 blackboard CAS upgrade path documented in §2.2 of parent spec |
| R9 | OPA Rego rule typos block all research dispatch | Boot-time policy validation per W8 OPA slim; CI gate runs `opa test policy/research/`; deploy refuses if test fails |

## 4. Test plan (≥8)

1. `TestResearchKind_Admission_RequiresSignerNonLocal` — submit `kind=research` with `signer.kind=local`; admission rejects.
2. `TestPHackGate_FDRThreshold` — 100 simulated comparisons; assert FDR-corrected verdict matches Benjamini-Hochberg reference.
3. `TestStatPowerGate_Threshold` — power=0.7 vs default 0.8; gate rejects; power=0.85 clears.
4. `TestLeakageGate_DetectsTrainTestOverlap` — synthetic dataset with row-level overlap; gate flags. No overlap → clears.
5. `TestStatTestGate_AppropriatenessHeuristic` — t-test on non-normal data → flag; t-test on normal data → clear.
6. `TestReproducibilityCron_K10_Drift` — seed deterministic agent; run K=10; drift below epsilon → no event; drift above → event emitted.
7. `TestPreregLock_Sha256ImmutableAfterDispatch` — prereg.dataset.sha256 set; attempt to mutate post-dispatch → ErrPreregLocked.
8. `TestPreregAmend_RequiresReviewerReClear` — amend prereg; reviewer must re-verdict; gate stays red until clearance.
9. `TestOPARule_OnlyResearchAuthorCanDispatch` — non-research-author identity submits → 403.
10. `TestOPARule_OnlyResearchReviewerCanClearVerdict` — non-reviewer attempts gate clearance → 403.
11. `TestExportBundle_LocalPublishDeterministic` — render same research item twice; bundles byte-equal.
12. `BenchmarkPHackGate_10kComparisons` — p99 ≤ 100ms.
13. `FuzzCanonicalizer_DatasetSha256` — random whitespace permutations canonicalize to same sha256.

## 5. Dep order

1. **MUST be merged first:** MVR-3-T1 Sigstore (the "publication-credible audit chain" prerequisite — see roadmap §3 reopen criteria and parent spec §0 trigger #4).
2. **MUST be merged first:** Phase S3-T1 W8 OPA slim (`docs/engineer/specs/2026-06-02-s3-t1-w8-opa-slim.md`) — Rego rule hot-reload + single-tenant Authorizer.
3. **MUST be merged first:** Phase S2 W9 substrate-default DurableHistory (`docs/engineer/specs/2026-06-02-s2-t1-w9-substrate-impl.md`) — K=10 cron's replay engine.
4. **MUST be merged first:** Substrate Wave 1 + S3-T2 cutover (`docs/engineer/specs/2026-06-02-s3-t2-substrate-cutover.md`).
5. **SHOULD be merged first:** 30-day-self-host-green trigger OR external research-customer ask on file (per parent spec §0).
6. **No dep on MVR-3-T2 / T3** — research-mode is orthogonal to Stripe billing and blackboard CAS (rides existing primitives; W11 upgrade path is forward-fit only).
7. **Trigger:** MVR-3 entry per roadmap §4 AND MVR-3-T1 merged AND 30-day-green OR research-customer ask.

## 6. Grade rubric (filled at dispatch time)

| Criterion | B (must) | A (should) | A+ (aspires) |
|---|---|---|---|
| `make check` clean | _filled at dispatch_ | _filled_ | _filled_ |
| ZERO new schemas / adapters / tables | _filled_ | _filled_ | _filled_ |
| Four gates land behind L0-L6 stack | _filled_ | _filled_ | _filled_ |
| K=10 cron rides W9 replay (no parallel impl) | _filled_ | _filled_ | _filled_ |
| Prereg lock is byte-equal across re-run | _filled_ | _filled_ | _filled_ |
| Deletion ledger (vs original 6-component proposal) | _filled_ | _filled_ | _filled_ |

## 7. What got smaller

Skeleton-tier inherits the 70% cut from the parent spec's adversarial review (6-component proposal → 1 CUE enum + 4 gates + 1 cron + 4 Rego rules). MVR-3-T4 ships ONLY this cut version — minimum surface that closes the "autonomous empirical research" claim blocking persona-B/C/D research-customer adoption. Publication-adapter + LLM-authored gates + cross-org dataset sharing stay deferred to followup wedges.
