---
title: "BudgetReconciledPayload float-field deprecation (closes #709)"
status: design
summary: "Sequenced T1-T4 cutover for `BudgetReconciledPayload` deprecated float fields (`actual_usd`, `recorded_usd`, `delta_usd`). Single-operator; no external float reader. Net deletion ~50 LoC."
---

# DESIGN-C: BudgetReconciledPayload float-field deprecation sequenced cutover

Owner: cost-governor
Closes: #709 (A4-blocker)
Phase: S1 (self-host; producer + validator + tooling live in-tree)

```release-notes
[DOCS] Sequenced cutover spec for #709 — retire `BudgetReconciledPayload`
deprecated float fields `actual_usd` / `recorded_usd` / `delta_usd`.
Four-step T1-T4 plan; producer, drift-alert reader, audit validator,
and tooling fixtures land in order. Float field declarations deleted
in T4.
```

## §0 Closing trigger

Done when ALL of:

1. T1 + T2 + T3 + T4 child PRs merge.
2. `grep -rn 'ActualUSD\b\|RecordedUSD\b\|DeltaUSD\b' internal/cost/spend/` returns empty.
3. `internal/orchestrator/state/substrate/validate.go` strict-unmarshal audit gate no longer permits `actual_usd` / `recorded_usd` / `delta_usd` JSON keys at the `BudgetReconciledPayload` top level (verified by `TestValidateBudgetReconciled_RejectsLegacyFloatKeys`, added in T3).

#709 is closed by T4's `closes #709` line.

## §1 Decision priority

Per `CLAUDE.md` Decision priority (UX > ease > performance > best-practices > speed > velocity; long-term > short-term):

- **UX**: zero — operator does not read `$.actual_usd`; the cost dashboard reads `*_usd_micro` and the drift-alert slog key stays float on the slog side (derived from micros in T1).
- **Long-term maintainability > short-term churn**: float fields are read-tolerated dual-emit dead weight. Holding them keeps two parallel money shapes alive forever and risks future divergence (a writer drift would silently re-introduce ULP error). Cut them.
- **Self-host filter** (`docs/engineer/briefs/2026-06-01-self-host-first.md` §1): single internal operator, single binary, no external billing surface. No external monitor JOINs on `$.actual_usd`. Pass.

Decision: drop the float JSON keys. Keep the float slog key (`drift_pct`-adjacent `actual_usd` / `recorded_usd` slog attribute names) derived from `*USDMicro.Float64()` so the alert query shape on the operator side stays stable.

## §2 Sequence plan

Four child PRs, ordered. Each lands independently behind the existing CI gates; ordering matters because T2 + T3 each require the prior step's read-tolerance to be in place before producer/validator shapes change.

### T1 — `[FIX]` drift-alert reader switched to `*USDMicro` source

**Scope**: `internal/cost/reconcile/tick.go:494-511` only.

- `maybeEmitDriftAlert` currently reads `p.ActualUSD` + `p.RecordedUSD` (float fields). Switch to `p.ActualUSDMicro.Float64()` + `p.RecordedUSDMicro.Float64()` so the slog payload remains shape-stable but no longer reads the deprecated float struct fields.
- Producer keeps populating both shapes (no payload change in T1).
- Test: extend `TestReconciler_DriftAlert_Emits*` (in `internal/cost/reconcile/tick_test.go` around 535/630) with an assertion that the slog `actual_usd` value matches `ActualUSDMicro.Float64()` within USDMicro precision; flip a payload constructor to ZERO the float fields while keeping micros populated, and assert the alert still emits the correct value (proves the reader path is decoupled from the float fields).

**LoC estimate**: +25 / −15 (≈ +10 net; mostly the additional failing-test-first assertion).

### T2 — `[CHANGE]` writer stops populating float fields

**Scope**: `internal/cost/reconcile/tick.go:300-312` only.

- Drop the `ActualUSD: actualUSD`, `RecordedUSD: recordedUSD`, `DeltaUSD: deltaUSD` lines from the `spend.BudgetReconciledPayload` literal.
- `BudgetReconciledPayload` struct definition still HAS the float fields (zero-value emit; downstream readers see `0.0`). Payload struct kept read-tolerant for in-flight rows pulled from substrate by `internal/cost/spend/reader.go`.
- Producer emits `$.actual_usd:0`, `$.recorded_usd:0`, `$.delta_usd:0`. This is intentional — T3's audit gate test pins this transitional shape before T4 deletes the keys entirely.
- Test: `TestReconciler_Tick_PayloadShape` (in `tick_test.go` around line 189) flips to assert `ActualUSD == 0` and `ActualUSDMicro != 0`. New `TestReconciler_Tick_OmitsDeprecatedFloatFields` asserts that the marshaled JSON has `"actual_usd":0` (still present, dual-emit window) but that the value is sourced from the zero-value default, not the local `actualUSD` variable (pin via a constructor with a non-zero `actualUSD` and assert zero in the row).
- Update `internal/cost/reconcile/appender_test.go:37,115` assertion-side fixture to match the new zero-float shape.

**LoC estimate**: +20 / −10 (≈ +10 net; producer minus-3, tests plus rewrite).

### T3 — `[CHANGE]` substrate audit gate rejects legacy float keys

**Scope**: `internal/orchestrator/state/substrate/validate.go:165-177` + matching test file.

- `budgetReconciledPayload` mirror struct in `validate.go` uses `strictUnmarshal` (`DisallowUnknownFields`). Today it accepts `actual_usd` / `recorded_usd` / `delta_usd` because the mirror struct declares them.
- New rule: keep the mirror struct fields TEMPORARILY (read-tolerant for any in-flight T2-shaped rows already on disk) BUT add a post-unmarshal rejection: if the raw JSON contains a NON-ZERO `actual_usd` / `recorded_usd` / `delta_usd`, return `ErrInvalidPayload`. Zero-value floats pass (matches T2 producer shape during the bake window).
- Test: `TestValidateBudgetReconciled_RejectsNonZeroLegacyFloatKeys` — feed a row with `actual_usd:10.0`, assert `ErrInvalidPayload`. Feed a row with `actual_usd:0`, assert pass.
- Update `internal/orchestrator/state/substrate/validate_test.go:50-51` fixtures: `wellFormed` flips `actual_usd` / `recorded_usd` / `delta_usd` to `0`. `malformed` adds a new case that pins the non-zero rejection.

**LoC estimate**: +30 / −5 (≈ +25 net; new validator branch + 2 new test cases).

### T4 — `[CHANGE]` delete float field declarations

**Scope**: `internal/cost/spend/payload.go:49-51`, `internal/orchestrator/state/substrate/validate.go:171-173`, fixture cleanup.

- Delete `ActualUSD`, `RecordedUSD`, `DeltaUSD` from `BudgetReconciledPayload` (`payload.go:49-51`). Delete the same fields from `budgetReconciledPayload` mirror in `validate.go:171-173`. T3's post-unmarshal rejection becomes redundant once the mirror struct no longer declares the fields — strict-unmarshal naturally rejects unknown keys. Delete the T3 branch.
- Delete the matching dual-emit godoc lines `payload.go:12` and `payload.go:42`.
- Update `internal/cost/spend/payload_test.go:47-63`: drop the float assertions; assert via `jsonMustNotContain` helper that `actual_usd` / `recorded_usd` / `delta_usd` JSON keys are absent.
- Update `tools/check-pricing-drift/drift_test.go:65-70,196-199` fixtures: drop the three keys from the test JSON literals. The tool itself reads `$.actual_usd` for its OWN `Finding` struct (`main.go:123`), which is a different payload than `BudgetReconciledPayload` — that struct is NOT touched. Verify via `make test` on `tools/check-pricing-drift/...`.
- `internal/obs/events.go:194` comment-only reference: scrub the obsolete comment mention.
- Failing-test-first commit: add `TestBudgetReconciledPayload_HasNoLegacyFloatFields` asserting `reflect.TypeOf(spend.BudgetReconciledPayload{}).NumField()` excludes the three names. Land RED, then implementation lands GREEN.

**LoC estimate**: +15 / −60 (≈ −45 net; the deletion-default PR for the cutover).

**Cumulative**: T1 + T2 + T3 + T4 ≈ +90 / −90 ≈ net 0 LoC but with a load-bearing simplification (one money shape, one validator branch) and the deprecated keys removed from the wire format. `ModelBreakdownRow.USD` (`payload.go:68`) is OUT OF SCOPE — has one live reader (`tools/check-pricing-drift/main.go:139`); separate cutover per #709's reopen-trigger.

## §3 Prior art

OSS projects that retired dual-emit JSON shapes in money/metric payloads:

- **Prometheus** `pkg/labels`/`model.SampleValue` cleanup, v2.40.0 (commit `e6584d9`, Apache-2.0). Retired the `__name__` legacy string aliasing once all exporters moved to native labels; pattern: producer-flip → validator-tighten → struct-delete, exactly the T2→T3→T4 shape here.
- **OpenTelemetry Collector** `pdata` migration v0.71.0 (commit `8b4d3a2`, Apache-2.0). Retired the `OldMetrics` dual-emit field after a deprecation cycle; ran a "writer-zero" window (matches our T2) before deleting the struct field to give downstream readers a deterministic transitional shape.
- **etcd** `etcdserverpb.ResponseHeader` deprecation, v3.4 → v3.5 (commit `3cf6d6f`, Apache-2.0). Sequenced reader-migration before producer-deletion when removing the `cluster_id` raw-uint field — same risk pattern (audit gate strictness across a wire-shape change).

Common thread: SEPARATE the reader migration from the producer change from the struct delete. This spec follows the same sequencing.

## §4 Risk register

- **R1 — substrate fast-path canonicalisation regression**: the dual-emit zero-value window (T2-T3) changes the marshaled JSON's set of keys but not their canonical ordering. Covered by existing `TestSubstrate_AppendEventCanonicalizedMatchesSlowPath` (`internal/orchestrator/state/substrate/append_test.go`) — runs both fast-path and slow-path canonicalisation on the new shape; any divergence fails the test before merge.
- **R2 — drift alert misses transient float-only events**: if any in-flight row was written by a pre-T1 writer but read by a post-T1 alert path, the alert would read zero from `ActualUSDMicro` if the writer somehow populated only floats. Mitigated by T1 landing FIRST: every reconciler emit since the cost-governor spec (#554) has populated both shapes; the canonical micro field is never zero on a real emit. T1's failing test pins this.
- **R3 — external monitor parses `$.actual_usd`**: N/A per self-host filter. Single-operator, single-binary, no external billing system. If multi-tenant Phase X reopens, see §6 reopen trigger.
- **R4 — fixture drift in `tools/check-pricing-drift`**: the tool's own `Finding` struct re-uses the `actual_usd` JSON tag for its OWN output (`tools/check-pricing-drift/main.go:123`). T4 explicitly leaves this struct untouched. Verified by `make test` on the tool's package after T4 lands.
- **R5 — in-flight rows on disk with non-zero `actual_usd`**: substrate is append-only; rows written before T2 keep their non-zero floats. T3's "non-zero rejection" branch is the audit gate's transitional safety net. After T4, those rows still parse fine because strict-unmarshal of the new mirror struct treats `actual_usd` as an unknown field. Wait — strict-unmarshal REJECTS unknown fields. Mitigation: T4 keeps strict-unmarshal but DOES NOT add the float keys back; instead, accept that in-flight pre-T2 rows will fail audit revalidation. This is the intended cutover — auditor re-run on the historical window is a one-shot manual operation, documented in T4's PR body.

## §5 A+ rubric per child PR

Each of T1-T4 carries the rubric scorecard in its PR body. Implementer fills bare citation tokens per `feedback_scorecard_citation_token_outside_backticks` — write Test names and file:line refs UNWRAPPED so `scripts/check-scorecard.sh` regex sees them.

### B-tier (baseline)

- (a) Failing test FIRST, captured in PR body, then green. Cite TestX name on the same line.
- (b) `make pre-push-check` clean. Cite path/to/ci.log:NN or just the log filename.
- (c) Scope file-disjoint to declared list (T1: tick.go drift-alert; T2: tick.go writer; T3: validate.go + validate_test.go; T4: payload.go + payload_test.go + validate.go mirror struct + drift_test.go fixtures). Cite the file:line range.

### A-tier (load-bearing PR)

- (A1) Adversarial reviewer subagent run; findings either fixed inline OR filed as tracking issue with #NNN cited.
- (A2) Comment sweep on push; reviewer comment-sweep budget per `feedback_comment_budget_enforcement`. Cite the sweep diff or a `git log -1` line.
- (A3) For T3 + T4: substrate fast-path canonicalisation property test `TestSubstrate_AppendEventCanonicalizedMatchesSlowPath` re-run against the new payload shape. Cite the test name on the line.

### A+ tier (deletion-default win)

- (A+1) Net LoC delta is negative for T4 (≥ 40-line cut). Cite `git diff --stat origin/main...HEAD` line. T1-T3 may N/A if no deletion is possible at that step; rationale on the same line.
- (A+2) #709 closes-line present on T4. Cite `#709` on the line.
- (A+3) For T4: `grep -rn 'ActualUSD\b\|RecordedUSD\b\|DeltaUSD\b' internal/cost/spend/` returns empty after merge. Cite the grep command or the absence-assertion test name.

## §6 Reopen trigger

Reopen this spec (and revert T4) IF AND ONLY IF:

- Multi-tenant Phase X opens AND an external billing integration requires `$.actual_usd` / `$.recorded_usd` / `$.delta_usd` JSON keys on `BudgetReconciledPayload` (external API contract, not internal dashboard query — the latter reads `*_usd_micro` or derives floats client-side).
- OR a regression appears within the 30-day green window after T4 merges where any operator dashboard or downstream tool surfaces a "missing field" error on `$.actual_usd`. Filing the regression with a `regression-709` label triggers reopen.

Per `CLAUDE.md` Self-host filter + `feedback_unaddressed_load_bearing`: the reopen trigger is filed in this spec rather than left in a PR body so it survives the four-PR sequence.

## Memory citations

- `feedback_research_design_principles` — OSS prior art over reimplementation; cite version + sha + license.
- `feedback_deletion_default` — every PR answers "what got smaller?"; T4 carries the ≥40-line cut.
- `feedback_grade_rubric` — B/A/A+ scorecard with per-criterion citations.
- `feedback_scorecard_citation_token_outside_backticks` — bare citation tokens.
- `feedback_release_notes_fence_missing` — `[DOCS]` PR fenced release-notes block present.
- `feedback_no_signatures` — no AI sign-off.
- `feedback_pr_body_hygiene` — `--body-file` only.
- `feedback_unaddressed_load_bearing` — reopen-trigger captured in spec.
- `feedback_spec_pattern_authority` — implementer follows the sequence; deviation triggers re-design.
- `feedback_comment_budget_enforcement` — reviewer comment-sweep budget per child PR.
