# Cost-governor incident playbook

Reader: on-call engineer or operator responding to a cost-governor
alert.
Read time: 8 minutes scan; each H2 below is a 1-2 minute targeted
read.
Goal: triage a cost-governor alert from slog event name to a concrete
recovery path within minutes.
Expires when: spec §3.4 failure-mode table changes or the cost-gov
slog event surface gains a new `obs.EventCost*` name.

Each H2 below follows the same 7-line shape — **Trigger** /
**Symptoms** / **First-check** / **Diagnose** / **Recovery** /
**Rollback** / **Spec-cite**. Operators read top-down by event name;
on-call escalates via "Where to find config" + "What this incident
affects".

## EventCostReconcileFailing fires

- **Trigger.** Reconciler hits ≥ 5 consecutive tick failures with the
  same upstream class — Anthropic Cost API 429s, persistent 5xx, or
  network down. ERROR slog with `reason=rate_limited` or
  `reason=upstream_down`.
- **Symptoms.** No new `budget_reconciled` substrate rows over the
  affected hours. Drift dashboards go flat (no fresh data, not zero
  drift). Pre-call deny gate continues against the last successful
  reconciliation per Fold semantics.
- **First-check.** Anthropic status page
  (https://status.anthropic.com). Check whether the admin-key
  fingerprint in the slog WARN-on-rotation matches the deployed
  fingerprint — `grep "regatta.cost.api_key_fingerprint" <log>`
  surfaces the first 8 chars of `sha256(key)`; never the raw key.
- **Diagnose.** Three failure classes. (a) Rate limit
  (`reason=rate_limited`) — exponential backoff is in effect (1s × 2^n
  capped 5min); the reconciler keeps trying. (b) Upstream 5xx
  (`reason=upstream_down`) — Anthropic Cost API is down; check
  status. (c) Network (also `reason=upstream_down`) — confirm the
  regatta host can reach `api.anthropic.com:443`.
- **Recovery.** Wait out the backoff. If persistent > 4h, temporarily
  raise `safety.cost.drift_alert_threshold_pct` to suppress noise per
  spec §9 R3 mitigation, then file an Anthropic support ticket
  citing the affected window. Restore the threshold once reconciler
  recovers.
- **Rollback.** None — reconciler is fail-soft. Pre-call deny gate
  continues against the last successful `budget_reconciled` row;
  degraded but not broken.
- **Spec-cite.** §3.4 failure-mode table (lines 247-248) + §9 R3
  (rate limit) + §9 R6 (Anthropic down).

## EventCostDriftAlert fires

- **Trigger.** `abs(actual_usd - recorded_usd) / max(actual_usd,
  0.01) > drift_alert_threshold_pct` (default 10%). WARN slog with
  `period_start`, `actual_usd`, `recorded_usd`, `delta_usd`,
  `drift_pct` attrs.
- **Symptoms.** Anthropic billed more than regatta recorded over the
  bucket. The pre-call deny gate's next read against the Cost-API
  path uses the actual figure; subsequent decisions reflect the
  drift.
- **First-check.** Was `obs.EventCostReconcileFallback
  reason=cost_api_unavailable` WARN-logged for the same bucket? If
  yes, Usage-API path applied pricing locally — pricing table is the
  suspect. Hit the "Pricing-table rollback" section if the pricing
  PR landed in the affected window.
- **Diagnose.** Flowchart. (a) Pricing-stale path → run the T5
  "Pricing refresh" procedure; compare against
  https://www.anthropic.com/pricing pinned at the bucket time. (b)
  Parser-miss path → search for `regatta.cost.spend_unknown` slog in
  the bucket window — fires when the spawner SIGKILL'd between a
  `result` event and the spawner-side `RecordCall` per spec §9 R13.
  (c) Substrate write-skew → look for the `llm_call` span with
  `error.type=record_call_failed` per spec §9 R4.
- **Recovery.** Usually self-healing — the next clean Cost API tick
  writes a `budget_reconciled` row that supersedes via LWW on
  `(tenant_id, period_start)`. For persistent drift, run the backfill
  recipe `regatta cost backfill --since 24h` once the followup CLI
  ships (tracking issue: backfill CLI per spec §9 R6 + R13).
- **Rollback.** Never auto-correct. Drift indicates a bug; silent
  correction would mask the diagnosis. The signal IS the recovery
  trigger.
- **Spec-cite.** §3.4 line 240 (drift semantics) + §9 R4
  (write-skew) + §9 R13 (SIGKILL drift).

## EventCostReconcileSkipped fires

- **Trigger.** Admin-key env var (configured by
  `safety.cost.usage_api_key_env`, default `ANTHROPIC_ADMIN_KEY`) is
  unset at boot or at reconcile-tick time. WARN slog with
  `reason=no_admin_key`.
- **Symptoms.** No HTTP call to Anthropic Cost / Usage API. No fresh
  `budget_reconciled` rows. Pre-call deny gate still functions
  against recorded spend — degraded, not broken.
- **First-check.** Confirm the env-var NAME in `safety.cost.usage_api_key_env`
  matches what the process environment exports. The env-var VALUE
  must be the Anthropic admin key (sha256-prefix logged at boot for
  verification).
- **Diagnose.** Common causes. (a) Secret-store integration dropped
  the env var on a deploy. (b) The operator renamed
  `safety.cost.usage_api_key_env` in `regatta.yaml` without updating
  the deployment env. (c) The Anthropic admin key was revoked /
  rotated and the new value never landed.
- **Recovery.** Export the env var per the deployment's secret-store
  procedure (1Password / k8s secret / systemd-credentials). Restart
  `regatta serve` — reconciler resumes on the next tick. The
  fingerprint in the boot-time slog WARN confirms the new key landed.
- **Rollback.** Pre-call deny gate continues against recorded spend;
  no rollback needed.
- **Spec-cite.** §3.4 failure-mode table line 246 + §9 R15 (admin
  key handling).

## EventCostSoftCapBreached fires

- **Trigger.** A work_item's pre-call estimate would cross
  `soft_pct × cap` for the scope. INFO-level signal via the
  `regatta.cost.soft_breached=true` attribute on the `cost.evaluate`
  span. (Note: this is not a slog event constant yet; it surfaces
  via the span attribute. The slog-event promotion is a deferred
  follow-up — see #289.)
- **Symptoms.** Span attribute fires; spawn proceeds. If the work_item
  carries `annotations.cost.allow_downgrade: true`, the spawner
  receives `Verdict.DowngradeTo` and routes to a cheaper SKU.
- **First-check.** Confirm whether downgrade is intended. If the
  spawn is supposed to proceed at the planned model and soft-cap is
  purely advisory, no action needed.
- **Diagnose.** Check the work_item's annotation map. If
  `cost.allow_downgrade` is absent, the WARN is by-design — soft-cap
  is a heads-up that the bucket is approaching hard-cap.
- **Recovery.** None if WARN-only is the desired behaviour. To enable
  downgrade, add `annotations.cost.allow_downgrade: true` to the
  work_item per spec §9 R10.
- **Rollback.** Ratchet rule per spec §9 R10 — once soft-cap fires
  for a (scope, period) tuple, the period stays in soft-cap state
  until the period rolls. No flapping mid-bucket.
- **Spec-cite.** §9 R10 (soft-cap thrash) + §3.7 OTel attr table.

## Anthropic admin key rotation procedure

- **Trigger.** Anthropic admin key rotation policy (quarterly per
  most operator security policies) or a suspected key compromise.
- **Symptoms.** N/A — this is a procedure, not an alarm response.
- **First-check.** Confirm the new key works against the Anthropic
  console BEFORE rotating in regatta — minimises the no-key window.
- **Diagnose.** N/A.
- **Recovery (step-by-step rolling restart, MVP-2).**
  1. Generate the new admin key in the Anthropic console.
  2. Set the new env-var value in the secret-store (1Password / k8s
     secret / systemd-credentials).
  3. Roll-restart `regatta serve` processes one at a time.
  4. Confirm the next reconcile tick succeeds — the WARN-on-rotation
     slog line carries the first 8 chars of `sha256(new_key)`.
  5. Revoke the old key in Anthropic console AFTER ≥ 2 successful
     reconcile ticks on the new key.
- **Rollback.** If the new key fails (e.g. typo), the old key is
  still valid in Anthropic; restore the old env-var value and
  roll-restart. Old-key revocation in step 5 is intentionally last —
  the operator keeps the rollback path until the new key has proven
  itself.
- **Spec-cite.** §9 R15 (admin key handling). In-process SIGHUP-style
  rotation is deferred — tracking issue #249.

## Pricing-table rollback

- **Trigger.** A pricing PR landed with a bad row (e.g. wrong USD
  rate, zero rate for an active SKU) and reconciliation drift is
  surfacing the error.
- **Symptoms.** Sustained `EventCostDriftAlert` after a pricing PR,
  with the drift sign matching the pricing direction (over-charged
  → recorded < actual; under-charged → recorded > actual).
- **First-check.** `git log -- internal/cost/pricing/anthropic.go`
  finds the bad PR. Confirm the affected SKUs from the PR diff.
- **Diagnose.** Compare the bad row against
  https://www.anthropic.com/pricing pinned at the PR's commit time.
- **Recovery (step-by-step).**
  1. `git revert <bad-pricing-commit>` on a fresh branch.
  2. Bump the `pricing_rev` constant — increment by one (revert
     counts as a refresh).
  3. Open an emergency PR titled `feat(cost/pricing): rollback bad
     <SKU> rate <date>` with `[cost-governor-rollback]` tag.
  4. The next reconcile tick after deploy uses the rolled-back
     table; substrate `budget_reconciled` rows from the bad window
     are NOT auto-corrected — LWW means the next clean tick
     supersedes via `(tenant_id, period_start)`.
  5. Optional: run `regatta cost backfill --since <bad-window>` once
     the followup CLI ships.
- **Rollback.** The rollback IS the recovery. Substrate rows are
  append-only; the LWW reducer on `budget_reconciled` self-corrects
  on the next clean tick.
- **Spec-cite.** §3.8 (Refresh runbook) + §3.5 (reducer semantics)
  + §7 B3 (append-only invariant). The boot validator that catches
  zero-rate rows before any Lookup is pinned by
  `TestPricing_BootRejectsKnownBadTable` against the
  `internal/cost/pricing/testdata/anthropic_bad_zero_row.go` fixture
  (closes #290).

## Spawner SIGKILL drift recovery (R13)

- **Trigger.** Persistent `drift_pct > 0` with no obvious failed-call
  alert. The spawner subprocess was SIGKILL'd between Anthropic's
  `result` event and the spawner-side `RecordCall` — Anthropic billed
  for the call; regatta has no `token_spend` row.
- **Symptoms.** `actual_usd > recorded_usd` on `budget_reconciled`
  rows during the affected hours. No `EventCostReconcileFailing`
  noise; the reconciler is healthy.
- **First-check.** Check the spawner process logs for SIGKILL in the
  affected bucket. If the followup `spawner reconciliation outbox`
  ships, look for `regatta.cost.spend_unknown` slog events in the
  bucket window.
- **Diagnose.** OOM-killer is the typical culprit on long Claude
  sessions with large workspace state. Confirm via
  `dmesg | grep -i killed`.
- **Recovery.** The drift signal IS the recovery — Anthropic billed
  for the lost call; regatta now knows about it via the
  `budget_reconciled` row's `actual_usd > recorded_usd`. The pre-call
  deny gate reads `actual_usd` when the Cost API path is fresh
  (per spec §3.4 semantics), so future caps reflect the true spend.
- **Rollback.** None.
- **Spec-cite.** §9 R13 (SIGKILL drift) + §3.4 (Cost API drift
  semantics).

## Where to find config

The full `safety.cost` config surface lives at
[../../operator/cost-governor.md](../../operator/cost-governor.md)
§"Config surface".

Spec for engineer-level references:
`docs/engineer/specs/2026-06-01-cost-governor-design.md`.

## What this incident affects

| Incident | Pre-call deny | Soft-cap WARN | Reconciler | Drift signal |
| --- | --- | --- | --- | --- |
| EventCostReconcileFailing | continues (stale data) | continues | DEGRADED | DEGRADED |
| EventCostDriftAlert | continues (uses actual_usd) | continues | continues | the alert itself |
| EventCostReconcileSkipped | continues (recorded only) | continues | DEGRADED | DEGRADED |
| EventCostSoftCapBreached | continues | the alarm itself | continues | continues |
| Admin key rotation | continues | continues | brief gap during restart | brief gap |
| Pricing-table rollback | continues | continues | next tick uses new table | self-corrects via LWW |
| Spawner SIGKILL drift | continues | continues | continues | the alert itself |

Triage rule. Customer-impacting only when "Pre-call deny" is
DEGRADED, which is never under any incident here. The reconciler
fail-soft contract means cost-governor is degraded-not-broken across
every named alarm.
