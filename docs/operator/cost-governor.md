# Cost governor — operator runbook

Reader: customer-operator wiring Regatta to per-DAG / per-operator USD
caps + Anthropic Usage-API reconciliation.
Read time: 10 minutes.
Goal: set spend caps that fire BEFORE the next LLM call, read drift
alerts when reconciliation finds a mismatch, refresh the pricing table
on Anthropic's cadence.
Expires when: the `safety.cost` CUE schema in
`docs/engineer/specs/2026-06-01-cost-governor-design.md` §3.6 changes.

## What you get

- **Pre-call deny.** The scheduler checks every spawnable work_item
  against the configured caps (DAG, operator, work_item, global). Over
  the cap, the spawn drops from the tick and re-evaluates next tick —
  no `claude` subprocess started, no shell setup paid, no MCP server
  bring-up. Catches the Waxell $47K-weekend pattern at the cheapest
  enforcement point.
- **Post-call recording.** On every `claude` `result` event, the
  spawner appends a substrate `token_spend` row with the call's USD +
  token counts. Append-only — never updated, never deleted.
- **Periodic reconciliation.** Default hourly tick fetches the
  just-closed bucket from Anthropic's Cost API (preferred) or Usage
  API (fallback), compares against `SUM(token_spend.usd)` over the
  same window, emits a `budget_reconciled` substrate row + a drift
  alert if `drift_pct > drift_alert_threshold_pct`.
- **Soft-cap WARN.** Crossing `soft_pct` (default 80%) WARN-logs and
  carries an optional downgrade hint. Default behaviour is WARN-only;
  model swap requires `work_item.annotations.cost.allow_downgrade:
  true` per spec §9 R10.

## Config surface

The full `safety.cost` block from `contracts/schemas/regatta.v1.cue`
§`#CostGovernor`. Every field optional except `soft_pct` /
`reconcile_interval` / `drift_alert_threshold_pct` / `usage_api_key_env`
which carry defaults. An empty `safety.cost: {}` is rejected at boot
(`ErrCostBlockEmpty`); all-zero caps is rejected (`ErrCostCapsAllZero`).

```cue
#CostGovernor: {
    per_dag_usd?:               int & >=0
    per_operator_usd?:          int & >=0
    per_work_item_usd?:         int & >=0
    period?:                    "1h" | "1d" | "7d" | "30d"
    soft_pct:                   *80 | int & >=50 & <=99
    reconcile_interval:         *"1h" | "5m" | "15m" | "30m" | "6h" | "24h"
    drift_alert_threshold_pct:  *10 | int & >=0 & <=100
    usage_api_key_env:          *"ANTHROPIC_ADMIN_KEY" | string
    estimation_strategy:        *"upper_bound" | "history"
    pricing_override_path?:     string
}
```

| Field | Default | Notes |
| --- | --- | --- |
| `per_dag_usd` | unset | Per-DAG USD cap. Sum of recorded spend across all calls under one DAG. |
| `per_operator_usd` | unset | Per-operator (agent_id) USD cap. Stacks with `per_dag_usd` per the precedence rule below. |
| `per_work_item_usd` | unset | Per-work-item USD cap. The legacy `safety.spend_cap_usd` is treated as an implicit `per_work_item_usd` when the `cost:` block is present. |
| `period` | lifetime | Rollover window for cap accounting. `1h`/`1d`/`7d`/`30d`. Unset = lifetime of scope. |
| `soft_pct` | `80` | Soft-cap percentage. Crossing fires WARN; downgrade is opt-in. Range 50-99. |
| `reconcile_interval` | `1h` | Reconciler cron cadence. Hourly is the smallest stable Anthropic bucket. |
| `drift_alert_threshold_pct` | `10` | Drift threshold for `obs.EventCostDriftAlert`. `abs(actual - recorded) / max(actual, 0.01)`. |
| `usage_api_key_env` | `ANTHROPIC_ADMIN_KEY` | Env-var name holding the Anthropic admin key. NEVER the key value itself. |
| `estimation_strategy` | `upper_bound` | Pre-call USD estimator. `upper_bound` (default) is deterministic + conservative — `(input × price_in + max × price_out)`. `history` is the opt-in p95-of-cohort estimator (spec §10 S1, issue #238): reads recent `token_spend` rows for the `(tenant, operator, model)` cohort, falls back to `upper_bound` on cold-start (< 10 samples). Switch only if upper-bound's pessimism triggers soft-cap thrash. |
| `pricing_override_path` | unset | Optional path to a JSON file that overrides or extends the hardcoded pricing table at boot. Per-key merge — each model key replaces the corresponding hardcoded row; siblings untouched; new SKUs (Bedrock/Vertex) added. See **Pricing refresh** below for format + file-mode requirements. |

## Precedence — most-restrictive-wins

**Every configured cap at every scope is checked. The spawn is denied
if any cap would be breached.** Most-restrictive-wins — caps stack as
parallel guards, never overrides.

Worked example. Operator sets:

```yaml
safety:
  cost:
    per_dag_usd: 100
    per_operator_usd: 50
```

A work_item routes to operator `qa-bot` whose recorded spend in the
current period is $47, under a DAG whose recorded spend is $30. The
next call is upper-bound estimated at $4.

- `per_dag_usd`: $30 + $4 = $34 < $100. PASS.
- `per_operator_usd`: $47 + $4 = $51 > $50. **DENY.**

Spawn drops; operator-cap is the binding guard. AWS Budgets shape — if
you have two budgets attached, the tighter one fires first.

The legacy `safety.spend_cap_usd` field (MVP-2 single integer ceiling)
is preserved byte-equal when `safety.cost` is absent; when `cost:` is
present, the legacy field is treated as an additional implicit
`per_work_item_usd` cap. Pin: precedence is syntactically explicit at
the schema level — no silent inheritance, no LiteLLM-#12905 footgun.

## Reading drift alerts

`obs.EventCostDriftAlert` fires when `abs(actual_usd - recorded_usd) /
max(actual_usd, 0.01) > drift_alert_threshold_pct`. The slog line
carries the substrate `budget_reconciled` payload attrs:

| Attr | Source | Meaning |
| --- | --- | --- |
| `period_start` | bucket lower bound | unix ms; the hour the reconciler closed |
| `period_end` | bucket upper bound | unix ms |
| `actual_usd` | Anthropic Cost API | authoritative USD for the bucket |
| `recorded_usd` | `SUM(token_spend.usd)` | regatta's record over the same window |
| `delta_usd` | `actual - recorded` | signed; positive = regatta missed events |
| `drift_pct` | `abs(delta) / max(actual, 0.01)` | dimensionless ratio |

Diagnose — don't auto-correct. Per spec §3.4, drift indicates one of
three bugs and silent correction would mask the diagnosis:

1. The stream-json parser dropped a `result` event (spawner crash
   between `result` and the spawner-side `RecordCall` — see spec §9
   R13).
2. The pricing table is stale (operator missed an Anthropic pricing
   refresh). Re-check via the "Pricing refresh" runbook below.
3. Anthropic billed for something regatta did not see (rare; usually
   a cache_creation row regatta's parser missed).

Recovery walks via the cost-governor-incidents playbook
(`docs/engineer/runbooks/cost-governor-incidents.md`, lands in #300)
"EventCostDriftAlert fires" section.

## Soft caps — WARN by default, opt-in downgrade

`soft_pct` (default 80) sets the warn-line. Crossing `soft_pct × cap`
WARN-logs `obs.EventCostReconcileFallback` is unrelated — the
soft-cap signal is a span attribute `regatta.cost.soft_breached=true`
on the `cost.evaluate` span + a WARN slog line. The spawn STILL
proceeds; soft-cap is an advisory.

To opt in to model downgrade on soft-cap, set the work_item annotation:

```yaml
work_item:
  annotations:
    cost.allow_downgrade: true
```

When set AND soft-cap fires, the gate's `Verdict.DowngradeTo` carries
a cheaper model SKU (e.g. `claude-sonnet-4-7 → claude-haiku-4-5`) that
the spawner honours via `Request.ModelOverride`. Default-off prevents
the silent-correctness-regression where a planner-chosen opus call is
swapped to haiku without operator consent.

Per spec §9 R10 anti-thrash ratchet: once soft-cap fires for a (scope,
period) tuple, the period stays in soft-cap state until the period
rolls. No flapping between WARN and clean within a single bucket.

## Pricing refresh

The pricing table at `internal/cost/pricing/anthropic.go` is the
hermetic source-of-truth. Anthropic does not expose a programmatic
pricing endpoint, so refreshes are operator-mediated PRs.

**Cadence: quarterly + ad-hoc on any Anthropic pricing-page diff.**

Step-by-step.

1. Visit https://www.anthropic.com/pricing (or the
   `anthropic.com/pricing` page current at refresh time).
2. Diff each active SKU's input / cache-read / cache-write / output
   rate against `internal/cost/pricing/anthropic.go`.
3. Edit the table. New rows for new SKUs; `RetiredAfter` set for
   sunsetted SKUs.
4. Bump the `pricing_rev` constant — increment by one. Every
   `token_spend` payload now carries the new rev.
5. Open a PR titled `feat(cost/pricing): refresh Anthropic rates
   YYYY-MM-DD`. Body MUST cite the Anthropic pricing-page URL pinned
   at the PR's commit time AND list the diff lines verbatim
   (`+claude-opus-4-7: 15.00 → 16.00`).
6. CI runs `TestPricing_AllActiveSKUsHavePositiveRows` (no zero rows
   for active SKUs — Portkey-trap defense per spec §3.1).
7. Reviewer confirms the URL cite resolves at the pinned commit time.
   Renovate-bot does NOT auto-bump; human-in-the-loop required.

Rollback procedure for a bad-pricing PR is in
cost-governor-incidents (`docs/engineer/runbooks/cost-governor-incidents.md`,
lands in #300) §"Pricing-table rollback".

### `pricing_override_path` — escape hatch for forked rates

The default refresh-via-PR flow is the right answer for ≥95% of
operators. The `pricing_override_path` config field is the escape
hatch for operators who run claude through Bedrock, Vertex, or a
marketplace re-seller whose rates the upstream pricing table cannot
mirror.

Format. JSON object keyed on model SKU; each row mirrors
`internal/cost/pricing.Row`.

```json
{
  "bedrock-claude-sonnet-4-7": {
    "InputUSDPerMTok": 3.30,
    "CacheReadUSDPerMTok": 0.33,
    "CacheCreationUSDPerMTok": 4.10,
    "OutputUSDPerMTok": 16.50
  }
}
```

Semantics. Per-key merge — each model key in the override file
replaces the corresponding hardcoded row entirely. Rows not present
in the override are untouched. The override can also ADD SKUs the
hardcoded table does not list (the Bedrock case above). Field-level
merge is intentionally NOT supported: zero values then become
ambiguous, and the override-as-full-row rule keeps the diff
reviewable.

Boot-time hard-fails (R14 mitigation — every check is a refusal, not
a warning):

- File does not exist or is unreadable → `pricing override: stat …`.
- Malformed JSON → `pricing override: parse …`.
- Unknown field (typo in `InputUSDPerMTok`) → strict-decoder error.
- Non-positive rate on an active SKU → `ErrOverrideInvalid`
  (same Portkey-trap defense as the in-tree table).
- World-writable file mode (POSIX `o+w` bit set) → `ErrOverrideUnsafe`.
  Defends against the attacker-rewrite vector.

The override file MUST be owned by the regatta process user and have
mode `0600` (`-rw-------`). Boot fails closed when this is not true.

Hot-reload is not supported in this wave: the override loads once at
process boot. Restart the regatta process after editing the file.

## OTel cardinality

The `cost.evaluate` span fires once per spawnable work_item per
scheduler tick. Span count per tick ≤ `lane_cap × num_lanes`. For
deployments with `lane_cap × num_lanes > 20` OR `tick_interval < 5s`,
sample down per W6 spec §9 R6:

```
OTEL_TRACES_SAMPLER=parentbased_traceidratio
OTEL_TRACES_SAMPLER_ARG=0.01
```

See `docs/operator/observability.md#sampler-customization` for the
sampler-arg semantics. The reconciler cron span (`cost.reconcile`)
fires once per `reconcile_interval` — default 1/hour — so it is
unaffected by the sampler and operators should NEVER drop those
spans.

Per-panel attribute breakdown lives in cost-governor-dashboards
(`docs/operator/cost-governor-dashboards.md`, lands in #301) — read it
when wiring Honeycomb / Grafana / Jaeger dashboards.

## Cost API vs Usage API fallback

Reconciler prefers the Anthropic Cost API
(`/v1/organizations/cost_report/messages`) because it returns USD
directly. Fallback to the Usage API
(`/v1/organizations/usage_report/messages`) + local-pricing
application happens ONLY when the Cost API is unavailable (404 / 5xx
persistent / network down).

Operator-visible signal: `obs.EventCostReconcileFallback reason=cost_api_unavailable`
at WARN level. When this fires, the next `budget_reconciled` row's
`actual_usd` was computed by applying the local pricing table to
Anthropic's reported tokens — pricing-table drift is invisible on
this path (both sides apply the same table). The next successful Cost
API tick self-heals via LWW reducer on the `(tenant_id,
period_start)` key.

Limitation: if a `pricing_rev` mismatch happens during a sustained
Cost-API outage, the Usage-API fallback under-reports drift until
Cost API returns. The drift will surface when Cost API recovers and
the next reconcile row over-writes the fallback estimate.

## Admin API key handling

The Anthropic admin key is loaded from the env var named in
`safety.cost.usage_api_key_env` (default `ANTHROPIC_ADMIN_KEY`). The
key value is NEVER logged, NEVER cached on disk, NEVER carried in
substrate payloads.

What IS logged at boot:

- The env-var NAME (e.g. `usage_api_key_env=ANTHROPIC_ADMIN_KEY`).
- The first 8 chars of `sha256(key)` as a fingerprint (e.g.
  `key_fingerprint=a1b2c3d4`), so operators can confirm rotation
  succeeded without exposing the key.

Rotation procedure (rolling restart required in MVP-2) is in
cost-governor-incidents (`docs/engineer/runbooks/cost-governor-incidents.md`,
lands in #300) §"Anthropic admin key rotation procedure". In-process
SIGHUP-style rotation is deferred per `[cost-governor-followup]` #249.

## Substrate shadow phase

The `token_spend` + `budget_reconciled` substrate kinds were
registered in substrate v2 Wave 1 §2.1 + §4 with reducers `append` /
`lww` respectively. Substrate v2 Phase B (shadow-write
reconciliation) runs `token_spend` rows through both the legacy event
table and the substrate `events` table; divergence is flagged when
the row counts diverge by > 0.5% over a 24h window.

If your deployment is still on Phase B at the time of this read,
divergence > 0.5% means the cost-governor's per-tick `BudgetState`
read is operating on a partial view — investigate the substrate
shadow-divergence runbook BEFORE acting on cost-gov alerts. Phase C
deployments skip this check; the substrate `events` table is the sole
source-of-truth. Per spec §7 A6.

## Example config

A commented-out demo block lives under `safety:` in
`examples/full/regatta.yaml`. Three realistic shapes:

```yaml
# Shape A — single per-DAG cap, lifetime, default soft-cap.
safety:
  cost:
    per_dag_usd: 100

# Shape B — stacked caps, hourly reconcile, tight monitoring.
safety:
  cost:
    per_dag_usd: 100
    per_operator_usd: 50
    period: 1d
    soft_pct: 80
    reconcile_interval: 1h
    drift_alert_threshold_pct: 10
    usage_api_key_env: ANTHROPIC_ADMIN_KEY

# Shape C — tight drift monitoring (5% threshold), 15-min reconcile.
safety:
  cost:
    per_dag_usd: 200
    soft_pct: 80
    reconcile_interval: 15m
    drift_alert_threshold_pct: 5
```

Uncomment the demo block in `examples/full/regatta.yaml` and run
`regatta validate-config examples/full/regatta.yaml` to verify the
shape your operator picks.

## What is intentionally missing

The MVP-4 W11 wedge ships pre-call deny + reconciliation + recording
on Anthropic native API only. Out-of-scope deltas with tracking
issues — query current status via:

```
gh issue list --label cost-governor-followup
```

The major gaps operators commonly expect:

- **Per-tenant + per-team budgets** — W8 RBAC ships `tenant_id`
  propagation; the cost-governor's scope enum extends then. Tracking
  issue: `[cost-governor-followup] per-tenant + per-team budgets`.
- **Stripe metered-billing webhook** — MVP-4 W12 owns this; this
  wedge stops at substrate `budget_reconciled` rows.
- **Predictive quota forecasting** — "DAG-X will blow cap in 12min,
  pause now" requires a time-series rollup that does not exist.
- **Auto-downgrade default ON** — opt-in only per work_item
  annotation; default-off prevents silent model swaps.
- **Real-time deny at credential layer** (Portkey virtual-key
  pattern) — would require regatta to proxy Anthropic API calls.
  Out-of-scope; pre-call deny at scheduler tick is the regatta-native
  equivalent.
- **Bedrock / Vertex first-class pricing** — first-class hardcoded
  rows still deferred; the `pricing_override_path` escape hatch (see
  **Pricing refresh** above) lets operators load their own JSON table
  in the meantime.
- **In-process admin-key rotation without restart** — rolling restart
  is the MVP-2 procedure.

## Where to look next

- Incident playbook for on-call response —
  `docs/engineer/runbooks/cost-governor-incidents.md` (sibling runbook,
  lands in #300; link restored in post-merge follow-up PR).
- Dashboard cite mapping every `regatta.cost.*` span attr +
  `obs.EventCost*` event to Honeycomb / Grafana / Jaeger queries —
  `docs/operator/cost-governor-dashboards.md` (sibling cite, lands in
  #301; link restored in post-merge follow-up PR).
- Design spec (engineers) —
  [docs/engineer/specs/2026-06-01-cost-governor-design.md](../engineer/specs/2026-06-01-cost-governor-design.md).
- OTel wiring + sampler customization —
  [docs/operator/observability.md](./observability.md).
