# Phase-S S2-T4 — Mutation testing on cost-governor + scheduler (design spec, v1)

_Author: design subagent, 2026-06-02. Scope: self-host-first brief Phase S2 task T4. Source-of-truth_:
- `docs/engineer/briefs/2026-06-01-self-host-first.md` §3 Phase S2 row S2-T4 ("Mutation testing on cost-governor + scheduler — top 2 A+ rubric items from prior waves").
- `docs/engineer/specs/2026-06-01-cost-governor-design.md` §7 A+ row ("Mutation-coverage ≥ 95% on `internal/cost/gate` via go-mutesting") — closed by this spec at the file-disjoint level.
- `docs/engineer/specs/2026-06-01-w10-sigstore-design.md` §7 A+ ("Mutation-coverage ≥ 95% on `internal/sign/sigstore/` via `go-mutesting`") — sibling pattern; this spec generalises the tool choice across two packages.
- Memory: `feedback_research_design_principles` (adoption-first; gremlins vs go-mutesting scored on maintenance + Go-compat), `feedback_grade_rubric` (B/A/A+ + verbatim scorecard in PR body), `feedback_pr_body_file_only` (`gh pr create --body-file`), `feedback_test_godoc_one_line` (one-line test godocs), `feedback_deletion_default`, `feedback_doc_check_banned_phrases`, `feedback_unaddressed_load_bearing`, `feedback_agent_pr_review`.

---

## §1 Goal + non-goal

### 1.1 Goal

Stand up a mutation-testing gate over the two packages that gate operator money and operator lane-capacity, so failing-to-detect mutations file as test-coverage gaps before they ship. Concretely: a single CI workflow runs `gremlins` against an allowlisted subset of `internal/cost/...` and `internal/orchestrator/scheduler` on a weekly cadence, posts a mutation-score artifact, and opens a `[followup]` issue when score drops below threshold. Adopts gremlins as the primary tool (see §2 scoring); keeps go-mutesting as a documented fallback only if gremlins drops upstream maintenance.

### 1.2 Non-goal

- **Mutation testing repo-wide.** Out of scope for v1. Explicit allowlist in §3.2; everything else is excluded. Repo-wide mutation testing is a followup once the two highest-leverage packages have a clean baseline.
- **Per-PR mutation gate.** Out of scope. Weekly cadence (§4) — running gremlins on every PR adds 10-30 min CI time which is prohibitive against the existing `make check` budget. Per-PR mode can opt-in on labelled PRs as a followup.
- **Mutation testing of generated code, integration tests, fixtures, or vendored deps.** Standard exclusions; spelled out in §3.3.
- **Adopting `stryker-mutator/go` (Stryker port).** No working Go port exists at time of writing — Stryker is JS/.NET/Scala native (see §2.3). Listed only as the prior-art-survey third option per `feedback_research_design_principles` (≥2 OSS cite requirement).
- **Mutation operators on metric-only / OTel-attribute-only code paths.** Skip-policy §3.4 — boolean flips on `slog.Info` log-level branches or OTel attr-emit paths produce equivalent mutants (no observable behaviour change) and waste reviewer cycles.
- **A custom mutation framework.** Explicitly rejected per `feedback_research_design_principles`. gremlins + go-mutesting are the proven Go-native primitives.
- **Mutation testing on `internal/cost/pricing` static lookup tables.** Pricing tables are data, not logic; mutation of literal floats has no test-leverage. Excluded in §3.2.

---

## §2 Prior art adopted

Per `feedback_research_design_principles` — ≥2 OSS cited; primary + fallback scored side-by-side on maintenance trajectory, Go-version compatibility, and mutation-operator coverage.

### 2.1 gremlins (primary)

- Repo: https://github.com/go-gremlins/gremlins (MIT, dedicated org).
- Maintenance trajectory: Tagged releases through 2025; quarterly cadence; active issue triage. Single-purpose tool (no multi-language scope-creep).
- Go-version compat: Tracks current Go (1.22 / 1.23 / 1.24) per release notes; runs on the same `go test` invocation regatta already uses.
- Mutation-operator coverage v0.5.x: arithmetic (`+`/`-`/`*`/`/`), conditional (`==`/`!=`/`<`/`<=`/`>`/`>=`), incremental (`++`/`--`), invert-negative, invert-loop-control, invert-bitwise. Eleven operator families — sufficient for cost-arithmetic + scheduler-bool-gate code paths.
- CI integration: native `--output xml|json` modes; `--workers N` for parallelism; `--threshold-efficacy` flag for kill-rate floor; native cover-profile reuse (`--coverage`) so gremlins skips lines `go test -cover` already shows untested.
- Output: per-mutant report with source span + survival reason; JSON shape stable enough to drive issue-filer in §4.3.

### 2.2 go-mutesting (fallback)

- Repo: https://github.com/zimmski/go-mutesting (MIT). Foundational Go mutation tester (2014-).
- Maintenance trajectory: Slower cadence; last tagged release lagged gremlins by ~14 months as of 2026-Q2 inspection. Still functional; still cited by prior cost-gov + W10 specs.
- Go-version compat: Tracks current Go but with longer lag on language-feature support (generics, range-over-func).
- Mutation-operator coverage: arithmetic, conditional, branch, statement-removal. Fewer operator families than gremlins; smaller test-suite footprint per run.
- CI integration: simpler stdout report; less native CI scaffolding than gremlins.
- Decision: keep documented as fallback; if gremlins drops upstream maintenance, the migration path is one-tool-swap, not a rewrite. Both consume `go test` output, so the harness (§4) is tool-agnostic.

### 2.3 stryker-mutator (surveyed; rejected)

- Repo: https://github.com/stryker-mutator (Apache-2.0). Multi-language framework (JS/TS, .NET, Scala).
- **No Go runner.** Stryker has no first-party Go support; community ports are abandoned. Listed as prior-art-survey per `feedback_research_design_principles` ≥2 OSS cite — but cannot be adopted for Go targets.
- Operator-coverage notes (for reference only): equivalence-class operators are richer than either Go tool, but not applicable here.

### 2.4 Side-by-side score

| Axis | gremlins | go-mutesting | stryker-mutator |
|---|---|---|---|
| Go support | native | native | none |
| Maintenance (last 12 mo) | active | slow | n/a for Go |
| Operator families | 11 | 4-5 | n/a for Go |
| CI artifacts | json + xml | stdout | n/a for Go |
| Cover-profile reuse | yes | no | n/a for Go |
| **Adoption decision** | **primary** | **fallback** | **rejected (no Go)** |

---

## §3 Scope

### 3.1 In / Out summary

#### IN

1. **Allowlisted subpackages** of `internal/cost/...` + `internal/orchestrator/scheduler` per §3.2.
2. **Single gremlins runner** invoked from `scripts/mutation/run-gremlins.sh` (NEW). Reads allowlist from `.gremlins.toml` (NEW).
3. **CI workflow** `.github/workflows/mutation-testing.yml` (NEW) on weekly cron + manual `workflow_dispatch`.
4. **Mutation-score threshold gate** — `--threshold-efficacy 70` (kill-rate ≥ 70%) per package. Below threshold ⇒ workflow fails ⇒ auto-files a `[followup][mutation-gap]` issue with the survived-mutants report attached.
5. **Skip-mutant policy** as TOML exclude rules per §3.4.
6. **Operator runbook** at `docs/operator/mutation-testing.md` (NEW) — how to reproduce locally, how to triage a failed mutant, how to extend the allowlist.
7. **A+ rubric closeout** — closes cost-gov spec §7 A+ row (`internal/cost/gate` mutation-coverage) at file-disjoint task T2 of §8.

#### OUT

- Mutation testing of any package outside §3.2 allowlist.
- Per-PR mutation gate (weekly only).
- Mutation operators on OTel-attr / log-level code paths (§3.4 skip policy).
- Mutation testing on `internal/cost/pricing` static tables (data, not logic).
- Custom mutation framework (per §1.2).
- Replacing existing unit / property / fuzz suites — mutation testing is additive, not substitutive.

### 3.2 Allowlist (exact paths)

Allowlisted for v1:

```
internal/cost/gate/         (gate.go, verdict.go, scope.go — pre-call deny primitive)
internal/cost/spend/        (writer.go, reader.go, payload.go, spawner_callback.go, scope.go — spend reducer + reader)
internal/cost/estimate/     (upper_bound.go, probe.go — pre-call estimate math)
internal/cost/reconcile/    (tick.go, window.go, backoff.go, client.go — provider reconciler loop)
internal/orchestrator/scheduler/ (scheduler.go — per-tick lane + hotspot + cost-gate sequencing)
```

**Not allowlisted** (data / generated / out-of-leverage):

```
internal/cost/pricing/      (lookup table — data, not logic; mutation-of-literal-float is noise)
```

Total in-scope production LoC: ~2,742 (post-test exclusions). Total test LoC backing the allowlist: ~2,548. Test-to-prod ratio ~0.93 — sufficient baseline for an initial ≥70% kill-rate gate (§4.2).

### 3.3 Standard exclusions (file-level)

Excluded from gremlins regardless of allowlist:

- `*_test.go` (test fixtures, not subject).
- `*.pb.go`, `*_gen.go`, `*_generated.go`, `mock_*.go` (generated / mock).
- `doc.go`, `package_*.go` containing only `package` clause + godoc.
- `cmd/**` (CLI wiring — covered by integration tests, low mutation leverage).

Configured via `.gremlins.toml` `[exclude.files]` glob list.

### 3.4 Skip-mutant policy

Operator-level skips (per `feedback_research_design_principles` — adopt the upstream skip flags rather than fork the tool):

- **Boolean operators on `slog.*` / `obs.Event*` emit branches.** Flipping a log level (e.g. `slog.Info` → `slog.Warn`) produces equivalent mutants — no observable behaviour change. Configured via `[exclude.line_patterns]` regex matches on `slog\.`, `obs\.Event`, `otel\.`, `span\.SetAttributes`.
- **Span-attribute-only branches.** A conditional that only chooses between two OTel attribute values without altering control flow is mutation-noise.
- **Arithmetic on duration / metric counters** that feeds only into telemetry (no downstream gate decision). Identified by reviewer during baseline calibration (§5.1); annotated with `// gremlins:skip` line comments.

Skip-decisions are reviewer-gated, not implementer-self-approved (per `feedback_subagent_verification`). Adversarial reviewer subagent runs over every skip annotation at PR review time.

### 3.5 Threshold rationale

- **70% kill-rate floor.** Industry-typical mutation-coverage targets sit at 60-80% (gremlins maintainers' own examples cite 75% as a healthy default). 70% is a conservative starting bar — high enough to catch most "test asserts nothing useful" defects, low enough to land without a multi-week test-rewriting campaign.
- **No per-mutant whitelist file.** Skip via patterns (§3.4) + `// gremlins:skip` line comments — both reviewer-gated. A per-mutant whitelist file is rejected: it becomes the dumping ground for "I cannot fix this right now" survivals and the kill-rate stops meaning anything.
- **Per-package threshold, not repo-wide.** Each allowlisted package has its own kill-rate; one slow-converging package cannot drag down or be hidden by another's high score. Configured as `[threshold.per_package]` in `.gremlins.toml`.

### 3.6 Adversarial-test-quality framing

Mutation testing is the canary on test-quality drift, not a coverage substitute. A passing kill-rate says "the tests notice when production code changes"; it does NOT say "the tests assert correctness". The skip policy (§3.4) is the guard against the inverse failure mode — equivalent mutants padding the score without raising the asserts-something bar. The reviewer-gated skip-approval (§3.4) is the spine of the adversarial framing.

---

## §4 Runtime budget + CI integration

### 4.1 Cadence: **weekly** cron + manual `workflow_dispatch`

Decision: weekly, not per-PR. Rationale:

- Gremlins on the §3.2 allowlist takes ~15-25 min on GitHub-hosted runners (5 packages × ~3-5 min average; gremlins runs `go test` per mutant). Per-PR cost is prohibitive against the existing `make check` ~3-5 min wall-clock.
- Mutation-score drift is a slow signal — daily or weekly granularity is sufficient to catch regressions before they accumulate.
- `workflow_dispatch` gives operators on-demand mode for verifying a specific PR's mutation impact pre-merge when leverage justifies the cost.

Cron expression: `0 7 * * 1` (Monday 07:00 UTC; aligns with operator-week start; results sit in the inbox by Monday morning Pacific).

### 4.2 Score threshold per package

| Package | v1 floor | Stretch (followup wave) |
|---|---|---|
| `internal/cost/gate` | 80% | 95% (closes cost-gov §7 A+ row) |
| `internal/cost/spend` | 75% | 90% |
| `internal/cost/estimate` | 75% | 90% |
| `internal/cost/reconcile` | 70% | 85% |
| `internal/orchestrator/scheduler` | 70% | 85% |

Rationale for tiering: `gate` is the load-bearing pre-call deny primitive — strictest. `scheduler` and `reconcile` are larger code surfaces with more equivalent-mutant noise — lower v1 floor. Tiering source: pre-spec calibration run (see §5.1).

### 4.3 Failure-mode workflow

When gremlins exits non-zero (threshold breach):

1. Workflow uploads the gremlins JSON report as a CI artifact (90-day retention).
2. Workflow runs `scripts/mutation/file-followup.sh` (NEW) which calls `gh issue create` with title `[followup][mutation-gap] <package>: kill-rate <X>% below floor <Y>%` and body = survived-mutant list + reproduce instructions.
3. Issue is labelled `[followup]` + `[mutation-gap]` + `[automated]`.
4. No automatic PR-blocking — weekly cadence makes failure-mode advisory, not blocking. The followup issue triages into a normal PRIORITY-list entry.

### 4.4 Local-developer mode

Operator can reproduce CI exactly with `make mutation-test`. Make target wraps `scripts/mutation/run-gremlins.sh` with `--workers $(nproc)` and `--no-threshold` (developers care about which mutants survived, not pass/fail). Wall-clock on M-series Mac: ~3-8 min.

### 4.5 Cost-budget alignment

Mutation testing's compute cost is GitHub Actions minutes, not Anthropic tokens — outside the cost-governor's USD-cap regime. Tracking-issue followup if mutation-testing exceeds 60 min weekly (would indicate scope-creep into non-allowlisted packages); listed in §10 followups.

---

## §5 Baseline calibration + skip-mutant audit

### 5.1 Pre-spec calibration run

Before this spec lands, a one-off gremlins run is needed against the §3.2 allowlist to (a) confirm wall-clock estimates in §4.1, (b) seed §4.2 thresholds with measured kill-rates, (c) identify equivalent-mutant clusters that need `gremlins:skip` annotations. Output: `docs/engineer/specs/2026-06-02-s2-t4-mutation-testing-baseline.md` (deferred sibling doc; not in this PR — calibration is a T0 step before T1, see §8).

### 5.2 Skip-annotation audit

After calibration but before merging the workflow, the adversarial reviewer subagent (per `feedback_agent_pr_review`) reviews every `// gremlins:skip` annotation + `[exclude.line_patterns]` entry. Skips that cannot defend themselves as equivalent-mutant or telemetry-only are rejected; the underlying test gap goes on the `[followup][mutation-gap]` triage instead. This is the adversarial-test-quality gate's spine.

---

## §6 B / A / A+ rubric (per `feedback_grade_rubric`)

### B — minimum (must-have to merge)

- [ ] `.gremlins.toml` configures §3.2 allowlist + §3.3 exclusions + §3.4 skip patterns + §4.2 per-package thresholds.
- [ ] `scripts/mutation/run-gremlins.sh` runs gremlins against the allowlist; exits non-zero on threshold breach.
- [ ] `.github/workflows/mutation-testing.yml` runs on weekly cron + manual dispatch; uploads JSON artifact on every run.
- [ ] `scripts/mutation/file-followup.sh` opens `[followup][mutation-gap]` issue with survived-mutant list on failure.
- [ ] `Makefile` target `mutation-test` wraps the runner with developer-friendly flags.
- [ ] `docs/operator/mutation-testing.md` runbook covers: how to reproduce locally, how to triage a survived mutant, how to add / remove an allowlist entry, how to defend a `gremlins:skip` annotation.
- [ ] Baseline kill-rates measured + recorded in calibration sibling doc; thresholds in §4.2 reflect measured floors + 5pp headroom.
- [ ] Zero new SQL migrations. Migration count delta = 0.
- [ ] `make check` clean; doc-check passes; PR-lint passes.

### A — target (expected)

All B, plus:

- [ ] `internal/cost/gate` kill-rate ≥ 85% on the first weekly run after merge (closes cost-gov §7 A+ row partially).
- [ ] All packages in §3.2 meet their v1 floor on the first weekly run.
- [ ] Adversarial reviewer subagent cleared every `// gremlins:skip` annotation; zero unaddressed Risk-tier findings (per `feedback_agent_pr_review`).
- [ ] Tracking issues filed for every followup in §10; cited by number in PR body (per `feedback_unaddressed_load_bearing`).
- [ ] Operator can dispatch the workflow manually + view the JSON artifact in < 30 seconds of GitHub-UI clicks.
- [ ] Mutation-testing wall-clock CI minutes ≤ 30 per weekly run.

### A+ — stretch (aspirational)

All A, plus:

- [ ] `internal/cost/gate` kill-rate ≥ 95% (closes cost-gov §7 A+ row in full).
- [ ] Per-PR opt-in mode via `[mutation-test]` PR label runs gremlins on the labelled-PR diff only (scoped to changed packages within the allowlist); follow-up wave deliverable.
- [ ] Mutation-score trend dashboard in `docs/engineer/` (CSV log of weekly scores + tiny ASCII trend) — operator can spot drift weeks before threshold breach.
- [ ] Cross-PR regression detection: if a PR drops a previously-killed mutant into the survived set, the weekly run posts a comment back on the offending PR.
- [ ] Skip-policy enforcement: no PR adds a `// gremlins:skip` annotation without an adversarial reviewer subagent's sign-off captured in the PR body.

---

## §7 File-disjoint task breakdown (preview only)

Full plan PR comes after this spec lands. Preview only; **NOT a task breakdown for execution**. Six tasks, file-disjoint where possible.

| # | Task | Files touched | OWNER notes |
|---|---|---|---|
| **T0** | Pre-merge calibration run + baseline doc | `docs/engineer/specs/2026-06-02-s2-t4-mutation-testing-baseline.md` (NEW) | OWNS the measured thresholds; T1-T5 consume them. No code changes. Sibling-doc to this spec; co-located per `feedback_cross_doc_link_phasing`. |
| **T1** | `.gremlins.toml` + allowlist + exclusions + skip patterns | `.gremlins.toml` (NEW) | Reviewer-gated; depends on T0's measured thresholds. |
| **T2** | Runner script + Makefile target | `scripts/mutation/run-gremlins.sh` (NEW), `scripts/mutation/run-gremlins_test.sh` (NEW), `Makefile` (edit: one target) | File-disjoint from T1 + T3 + T4 + T5. |
| **T3** | CI workflow | `.github/workflows/mutation-testing.yml` (NEW) | Depends on T2 (calls the runner). File-disjoint from T1 + T2 + T4 + T5. |
| **T4** | Followup-issue filer | `scripts/mutation/file-followup.sh` (NEW), `scripts/mutation/file-followup_test.sh` (NEW) | Depends on T2 (consumes runner's JSON output). File-disjoint from T1-T3 + T5. |
| **T5** | Operator runbook | `docs/operator/mutation-testing.md` (NEW) | Depends on T1-T4; lands last. File-disjoint from all code tasks. |

**Total: 6 file-disjoint tasks**. T0 is doc-only + measurement; T1 dispatches after T0; T2 + T3 + T4 dispatch in a second wave; T5 lands last. Migration number lock per `feedback_migration_number_lock`: **N/A — zero migrations added**.

Test godocs: one-line per `feedback_test_godoc_one_line` — every shell-test in T2/T4 has a single-line header comment ≤ 100 chars.

---

## §8 Sequencing

S2-T4 lands **AFTER** the cost-governor Wave 3 docs (S1-T4 IN FLIGHT per brief) so the §3.2 allowlist matches the merged code surface. Independent of S2-T1 (replay+diff), S2-T2 (adversarial reviewer gate), S2-T3 (followup auto-triage) — those are gates the operator runs; mutation testing is a CI-side gate that runs against the merged code.

```
S1-T4 (cost-gov W3 docs, IN FLIGHT)
    │
    ▼
T0 (calibration baseline)
    │
    ▼
T1 (.gremlins.toml)
    │
    ├──────┬──────┬──────┐
    ▼      ▼      ▼      ▼
   T2     T3     T4     (parallel)
  runner  CI    filer
    │      │      │
    └──────┼──────┘
           ▼
          T5 (runbook)
```

T1 must merge before T2-T4 (allowlist contract). T2-T4 dispatch in parallel after T1. T5 lands last (consumes the merged behaviour for the runbook).

---

## §9 Risks + caveats

### R1 — Equivalent-mutant noise

If equivalent-mutant clusters dominate survived-mutants, kill-rate stops measuring test quality and starts measuring annotation discipline. **Mitigation**: reviewer-gated skip annotations (§3.4); annotation count tracked in the runbook (§B-checklist runbook entry); spike in skip-annotation count triggers a `[followup][mutation-gap]` issue independent of kill-rate.

### R2 — Wall-clock CI cost

Gremlins is `go test` × N-mutants — order-of-magnitude slower than `make check`. **Mitigation**: weekly cadence (§4.1); §3.2 allowlist keeps surface small; `--workers $(nproc)` parallelism; cover-profile reuse skips already-untested lines. Tracking issue filed if exceeds 30 min weekly (§10).

### R3 — go-mutesting drift if gremlins upstream stalls

Fallback path (§2.2) requires re-targeting the runner. **Mitigation**: runner script wraps gremlins behind a single env-var (`MUTATION_TOOL=gremlins|go-mutesting`); switch cost is one-file. Tracking issue files when gremlins last-release exceeds 12 months (§10).

### R4 — Skip-policy becomes a dumping ground

Adversarial-test-quality gate fails if skips accumulate without reviewer push-back. **Mitigation**: adversarial reviewer subagent (per `feedback_agent_pr_review`) on every PR that adds a `gremlins:skip`; skip-count tracked weekly in the trend dashboard (A+ row).

### R5 — Mutation testing on flaky tests amplifies flakiness

If a unit test is flaky, gremlins reports it as a survived mutant with N% confidence. **Mitigation**: pre-spec test-flake audit (T0 calibration step §5.1); flake-rate > 1% on any §3.2 package blocks T1 until the underlying flake is fixed at root cause (per `feedback_root_cause`).

### R6 — Per-PR mode (A+ stretch) doubles CI cost long-term

Stretch row in §6 A+ proposes opt-in `[mutation-test]` label triggering gremlins per-PR. **Mitigation**: scoped to changed packages within allowlist only; default-off; documented opt-in. Followup wave decides if cost-benefit justifies promoting from opt-in to default.

### R-A1 — Caveat: 70% threshold means 30% of mutations are not caught

The v1 threshold (§3.5) is a starting bar, not an A+ target. The A+ stretch rows in §6 close the gap to 95%+. The threshold is honest about its conservatism: it is the floor below which a `[followup][mutation-gap]` issue fires, not the bar at which the test-quality gate is complete.

### R-A2 — Caveat: mutation testing does not catch logic gaps

If a code path has zero tests, it is excluded from gremlins via `--coverage` cover-profile reuse (§2.1) — gremlins skips lines `go test -cover` already shows untested. Therefore kill-rate measures test-quality of tested code; coverage measures test-existence. **Both gates are needed**; mutation testing supplements, not replaces, `go test -cover` thresholds.

---

## §10 Deferred + followups (pre-enumerated)

Per `feedback_unaddressed_load_bearing` — file as gh issues, cite by number in PR body before merge.

1. **Repo-wide mutation testing.** Expand allowlist beyond §3.2 once two highest-leverage packages have a clean weekly baseline for ≥ 4 consecutive runs.
2. **Per-PR opt-in mode.** A+ stretch row §6; promote to default once cost-benefit is measured.
3. **Mutation-score trend dashboard.** A+ stretch; CSV log + tiny ASCII trend; lands in `docs/engineer/`.
4. **Cross-PR regression detection.** A+ stretch; weekly run comments on the PR that introduced a previously-killed-now-survived mutant.
5. **CI minutes budget enforcement.** Tracking issue if weekly run exceeds 30 min (R2).
6. **go-mutesting fallback wiring.** Land the env-var switch (R3) before it is needed, not after.
7. **Skip-policy audit cron.** Weekly job lists all `// gremlins:skip` annotations + `[exclude.line_patterns]` entries; fires followup if count rises week-over-week (R4).
8. **Test-flake baseline on §3.2 packages.** Pre-calibration step; if flake-rate > 1%, fix root cause before merging T1 (R5).
9. **`internal/cost/gate` 95% kill-rate.** Closes cost-gov §7 A+ row in full (currently §6 A+ row in this spec).
10. **Mutation-score per-commit-author breakdown.** If kill-rate drops, who authored the un-killed mutant's enclosing file? Lightweight audit aid.
11. **Mutation-operator family enable/disable per package.** Some packages benefit from arithmetic operators only; others from conditional only. Per-package operator tuning is a refinement after the v1 baseline.
12. **Mutation testing on `internal/gates/...` (W8 OPA RBAC, W9 replay+diff).** Sibling expansion; depends on W8 + W9 merge state.
13. **Mutation testing on `internal/sign/sigstore` (W10).** Closes W10 §7 A+ row; sibling expansion once W10 merges.

---

## §11 References

- Self-host-first brief: `docs/engineer/briefs/2026-06-01-self-host-first.md` §3 Phase S2 row S2-T4.
- cost-governor spec (closes A+ row): `docs/engineer/specs/2026-06-01-cost-governor-design.md` §7 A+.
- W10 sigstore spec (sibling A+ pattern): `docs/engineer/specs/2026-06-01-w10-sigstore-design.md` §7 A+ (deferred to followup #13).
- gremlins: https://github.com/go-gremlins/gremlins (MIT, 2025 active).
- go-mutesting: https://github.com/zimmski/go-mutesting (MIT, 2014-).
- stryker-mutator: https://github.com/stryker-mutator (Apache-2.0, multi-language; no Go runner).
- Memory: `feedback_research_design_principles`, `feedback_grade_rubric`, `feedback_pr_body_file_only`, `feedback_test_godoc_one_line`, `feedback_deletion_default`, `feedback_doc_check_banned_phrases`, `feedback_unaddressed_load_bearing`, `feedback_agent_pr_review`, `feedback_subagent_verification`, `feedback_cross_doc_link_phasing`, `feedback_migration_number_lock`, `feedback_root_cause`.
