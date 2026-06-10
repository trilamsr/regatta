---
title: "Live-outcome validation loop — Design Spec"
status: phase-x-deferred
summary: "Closes the trust gap between merged and actually-fixed. Three layers: L1 pre-merge canary (spin two binaries, diff observable behavior over fixture inputs), L2 post-merge probe (issue/PR body encodes a runnable acceptance probe; scheduler asserts probe passes against the merged binary before declaring work_item DONE), L3 cross-ref to W9 replay-diff harness as the deep variant of L1. Pairs with the manual operator-driven smoke (#864) for full coverage. Four-slice implementer roadmap: `regatta validate-pr` skeleton, ephemeral sandbox + L1 diff engine, L2 probe parser + outcome-verify runner, cost-gate integration."
deferred_on: 2026-06-10
---

# Live-outcome validation loop — Design Spec

Status: ready for review
Date: 2026-06-08
Author: design subagent
Tracks: #864 (manual smoke companion), #909 (reviewer-verdict gate; static), #917 (`regatta doctor` preflight; static)
Cross-ref: `docs/engineer/specs/2026-06-01-w9-replay-diff-harness-design.md` (L3 deep variant), `docs/engineer/specs/2026-06-07-autotuner-closed-loop.md` (consumes L2 verdict as a finding source).

Memory rules in force: `feedback_decision_priority`, `feedback_default_simpler`, `feedback_root_cause`, `feedback_deletion_default`, `feedback_adversarial_review`, `feedback_adversarial_review_every_step`, `feedback_unaddressed_load_bearing`, `feedback_spec_pattern_authority`, `feedback_no_signatures`, `feedback_validate_before_ship`, `feedback_dispatch_brief_only`, `feedback_research_design_principles`.

---

## §1 Problem

Today's merge contract:

`unit-tests-pass AND L4-reviewer-verdict=APPROVE AND ci-green ⇒ change is correct`

This is a static-text contract — none of the three conjuncts run the merged binary against an observable input and prove the *claim* of the change. Symptom inventory:

- `regatta doctor` (#917) is preflight-only; runs before a spawn, not after merge.
- L4 reviewer gate (#909) reads the diff text adversarially but does not execute it.
- CI green = "compiles + unit-tests-pass" — fixture coverage is whatever the implementer remembered.
- W4 self-improve detector pattern-classifies post-merge symptoms but is reactive (an SLO has already breached when it fires).
- Alarm-webhook fires only after a customer-visible regression — too late.

Adversarial framing: tests can pass while shipping wrong behavior (e.g. test asserts `err != nil` when implementation returns `ErrUnknown` instead of the documented `ErrSourceMutated`); the L4 reviewer reads the assertion as written and finds nothing wrong; CI is green; the change ships; the next operator hits the surprise.

The manual variant of the answer is #864 (self-host-smoke-harness): operator runs the binary against a curated input post-merge. That is **operator-driven**. This spec is the **automated per-PR** companion — sequenced so the cheap automated step runs before the manual sweep.

Trust gap closed when: every load-bearing PR carries an executable witness that the claimed change was actually applied to runtime behavior, and an operator can rerun that witness with one CLI verb.

## §2 Goal

Operators can answer "did the merged PR actually fix what it claimed?" with `regatta validate-pr <PR#>` and a programmatically-collected verdict surface, without exposing the orchestrator binary to external customer traffic and without trained-model inference.

Three-layer ladder, sequenced cheapest-first:

- **L1** — pre-merge canary diff (this spec ships).
- **L2** — post-merge probe verify (this spec ships).
- **L3** — counterfactual replay/diff (cross-ref to W9; this spec does NOT re-implement).

L1 and L2 are file-disjoint slices that can land in current phase. L3 is Phase-X-forward-fit through W9.

## §3 Scope (in)

3.1 New CLI subcommand `regatta validate-pr <PR#> [--layer=l1|l2|both] [--budget-usd=N] [--report=<path>]` under `cmd/regatta/validate_pr.go`. Default `--layer=both`; default budget read from `regatta.yaml` `validation.budget_usd_default`.

3.2 New package `internal/validation/` exporting:

- `Layer` enum (`L1Canary`, `L2Probe`).
- `Report` struct with per-fixture / per-probe `Outcome` (`pass`, `fail`, `skipped`, `cost_gated`).
- `Runner` interface — one `Run(ctx, prNumber, opts) (Report, error)` entry point dispatched to L1 / L2 sub-runners.

3.3 L1 sub-package `internal/validation/canary/`:

- `sandbox.go` — ephemeral working directory + `os/exec` driver that builds the PR HEAD binary and the `origin/main` binary into two tmpdirs, runs each against the same fixture inputs read from `testdata/validation/fixtures/*.yaml`, captures stdout + structured-log + substrate-event tail.
- `diff.go` — reducer-aware comparison reusing the substrate event-equality predicate (substrate spec §4 LWW-vs-append). Output: per-fixture `pass | drift_detected | error`.
- Fixture inputs use the existing `_testdata/golden/` corpus shape (deterministic seed, sealed clock, pinned model = stub).

3.4 L2 sub-package `internal/validation/probe/`:

- `parser.go` — extracts `<!--regatta:probe ... -->` HTML-comment block from issue body OR PR body. Allowed block fields: `kind: test|script|exit_code`, `cite: #NNN` (REQUIRED — must reference the issue closed by the PR), `target: <go-test-name-or-path>`, `timeout_seconds: <int<=300>`.
  - **`closes` regex (pinned, closes #989):** `cite` matching uses the regex `(?im)^(?:closes|fixes|resolves)[[:space:]]+#([0-9]+)\b`, case-insensitive, matched against the PR body line-by-line. Body MUST contain ≥1 such line whose captured issue number equals `cite`'s. The match consumes only the `closes` family (GitHub auto-close keywords) — `see #NNN`, `re #NNN`, prose mentions DO NOT count. Multiple `closes #N` lines all valid; `cite` matches any one.
- `runner.go` — dispatch per `kind`:
  - `kind: test` → `go test -run <target> -count=1 -timeout <timeout>s` against the **merged-SHA** tree (closes #989): runner clones the repo at `${merged_sha}` into an ephemeral working dir, runs the test there. The probe NEVER runs against current `main` — the test sources at probe-execution time MUST be the test sources of the commit being validated. Pass iff exit 0.
  - `kind: script` → exec `target` (path inside repo, no shell expansion); pass iff exit 0.
  - `kind: exit_code` → spawn `regatta <target>` (regatta subcommand only) and assert exit code matches the block's `expect_exit`.
- `verdict.go` — emits `Outcome` + structured reason; writes a `KindLiveValidation` substrate event tagged with `layer=l2`, `pr=<n>`, `probe_kind=<k>`, `verdict=<v>`.

3.5 New substrate event kind `KindLiveValidation` added to the enum at `internal/orchestrator/state/substrate/event.go`. Payload (`payload_json`):

```json
{
  "pr_number": 942,
  "layer": "l1" | "l2",
  "verdict": "pass" | "fail" | "skipped" | "cost_gated",
  "fixture_count": 3,
  "drift_count": 0,
  "probe_kind": "test",
  "probe_target": "TestGitHubIssues_Skip",
  "probe_cite": "#841",
  "duration_seconds": 12.4,
  "cost_usd": 0.0,
  "signed_by": "<orchestrator-kid>"
}
```

3.6 Cost-gate integration via existing `internal/cost/cap/` package:

- L1 fixture run pre-flights `cap.PredictUSD(fixtureCount)` (existing predicate in `cap.go`). Skip with `cost_gated` verdict + reason string when prediction exceeds `--budget-usd`.
- L2 probe runs are bounded to `timeout_seconds ≤ 300` per block + no `cost_usd` charge (probe MUST NOT call paid LLM APIs — fail-closed in `runner.go` by setting `regatta.l4.disabled=true` env var for the probe process).

3.7 New OTel meters (additive, three counters + one histogram):

- `regatta.validation.runs_total` (Int64Counter, attrs `layer`, `verdict`).
- `regatta.validation.drift_detected_total` (Int64Counter, attrs `layer`, `pr_number`).
- `regatta.validation.cost_gated_total` (Int64Counter, attrs `layer`).
- `regatta.validation.duration_seconds` (Float64Histogram, attrs `layer`).

3.8 Trigger surface (no new CI workflow; reuses pr-lint pattern):

- L1 runs ON-DEMAND from `regatta validate-pr <n>` only — no PR-open auto-trigger. Rationale: ephemeral sandbox spawning is expensive; let the operator opt in per PR.
- L2 runs from `regatta validate-pr <n> --layer=l2` AND is auto-invoked by the existing scheduler tick (`internal/orchestrator/scheduler/scheduler.go`) when a `work_item` transitions to `merged`. The post-merge auto-invocation gates `work_item.state` from `merged → closed` on probe pass.

3.9 Skip predicate (cheap, no fancy classifier): the `regatta validate-pr` runner reads the PR's release-notes block prefix; if it matches `[DOCS]|[CHORE]|[CI]|[NONE]|[CHANGE]`, both layers exit 0 fast with `skipped` verdict + reason `release_notes_category_exempt`. Same auto-skip semantics as `scripts/check-scorecard.sh`.

## §4 Scope (out)

4.1 No Phase-X tenant isolation per validation run. Single-tenant deployments use the `substrate.DefaultTenantID` constant; multi-tenant isolation lands when W8 OPA `RBAC` closes (cross-ref `docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md`).

4.2 No production-canary against external customer traffic. There are no external customers in self-host phase; reopen-trigger fires only on first LOI per `docs/engineer/briefs/2026-06-01-self-host-first.md`.

4.3 No auto-rollback on L2-fail post-merge. Operator-driven only — L2-fail emits a substrate event + files a tracking issue tagged `validation-failed` + posts an L2-failed comment on the closed PR. Revert PR authorship is operator work; the orchestrator does NOT mint revert PRs in v1 (defer until ≥30 days of green L2 history per `feedback_default_simpler`).

4.4 No GPU-class compute, no trained-model outcome classifier. The diff predicate is the existing substrate equality reducer (deterministic, byte-comparable). Drift = byte-diff in tail substrate events OR exit-code mismatch OR captured-stdout diff. ML-trained outcome classification is rejected as over-engineering — three similar lines beat a premature abstraction.

4.5 No cross-PR fixture amortization. Each `regatta validate-pr <n>` invocation builds two binaries fresh. Caching the `origin/main` binary across invocations is a v2 follow-up (track as `validation-followup`).

4.6 No replay-diff implementation here. L3 lives in W9 spec; this spec only names L3 as the long-term destination for L2 probes (probe block becomes a W9 `replay --from=<node>` invocation in v2 once W9's `ReplayOpts.FromNodeID` ships).

4.7 No new dep. Sandbox is `os.MkdirTemp` + `os/exec` (stdlib); diff is the existing substrate predicate; probe runner reuses `go test -run` invocation pattern already used by `scripts/check-tdd-test.sh`.

## §5 Architecture

### 5.1 Three layers, sequenced

```
PR-open
  │
  ▼
[ L1 — pre-merge canary diff ]
  │   regatta validate-pr <n> --layer=l1 (on-demand, operator-invoked)
  │   build(HEAD)  build(main)
  │     │             │
  │     ▼             ▼
  │   sandbox-a    sandbox-b
  │     │             │
  │     └──► fixtures ◄──┘
  │             │
  │           diff
  │             │
  │       verdict + KindLiveValidation event
  │
  ▼
[ merge ]
  │
  ▼
[ L2 — post-merge probe ]
  │   scheduler tick (work_item.state == merged)
  │     │
  │     ▼
  │   parse <!--regatta:probe ...--> from issue/PR body
  │     │
  │     ▼
  │   run probe against merged binary
  │     │
  │       verdict + KindLiveValidation event
  │     │
  │     ├─ pass  → work_item.state → closed
  │     └─ fail  → emit alarm + file tracking issue (no auto-rollback)
  │
  ▼
[ L3 — counterfactual replay (W9 cross-ref) ]
    regatta replay <run_id>  ← out of scope for this spec
```

### 5.2 Why three layers, why this order

L1 is cheapest and runs before merge; catches behavior drift the unit tests missed.
L2 is the only layer that proves the *specific claim* (because it cites the issue's symptom — see §6.2). L1 cannot prove the claim — it can only prove non-regression on the curated fixture set.
L3 is the most expensive (replays a full DAG); reserve for cross-version regression analysis.

The order matches the cost ladder: build-two-binaries-and-diff (L1, ~minutes) < run-one-probe (L2, ~seconds) < replay-a-run (L3, ~tens of minutes).

L2 is ordered after L1 in the diagram but is **temporally** cheaper. Cheaper post-merge work is correct: pre-merge L1 is the gate; L2 confirms post-merge.

### 5.3 Probe-block schema (L2)

```
<!--regatta:probe
kind: test
cite: #841
target: TestGitHubIssues_Skip
timeout_seconds: 30
-->
```

Two more concrete forms:

```
<!--regatta:probe
kind: exit_code
cite: #917
target: doctor
expect_exit: 0
timeout_seconds: 10
-->
```

```
<!--regatta:probe
kind: script
cite: #864
target: scripts/smoke/loop-closure.sh
timeout_seconds: 120
-->
```

Block missing OR `cite:` missing OR `cite:` does not match the PR's `closes #NNN` line → parser emits `skipped` verdict + reason `probe_block_missing_or_uncited`; the merge proceeds (L2 is best-effort in v1) but `regatta.validation.cost_gated_total{verdict="skipped"}` ticks and the post-merge tracking issue is filed with label `validation-probe-missing`.

## §6 Adversarial pass per layer

### 6.1 L1 — pre-merge canary diff

| Adversary move | Mitigation |
|---|---|
| Fixture-suite curation drift — fixture corpus stops matching real-world inputs | Quarterly fixture-rotation audit; `validation.fixtures.last_rotated` field in `regatta.yaml`; `make check` warns when ≥90 days stale. |
| Sandbox escape — PR touches `internal/supervisor/` or `internal/sandbox/` and the L1 sandbox itself is compromised | L1 sandbox runs each binary inside the existing `internal/sandbox/` jail; PRs touching `internal/sandbox/**` auto-mark `--require-l1=manual` (operator must invoke + sign off on report). |
| Determinism trap — fixture uses wall clock or `rand` ⇒ false drift | Sandbox seeds `regatta.clock.fixed=<rfc3339>` env + `regatta.rand.seed=<int>` env; L1 fails closed if a fixture binary emits a `KindNonDeterministic` event. |
| Cost explosion — bad-actor fixture submits a 100MB input | Per-fixture byte cap (256KiB stdin, 4MiB testdata-file); `validation.budget_usd_default` is an upper-bound; cost-gate skip emits `cost_gated` not `pass`. |
| False-pass via fixture removal — PR deletes a fixture instead of fixing the bug | `scripts/check-validation-fixtures.sh` enforces `testdata/validation/fixtures/*.yaml` is append-only on PRs labeled `[FEAT]`/`[FIX]` (file deletions require `[CHORE] validation: rotate fixture #NNN` release-notes prefix). |

### 6.2 L2 — post-merge probe

| Adversary move | Mitigation |
|---|---|
| Probe spoofing — operator drops `kind: exit_code, target: doctor, expect_exit: 0` for an unrelated PR | `cite: #NNN` is REQUIRED; parser enforces `cite` matches one of the PR's `closes #NNN` lines (else `skipped`). Reviewer L4 lens explicitly checks probe-claim ↔ issue-symptom alignment. |
| Probe-block syntax injection — issue body contains `<!--regatta:probe ...; rm -rf /-->` style payload | Parser is YAML-only (no shell); `kind` is an allowlist of three values; `target` for `kind: script` MUST be a path inside the repo (no `..`, no absolute paths); `kind: exit_code` `target` MUST be a known `regatta` subcommand (allowlist). |
| Probe always-passes — `kind: script, target: scripts/true.sh` | Reviewer-verdict gate adds a lens: probe-target file MUST have been touched in the PR diff OR MUST live under `testdata/validation/probes/`. Drift trigger: reviewer rejects PR if probe is unrelated to the change. |
| Timeout-side-channel — probe sleeps 299s to mask a slow regression | `timeout_seconds ≤ 300` enforced at parse; OTel histogram `regatta.validation.duration_seconds` exposes outlier alerts; SLO YAML `slo.validation.l2.p99_seconds < 60` ships in §8 T3. |
| Probe runs paid LLM — operator-authored test calls Anthropic API | `regatta.l4.disabled=true` env set for the probe process; `internal/llm/` clients fail closed on that env; OTel counter `regatta.llm.calls_total{ctx="validation_probe"}` MUST be zero (alarm if non-zero). |

### 6.3 Cross-layer — cost explosion through bad-actor fixture submissions

| Adversary move | Mitigation |
|---|---|
| Adversarial fixture inflates `validation.budget_usd_default` over time | `regatta.yaml` `validation.budget_usd_default` is on the autotuner-forbidden allowlist (§4 of `2026-06-07-autotuner-closed-loop.md`); only the operator can raise it via signed commit. |
| Adversarial PR adds 500 fixtures | Per-PR fixture-add cap (`scripts/check-validation-fixtures.sh`): max 5 new fixture files per PR; exceeding → fail check with `validation: fixture-add cap (5) exceeded`. |

## §7 Test plan (TDD required)

The implementer authors these tests FIRST. Each acceptance test below is the failing test for the corresponding slice (§9):

7.1 `TestValidatePR_DetectsBehaviorDrift` (Slice 2): given two stub binaries that diverge on the same fixture input, `validate-pr --layer=l1` exits non-zero AND the report `Outcome` for that fixture is `drift_detected` AND a `KindLiveValidation` substrate event with `verdict=fail, layer=l1` is emitted.

7.2 `TestValidatePR_SkipsDocsOnly` (Slice 1): given a PR with release-notes prefix `[DOCS]`, `validate-pr --layer=both` exits 0 in < 2 seconds AND the report `Outcome` is `skipped` with reason `release_notes_category_exempt`.

7.3 `TestProbeRun_FailsOnUnsatisfiedClaim` (Slice 3): given a probe block `kind: script, target: scripts/testdata/probe-fails.sh` (a script that exits 1), `validate-pr --layer=l2` exits non-zero AND emits `verdict=fail, layer=l2`.

7.4 `TestProbeParser_BlockMissingDefaultsToL1Only` (Slice 3): given an issue body with no `<!--regatta:probe ...-->` block, `validate-pr --layer=both` runs L1 normally AND emits L2 `verdict=skipped, reason=probe_block_missing_or_uncited`.

7.5 `TestValidatePR_CostGate` (Slice 4): given `--budget-usd=0.00`, `validate-pr --layer=l1` against any non-trivial fixture exits 0 with `Outcome=cost_gated` AND increments `regatta.validation.cost_gated_total`.

7.6 `TestProbeParser_RejectsShellInjection` (Slice 3): given an issue body containing `<!--regatta:probe\nkind: script\ntarget: ../../etc/passwd\n-->`, parser returns `ErrProbeTargetEscape` AND L2 verdict is `skipped` with reason `probe_target_escape`.

7.7 `TestProbeRun_FailsClosedOnPaidLLMCall` (Slice 3): given a probe `kind: test` whose test code makes an HTTP request to any `*.anthropic.com` host, the runner intercepts via `regatta.l4.disabled=true` env propagation AND the test exits non-zero AND `regatta.llm.calls_total{ctx="validation_probe"}` stays 0.

7.8 `TestProbeParser_AlwaysPassesProbeRejected` (Slice 3, closes #989): given a probe `kind: script, target: scripts/true.sh` (or any target whose path is NOT touched in the PR diff AND NOT under `testdata/validation/probes/`), parser returns `ErrProbeTargetUnrelated` AND L2 verdict is `skipped` with reason `probe_target_unrelated_to_pr_diff`. The reviewer-lens mechanical backstop from §6.2 row 2 moves from Slice 4 into Slice 3.

7.9 `TestProbeParser_SpoofingCiteMismatchRejected` (Slice 3, closes #989): given a probe `cite: #999` while the PR body's `closes #` lines reference only `#42` and `#100`, parser returns `ErrProbeCiteMismatch` AND L2 verdict is `skipped` with reason `probe_cite_not_in_closes_list`. The `(?im)^(?:closes|fixes|resolves)[[:space:]]+#([0-9]+)\b` regex is the matching contract pinned in §3.4.

7.10 `TestProbeRun_TimeoutSideChannelMitigated` (Slice 3, closes #989): given a probe with `timeout_seconds: 300` and a test that intentionally sleeps 290s, the runner enforces the timeout via `context.WithTimeout` AND emits `verdict=fail, reason=timeout` rather than allowing an attacker to wedge the queue. `kind: script` follows the same contract; the runner MUST NOT honor `timeout_seconds > 300` (parser rejects).

Tests 7.1, 7.3, 7.4, 7.6, 7.8, 7.9, 7.10 are the load-bearing adversarial pins. Each MUST land as a failing-test commit FIRST per TDD.

## §8 Cross-cuts

8.1 New SLO `slo.validation.l2.p99_seconds < 60` ships in slice 4 alongside the cost-gate; YAML lives at `slo/validation.yaml`. Alarm-webhook routes SLO breach to a `[self-improvement]` issue same as existing SLOs.

8.2 Autotuner cross-ref: `KindLiveValidation` events with `verdict=fail` are a new W4 detector R-rule (`R8 l2_probe_fail` to be specced in a follow-up; out of scope here). Autotuner consumes nothing in v1.

8.3 `regatta doctor` (#917) cross-ref: doctor remains preflight (before-spawn); `validate-pr` is the after-merge dual. No code shared; they answer different questions.

8.4 Reviewer-verdict gate (#909) cross-ref: the reviewer template gains a lens (`docs/engineer/dispatch-templates/reviewer.md` lens 10): "probe-block presence + cite alignment" — failing the lens drops verdict to `REVISE`. Lens addition is a follow-up PR, not part of slice 1–4.

## §9 Implementer brief — 4 slices, each a separate PR

Each slice is file-disjoint with the others by package boundary; parallel dispatch is safe AFTER slice 1 lands (slices 2–4 import the `internal/validation/` types from slice 1).

### Slice 1 — `regatta validate-pr` subcommand skeleton

- Package surfaces: new `cmd/regatta/validate_pr.go`, new `internal/validation/{validation.go,outcome.go}` (just types + Runner interface; no implementations).
- Behavior: parse `--layer` / `--budget-usd` / `--report` flags; emit `skipped` for `[DOCS]|[CHORE]|[CI]|[NONE]|[CHANGE]` release-notes prefix; emit `not_implemented` for everything else (exit 0 with reason).
- Acceptance: `TestValidatePR_SkipsDocsOnly` (§7.2) green.
- Phase: current (no Phase-X tokens).
- Reviewer-verdict required: YES (cmd/ + new internal package).

### Slice 2 — Ephemeral sandbox + L1 diff engine

- Package surfaces: new `internal/validation/canary/{sandbox.go,diff.go,fixtures.go}`, new `testdata/validation/fixtures/` directory with 3 seed fixtures.
- Behavior: build both binaries, drive identical inputs, diff substrate-event tail + stdout; emit `KindLiveValidation` event.
- Acceptance: `TestValidatePR_DetectsBehaviorDrift` (§7.1) green.
- Phase: current.
- Reviewer-verdict required: YES (sandbox = security-adjacent).

### Slice 3 — L2 probe parser + outcome-verify runner

- Package surfaces: new `internal/validation/probe/{parser.go,runner.go,verdict.go}`, new `scripts/check-validation-probe.sh` (lint-stage check that probe-block cite matches PR `closes` lines), wire scheduler tick to invoke L2 on `work_item.state == merged`.
- Behavior: parse probe block; dispatch by `kind`; emit verdict event; reject shell injection / path escape / paid-LLM calls.
- Acceptance: `TestProbeRun_FailsOnUnsatisfiedClaim` (§7.3), `TestProbeParser_BlockMissingDefaultsToL1Only` (§7.4), `TestProbeParser_RejectsShellInjection` (§7.6), `TestProbeRun_FailsClosedOnPaidLLMCall` (§7.7), `TestProbeParser_AlwaysPassesProbeRejected` (§7.8), `TestProbeParser_SpoofingCiteMismatchRejected` (§7.9), `TestProbeRun_TimeoutSideChannelMitigated` (§7.10).
- Phase: current.
- Reviewer-verdict required: YES (parser is attack surface).

### Slice 4 — Cost-gate integration

- Package surfaces: edit `internal/validation/canary/sandbox.go` to call `cap.PredictUSD`; new `slo/validation.yaml`; new OTel meter registrations in `internal/validation/metrics.go`.
- Behavior: skip with `cost_gated` when prediction > budget; emit cost-gated metric.
- Acceptance: `TestValidatePR_CostGate` (§7.5) green; SLO YAML compiles via `make slo-compile-test`.
- Phase: current.
- Reviewer-verdict required: NO (file-edit + YAML; under the proportional-skip predicate per `feedback_review_proportional`).

## §10 Decision priority recap

UX (operator can answer "did it actually fix it" in one CLI verb) > ease (one new package, three sub-packages, no new dep) > performance (sandbox build is a 30s cost ceiling, not a hot path) > best-practices (TDD-required acceptance tests per slice) > speed > velocity. Long-term > short-term: the L2 probe block is a forward-fit seam for W9 replay (the `cite:` field becomes the W9 `--from=<node>` argument when W9 ships).

## §11 Closes / tracks

- Tracks (not closes — this spec is design only): #864 (manual smoke companion), #909 (reviewer-verdict gate; static), #917 (`regatta doctor` preflight; static).
- Cross-ref forward-fit: W9 replay-diff harness (`2026-06-01-w9-replay-diff-harness-design.md`) — L3 destination.
- Cross-ref consumer: autotuner closed-loop (`2026-06-07-autotuner-closed-loop.md`) — future R8 rule consumes `KindLiveValidation{verdict=fail}` findings.
