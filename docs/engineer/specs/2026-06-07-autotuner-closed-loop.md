---
title: "Autotuner closed-loop write-back — Design Spec"
status: active
summary: "Closed-loop consumer for W4 self-improvement findings. Mints reduced-powers PRs against a fixed yaml-path allowlist (cost caps, append-only dispatch-template + banned-phrase additions). Phase B operator-merge default; zero new write surface, zero new HMAC key. Reuses existing CUE gate (`internal/config/validate/load.go`) + existing substrate signing chain. Rule-based rate-limiter with asymmetric raise/lower caps (HPA-shape); no PID, no bandit. Forbidden inputs: R3 / R5 / Phase-X tuning knobs / `kind: research` items."
---

# Autotuner closed-loop write-back — Design Spec

Status: ready for review
Date: 2026-06-07
Author: design subagent
Source brief: `docs/engineer/briefs/2026-06-04-autotuner-closed-loop-design.md`
Companion brief: `docs/engineer/briefs/2026-06-04-roadmap-reorder-self-improve-priority.md` §5 (MVR-1.5-C)
Depends on: PHASE-AUTONOMY-W4 detector (`internal/selfimprove/detector.go`), substrate event signing chain (`internal/orchestrator/state/substrate/event.go`), CUE config validator (`internal/config/validate/load.go`).
Tracks: #832, #852

Memory rules in force: `feedback_decision_priority`, `feedback_default_simpler`, `feedback_root_cause`, `feedback_deletion_default`, `feedback_research_design_principles`, `feedback_adversarial_review`, `feedback_spec_pattern_authority`, `feedback_unaddressed_load_bearing`, `feedback_no_signatures`, `feedback_dispatch_brief_only`, `feedback_grade_rubric`.

---

## §1 Problem

The W4 self-improvement detector (`internal/selfimprove/detector.go:34-104`) emits `Finding` rows. Nothing consumes them — the operator hand-triages each `[self-improvement]` GitHub issue, edits the relevant file, and opens the PR. The closed loop does not close.

Two concrete leaks this spec closes:

- #832 (W4.5 cost-outlier R7): cost-outlier fires repeatedly without anyone adjusting the cap.
- #852 (L4 cache hit-rate as autotune signal): `regatta.l4.cache.hits` / `regatta.l4.cache.misses` already meter (`internal/gates/l4/metrics.go:40-41`) but no consumer adjusts cache TTL or warm-set policy.

Today's flow: Finding → GH issue → operator → hand-edit → PR. Latency: hours to days. Target: minutes to hours, operator-merge backstop intact.

The single load-bearing constraint: **the autotuner has zero direct-write authority**. It mints PRs against a fixed yaml-path allowlist; every PR clears `make ci-check` + L4 reviewer + GitHub branch-protection merge — the same gates as any other PR.

## §2 Goal

Promote the brief's closed-loop design to an impl-ready spec. Deliverable: one PR-author consumer that converts W4 findings into reduced-powers PRs against `regatta.yaml` + two append-only markdown surfaces. Phase B operator-merge is the v1 default; Phase C unattended-live is forbidden until ≥90 days green Phase B history (§7).

Out-of-band scope contract: this spec promotes ONLY the brief's §1-§9 surface. The brief's §10 (adoption-first audit) and §12 (adversarial-review findings) are merged inline where they bind implementation; §11 open questions are carried forward as `decision_required` markers.

## §3 Scope (in)

3.1 New package `internal/autotuner/` — consumer for `selfimprove.Finding` rows, candidate renderer, CUE-gate caller, branch + PR minter via existing `gh` shell-out path used by L4 (`internal/gates/l4/adapter.go` pattern).

3.2 New substrate event kind `KindAutotuneAction` added to the enum at `internal/orchestrator/state/substrate/event.go:20-67` (parity with `KindGateVerdict`, `KindTokenSpend`). Payload schema in §6.

3.3 New CLI verbs under `cmd/regatta/autotune.go`:
- `regatta autotune pause [--rule <id>] [--target <yaml-path>]`
- `regatta autotune unpause [--rule <id>] [--target <yaml-path>]`
- `regatta autotune dry-run --finding <dedup-key>`
- `regatta autotune revert <action-id>`
- `regatta autotune status` (read-only substrate query)

3.4 New CI gate `scripts/check-autotune-scope.sh`, added to `make check` battery — fails closed if a PR labeled `autotune` touches files outside the compiled-in allowlist.

3.5 Two label conventions (no GitHub config change required — labels are created on first use by `gh issue edit` / `gh pr edit`):
- PR label `autotune` (auto-applied by the autotuner on PR-open).
- Issue label `autotune-disabled` (operator-applied to a W4 finding to opt that dedup-key out).

## §4 Scope (out)

4.1 No new OTel meters. The autotuner reads only signals that already ship:
- `regatta_cost_cap_24h_spend_usd` (`internal/cost/cap/cap.go:151`) — Float64Gauge.
- `regatta_cost_cap_throttled_total` (`internal/cost/cap/cap.go:148`) — Int64Counter.
- `regatta.l4.cache.hits` / `regatta.l4.cache.misses` (`internal/gates/l4/metrics.go:40-41`) — Int64Counter.
- `regatta.scheduler.tick.latency_ms` (`internal/orchestrator/scheduler/scheduler.go:281`) — Float64Histogram.
- W4 `selfimprove.Finding` rows as the discrete trigger surface.

4.2 No new HMAC key. The `signed_by` field in the `KindAutotuneAction` payload is the same orchestrator KID that signs every other substrate event today.

4.3 No PID / no bandit / no ML. §8 picks rule-based asymmetric rate-limit (HPA shape) per `feedback_default_simpler`. Full PID is rejected as overkill for a once-daily nightly scan.

4.4 No control over Phase-X knobs. Out of allowlist permanently: `safety.iteration_cap`, `safety.canary_rate`, `safety.agent_creds_scope`, `safety.soft_cap_mode`, `safety.soft_cap_acknowledge_overrun`. Each widens the autonomy envelope. (Brief §1 categories #2/#4/#5/#7.)

4.5 No control over operator-authored semantic claims. Out of allowlist permanently: spec frontmatter `status:`, `.regatta/items/*.md` archive moves, any `prereg.*` field in any item, `policies/research/*.rego`.

4.6 No new dep. Renovate-shape PR-author pattern (brief §10) is adopted in spirit — no Renovate code or binary. The autotuner is a Go package that shells out to `gh` exactly like the existing L4 adapter.

4.7 No Phase C in v1. Phase C unattended-live is `decision_required` per §11 #1; implementation deferred until reopen-trigger fires.

## §5 Closed-loop architecture

```
                +-------------------------+
                |  internal/selfimprove   |
                |  detector.go (W4)       |
                |  -> Finding{rule_id,    |
                |     dedup_key, target}  |
                +-------------+-----------+
                              |
                              v  (findings channel)
                +-------------+-----------+
                |  internal/autotuner     |
                |  consumer.go            |
                |                         |
                |  1. denylist filter     |
                |  2. cooldown check      |   reads:
                |  3. signal sample       |---> regatta_cost_cap_24h_spend_usd
                |  4. candidate render    |---> regatta.l4.cache.{hits,misses}
                |  5. CUE-gate            |---> regatta.scheduler.tick.latency_ms
                |  6. scope-check         |
                |  7. damping-cap check   |
                |  8. PR mint (gh)        |
                +-------+-----------+-----+
                        |           |
                        v           v
            +-----------+--+   +----+------------+
            |  GitHub PR   |   | substrate event |
            |  label:      |   | KindAutotuneAction
            |  autotune    |   | (audit + dedup) |
            +------+-------+   +-----------------+
                   |
                   v
            +------+--------+
            |  make ci-check|  <-- includes check-autotune-scope.sh
            |  L4 reviewer  |
            |  operator     |  <-- second key; branch-protection merge
            |  merges       |
            +---------------+
```

Loop closure: operator merge → next W4 scan no longer fires the dedup_key → no candidate → closed loop. Failure mode (oscillation) handled by §8 damping.

## §6 Knobs — what gets written back

Each knob is a CUE-path or repo file path. Knobs outside the allowlist are rejected at step 6 (`check-autotune-scope.sh`). The allowlist is a compiled-in Go slice; growing it requires an operator-authored PR to `internal/autotuner/allowlist.go`.

| # | Knob | CUE-path / file | Direction | Source finding |
|---|---|---|---|---|
| K1 | Daily cost cap | `safety.cost.cap.daily_usd` | raise OR lower | cost-outlier finding (W4.5 R7, #832) AND optional `cap-thrash` co-fire |
| K2 | Per-day spend cap | `safety.spend_cap_usd_per_day` | raise OR lower | same as K1 |
| K3 | Global spend cap | `safety.spend_cap_usd` | raise OR lower | same as K1 |
| K4 | Dispatch template append | `docs/engineer/dispatch-templates/{implementer,reviewer,designer,triage}.md` | append-only inside fence | W4 R2 banned-phrase-recurrence; W4 R4 load-bearing-leftover-pattern |
| K5 | Banned-phrase list append | `scripts/doc-check.sh` (banned-phrase array) | append-only | W4 R2 banned-phrase-recurrence (≥5 fires / 30d on same novel token; token absent from any `.md` outside fenced backticks) |

Fence convention for K4 (append-only enforcement at scope-check):
```
<!-- autotune-appended-start -->
- <appended line>
<!-- autotune-appended-end -->
```
Operator content outside the fence is byte-stable. `scripts/check-autotune-scope.sh` rejects any diff hunk for K4 files that touches lines outside the fence.

For K5, the append shape is one literal phrase token added to the banned-phrase array; the gate checks the diff is a one-line insertion inside the array literal and the token does not appear in any tracked `.md` outside fenced backticks.

L4 cache-hit-rate signal (#852) is read-only telemetry that informs the *direction* of K1/K2/K3 adjustments — sustained high hit-rate + repeated cost-outliers ⇒ raise candidate admissible; sustained low hit-rate ⇒ cap mis-fit is upstream, autotuner refuses to raise. The cache TTL itself is **not** an autotuner knob in v1 (cache-config knobs would widen the surface to L4 internals and require a new schema delta — out of scope per `feedback_default_simpler`).

## §7 Signals — what gets consumed

Each signal is an existing OTel meter or substrate query. No new instrumentation.

| Signal | Source | Sample shape | Used for |
|---|---|---|---|
| S1 | `regatta_cost_cap_24h_spend_usd` (Float64Gauge, `internal/cost/cap/cap.go:151`) | last value over 7d | K1/K2/K3 cap fit |
| S2 | `regatta_cost_cap_throttled_total` (Int64Counter, `internal/cost/cap/cap.go:148`) | sum over 7d | K1/K2/K3 raise admissibility |
| S3 | `regatta.l4.cache.hits` / `regatta.l4.cache.misses` (Int64Counter, `internal/gates/l4/metrics.go:40-41`) | ratio over 7d | K1/K2/K3 direction; high-hit + throttled ⇒ raise; low-hit + throttled ⇒ refuse |
| S4 | `regatta.scheduler.tick.latency_ms` (Float64Histogram, `internal/orchestrator/scheduler/scheduler.go:281`) | p95 over 7d | sanity guard — if scheduler is saturated, drop candidate (root cause is not cap fit) |
| S5 | W4 `selfimprove.Finding` rows | event-discrete | trigger surface |
| S6 | Prior `KindAutotuneAction` rows (same `target`) | last 24h / 30d window | cooldown check |

Sample windows are read against the substrate / OTel push-window already configured in `regatta.yaml`; the autotuner does not configure its own collection window.

## §8 Decision algorithm — rule-based asymmetric rate-limiter

**Algorithm choice: rule-based**, modeled on Kubernetes HPA `behavior.scaleUp` / `behavior.scaleDown` (stable since v1.18, 2020). Per `feedback_default_simpler` + `feedback_research_design_principles`.

Rejected alternatives:
- **PID controller**: integral wind-up is meaningless when cooldown gates zero the integrator between samples. The rate-limiter IS the load-bearing primitive; the PID adds tunables (Kp, Ki, Kd) without behavior gain.
- **Multi-armed bandit**: needs a reward signal beyond "did the cap stop firing", which requires a counterfactual the operator cannot supply at single-operator scope. Adopt-first audit: no OSS bandit lib fits self-host single-operator constraints.
- **LLM-decided adjustments**: introduces the exact widening-the-envelope adversarial surface §2 of the brief closes (a compromised orchestrator that authors its own prompt). Out.

**Decision pseudo-code** (~15 lines, lives at `internal/autotuner/decide.go`):

```
for each Finding f in W4 scan:
    if f.rule_id in DENYLIST: skip                             # §9
    if f.target not in ALLOWLIST: skip                         # §6
    if recent_action(target=f.target, within=cooldown(f)) != nil: skip
    signals := sample(S1..S4, window=7d)
    direction := decide_direction(f, signals)                  # raise | lower | nil
    if direction == nil: skip
    delta := bounded_delta(f.target, direction, signals)       # respects raise≤10% / lower≤30%
    candidate := apply_delta(current_yaml, f.target, delta)
    if !cue_validate(candidate): emit_rejected_action(f); skip # §10
    if !scope_check(candidate.diff): emit_rejected_action(f); skip
    pr := mint_pr(candidate, label=autotune)
    emit_action(target=f.target, before, after, pr, signed_by=orchestratorKID)
```

**Damping caps** (asymmetric — fail-safe in the contraction direction, per HPA discipline):

| Axis | Raise cap | Lower cap | Cooldown |
|---|---|---|---|
| K1 `safety.cost.cap.daily_usd` | ≤10% / 24h | ≤30% / 24h | 24h between candidates |
| K2 `safety.spend_cap_usd_per_day` | ≤10% / 24h | ≤30% / 24h | 24h |
| K3 `safety.spend_cap_usd` | ≤10% / 24h | ≤30% / 24h | 24h |
| K4 dispatch-template append | ≤1 line / 30d / template | N/A (append-only) | 30d |
| K5 banned-phrase append | ≤1 token / 30d | N/A (append-only) | 30d |

Cooldown enforcement: §7 S6 — autotuner reads `KindAutotuneAction` events on the same `target` within the cooldown window (regardless of PR outcome — merged, closed, abandoned all count). Any hit ⇒ drop the candidate.

**Implicit floor / ceiling**: CUE schema enforces `>=0` on every cost-cap field (`#CostCap.daily_usd` etc.). A floor of $0 is recoverable via single `regatta autotune revert`. No ceiling is encoded beyond Phase B operator-merge backstop catching absurd values via the L4 reviewer + human eye. (Brief §12 #3 — accepted residual risk for v1.)

## §9 Safety floors — denylist

Three rule-IDs from `internal/selfimprove/rules.go:13-17` MUST be filtered from autotuner input. Each is the exact lever an adversary would pull to widen the autonomy envelope.

| Constant | Reason excluded |
|---|---|
| `RuleSubagentClaimedCleanButCIFailed` (R3, `rules.go:15`) | An adversary floods R3 fires; autotuner response would relax claim-verification or raise cost cap to absorb rework. Both adversarial wins. Operator-triage only. |
| `RuleReaperKillsSameAgent` (R5, `rules.go:17`) | Natural autotuner response is to lengthen heartbeat timeout or raise iteration cap. Both widen the autonomy envelope. Operator-only. |
| Future R11 `cap-thrash` *without* §8 damping in force | R11 is "cost cap oscillating". Consuming R11 without §8 cooldown is the oscillation amplifier. Admissible ONLY when §8 wired AND R11 has fired ≥3 times in a 14-day window (de-noise). |

W4 R1 `RuleSameGateFailRepeats` + R4 `RuleLoadBearingLeftoverPattern` are MVP-eligible inputs once R2 `RuleBannedPhraseRecurrence` (the safest first feed: append-only, narrow allowlist, replay-diffable) proves out the template path.

**Research-mode hard exclusion** (`docs/engineer/briefs/2026-06-01-regatta-research-vision.md` §38):
- Any `Finding` whose `target` resolves to `.regatta/items/*.md` where the item frontmatter contains `kind: research` ⇒ skip.
- Any `Finding` whose source event payload carries `work_item_kind = "research"` ⇒ skip.
- Any `Finding` whose `target` resolves to a `prereg.*` field in any item ⇒ skip.

Autotuner modifying a prereg post-hoc is structurally identical to p-hacking; this is an ethics-layer veto, not a soft preference. Rejected-candidate events still emitted (target = `"REJECTED:research-kind"`) so audits capture the decision.

## §10 CUE validation gate — fail-closed

Every candidate yaml MUST pass `validate.LoadConfig` (`internal/config/validate/load.go:154-181`) before the PR is minted. This is the existing fail-closed gate behind `regatta validate-config`. No new schema, no new validator.

Flow:
1. Read current `regatta.yaml` bytes.
2. Apply candidate delta in-memory.
3. Call `validate.LoadConfig(candidateBytes)` — CUE resolves defaults + concrete-constraint-validates.
4. On CUE error: drop candidate, emit `KindAutotuneAction` with `pr_number=0, after=""` (rejected-candidate audit trail).
5. On CUE pass: open PR, emit full event.

For K4/K5 (non-yaml): the equivalent gate is `make check` (doc-check, scope-check, etc.). Autotuner runs `make check` locally on the candidate before opening the PR; failure drops the candidate identically.

## §11 Reversibility + rollback path

Every action is revertible. Two layers:

**Layer 1 — PR-as-revert (Phase B default)**: `regatta autotune revert <action-id>` reads the `KindAutotuneAction` row, reconstructs the inverse diff (after → before), opens a revert PR citing the original action-id, exits 0. Operator merges normally. No new write authority — the revert is also a PR.

**Layer 2 — operator close-before-merge**: while a candidate PR sits in review, the operator may close it. Closed PRs leave the `KindAutotuneAction` event in place (audit trail intact); cooldown §8 reads it the same as merged.

`KindAutotuneAction` payload schema:

```
{
  "target": "safety.cost.cap.daily_usd",    // CUE-path or repo-relative file
  "before": "<pre-image>",                  // yaml subtree or file region
  "after":  "<post-image>",                 // empty string for rejected-candidate
  "finding_dedup_key": "<W4 Finding.DedupKey>",
  "signed_by": "<orchestrator HMAC KID>",   // existing chain; no new key
  "pr_number": 12345,                       // 0 for rejected-candidate
  "ts": 1717000000000000000                 // unix-nano
}
```

Stored append-only via the existing substrate writer. Parity with `KindGateVerdict` / `KindTokenSpend`. `TestSubstrate_EventKindEnumMatchesSQLCheck` (referenced in `internal/orchestrator/state/substrate/event.go:14-15`) extended to cover the new kind.

Dedup constraint: `(finding_dedup_key, target)` uniqueness at the autotuner-side — one open autotune PR per `(finding, target)` pair at a time. Companion brief §5.1 "≤1 autotune PR open per target".

Retention: same as all substrate events (`KindFact` retention, `event.go:23`). Audit-grade longer retention deferred to Phase C reopen.

## §12 Operator-override surface

Four mutating verbs + one read verb + two labels. All read-only or revert-only — no autotuner-extending verbs (autotuner cannot grant itself new authority).

| Verb / label | Semantics |
|---|---|
| `regatta autotune pause [--rule <id>] [--target <yaml-path>]` | Writes `KindAutotunePause` substrate event; autotuner skips matching findings until `unpause`. |
| `regatta autotune unpause [--rule <id>] [--target <yaml-path>]` | Inverse. |
| `regatta autotune dry-run --finding <dedup-key>` | Renders candidate diff to stdout WITHOUT opening a PR, WITHOUT writing any substrate event. Pure preview. |
| `regatta autotune revert <action-id>` | Opens inverse PR per §11. |
| `regatta autotune status` | Read-only: lists queued findings + cooling-down targets + last N actions per target. |
| Issue label `autotune-disabled` | Operator applies to a W4 self-improvement issue to mark "I will triage manually". Autotuner skips findings whose dedup-key matches a labeled issue. |
| PR label `autotune` | Auto-applied by autotuner. Operator filters via `gh pr list --label autotune --json number,state,mergeStateStatus,statusCheckRollup,isDraft,headRefName,labels -L 20`. |

CLI invocations follow `feedback_gh_minimal_fields` — minimal `--json` allowlist + `-L 20` documented in subcommand help.

Note: `KindAutotunePause` is a second new substrate kind added alongside `KindAutotuneAction` (parity with the existing operator-control kinds; allows pause state to ride the same audit chain).

## §13 Phase A / B / C latency tiers

| Phase | Flow | Latency | Blast radius | Default? |
|---|---|---|---|---|
| A (today) | finding → GH issue → operator triage → manual edit → PR → merge | hours–days | nil | replaced by B |
| **B (v1 default)** | finding → autotuner → candidate yaml → CUE-validate → PR opened → operator merge | minutes–hours | nil (PR gate intact) | YES |
| C (gated reopen) | finding → autotuner → live yaml write + auto-merge once reviewer PASSes → revert journal | seconds | bounded by allowlist + damping | NO — reopen-trigger §16 #1 |

**Phase B is v1 default**:
- Operator-merge is the second "key" the trust-boundary leans on; removing it removes the only out-of-band signing event.
- Phase B blast-radius is byte-equal to any other PR's. Adding any "fast path" before 90 days of green Phase B history is unjustified per `feedback_default_simpler`.

Phase C reopen-trigger (decision_required, §16 #1): ≥90 days of green Phase B autotune PRs (zero reverted, zero CI-failed merges) AND operator approves audit. Until then, Phase C is closed.

## §14 Acceptance criteria

A1. New package `internal/autotuner/` exists with `consumer.go`, `decide.go`, `allowlist.go`, `cooldown.go`.

A2. New substrate event kinds `KindAutotuneAction` + `KindAutotunePause` exist; enum-parity test passes.

A3. `regatta autotune {pause,unpause,dry-run,revert,status}` subcommands exist and pass help-text golden test.

A4. `scripts/check-autotune-scope.sh` runs in `make check` battery and rejects any PR labeled `autotune` whose diff touches a file outside the allowlist OR violates the K4 fence convention OR violates the K5 single-token-insert shape.

A5. Denylist filter (§9) is enforced — RED test asserts R3 / R5 findings produce zero candidates.

A6. CUE-gate failure produces a rejected-candidate substrate event (pr_number=0, after="") — RED test asserts this audit trail.

A7. Cooldown enforcement (§8) — RED test: two findings on same target within 24h ⇒ exactly one candidate minted.

A8. Damping caps (§8) — RED test: raise candidate >10% / 24h is rejected; lower candidate up to 30% / 24h is admitted.

A9. Research-mode hard exclusion — RED test: finding with `work_item_kind = "research"` produces zero candidates; rejected-candidate event payload `target = "REJECTED:research-kind"`.

A10. `regatta autotune revert <action-id>` opens an inverse PR — integration test asserts the reverse diff against a fixture action.

A11. Phase B default holds — no code path mints live yaml writes (auto-merge or otherwise) without `--phase-c-unsafe-i-know-what-im-doing` flag that is itself behind a `decision_required: phase-c-unattended` operator marker absent in v1.

A12. PR label `autotune` is auto-applied on PR-open; no PR is opened without it. Scope-check gate keys on this label.

A13. New SLO YAMLs are NOT required for v1 — the autotuner reads existing meters; no new instrumentation in scope §4.1.

## §15 Test scaffold — RED tests required (file FIRST, then implement)

Failing test names per `feedback_tdd_discipline`. Each name asserts one acceptance criterion above. All live under `internal/autotuner/` unless noted.

T1. `TestAutotuner_R3FindingProducesNoCandidate` — denylist enforcement (§9). Maps to A5.

T2. `TestAutotuner_R5FindingProducesNoCandidate` — denylist enforcement (§9). Maps to A5.

T3. `TestAutotuner_ResearchKindWorkItemProducesNoCandidate` — research-mode exclusion (§9). Maps to A9.

T4. `TestAutotuner_OutOfAllowlistTargetProducesNoCandidate` — allowlist gate (§6). Maps to A4.

T5. `TestAutotuner_CooldownDropsSecondCandidate` — §8 cooldown. Maps to A7.

T6. `TestAutotuner_RaiseExceedsTenPercentRejected` — §8 raise damping. Maps to A8.

T7. `TestAutotuner_LowerUpToThirtyPercentAdmitted` — §8 lower damping. Maps to A8.

T8. `TestAutotuner_CueGateFailureEmitsRejectedCandidateEvent` — §10 audit trail. Maps to A6.

T9. `TestAutotuner_ScopeCheckRejectsK4OutsideFence` — §6 K4 fence enforcement. Lives under `scripts/check-autotune-scope_test.sh` (shell test). Maps to A4.

T10. `TestAutotuner_ScopeCheckRejectsK5MultiTokenInsert` — §6 K5 shape enforcement. Shell test. Maps to A4.

T11. `TestAutotuner_RevertReconstructsInverseDiff` — §11 revert path. Integration test against fixture `KindAutotuneAction` row. Maps to A10.

T12. `TestAutotuner_PauseStateSkipsMatchingFindings` — §12 operator-override pause. Maps to A1+A3.

T13. `TestAutotuner_PROpenAttachesAutotuneLabel` — §12 label auto-application. Maps to A12.

T14. `TestSubstrate_KindAutotuneActionEnumParity` — extends `TestSubstrate_EventKindEnumMatchesSQLCheck`. Maps to A2.

T15. `TestAutotuner_PhaseBDefault_NoLiveWriteCodePath` — static analysis test pinning A11. Scope (closes #988):
- Grep target: `internal/selfimprove/autotuner/` + `cmd/regatta/autotune.go` (Go files only; vendor/ + testdata/ + `_test.go` excluded).
- Failing pattern: any literal string `"--auto-merge"` OR `gh.AutoMergeArg` constant reference whose lexical neighborhood (±20 lines) does NOT also contain `phase-c-unsafe-i-know-what-im-doing` AND NOT inside a `// +build phase_c_unsafe` build-tagged file.
- `decision_required` marker storage: substrate `events` table, `kind='autotune_decision_required'`, `payload.marker='phase-c-unattended'`. Resolution path = `regatta autotune unblock --marker phase-c-unattended` CLI subcommand (deferred until Phase C reopens; in Phase B the test asserts NO row of this kind exists). Maps to A11.

T16. `TestAutotuner_HighCacheHitRateAdmitsRaise` — §7 S3 signal direction. Maps to A1.

T17. `TestAutotuner_LowCacheHitRateRefusesRaise` — §7 S3 signal direction inverse. Maps to A1.

T18. `TestAutotuner_SchedulerSaturatedDropsCostCapCandidate` — §7 S4 sanity guard. Maps to A1.

Commit-order discipline: every test above lands as a FAILING commit BEFORE its implementation commit. `git log --reverse <branch>` MUST show RED-first per `feedback_tdd_discipline`. PR body captures failing output of T1-T3 minimally (representative slice).

## §16 Decision-required open questions

Carried forward from the brief's §11. Each is reversible; each has a default proposed.

1. **Phase C reopen-trigger threshold.** §13 picks "≥90 days green Phase B history". Operator may prefer count-based ("≥50 green autotune PRs merged, zero reverted") or hybrid. **Default proposed: 90 days AND 50 PRs.** Reopen at trigger fire.

2. **Banned-phrase append authority (K5).** §6 admits autotuner to extend `scripts/doc-check.sh`. The banned-phrase array is the most visible operator-authored governance surface (CLAUDE.md cites it by name), so unrestricted autotune-mint is meta-load-bearing. **Default proposed (hardened post-#988 review): NO autotune-mint without a second-key acknowledgement** — autotuner files a `[self-improvement]` issue carrying the proposed token + its R2 fire-count evidence; only when that issue picks up an explicit `autotune-k5-approved` operator label may the autotuner open the K5 PR. **Label-name comparison is BYTE-EQUAL lowercase** (closes #1005). Variants like `Autotune-K5-Approved`, `autotune-K5-approved`, or any other case form do NOT satisfy the gate; the autotuner check uses `strings.Equal(label, "autotune-k5-approved")`, never `strings.EqualFold`. A `scripts/check-autotune-k5-label.sh` precondition runs at config-load and rejects any repo carrying a near-duplicate label (defined as: `strings.ToLower(label) == "autotune-k5-approved"` AND `label != "autotune-k5-approved"`) so operators are forced to clean up before the autotuner runs. The existing damping cap (≤1 token / 30d) and novel-token check still apply. Append-only shape stays. Reopen-trigger to relax (drop second-key): ≥10 K5 autotune PRs merged-then-zero-reverted under second-key, AND operator signals fatigue with the second-key step.

3. **Dispatch-template ownership (K4).** §6 admits append-only changes inside fence. Operator may prefer single per-template "autotune-appended" section delimited by a fence marker. **Default proposed: YES, fenced section (already specified §6).**

4. **`KindAutotuneAction` retention.** §11 says "same as all substrate events". Operator may want longer retention specifically for autotune-action rows. **Default proposed: same retention; defer extension to Phase C reopen.**

5. **R3 / R5 escape from denylist.** §9 hard-codes the exclusion. Operator may want a future override-flag. **Default proposed: NO — the denylist is the design's adversarial-defense load-bearer; opening it requires re-running this trust-boundary review.**

## §17 Adoption-first audit (folded from brief §10)

| Primitive | Adopt-first reference | Decision |
|---|---|---|
| PR-as-write-channel | Renovate `automergeStrategy: pull-request` (v37+) | Adopt shape verbatim — autotuner is a reduced-powers PR author. Zero new dep. |
| yaml-path allowlist | GitHub Actions `environment` protection rules | Lighter — compiled-in Go slice (`internal/autotuner/allowlist.go`) + `check-autotune-scope.sh`. |
| CUE validation gate | Existing `regatta validate-config` (`internal/config/validate/load.go`) | Reuse byte-for-byte. |
| Append-only audit | Existing substrate signing chain (`internal/orchestrator/state/substrate/event.go`) | Reuse — `KindGateVerdict` HMAC chain provides equivalent append-only semantics. |
| Damping cap | Kubernetes HPA `behavior.scaleUp/scaleDown.policies` (v1.18+ stable) | Adopt asymmetric-rate-limit shape. PID rejected (§8). |
| Reversibility | Argo Rollouts revert | Lighter — single revert PR vs Argo's analysis-template machinery. |
| Two-key gate | HashiCorp Vault transit-engine multi-key | Reject — overkill. Existing GitHub operator-merge IS the second key. |
| Research-mode exclusion | None (bespoke) | Hard exclusion is the cheapest mechanism. |

## §18 Out-of-band followups (file as separate issues if approved)

- F1. Tracking issue for §16 #1 Phase C reopen-trigger — file at first sign of operator interest, not before.
- F2. Tracking issue for K4 fence-marker addition to the four dispatch templates (one-line markup; pre-conditioning the surface for autotuner). Single-file edits; mechanical.
- F3. Tracking issue for documenting the autotuner UX in `docs/operator/configure.md` once impl ships (this spec is design-only).
- F4. Phase-C cutover spec — reopens at §16 #1 trigger. Material elaboration deferred until trigger fires.
- F5. Audit hardening — extend `KindAutotuneAction` retention beyond default (decision_required §16 #4). Reopen at first audit need.

## §19 Implementer brief (per CLAUDE.md `feedback_dispatch_brief_only`)

This brief is the per-task dispatch handed to implementer subagents — full spec stays with the main thread; subagents receive ONLY this section.

**Task scope:** Implement the §5 closed-loop autotuner that consumes §7 signals, applies §8 rule-based asymmetric rate-limiter, and writes §6 K1-K5 knob changes through the PR pipeline. Phase B (operator-merge backstop) only — Phase C unattended-live is `decision_required: phase-c-unattended` and not part of this dispatch.

**Files to create (file-disjoint):**
- `internal/selfimprove/autotuner/loop.go` — main loop (tick cadence per §13 Phase B latency tier).
- `internal/selfimprove/autotuner/knobs.go` — K1-K5 dispatch table mapping signal-class → knob writer.
- `internal/selfimprove/autotuner/damping.go` — §8 rate-limiter + §9 floor enforcement.
- `internal/selfimprove/autotuner/scope.go` — §6 file-allowlist + §6 K5 single-token-insert shape check.
- `cmd/regatta/autotune.go` — `regatta autotune` subcommand (revert, status, pause).
- `scripts/check-autotune-scope.sh` — A4 mechanical gate.

**TDD discipline (per CLAUDE.md):** §15 T1-T16 commit failing-FIRST. Capture failing-test output in PR body. T15 grep scope per §15 update from #988.

**K5 second-key constraint (§16 #2 hardened):** autotuner MUST NOT open K5 PRs directly; instead, file a `[self-improvement]` issue carrying the candidate token + R2 evidence, then open the K5 PR only after the issue picks up the `autotune-k5-approved` label.

**Acceptance:** §14 A1-A12 all green; §15 T1-T16 all green; `make check` passes including the new `scripts/check-autotune-scope.sh` gate.

**Reviewer dispatch:** load-bearing per CLAUDE.md (concurrency + governance surfaces); spawn independent reviewer subagent in fresh slot, capture `Reviewer-recommendation: APPROVE` in PR body footer.
