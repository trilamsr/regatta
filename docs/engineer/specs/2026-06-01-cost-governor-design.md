---
title: "MVP-4 Cost Governor (P8) — Design Spec"
status: active
phase: x-forward-fit
summary: "Cost governor wedge — pre-call USD+token caps + Anthropic Usage API reconciliation. Forward-fits tenant_id + Stripe billing seams that activate in Phase X."
---

# MVP-4 Cost Governor (P8) — Design Spec

Status: ready for review
Date: 2026-06-01
Author: design subagent <tri@maydow.com>
Issue umbrella: TBD (this spec stands up the umbrella)
Depends on:
- **Hard prereq (merged):** W6 OTel T1 (SDK setup), T2 (slog bridge), T4 (stream-json GenAI parser), T5 (Config.Tracer injection across 8 components) — `docs/engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md`. The W6 stream-json parser at `internal/orchestrator/spawner/genai.go` is the LLM-call seam this wedge piggy-backs on for `token_spend` emission and for the `gen_ai.usage.*` attribute source.
- **Hard prereq (merged):** Unified Substrate v2 Wave 1 — `docs/engineer/specs/2026-06-01-unified-substrate-design.md`. This wedge writes `events.kind='token_spend'` and `events.kind='budget_reconciled'` and reads cumulative spend via reducer `append` + caller-side `SUM(payload->>'usd')`. Both kinds are already registered in substrate v2 §2.1 + §4 with `defaultReducer(kind)` strategies — `token_spend=append`, `budget_reconciled=lww`. No new substrate enum slot is required.
- **Soft prereq:** W7 Operator UI cost panel (separate wedge) will consume the substrate-derived `BudgetState` view this spec materialises.
- **Soft prereq:** W8 RBAC `tenant_id` propagation lands ≥1 release after; this spec emits `tenant_id` placeholder `substrate.DefaultTenantID` and the swap is one-line per the W6/substrate forward-fit pattern.
Binding brief: `docs/engineer/briefs/2026-05-31-mvp-3-next-level.md` §1 (cost governor named in MVP-2 inventory) + §2 (gap: "cost governor records spend internally; no per-tenant invoice CSV, no Stripe metered-billing webhook, no chargeback report" — MVP-4 W12 consumes this wedge's substrate output) + §4 W6 T4 "GenAI semantic-convention attribute set on `llm_call` spans" + §5 thread #2 "Observability backbone is the spine: W6 underpins … W12 (usage rollups aggregate spans)" + §6 red-team #1 (OTel reconciliation needs canonical token counts).
Roadmap fit: `wedge_roadmap_assessment` MVP-2 W1 row was deferred to land **after substrate Wave 1** so the storage layer is unified per `wedge_cost_governor` — this spec promotes that placement to **MVP-4 W11 (cost-governor-on-substrate)** per the autonomous-session prompt §PRIORITY-2. Trap pattern **P8 (spend / iteration brakes with mandatory re-approval — load-bearing)** is the canonical incident-justified pattern this wedge mitigates.
Memory rules in force: `feedback_research_design_principles` (adopt OSS), `feedback_decision_priority` (UX > best-prac > velocity), `feedback_grade_rubric` (B/A/A+ tool-checkable), `feedback_adversarial_review` (hostile-read mandate), `feedback_spec_pattern_authority` (one pattern mandated), `feedback_unaddressed_load_bearing` (named-but-deferred → tracking issue), `feedback_deletion_default` (what got smaller?), `feedback_simplify_reviewer` (mandatory deletion proposal), `wedge_cost_governor` (prior-art patterns), `wedge_roadmap_assessment` (P8 trap pattern).

---

## §1 Problem

The MVP-2 inventory in brief §1 names the cost governor explicitly: "per-DAG/operator USD+token caps, soft (80%) downgrade Sonnet→Haiku, hard pause, Anthropic Usage-API reconciliation." Brief §2 then names the gap: today regatta has exactly one cost knob — `safety.spend_cap_usd: *50` (per `contracts/schemas/regatta.v1.cue` §`#Safety`, a single integer ceiling applied at config-load time, never read post-load). There is no scope (operator vs DAG vs work_item), no period, no enforcement seam, no reconciliation against the authoritative Anthropic Usage API, no `tenant_id` scoping for W8, no Anthropic-pricing source-of-truth, no substrate emission of per-call spend. The autonomous-session-prompt's PRIORITY-2 entry states the wedge is **moat-widening** because Claude Code "has no cross-session spend tracking" and Anthropic Console has org-tier limits but **no DAG-aware mid-DAG enforcement** (`wedge_cost_governor` §"Why this is moat vs Claude Code").

The Waxell $47K incident (`wedge_cost_governor` pattern #8) is the load-bearing real-world: a single agent loop that nobody intercepted before the next LLM call burned $47,000 of budget across one weekend. Pre-call deny is the gap. Regatta's scheduler tick is the natural owner because (a) it is the cheapest enforcement point (deny **before** spawn, not before kill), (b) it already runs cumulative-state reads against `state.DB` for lock/lane occupancy on every tick, (c) it already filters spawnable in-place via the W2 approval-gate pass landed in #114. The substrate v2 `events` table with `token_spend` + `budget_reconciled` kinds is the storage layer; the W6 stream-json parser is the per-call data source; the Anthropic `/v1/organizations/usage_report/messages` endpoint is the reconciliation truth. All three pieces already exist or land before this wedge dispatches. The wedge is wiring, not invention.

Strategic framing per `wedge_roadmap_assessment` §"Strategic framing": "regatta = control plane for AI labor. Position every feature as scheduling + budgeting + audit + recovery for fleets of LLM workers." A control plane that cannot enforce a budget cannot operate fleets at scale safely. Without this wedge, the control-plane-for-AI-labor framing has a public hole on its primary value prop. With it, regatta ships the operator surface (per-DAG/operator USD+token caps + reconciliation + audit trail) that CC structurally cannot ship without becoming a different product.

---

## §2 Scope

### In scope (this wedge / MVP-4 W11)

1. **New package `internal/cost/`** with sub-packages: `internal/cost/gate/` (pre-call deny gate seam + scheduler hook), `internal/cost/estimate/` (input-token + max-token upper-bound pricer), `internal/cost/pricing/` (hardcoded per-model `$/Mtok` table + refresh runbook), `internal/cost/reconcile/` (Anthropic Usage API poller + drift detector), `internal/cost/spend/` (substrate write helpers + fold-derived cumulative reader).
2. **Pre-call deny gate** at the scheduler tick, between `applyApprovalGates` and `reserveFromSpawnable` (`internal/orchestrator/scheduler/scheduler.go:Tick` step 0.6). Filters spawnable in-place: an over-cap work_item drops from this tick with status remaining `planned` so the next tick re-evaluates after reconciliation; soft-cap (80%) downgrade is annotation-driven (Sonnet → Haiku) and lands as an enrichment on the spawnable element (no new state column).
3. **Spawner stream-json hook** — extends W6 T4's `internal/orchestrator/spawner/genai.go` parser. On the `result` event the parser ALREADY opens/closes the `llm_call` span and sets `gen_ai.usage.input_tokens` + `gen_ai.usage.output_tokens` (W6 §3.4). This wedge ADDS one call to `cost.spend.RecordCall(ctx, tx, CallRecord{…})` which appends a substrate `events.kind='token_spend'` row with `payload_json = {usd, model, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens}`. The W6 parser already owns the transaction context — this wedge piggy-backs.
4. **Reconciler tick** — periodic cron (default `1h`, configurable `safety.cost.reconcile_interval`) polls Anthropic `/v1/organizations/usage_report/messages` for the org-key window, sums per-bucket cost, compares against `SUM(spend_usd)` over substrate `events WHERE kind='token_spend' AND written_at IN [bucket]`. Writes `events.kind='budget_reconciled'` with `payload = {period_start, period_end, actual_usd, recorded_usd, delta_usd, drift_pct}`. Emits an audit-log event + OTel span attribute on drift > threshold (default 10%).
5. **Config surface extension** — extends `#Safety` in `contracts/schemas/regatta.v1.cue` (BACKWARDS-COMPATIBLE) with one new sub-section `safety.cost` whose fields default to "preserve current behaviour" when unset:
   ```cue
   #Safety: {
     // … existing fields preserved verbatim, including spend_cap_usd: *50 …
     cost?: #CostGovernor
   }
   #CostGovernor: {
     // Per-scope USD caps. Null = no cap at that scope.
     // Precedence rule: EVERY configured cap is checked; spawn is
     // denied if ANY cap would be breached (most-restrictive wins).
     // The legacy safety.spend_cap_usd field is treated as an
     // additional per-work-item cap when this block is present.
     per_dag_usd?:               int & >=0
     per_operator_usd?:          int & >=0
     per_work_item_usd?:         int & >=0
     // Period for cap rollover. Null = lifetime of scope.
     period?:                    "1h" | "1d" | "7d" | "30d"
     // Soft-cap threshold; spawn carries a warn signal (and an
     // optional downgrade hint when allow_downgrade annotation set).
     soft_pct:                   *80 | int & >=50 & <=99
     // Reconciliation cron interval.
     reconcile_interval:         *"1h" | "5m" | "15m" | "30m" | "6h" | "24h"
     // Drift threshold: if abs(actual - recorded)/actual > pct, alert.
     drift_alert_threshold_pct:  *10 | int & >=0 & <=100
     // Anthropic Usage API admin key env var name. If unset,
     // reconciler logs warn + skips (governor still does pre-call
     // deny against recorded spend, no auth-reconciled view).
     usage_api_key_env:          *"ANTHROPIC_ADMIN_KEY" | string
   }
   ```
   All fields optional. A `safety: {}` config keeps every MVP-2 behaviour byte-equal — no pre-call deny, no reconciliation, no substrate write, no cron. An EMPTY `safety: { cost: {} }` block is rejected by the validator (no caps configured ⇒ gate would be no-op overhead; explicit error message guides the operator). Operator opts in by setting ≥ 1 cap.

   _Wave-1 deliberately ships ONLY `upper_bound` estimation (deterministic, replay-safe). `history` estimator + `pricing_override_path` are deferred per S1 + S2 in §10 — tracking issues filed pre-merge._
6. **OTel attribute extension** — extends the W6 `llm_call` span attribute set with four new attrs (regatta-namespace, not `gen_ai.*` semconv so no upstream collision): `regatta.cost.usd_estimate`, `regatta.cost.cap_dag_usd`, `regatta.cost.cap_op_usd`, `regatta.cost.allow` (bool). Cite W6 spec §3.4.
7. **Pricing source-of-truth** — hardcoded Go table at `internal/cost/pricing/anthropic.go` keyed on model SKU. Each row has `(input_usd_per_mtok, cache_read_usd_per_mtok, cache_write_usd_per_mtok, output_usd_per_mtok, retired_after time.Time)`. Operator runbook documents the manual-refresh cadence (per Anthropic pricing-page diff). Override-file path is DEFERRED to a follow-up wedge per S2 (§10) — pre-W11 deployments either use the hardcoded table or fork the repo for custom pricing.
8. **Operator doc** `docs/operator/cost-governor.md` covering env-var contract, scope precedence, drift-alert reading, soft-cap downgrade semantics, pricing-refresh runbook.

### Out of scope (deferred — separate issues filed at impl-time per `feedback_unaddressed_load_bearing`)

- **Per-tenant + per-team budgets**: W8 RBAC ships `tenant_id` on every substrate read path; this wedge's `BudgetScope` enum will then gain `tenant`/`team` rows. Tracking issue: `[cost-governor-followup] per-tenant + per-team budgets` (lands W8 wave + 1).
- **Streaming Stripe metered-billing webhook**: brief §4 W12 explicitly owns this. This wedge stops at the substrate `budget_reconciled` event; W12 maps those to Stripe `usage_records.create`.
- **Predictive quota forecasting** (e.g. "DAG-X is on track to blow cap in 12min — pause now"): requires a time-series rollup that does not exist. Tracking issue.
- **Mid-DAG kill semantics with `reversibility` tag + `on_budget_kill` compensation hook** per `wedge_cost_governor` §"Open problems regatta could lead". Compensation primitives are out-of-band for MVP-4. Tracking issue.
- **Cache-aware budgeting beyond passive accounting** (e.g. budget encourages cache-friendly prompt structure via fee-discount on cache_read tokens). Pricing table already separates cache_read/cache_write rates so the data is faithfully recorded; "encourage" is a future planner-side concern. Tracking issue.
- **Cross-fleet attribution for shared MCP keys** (`wedge_cost_governor` §"Open problems"). Tracking issue.
- **Bedrock/Vertex pricing first-class support** (only Anthropic native API in scope). Custom pricing requires a code-level fork until the override-file follow-up lands. Tracking issue.
- **`pricing_override_path` config surface** — operator-supplied JSON file overriding the hardcoded table. Cut from Wave 1 per S2 (§10) — closes R14 (override-tampering surface) entirely and matches Helicone/Portkey/LiteLLM v1 shape (refresh via code change). Tracking issue: `[cost-governor-followup] pricing_override_path config surface`.
- **`history` estimation strategy** — rolling p95 of recent same-model calls. Cut from Wave 1 per S1 (§10) — non-deterministic across W9 replay, cold-start always falls back to `upper_bound` anyway. Tracking issue: `[cost-governor-followup] history estimator opt-in`.
- **Auto-downgrade on soft cap** — default behaviour is WARN-ONLY (no model swap) per R5 mitigation. Auto-downgrade requires opt-in `work_item.annotations.cost.allow_downgrade: true`; the spawner only honours `Verdict.DowngradeTo` when the annotation is present. Default-off prevents the silent-correctness-regression where a planner-chosen opus call is downgraded to haiku by cost-policy.
- **Progress-gated renewal** ("remaining budget is just permission to keep being wrong" — `wedge_cost_governor` §"Open problems"). Requires a verifier-progress signal that ships in a separate wedge.
- **Real-time deny at credential layer (Portkey virtual-key pattern)** — would require regatta to proxy Anthropic API calls. Out-of-scope; pre-call deny at scheduler tick is the regatta-native equivalent.

---

## §3 Architecture

### §3.1 Adopted-OSS scan (per `feedback_research_design_principles`)

Five candidate prior-art systems were scanned. Decision per the priority order (UX > reference-bar > best-practices > long-term repo+user benefit).

| Candidate | Adopt? | What we take | What we leave | Why |
|---|---|---|---|---|
| **[Helicone v3.x](https://docs.helicone.ai/features/advanced-usage/custom-rate-limits)** — `Helicone-RateLimit-Policy: 500;w=3600;u=cents;s=user` policy-header pattern | **Pattern only, not the runtime.** | Declarative policy *shape* (`limit;window;unit;scope`) reflected in our YAML `#CostGovernor` fields. Adopt the lesson that operator-readable policy beats a buried numeric constant. | The Helicone proxy. Regatta does NOT proxy Anthropic; we observe via the claude CLI stream-json. A proxy buys us pre-call deny at the credential layer but adds a network hop on every LLM call and breaks the claude CLI subprocess seam, which is regatta's primary integration shape. | UX wins: operator writes policy in YAML, not a curl header. Reference-bar: Helicone v3 is the reference for policy-shape — copy it. Best practices: declarative > imperative. Long-term: not coupling to Helicone's runtime preserves the swap-out story (operator can layer Helicone IN FRONT of regatta if they want a proxy). |
| **[Portkey Enterprise](https://portkey.ai/docs/product/ai-gateway/virtual-keys/budget-limits)** — virtual-key per-DAG with auto-expiry, 429 on cap | No (runtime); Yes (mental model). | The mental model: "budget is attached to a credential, deny at credential layer." Our `WorkItem.AgentID` is the regatta-native equivalent of Portkey's virtual key. | Enterprise pricing + the proxy. Portkey's pricing table reads `0 cents` for unsupported models (per its docs §Availability), which is the same trap our `pricing.Lookup` must guard against — hard-error on missing-pricing-row, not silent-zero. | Best practices: documented warning ("`0 cents` row = silent zero-deny"). We adopt the warning and hard-error in `pricing.Lookup`. |
| **[LiteLLM proxy / liteLLM ≥ 1.43](https://docs.litellm.ai/docs/proxy/users)** — org / team / key / user precedence tiers | No (runtime); Yes (anti-pattern lesson). | The hierarchy SHAPE (operator → DAG → work_item) and the EXPLICIT-PRECEDENCE-TABLE rule. | The proxy. Plus the silent-inheritance footgun reported in [BerriAI/litellm#12905](https://github.com/BerriAI/litellm/issues/12905) — "user budgets get silently ignored when keys have a `team_id`". | The bug is the primary lesson: define precedence in the YAML schema, not by inheritance. Our `#CostGovernor` makes precedence syntactically explicit (most-specific wins, no silent override). |
| **[OpenLLMetry / Traceloop SDK 0.16+](https://github.com/traceloop/openllmetry)** — OTel GenAI semconv emission for LLM calls | No (already covered by W6). | nothing — W6's `internal/orchestrator/spawner/genai.go` already emits the GenAI semconv attr set per W6 §3.4. | Whole SDK — OpenLLMetry instruments Python/Node SDKs; we instrument the claude CLI stream-json which is a different seam. | W6 ate this. We just add four `regatta.cost.*` attrs alongside the W6 `gen_ai.*` set on the SAME `llm_call` span. Single-source. |
| **[Anthropic Usage + Cost API](https://docs.anthropic.com/en/api/usage-cost-api)** — `/v1/organizations/usage_report/messages` endpoint | **Yes, verbatim** | The authoritative usage source for reconciliation. Direct HTTP call, no SDK adoption needed — single endpoint with `starting_at`/`ending_at`/`group_by`/`bucket_width` params. | Nothing — this is the canonical ground truth. The Anthropic admin key + admin scope is documented requirement (`x-api-key: $ANTHROPIC_ADMIN_KEY`). | This is the canonical-truth datasource. No bespoke "shadow scraping" possible. Operator must hold an admin key for reconciliation; fail-soft if missing (governor still deny-on-recorded works) per §3.4. |

**Net adoption summary:**
- **Build:** `internal/cost/{gate,estimate,pricing,reconcile,spend}/` — five sub-packages, ~1100 LoC total.
- **Adopt verbatim:** Anthropic Usage API HTTP shape (one endpoint, three query params, JSON shape per `Untitled (3)` in fetched docs).
- **Adopt pattern only:** Helicone policy-header *shape* → YAML config shape; Portkey hard-error-on-missing-row → `pricing.Lookup` returns `ErrPricingMissing`; LiteLLM explicit-precedence → schema-level rule.
- **Reject:** every runtime adoption — preserves swap-out story and avoids the proxy seam tax.

### §3.2 Pre-call deny gate seam — scheduler-tick position

The gate fires at `internal/orchestrator/scheduler/scheduler.go::Scheduler.Tick`, **between** the existing `applyApprovalGates` step (added by #114) and the existing `reserveFromSpawnable` step. Spec name: **step 0.6 — cost-governor pre-call deny**. Mirrors the step-0.5 approval-gate filter so the seam shape is identical and code-review-able alongside the existing pattern.

```
// Tick (post-this-wedge):
//   step 0.0  evalPendingEdges                           // existing
//   step 0.1  ExpireStaleLocks                           // existing
//   step 0.2  ListSpawnable                              // existing
//   step 0.5  applyApprovalGates(spawnable)              // existing (#114)
//   step 0.6  applyCostGovernor(spawnable)               // NEW (this wedge)
//   step 1.0  CountAgentsByLane                          // existing
//   step 2.0  reserveFromSpawnable(spawnable, …)         // existing
```

**Why scheduler tick, not spawner SupervisorLimits**: deny **before** spawn is structurally cheaper than deny-and-kill. The Waxell $47K incident lesson (`wedge_cost_governor` §"Patterns to steal" #8) is "intercept *before* next LLM call." A `claude` subprocess that has already been spawned has already paid for shell setup + workspace clone + MCP server-process bring-up + the first system-init message (which is itself an LLM call). Denying at the scheduler tick is the unique enforcement point that catches the *next* claude session before any of that.

Spawner SupervisorLimits (deferred MVP-1; per `wedge_roadmap_assessment` §"Current regatta state") remains the **fallback for already-running operators**: when a long-running spawn crosses the per-DAG cap mid-stream (e.g. accumulated spend during the spawn's `claude --continue` loop exceeds cap between tick boundaries), SupervisorLimits kills the subprocess via SIGTERM-then-SIGKILL (per `internal/orchestrator/spawner/claude.go:140` comment "SIGTERM-then-SIGKILL escalation lands with SupervisorLimits (#28)"). That is the kill-already-running path. THIS wedge owns the prevent-spawn path; SupervisorLimits owns the kill path. Both consume the same `cost.spend.BudgetState(ctx, scope)` reader function — the seam is the reader, not the enforcement primitive.

**Gate seam interface** (mandated pattern per `feedback_spec_pattern_authority`; deviation requires re-spawning this subagent):

```go
// internal/cost/gate/gate.go

// Gate is the cost-governor pre-call deny primitive. It is consumed by
// the scheduler (per-tick) and by the spawner SupervisorLimits (per
// running-agent post-stream tick). A single concrete type, no
// interface, per the substrate-spec S5 pattern.
type Gate struct {
    cfg      Config            // safety.cost.* values, resolved at process start
    pricing  pricing.Table     // hardcoded model→price map; override-loader deferred per S2 (§10)
    spend    *spend.Reader     // BudgetState reader against substrate
    estim    estimate.Estimator
    tracer   trace.Tracer
    log      *slog.Logger
}

// Verdict is the pre-call decision.
type Verdict struct {
    Allow            bool
    Reason           string            // "" if Allow; "cap_exceeded:dag:xyz" otherwise
    USDEstimate      float64           // upper-bound estimate that drove the decision
    SoftCapBreached  bool              // 80% threshold crossed; spawner may downgrade
    DowngradeTo      string            // suggested cheaper model when SoftCapBreached; "" if no downgrade applicable
    CapDAGUSD        float64           // for OTel attrs
    CapOperatorUSD   float64           // for OTel attrs
}

// Evaluate decides whether one work_item may spawn. Returns Allow=false when
// any active cap (dag, operator, work_item, global) would be breached by
// the estimated next call. Idempotent + side-effect-free EXCEPT for span
// attribute emission via cfg.Tracer.
func (g *Gate) Evaluate(ctx context.Context, w WorkItemScope) (Verdict, error)

// WorkItemScope holds the read-only scope keys for the decision. The
// scheduler builds this from state.WorkItem; the spawner SupervisorLimits
// builds this from the running agent's currently-known model + cumulative
// spend.
type WorkItemScope struct {
    WorkItemID  string
    DAGID       string
    OperatorID  string             // agent_id; same shape used in obs.KeyAgentID
    TenantID    string             // substrate.DefaultTenantID until W8
    Model       string             // request-target model (default per regatta.yaml)
    EstHint     estimate.Hint      // optional planner hint; nil falls back to upper-bound default
}
```

Scheduler step 0.6 calls `Gate.Evaluate` once per spawnable work_item. Items returning `Allow=false` drop from this tick's spawnable slice and persist in `planned` status; the next tick re-runs the gate (after the reconciler may have caught up). Items returning `SoftCapBreached=true` keep their spawn position but receive a `Verdict.DowngradeTo` model name that the spawner consumes via `Request.ModelOverride`. The downgrade is a spawn-time annotation, NOT a state mutation — preserves single-source-of-truth in `regatta.yaml`.

### §3.3 Token estimation pre-call

**Chosen strategy: `upper_bound` (default)** — `est_usd = (input_tokens_seen_so_far × price_in) + (max_tokens × price_out)`. Deterministic, conservative (always upper-bound), zero training cost.

**Why upper-bound, not predicted-mean:**
- **Conservative-is-correct for a budget.** Predicted-mean undercounts in the worst case → spawning continues past the actual cap → exactly the failure mode `wedge_cost_governor` §"Risks" "Reconciliation drift" calls out ("vendor invoice 24h late shows 15% overrun"). Upper-bound never undercounts; only worst case is "soft-cap-fires-pessimistically" which downgrades unnecessarily — acceptable user-facing failure mode.
- **Deterministic ⇒ replayable.** W9 replay (deferred) needs cost-decisions to be deterministic-against-same-inputs. Upper-bound is a pure function of (input_tokens, max_tokens, price_in, price_out). Predicted-mean depends on a rolling p95 of recent calls which is non-deterministic across replays.
- **Cold-start friendly.** First call ever for a new operator has no history; predicted-mean has nothing to estimate from. Upper-bound works on call #1.

**Token-count source for `input_tokens` pre-call:**
- For the FIRST `claude` invocation in a session: count tokens in the rendered prompt (system + user messages + tools manifest) via `claude --count-tokens` or, when not available, a deterministic fallback of `len(promptBytes)/4` (4-bytes-per-token rough OpenAI/Anthropic heuristic, documented as worst-case approximation). The `claude` CLI shipping `--count-tokens` is preferred and discovered at process start via a one-time probe.
- For the SECOND+ invocation in the same `claude --continue` session: cumulative `input_tokens` from the prior `result` event (already captured by W6 stream-json parser). This is exact, not estimated.

**`history` strategy DROPPED from Wave 1 per S1 (§10).** Per the deletion-default review pass: it falls back to `upper_bound` on cold-start (< 10 samples) anyway, introduces non-determinism that breaks W9 replay, and adds ~80 LoC for an opt-in operators may never reach for. Tracking issue: `[cost-governor-followup] history estimator opt-in`. When/if the issue is picked up, the field reappears as `safety.cost.estimation_strategy: history` (additive — no breaking change).

**Rejected alternative: deferred-post-stream estimation**. "Spawn anyway, charge after." This is the Waxell $47K trap — by the time you've charged, you've spent. Pre-call upper-bound is the load-bearing prevention pattern.

### §3.4 Post-hoc reconciliation against Anthropic Cost API (preferred) + Usage API (fallback)

**Authoritative source: Anthropic Cost API**, sibling endpoint to the Usage API documented at the same page. The Cost API returns USD directly — eliminating the R-A4 "pricing-applied-twice" defect where comparing `our_pricing(anthropic_tokens)` vs `our_pricing(our_tokens)` would yield zero drift even when our pricing table is wrong. Reconciler prefers Cost API; falls back to Usage API + local pricing application ONLY when Cost API is unavailable (and emits an `obs.EventCostReconcileFallback reason=cost_api_unavailable` WARN so operator knows the comparison is pricing-self-referential).

**Endpoint (Cost API, preferred):**
```
GET https://api.anthropic.com/v1/organizations/cost_report/messages
    ?starting_at=2026-06-01T00:00:00Z
    &ending_at=2026-06-01T01:00:00Z
    &bucket_width=1h
    &group_by[]=model
Headers:
  anthropic-version: 2023-06-01
  x-api-key:         ${SAFETY_COST_USAGE_API_KEY_ENV}   // env var name from config; default ANTHROPIC_ADMIN_KEY
  User-Agent:        regatta/<buildinfo.Version> (https://github.com/maydow/regatta)
```

**Endpoint (Usage API, fallback)** — verbatim per fetched docs:
```
GET https://api.anthropic.com/v1/organizations/usage_report/messages
    ?starting_at=...&ending_at=...&bucket_width=1h&group_by[]=model
```
Same auth headers.

The Anthropic admin key is the documented requirement for both endpoints (`x-api-key: $ANTHROPIC_ADMIN_KEY`). User-Agent per Anthropic's "Set a User-Agent header for integrations" recommendation.

**Reconciler tick:**
- **Cadence**: `safety.cost.reconcile_interval` default `1h`. Hourly is the smallest bucket Anthropic supports cleanly per `bucket_width=1h` in fetched docs. `1m` is mentioned as "near-real-time" only — we default to `1h` to avoid Anthropic-side rate-limit pressure and let operators tune.
- **Window**: reconciler runs at top-of-hour + `2min` jitter; fetches the just-closed hour's bucket. 2min pad is conservative for the docs' implicit bucket-settle behaviour.
- **Comparison (Cost API path, preferred)**:
  ```
  actual_usd   = SUM(response.bucket[i].cost_usd)  // Anthropic returns USD directly
  recorded_usd = SELECT SUM(json_extract(payload_json, '$.usd')) FROM substrate_events WHERE kind='token_spend' AND written_at >= bucket_start AND written_at < bucket_end
  delta_usd    = actual_usd - recorded_usd
  drift_pct    = abs(delta_usd) / max(actual_usd, 0.01)   // div-by-zero guard
  ```
  Drift > threshold means EITHER our stream-json parser missed events OR our pricing table is wrong OR Anthropic billed for something we did not see. Three distinct failure modes; the alert just signals "investigate."
- **Comparison (Usage API fallback path)**:
  ```
  actual_usd_fallback = SUM(bucket.input_tokens × p_in + bucket.output × p_out + bucket.cache_read × p_cr + bucket.cache_creation × p_cw) per model row
  ```
  Per R-A4 caveat: when this path is taken, pricing-table drift is invisible (both sides apply the same table). The fallback signal catches stream-json parser drift only. Documented limitation; operator runbook covers.
- **Emit**: substrate `events.kind='budget_reconciled'` row with `payload_json = {period_start, period_end, actual_usd, recorded_usd, delta_usd, drift_pct, model_breakdown[]}`.
- **Alert**: if `drift_pct > safety.cost.drift_alert_threshold_pct` (default 10%), emit `obs.EventCostDriftAlert` slog (auto-bridged to OTel via W6 T2). NEVER auto-correct — drift indicates a bug (stream-json parser miss, pricing-table stale, network drop) and silent correction would mask it.

**Failure-mode contract:**

| Scenario | Behaviour |
|---|---|
| Admin key env var unset | Reconciler logs `obs.EventCostReconcileSkipped reason=no_admin_key` at WARN; the pre-call deny gate still functions against recorded spend. No-op gracefully. |
| Anthropic Usage API returns 429 | Exponential backoff (1s × 2^n, capped 5min); persistent 429 ≥ 5 consecutive attempts emits `obs.EventCostReconcileFailing reason=rate_limited` at ERROR; reconciler keeps trying. |
| Anthropic Usage API returns 5xx / network down | Same exponential backoff; persistent failure ≥ 5 attempts emits `obs.EventCostReconcileFailing reason=upstream_down`. Pre-call deny gate continues uninterrupted against the most recent successful `budget_reconciled` row (Fold semantics). |
| Anthropic returns an incomplete bucket (settle race) | Reconciler retries the bucket on the next tick. `budget_reconciled` is `lww` per substrate v2 §4 — the corrected row wins. |
| Reconciler can never reach a bucket (offline > 24h) | Records a `[cost-governor-followup] backfill recipe` issue at impl-time. Backfill scope: extends `bucket_width=1d` query over the offline window, dedupes against existing `budget_reconciled` rows by `period_start`. |
| Anthropic invoice 24-72h late vs Usage API | Usage API is "near-real-time" per docs; invoice-level reconciliation is a separate concern (W12 territory). This wedge reconciles against Usage API, not against the invoice. |

### §3.5 Substrate hook

Two kinds already registered in substrate v2 §2.1 + §4 — this wedge OWNS the per-kind payload typed structs but does NOT modify the substrate enum or DDL.

**`payload_json` typed structs (`internal/cost/spend/payload.go`):**

```go
// Reused via dispatch table in internal/orchestrator/state/substrate/validate.go
// per substrate v2 §2.1 "Per-kind payload validation".

// TokenSpendPayload — substrate events.kind='token_spend'
type TokenSpendPayload struct {
    USD                   float64 `json:"usd"`                     // signed-payload-driven; reconciler-comparable
    Model                 string  `json:"model"`                   // gen_ai.response.model preferred; falls back to gen_ai.request.model
    InputTokens           int64   `json:"input_tokens"`            // gen_ai.usage.input_tokens
    OutputTokens          int64   `json:"output_tokens"`           // gen_ai.usage.output_tokens
    CacheReadTokens       int64   `json:"cache_read_tokens"`       // gen_ai.usage.cache_read.input_tokens
    CacheCreationTokens   int64   `json:"cache_creation_tokens"`   // for cache-write billing rate
    OperatorID            string  `json:"operator_id"`             // agent_id at call time
    DAGID                 string  `json:"dag_id"`                  // work_item.dag_id
    WorkItemID            string  `json:"work_item_id"`            // also present at substrate column level; payload mirror for fold-by-key
    PricingRev            string  `json:"pricing_rev"`             // commit-sha or table-version that priced this row
    CallID                string  `json:"call_id"`                 // gen_ai.response.id; idempotency anchor at app-layer
}

// BudgetReconciledPayload — substrate events.kind='budget_reconciled'
type BudgetReconciledPayload struct {
    PeriodStart    int64                 `json:"period_start"`    // unix ms; bucket boundary
    PeriodEnd      int64                 `json:"period_end"`      // unix ms; period_start + bucket_width
    ActualUSD      float64               `json:"actual_usd"`      // sum from Anthropic Usage API response
    RecordedUSD    float64               `json:"recorded_usd"`    // sum from substrate token_spend over same window
    DeltaUSD       float64               `json:"delta_usd"`       // actual - recorded
    DriftPct       float64               `json:"drift_pct"`       // abs(delta)/max(actual, 0.01)
    ModelBreakdown []ModelBreakdownRow   `json:"model_breakdown"` // per-model rows from response
    APIResponseSig string                `json:"api_response_sig"`// sha256 of the canonical Anthropic response body for audit replay
}
```

**Reducer semantics** (already locked in substrate v2 §4):
- `token_spend` = `append`. Idempotency at `UNIQUE(run_id, written_by, nonce)` substrate column. Cumulative spend = `SUM(payload->>'usd')` over filtered window. Reconciliation does NOT rewrite `token_spend` rows — it emits a `budget_reconciled` correction row.
- `budget_reconciled` = `lww` per `(tenant_id, period_start)`. Most-recent Usage-API row wins; replays do not double-count.

**Reader (`internal/cost/spend/reader.go`):**

```go
// Reader is the gate-side cumulative-spend reader. One-line wrapper over
// substrate Fold + SUM aggregation. Used by Gate.Evaluate.
type Reader struct {
    db    *sql.DB
    clock func() time.Time
}

// BudgetState returns cumulative recorded spend for a scope over a period.
// Reads substrate events.kind='token_spend' rows whose payload matches the
// scope and whose written_at falls in the period window. SQL is a single
// SELECT with json_extract — no app-side loop.
func (r *Reader) BudgetState(ctx context.Context, scope ScopeKey, period time.Duration) (USD float64, err error)

// LastReconciliation returns the most recent budget_reconciled row for
// the scope tenant. Used by drift-aware deny decisions.
func (r *Reader) LastReconciliation(ctx context.Context, tenantID string) (BudgetReconciledPayload, error)

type ScopeKey struct {
    Kind     ScopeKind   // dag | operator | work_item | global
    DAGID    string      // populated when Kind==dag
    OperatorID string    // populated when Kind==operator
    WorkItemID string    // populated when Kind==work_item
    TenantID string      // always populated; substrate.DefaultTenantID until W8
}
```

**Writer (`internal/cost/spend/writer.go`):**

```go
// RecordCall is the spawner-side per-LLM-call writer. Called from the
// W6 stream-json parser at the `result` event line (one call, one row).
// Constructs TokenSpendPayload, calls substrate.AppendEvent within the
// transaction the spawner already owns. Idempotency via substrate's
// UNIQUE(run_id, written_by, nonce) column; nonce derived from
// CallID + retry_seq.
func RecordCall(ctx context.Context, tx *sql.Tx, r CallRecord) error
```

The writer is invoked from `internal/orchestrator/spawner/genai.go` at the `result` event close, AFTER the W6 parser sets the `gen_ai.usage.*` span attrs (per W6 §3.4) but BEFORE the parser closes the `llm_call` span. Single transaction, same `ctx`. This wedge does not modify the W6 parser shape — it adds one line.

### §3.6 Config surface — backwards-compatible CUE extension

CUE schema change at `contracts/schemas/regatta.v1.cue` §`#Safety`:

```cue
// existing #Safety unchanged except for the new optional cost: field

#Safety: {
    destructive_ops_deny:    *[] | [...string]
    agent_creds_scope:       *"dev_only" | "test" | "scoped"
    iteration_cap:           *50 | int & >=1 & <=500
    spend_cap_usd:           *50 | int & >=0          // PRESERVED — MVP-2 behaviour intact
    spend_cap_usd_per_day:   *200 | int & >=0         // PRESERVED — MVP-2 behaviour intact
    canary_rate:             *0.05 | float & >=0 & <=0.2
    cost?:                   #CostGovernor            // NEW: optional, default unset = MVP-2 byte-equal
}

#CostGovernor: {
    per_dag_usd?:               int & >=0
    per_operator_usd?:          int & >=0
    per_work_item_usd?:         int & >=0
    period?:                    "1h" | "1d" | "7d" | "30d"
    soft_pct:                   *80 | int & >=50 & <=99
    reconcile_interval:         *"1h" | "5m" | "15m" | "30m" | "6h" | "24h"
    drift_alert_threshold_pct:  *10 | int & >=0 & <=100
    usage_api_key_env:          *"ANTHROPIC_ADMIN_KEY" | string
    // estimation_strategy + pricing_override_path DROPPED per S1 + S2
    // (§10). Wave 1 = upper_bound only; refresh via code change.
}
```

**Precedence rule (locked — addresses R-A2):**
> **EVERY configured cap at every scope is checked. The spawn is denied if ANY cap would be breached (most-restrictive-wins).** This matches the operator-intuitive AWS Budgets behaviour: setting both `per_operator_usd=50` AND `per_dag_usd=100` means "this operator cannot spend > $50 total AND no single DAG can spend > $100." Most-specific scope does NOT override broader scope — they stack as parallel guards. The legacy `safety.spend_cap_usd` field is treated as an additional implicit `per_work_item_usd` cap when `safety.cost` block is present (preserves MVP-2 mental model: the legacy cap is per-work-item by historical convention).

Mirrors LiteLLM's explicit-precedence-table rule (per §3.1 candidate scan) AND closes the LiteLLM bug #12905 footgun: no silent inheritance, no "more specific overrides broader" surprises — every cap is an independent guard.

**Validator at config-load time** (`internal/config/validate/`):
- Reject empty `safety.cost: {}` block (no caps configured). Error: `ErrCostBlockEmpty` with message "`safety.cost` is set but no caps are configured — either set ≥ 1 cap field or omit the cost block entirely." Closes I4 (empty-block overhead).
- Reject `safety.cost.per_dag_usd == 0` AND `safety.cost.per_operator_usd == 0` AND `safety.cost.per_work_item_usd == 0` AND `safety.spend_cap_usd == 0` (all explicit zero ⇒ deny-everything; almost certainly a typo). Error: `ErrCostCapsAllZero` with the message "all configured caps are zero — this would deny every spawn. To opt out of cost governance entirely, omit the `safety.cost` block; to allow unbounded, omit individual caps."

### §3.7 OTel hook — extending W6's `llm_call` span

This wedge extends the W6 T4 GenAI semconv attribute set on the `llm_call` span (per W6 spec §3.4) with four new `regatta.cost.*` attrs. The W6 spec §3.4 table already lists the `gen_ai.usage.*` attrs; these four are namespaced under `regatta.cost.*` (not `gen_ai.*`) so we do NOT shadow or fork the upstream semconv:

| New attribute | Type | Source | Notes |
|---|---|---|---|
| `regatta.cost.usd_estimate` | float64 | `Gate.Evaluate` Verdict.USDEstimate at scheduler tick | Set on the `tick`-level `cost.evaluate` span (a sibling of W6's `gate.evaluate`); also mirrored on the `llm_call` span when emitting `token_spend` so a single dashboard query can correlate estimate-vs-actual. |
| `regatta.cost.cap_dag_usd` | float64 | `Verdict.CapDAGUSD` | 0 (zero) is sentinel meaning "no cap configured" — operator doc names this. |
| `regatta.cost.cap_op_usd` | float64 | `Verdict.CapOperatorUSD` | Same sentinel. |
| `regatta.cost.allow` | bool | `Verdict.Allow` | Pinned on the `cost.evaluate` span (the gate decision span); pinned on the `llm_call` span as "this call was allowed by the gate at evaluate-time". |

**New `cost.evaluate` span**: opened by the scheduler at step 0.6 for every work_item evaluated. Parent = the W6 `tick` span. Carries the four attrs above plus `regatta.work_item_id`, `regatta.dag_id`, `regatta.operator_id`. Mirrors the W6 §4.1 hierarchy verbatim (one span per gate-class). This is one new span per spawnable per tick — bounded by `lane_cap × num_lanes` so cardinality is operator-known.

Cite W6 spec §3.4 (GenAI semconv attribute set) + §4.1 (span hierarchy). The wedge does NOT add a new top-level span; `cost.evaluate` slots into the W6 hierarchy as a `tick`-child gate span identical-in-shape to the existing `gate.evaluate`.

### §3.8 Pricing source

**Hardcoded Go table at `internal/cost/pricing/anthropic.go`** keyed on model SKU. Rationale: pricing is critical-path data that MUST be hermetic (no boot-time network call) and MUST be reviewable in the diff (any pricing change is a code-review-able event). Anthropic does NOT expose a programmatic pricing endpoint as of 2026-06-01 (the `/v1/models` endpoint lists models but not prices); operator-mediated runbook is required regardless of source.

```go
// internal/cost/pricing/anthropic.go

// Table maps model SKU → per-million-token USD rates.
// Source of truth: https://docs.anthropic.com/en/docs/about-claude/pricing
// Refresh runbook: docs/operator/cost-governor.md §"Pricing refresh".
// EVERY change to this table is a code-review event with a PR body
// citing the pricing-page screenshot or commit-pinned URL.
var Anthropic = map[string]Row{
    "claude-opus-4-7":     {15.00, 1.50, 18.75, 75.00, time.Time{}},
    "claude-sonnet-4-7":   { 3.00, 0.30,  3.75, 15.00, time.Time{}},
    "claude-haiku-4-5":    { 0.80, 0.08,  1.00,  4.00, time.Time{}},
    // … add rows as Anthropic ships SKUs; mark retired_after to keep history …
}

type Row struct {
    InputUSDPerMTok          float64
    CacheReadUSDPerMTok      float64
    CacheCreationUSDPerMTok  float64
    OutputUSDPerMTok         float64
    RetiredAfter             time.Time   // zero = still active; set when model is sunsetted
}

// Lookup returns the priced row for a model SKU. ErrPricingMissing
// when the SKU is unknown — caller MUST hard-fail; silent-zero is the
// Portkey trap per §3.1.
func Lookup(model string) (Row, error)
```

**Refresh runbook** (in `docs/operator/cost-governor.md` §"Pricing refresh"):
1. Quarterly cadence + ad-hoc on Anthropic pricing-page diff.
2. PR title: `feat(cost/pricing): refresh Anthropic rates YYYY-MM-DD`.
3. PR body cites pricing-page URL at commit-pinned time AND lists the diff lines (`+claude-opus-4-7: 15.00 → 16.00`).
4. PR triggers `make check` which runs `TestPricing_AllActiveSKUsHavePositiveRows` (no zero rows for active SKUs).
5. Ship via normal merge flow; renovate-bot does NOT auto-bump (human-in-the-loop required).

**Override path DEFERRED per S2 (§10).** Wave 1 ships with no override mechanism — pricing changes are PR-reviewed code changes. Operators needing Bedrock/Vertex/marketplace rates must fork the repo until the follow-up issue lands. Closes R14 (override-tampering surface) entirely. Tracking issue: `[cost-governor-followup] pricing_override_path config surface`.

**Why not Anthropic Pricing API**: per fetched docs, Anthropic exposes pricing in `pricing` field of `/v1/models` endpoint as a tier-table per Workspace but the field is documented as advisory and is NOT promised stable across SKU sunsets. Operator-reviewable hardcoded table is the safer source-of-truth; the `/v1/models` endpoint becomes a `[cost-governor-followup]` enhancement to auto-flag drift between table and API ("hardcoded row says $15/Mtok, API says $16/Mtok — file PR").

---

## §4 Data flow + state

The pre-call → call → record → reconcile loop, in prose:

```
T+0.0   scheduler.Tick begins.
T+0.5   applyApprovalGates filters spawnable (#114).
T+0.6   applyCostGovernor — for each spawnable wi:
          - g.Evaluate(ctx, scope)
          - estim.UpperBound(model, input_hint, max_tokens) → est_usd
          - spend.Reader.BudgetState(ctx, scope.DAG) → recorded_usd
          - if recorded_usd + est_usd > cap_dag: Verdict.Allow=false, drop from slice.
          - if recorded_usd + est_usd > soft_pct × cap_dag: Verdict.SoftCapBreached=true, DowngradeTo=cheaper SKU.
          - cost.evaluate span emits regatta.cost.* attrs.
T+1.0   scheduler.reserveFromSpawnable consumes allowed wi's (with optional DowngradeTo).
T+1.5   spawner.Spawn launches claude subprocess, passing ModelOverride from Verdict.DowngradeTo.

(in parallel, claude subprocess runs)

T+2.0   spawner.genai.go (W6 T4) parses stream-json.
T+2.1   `system.init` event → opens llm_call span, sets gen_ai.request.* attrs.
T+2.5   …LLM call returns, claude subprocess emits `result` event…
T+3.0   `result` event → parser sets gen_ai.usage.* attrs (W6).
T+3.0a  (NEW, this wedge) parser calls cost.spend.RecordCall(ctx, tx, …):
          - pricing.Lookup(model) → Row{p_in, p_out, …}
          - usd = (input_tokens × p_in + output_tokens × p_out + cache_read × p_cr + cache_creation × p_cw) / 1e6
          - constructs TokenSpendPayload, calls substrate.AppendEvent (tx-bound)
          - substrate appends row, signs HMAC, validates payload, UNIQUE(run_id, written_by, nonce) gates idempotency
T+3.1   parser closes llm_call span (W6 unchanged).

(later, asynchronously)

T+H+0   reconciler.Tick fires (cron at safety.cost.reconcile_interval).
T+H+0a  reconciler builds bucket window [H, H+interval).
T+H+0b  HTTP GET Anthropic /v1/organizations/usage_report/messages
        ?starting_at=H&ending_at=H+interval&bucket_width=1h&group_by[]=model
T+H+1   parse response, compute actual_usd per model breakdown row.
T+H+2   recorded_usd = substrate Fold token_spend WHERE written_at ∈ [H, H+interval) SUM.
T+H+3   delta_usd, drift_pct computed.
T+H+4   substrate.AppendEvent BudgetReconciledPayload (kind='budget_reconciled', lww per (tenant, period_start)).
T+H+5   if drift_pct > threshold: obs.EventCostDriftAlert at WARN.

(next scheduler tick)

T+H+0+s scheduler.Tick step 0.6 calls Gate.Evaluate.
T+H+0+t Reader.BudgetState now reads against the most recent reconciled view; drift is implicit
        because token_spend rows are unchanged — recorded view continues; reconciled view is a
        sibling channel an operator inspects.
```

**State invariants:**
- substrate `events.kind='token_spend'` is append-only forever. No row mutation. Replay-safe per substrate v2.
- substrate `events.kind='budget_reconciled'` is `lww` per `(tenant_id, period_start)`. Correction emits a new row with same `period_start` — the old row stays in the log for forensic audit (substrate is append-only) but `Fold` returns the LWW winner.
- The pre-call gate reads against recorded spend (not reconciled). Drift is surfaced as an operator alert; reconciliation does NOT reach back to mutate any cap decision. Decisions are deterministic-against-substrate.

---

## §5 Components (file-disjoint task breakdown)

Per S3 (§10) and `feedback_deletion_default`, T5 (config + scheduler hook) is folded into T1 because the CUE schema, validator, and step-0.6 hook all depend tightly on T1's `Gate.Evaluate` signature — separating them creates a cross-PR coordination tax for < 200 LoC. **5 file-disjoint tasks (was 6).**

| ID | Owner slot | Path | Depends-on | Description |
|---|---|---|---|---|
| **T1** | impl-1 | `internal/cost/gate/{gate,verdict,scope}.go` + `_test.go`; `internal/cost/spend/{reader,scope}.go` + `_test.go`; `contracts/schemas/regatta.v1.cue` (additive `cost?:` field); `internal/config/validate/cost.go` + `cost_test.go`; `internal/orchestrator/scheduler/scheduler.go` (one-method addition + one-line Tick step-0.6 hook) | W6 T1; substrate v2 Wave 1 | Gate seam (concrete type) + spend Reader + CUE/validator + scheduler step-0.6 wiring. Owns `Gate.Evaluate`, `Reader.BudgetState`, the config surface, the validator, and the scheduler hook. ~500 LoC. |
| **T2** | impl-2 | `internal/cost/estimate/{upper_bound,probe}.go` + `_test.go`; `internal/cost/pricing/{anthropic,lookup}.go` + `_test.go` | — | Upper-bound estimator + pricing table + `claude --count-tokens` probe. `history` strategy + `override` loader cut per S1 + S2. ~180 LoC. |
| **T3** | impl-3 | `internal/cost/spend/{writer,payload}.go` + `_test.go`; substrate validate.go dispatch-table addition; one-line addition to `internal/orchestrator/spawner/genai.go` `result`-event handler | W6 T4; T1; substrate v2 Wave 1 | Spawner post-stream `token_spend` emission. Adds typed `TokenSpendPayload` + `BudgetReconciledPayload` to substrate validate dispatch; one-line `RecordCall(...)` invocation in W6 parser. ~250 LoC. |
| **T4** | impl-4 | `internal/cost/reconcile/{tick,client,window,backoff}.go` + `_test.go`; `internal/cost/reconcile/testdata/anthropic_{cost,usage}_*.json` (fixtures) | T1; T3 (payload type) | Reconciler cron + Anthropic Cost API client (preferred) + Usage API fallback + exponential backoff + drift detector. ~350 LoC + fixtures. |
| **T5** | impl-5 | `docs/operator/cost-governor.md` + `cost_governor_test.go`; `examples/full/regatta.yaml` (add demo `cost:` block, commented-out by default) | T1, T2, T3, T4 | Operator-facing doc + example: env-var contract, precedence rule (most-restrictive-wins), drift-alert reading, soft-cap WARN semantics (downgrade is opt-in per work-item annotation), pricing-refresh runbook, OTel cardinality recommendation, dashboard-query examples. ~300 lines. |

Total: **5 file-disjoint tasks**. Implementer subagents work in parallel within waves (§10).

Owner-slot naming intentionally generic (`impl-N`) — dispatch step assigns each to a fresh subagent per `feedback_parallel_dispatch`.

**Shared-primitive owner** per `feedback_shared_primitive_owner`: T1 owns the `Verdict` + `ScopeKey` + `Gate` types; T3, T4, T5 import these and only these. T3 owns the `TokenSpendPayload` + `BudgetReconciledPayload` typed structs (and is OWNER for the substrate validate-dispatch addition); T1 + T4 import the payload types from T3. No duplicate-primitive risk.

---

## §6 Test plan (TDD-ready)

Each test below is a regression-guard for one named invariant. Listed by task. Implementer captures failing-test output before writing impl per `feedback_tdd_discipline`.

### T1 — Gate + Reader + Config + Scheduler hook

- `TestGate_NoConfig_AllowsAll` — `safety.cost` unset → Gate.Evaluate returns Allow=true for any scope. Pins MVP-2 byte-equal default.
- `TestGate_PerDAGCap_DeniesOverBudget` — set `per_dag_usd: 100`; recorded spend on DAG is $95; estimate is $10; gate denies with `Reason="cap_exceeded:dag:..."`.
- `TestGate_PerDAGCap_AllowsUnderBudget` — recorded $80, estimate $10, cap $100 → Allow=true.
- `TestGate_SoftCapBreached_WarnByDefault` — recorded $80, estimate $5, cap $100, soft_pct=80, NO annotation → Allow=true, SoftCapBreached=true, DowngradeTo="" (empty). Pins WARN-only default (R10 mitigation).
- `TestGate_SoftCapBreached_DowngradeOnlyWithAnnotation` — same scenario + `work_item.annotations.cost.allow_downgrade=true` → DowngradeTo="claude-haiku-4-5". Pins opt-in downgrade.
- `TestGate_PrecedenceMostRestrictiveWins` — per_dag_usd=100, per_operator_usd=50 → denial fires when EITHER cap would breach; e.g. recorded $48 on operator + $10 estimate → operator-cap denial even though DAG cap has $92 headroom. Pins R-A2 precedence rule.
- `TestGate_NilTracerFallsBackToGlobal` — Gate constructed with Config.Tracer=nil resolves to `otel.Tracer("internal/cost/gate")`; no panic.
- `TestGate_EmitsCostEvaluateSpan` — capture spans via test SpanRecorder; one `cost.evaluate` span per Evaluate call; attrs include `regatta.cost.usd_estimate`, `regatta.cost.allow`, `regatta.cost.cap_dag_usd`, `regatta.cost.cap_op_usd`, `regatta.cost.soft_breached`, `regatta.work_item_id`, `regatta.dag_id`, `regatta.operator_id`.
- `TestReader_BudgetState_SumOverWindow` — insert N substrate token_spend rows with payload.usd → BudgetState SUM matches.
- `TestReader_BudgetState_PeriodWindow_ExcludesStale` — rows outside `period` window NOT counted.
- `TestReader_LastReconciliation_LWWPerPeriod` — two budget_reconciled rows same period_start, latest written_at wins per substrate v2 §4.
- `TestReader_FiltersOnTenantID` — Reader query includes `WHERE tenant_id = ?`; cross-tenant rows NOT counted. Pins R9 W8-forward-fit.
- `TestCUEValidate_CostUnset_PassesAndDefaults` — `safety: {}` loads cleanly; cost is nil. Pins backwards-compat.
- `TestCUEValidate_EmptyCostBlock_Rejected` — `safety: { cost: {} }` → `ErrCostBlockEmpty`. Pins I4 (no overhead from empty block).
- `TestCUEValidate_AllCapsZero_RejectedWithMessage` — every cap field 0 → `ErrCostCapsAllZero`. Pins R7 misconfig-defense.
- `TestCUEValidate_SoftPctOutOfRange_Rejected` — soft_pct=49 → CUE rejects.
- `TestSchedulerTick_Step06_RunsBeforeReserve` — capture span tree; `cost.evaluate` spans appear between `gate.evaluate` (approval) and `reserveFromSpawnable`-internal spans.
- `TestSchedulerTick_DeniedWorkItemStaysPlanned` — wi with over-cap estimate → wi.Status remains 'planned' after Tick; next Tick re-evaluates.
- `TestSchedulerTick_HookIsNoopWhenCostUnset` — `safety.cost == nil` → applyCostGovernor returns input slice byte-equal, no substrate read, no span. Pins I6 zero-overhead invariant.
- `TestSchedulerTick_SoftCapDowngrade_PassesModelOverride` — soft-cap-breached wi with `allow_downgrade=true` annotation → reserveFromSpawnable receives Request.ModelOverride set to Verdict.DowngradeTo.

### T2 — Estimator + Pricing

- `TestPricing_AllActiveSKUsHavePositiveRows` — every row in `Anthropic` map with `RetiredAfter.IsZero()` has all four rates > 0. Pins the Portkey-trap defense.
- `TestPricing_Lookup_UnknownModelErrors` — Lookup("gpt-4") returns `ErrPricingMissing`. Hard-fail invariant.
- `TestEstimate_UpperBound_Deterministic` — same `(input_tokens, max_tokens, model)` → same USD across 100 invocations. Pins replay-safety.
- `TestEstimate_UpperBound_NeverUndercountsActual` — fuzz: random `(input, max_tokens)`; UpperBound(...) ≥ ActualCostFromKnownRows(input, output_tokens=anything≤max_tokens). Pins conservative invariant.
- `TestProbe_CountTokensClaudeCLI_DetectsCapability` — probe runs claude --count-tokens on a stub; if claude CLI supports it, probe returns ok; else returns fallback heuristic. No panic on missing-flag claude binary.
- `TestProbe_HeuristicFallbackAddsSafetyMargin` — heuristic mode active → estimator output is at least 50% above raw `len(bytes)/4 × p_in + max_tokens × p_out`. Pins I1 mitigation.

### T3 — Spawner post-stream `token_spend` emission

- `TestRecordCall_AppendsTokenSpendEvent` — invoke RecordCall against a substrate test DB → one row exists with kind='token_spend', payload matches CallRecord verbatim.
- `TestRecordCall_PricingMissingErrorsHard` — RecordCall with unknown model → returns ErrPricingMissing; NO substrate row written; span attribute `regatta.cost.error=pricing_missing` set on llm_call span.
- `TestRecordCall_PayloadIncludesAllFields` — payload_json has all 10 TokenSpendPayload fields populated.
- `TestRecordCall_IdempotentOnReplay` — same CallID twice → second insert returns substrate ErrReplay (UNIQUE nonce collision); single row exists. Pins replay-safety.
- `TestGenAIParser_InvokesRecordCallOnResultEvent` — feed stream-json fixture; assert RecordCall was called exactly once per `result` event with parser-derived fields.
- `TestGenAIParser_NoRecordCallWhenStreamJsonOff` — parser disabled → no RecordCall, no substrate row. Pins legacy-flag-off invariant (mirrors W6 T4 test).
- `TestSubstrate_TokenSpendPayloadValidates` — substrate validate dispatch table accepts well-formed payload, rejects malformed (missing required field → ErrInvalidPayload).

### T4 — Reconciler + Anthropic Cost/Usage API client

- `TestReconciler_TickEmitsBudgetReconciled_CostAPIPreferred` — stub Cost API returns canned response with `cost_usd` field; Tick parses, writes one budget_reconciled row with correct payload; Usage API NOT called.
- `TestReconciler_FallsBackToUsageAPI_WhenCostAPI404` — Cost API returns 404; reconciler retries via Usage API + local pricing application; emits `obs.EventCostReconcileFallback reason=cost_api_unavailable` at WARN; row written.
- `TestReconciler_DriftBelowThreshold_NoAlert` — actual=$100, recorded=$95, drift=5%; threshold=10%; row emitted, NO obs.EventCostDriftAlert.
- `TestReconciler_DriftAboveThreshold_EmitsAlert` — actual=$100, recorded=$80, drift=20%; threshold=10%; row emitted, ONE obs.EventCostDriftAlert at WARN.
- `TestReconciler_DriftAlertDedupedAcrossTicks` — same period_start, same drift_pct rounded to 2dp across 3 consecutive ticks → exactly ONE alert (rubric A4). Pins anti-noise.
- `TestReconciler_AdminKeyUnset_LogsAndSkips` — env unset → no HTTP call, obs.EventCostReconcileSkipped at WARN, no row. Pins fail-soft.
- `TestReconciler_429Backoff_RespectsRetryAfterHeader` — stub returns 429 with `retry-after: 12` three times then 200; reconciler honours header (mock clock asserts wait ≥ 12s); succeeds on 4th try. Pins R3 mitigation + A3 rubric.
- `TestReconciler_Network5xx_KeepsTickingAndNeverPanics` — stub returns 500 persistently; reconciler emits obs.EventCostReconcileFailing after 5 attempts; next Tick continues. No goroutine leak.
- `TestReconciler_LWWCorrectionEmitsNewRow` — first Tick writes reconciled row; second Tick same period writes ANOTHER row; Fold returns the later one (LWW per substrate v2 §4).
- `TestReconciler_BucketWindowMatchesAnthropicSpec` — Tick fires at top-of-hour+2min; HTTP request `starting_at` + `ending_at` align with just-closed hour; bucket_width=1h.
- `TestReconciler_AnthropicResponseSig_IsSHA256OfCanonicalBody` — payload.api_response_sig is sha256-canonical(response body). Pins audit-replay invariant.
- `TestReconciler_NeverLogsKeyValue` — across every error path, log capture asserts the admin key value never appears in any record. Pins R15.

### T5 — Operator doc

- `TestCostGovernorDoc_LinksValid` — markdown link checker passes (`make doc-check`).
- `TestCostGovernorDoc_DocumentsAllConfigFields` — grep-based test asserts every field in `#CostGovernor` CUE schema appears in the doc.
- `TestCostGovernorDoc_PricingRefreshRunbookExists` — heading "Pricing refresh" present + cites Anthropic pricing-page URL.
- `TestCostGovernorDoc_OTelCardinalityGuidanceExists` — heading "OTel cardinality" present + cites the `OTEL_TRACES_SAMPLER` env var + W6 R6. Pins R14 mitigation.
- `TestCostGovernorDoc_PrecedenceRuleIsMostRestrictiveWins` — doc body contains the most-restrictive-wins precedence statement verbatim. Pins R-A2.

---

## §7 Grade rubric (B / A / A+ — tool-checkable)

Per `feedback_grade_rubric`. Each item has a `Verify:` clause naming the command.

### B (floor — ships)

- **B1.** All §6 tests green. **Verify:** `make check && go test ./internal/cost/...`.
- **B2.** `safety: {}` config keeps MVP-2 behaviour byte-equal: no pre-call deny, no reconciler goroutine, no substrate write, no cost.evaluate span. **Verify:** `TestGate_NoConfig_AllowsAll` + manual `regatta serve` with `lsof -p <pid>` showing no Anthropic Usage API socket.
- **B3.** Substrate `token_spend` rows are append-only — no UPDATE/DELETE in `internal/cost/`. **Verify:** `grep -rE '\b(UPDATE|DELETE)\b' internal/cost/ | grep -v _test.go` returns zero matches.
- **B4.** `make check` clean (doc-check, prose-dup, vet, lint, tidy-check, mod-verify, go-check, property-test). **Verify:** `make check` exit 0.
- **B5.** PR body carries `release-notes` block with `[FEATURE]` category. **Verify:** `scripts/pr-lint.sh` exit 0.
- **B6.** Every production `*.go` added ships with a matching `*_test.go` in the same PR. **Verify:** `scripts/check-tdd.sh` exit 0.
- **B7.** Pricing table has zero zero-rate rows for active SKUs (Portkey-trap defense). **Verify:** `TestPricing_AllActiveSKUsHavePositiveRows`.
- **B8.** CUE config additions are backwards-compatible — every existing MVP-2 regatta.yaml in `examples/`, `cmd/regatta/init_assets/`, and `internal/config/validate/load_test.go` validates without modification. **Verify:** `go test ./internal/config/...` + `grep -L 'safety.cost' examples/*/regatta.yaml | xargs -I{} regatta validate-config {}` exit 0.

### A (target — expected outcome)

- **A1.** B + adversarial-reviewer subagent runs against the spec + diff and finds zero unaddressed issues. **Verify:** reviewer subagent output explicitly attests "no unresolved findings" per `feedback_adversarial_review`.
- **A2.** Pricing source-of-truth refresh runbook documents quarterly cadence + ad-hoc-on-diff trigger + commit-pinned URL citation rule. **Verify:** `TestCostGovernorDoc_PricingRefreshRunbookExists` + manual diff review.
- **A3.** Anthropic Usage API client respects `retry-after` header on 429 per the rate-limits doc. **Verify:** `TestReconciler_429Backoff` + a new `TestReconciler_RespectsRetryAfterHeader` test.
- **A4.** Drift alert fires exactly once per drift event (not per Tick). Deduplication anchor = `(period_start, drift_pct rounded-to-2dp)`. **Verify:** `TestReconciler_DriftAlertDedupedAcrossTicks`.
- **A5.** OTel attribute set on `cost.evaluate` span matches §3.7 verbatim (4 cost attrs + 3 regatta-scope attrs). **Verify:** `TestGate_EmitsCostEvaluateSpan`.
- **A6.** Substrate Phase-B shadow-write reconciliation runbook (if substrate hasn't yet hit Phase C for `token_spend`) documents the divergence-check threshold and recovery path. **Verify:** `docs/operator/cost-governor.md` §"Substrate shadow phase" cites substrate spec §3 Phase B + concrete threshold.
- **A7.** Every named-but-deferred sub-decision filed as tracking issue with title prefix `[cost-governor-followup]`. **Verify:** `gh issue list --label cost-governor-followup` lists ≥ 13 issues (per-tenant budgets, Stripe webhook, predictive forecasting, mid-DAG kill+compensation, cache-aware budgeting, cross-fleet MCP attribution, Bedrock pricing, Pricing API auto-flag, backfill recipe, progress-gated renewal, history estimator opt-in [S1], pricing_override_path config surface [S2], admin-key-vault integration [R15], spawner reconciliation outbox).
- **A8.** `Gate` + `Reader` + `Reconciler` injection pattern uniform with W6 Config.Tracer / Config.Logger normalization. One pattern, no overload drift. **Verify:** `grep -RnE 'Config\s+struct' internal/cost/ | wc -l` ≥ 4 + `grep -RnE 'WithTracer\(' internal/cost/ | wc -l` returns 0. Pins `feedback_spec_pattern_authority`.

### A+ (stretch — exceptional)

- **A+1.** A + property test sweeps 200 synthetic spend timelines (random per-DAG/operator caps, random spend distributions, random reconciler delays) and asserts (a) no Allow=true after recorded > cap; (b) no drift alert misfire; (c) no goroutine leak on reconciler shutdown. **Verify:** `make property-test` + `goleak.VerifyNone` clean.
- **A+2.** Performance baseline: `BenchmarkGateEvaluate` shows ≤ 1ms p95 per work_item evaluation (single SUM-aggregate substrate read). **Verify:** `make bench` shows BenchmarkGateEvaluate p95 ≤ 1ms; pinned to substrate v2's §7 napkin math.
- **A+3.** E2E test that spins up a stub Anthropic Usage API HTTP server in docker-compose + a stub LLM that emits canned stream-json + a 60s synthetic DAG, asserts (a) `token_spend` rows accumulate per call; (b) `budget_reconciled` row appears after one tick; (c) over-cap DAG denies the next spawn. **Verify:** `go test -tags e2e_cost ./internal/cost/...` exit 0 in CI.
- **A+4.** Dashboard query examples in operator doc include verified Honeycomb + Grafana + Jaeger queries that drop into the operator's existing observability stack (per W6 dashboard-friendly framing). **Verify:** PR body includes one screenshot per backend.
- **A+5.** Pricing-table-drift autocheck: a `tools/check-anthropic-pricing-drift.go` script compares the hardcoded table against the most-recent successful `budget_reconciled` payloads' implied rates (Cost API path: `cost_usd ÷ tokens`; Usage API path: skipped — self-referential). Warns at PR-time if drift > 5%. **Verify:** script lands as a tool; CI invokes nightly; PR body cites two consecutive nights' clean output.

---

## §8 File-disjoint task breakdown (parallel-dispatch table)

Copy this table into the Wave-0 dispatch prompt. Each task is owned by one implementer subagent; the listed Path slice is its exclusive write scope. Tests for a task live under the task's package.

| Task | Owner | Path (exclusive) | Depends-on | Effort |
|---|---|---|---|---|
| T1 Gate + Reader + Config + Scheduler hook | impl-1 | `internal/cost/gate/{gate,verdict,scope}.go`, `_test.go`; `internal/cost/spend/{reader,scope}.go`, `_test.go`; `contracts/schemas/regatta.v1.cue` (additive `cost?:` field); `internal/config/validate/cost.go`, `cost_test.go`; `internal/orchestrator/scheduler/scheduler.go` (step 0.6 hook, < 30 LoC delta) | W6 T1; substrate v2 Wave 1 | M-L |
| T2 Estimator + Pricing | impl-2 | `internal/cost/estimate/{upper_bound,probe}.go`, `_test.go`; `internal/cost/pricing/{anthropic,lookup}.go`, `_test.go` | — | S |
| T3 Spawner emit | impl-3 | `internal/cost/spend/{writer,payload}.go`, `_test.go`; `internal/orchestrator/state/substrate/validate.go` (dispatch-table addition); `internal/orchestrator/spawner/genai.go` (one-line addition only) | W6 T4; T1; substrate v2 Wave 1 | M |
| T4 Reconciler | impl-4 | `internal/cost/reconcile/{tick,client,window,backoff}.go`, `_test.go`; `internal/cost/reconcile/testdata/anthropic_{cost,usage}_*.json` (fixtures) | T1; T3 (payload type) | M |
| T5 Operator doc | impl-5 | `docs/operator/cost-governor.md`, `cost_governor_test.go`; `examples/full/regatta.yaml` (commented demo block) | T1, T2, T3, T4 | S |

**Inter-task seam contracts (load-bearing — implementer MUST honour exactly):**

- T1 exports `gate.Gate`, `gate.Verdict`, `gate.WorkItemScope`, `gate.Config`, `spend.Reader`, `spend.ScopeKey`, sentinels `ErrCapExceeded`, `ErrCostBlockEmpty`, `ErrCostCapsAllZero`. T3/T4/T5 import these and only these.
- T3 owns `spend.TokenSpendPayload`, `spend.BudgetReconciledPayload`, `spend.CallRecord`, `spend.RecordCall(ctx, tx, r)`. T1 reads `TokenSpendPayload`; T4 reads/writes `BudgetReconciledPayload`. Per `feedback_shared_primitive_owner`.
- T2 exports `pricing.Lookup(model) (Row, error)`, sentinel `pricing.ErrPricingMissing`. T1 + T3 call only these.
- **W6 T4 tx-export prereq (closes I5):** T3 MUST verify W6 T4's `internal/orchestrator/spawner/genai.go` `result`-event handler has access to a `*sql.Tx` at the call site. If T4 holds the tx internally and does not expose it at the seam, T3 opens a tiny W6 T4 amendment PR FIRST (call-site refactor: thread `tx` from the caller of `parseResultEvent` down to the handler). The amendment is mechanical (no behaviour change) and reviewed alongside T3's PR.
- T3 modifies `internal/orchestrator/spawner/genai.go` by adding EXACTLY ONE statement (`if err := cost.spend.RecordCall(ctx, tx, …); err != nil { /* log + set span attr */ }`) inside the `result`-event handler. NO other change to that file (excluding the I5 tx-export amendment which is a separate W6 T4 PR). T3's primary diff for genai.go must be ≤ 6 lines.
- T1 modifies `internal/orchestrator/scheduler/scheduler.go` by adding EXACTLY ONE function call (`spawnable, err = s.applyCostGovernor(ctx, spawnable)`) between `applyApprovalGates` and `reserveFromSpawnable`, plus a method body for `applyCostGovernor`. The call is conditionally registered — `applyCostGovernor` short-circuits to identity when `s.cfg.Safety.Cost == nil` (closes I6 — zero overhead when block unset).

---

## §9 Risk preemption (adversarial red-team)

### R1 — Pricing drift (table stale vs Anthropic actual)

**Threat:** Anthropic ships a price-rise; our hardcoded table is now wrong; pre-call estimate undercounts; budget under-denies; operator overshoots cap.
**Mitigation:** A+5 drift autocheck (script compares hardcoded table against `budget_reconciled` implied rates via Cost-API path, warns at PR-time). Plus refresh runbook (quarterly + on-diff). Plus the reconciler emits `obs.EventCostDriftAlert` when actual >> recorded which is the runtime smoke alarm. (Operator escape hatch via override-file deferred per S2 — fork-the-repo until the follow-up lands.)
**Verify:** A+5 rubric entry + `TestReconciler_DriftAboveThreshold_EmitsAlert`.

### R2 — Race between pre-call deny and concurrent calls (TOCTOU)

**Threat:** Scheduler step 0.6 reads cumulative spend at time T; a parallel spawn from a previous tick writes more `token_spend` rows between T and the new spawn at T+δ; total spend exceeds cap before the new spawn fires.
**Mitigation:** Two layers. (1) The gate budget includes the upper-bound estimate of the CURRENT call before adding to recorded — so the decision is "would recorded + this_estimate cross cap" — pessimistic by design. (2) For long-running spawns whose mid-stream spend crosses cap between ticks, SupervisorLimits is the fallback (per §3.2 — kill the running subprocess via SIGTERM-then-SIGKILL). Pre-call deny prevents NEW spawns; SupervisorLimits prevents continued spending by running spawns. Together they bound overshoot to (one spawn's first call) + (one tick's worth of in-flight spend per running spawn).
**Residual exposure:** if N spawns clear the gate in the same tick, each consuming est_n, total worst-case overshoot is `Σ est_n`. Documented in operator runbook; per-tick spawn count is bounded by lane_cap so the operator can model worst-case.
**Verify:** `TestGate_BudgetIncludesEstimateNotJustRecorded` + operator doc §"Worst-case overshoot model".

### R3 — Anthropic Usage API rate limit (reconciler 429s persistently)

**Threat:** Org-level rate-limit on the admin endpoint trips; reconciler can never settle; drift goes undetected for hours.
**Mitigation:** Exponential backoff (1s × 2^n, cap 5min, never give up). `retry-after` header honoured (A3 rubric). Five consecutive failures → ERROR-level slog + `[cost-governor-followup]` alarm. The pre-call deny continues to work against recorded spend independently — degraded but not broken. Operator runbook documents "if reconciler ERROR persists > 4h, file ticket with Anthropic + temporarily raise drift_alert_threshold_pct to suppress noise."
**Verify:** `TestReconciler_429Backoff` + `TestReconciler_Network5xx_KeepsTickingAndNeverPanics`.

### R4 — Substrate write-skew under load (token_spend missed)

**Threat:** Spawner crashes between W6 parser's `result` event and the `cost.spend.RecordCall` substrate write; token_spend row missing; pre-call deny under-counts; cap silently breaches.
**Mitigation:** RecordCall lives INSIDE the W6 parser's existing transaction (per §3.5 + T3 seam contract — single ctx, single tx). If the substrate INSERT fails, the parser does NOT close the `llm_call` span — the operator sees an open span as the smoke alarm. The reconciler catches the gap on the next Tick (recorded < actual by exactly the missed call). Then either (a) the operator backfills via `regatta cost backfill <run_id>` CLI [tracking issue] or (b) accepts the gap as a drift event already raised.
**Verify:** `TestRecordCall_AppendsTokenSpendEvent` (positive path); `TestGenAIParser_RecordCallFailureLeavesSpanOpen` (negative path) — failing-by-design test that proves the smoke alarm is observable.

### R5 — Reconciler clock drift

**Threat:** Reconciler host clock drifts; bucket window shifts; Anthropic Usage API returns wrong slice; recorded vs actual compared against wrong period.
**Mitigation:** Reconciler reads `time.Now().UTC()` once at the top of Tick and uses that single value for both the substrate Fold window and the Anthropic request `starting_at`/`ending_at` — internally consistent regardless of clock drift. Drift relative to Anthropic's clock is bounded by Anthropic's bucket-settling behaviour (we pad +2min). For host clock skew > 5min, the substrate's `lastWrittenAt` monotonicity check (per substrate v2 §8) catches the worst case. Plus: operator runbook recommends ntpd / chrony — non-negotiable for any production deployment.
**Verify:** `TestReconciler_BucketWindowMatchesAnthropicSpec`; operator doc §"Clock-sync requirement".

### R6 — Anthropic Usage API down ("can't reconcile")

**Threat:** Anthropic infrastructure outage; reconciler returns 5xx for hours/days; drift accumulates undetected.
**Mitigation:** Per §3.4 failure-mode table: pre-call deny gate continues uninterrupted against recorded spend (which is local + always available). Reconciler logs ERROR after 5 attempts; surfaces via existing alerting pipeline (slog → OTel logs → operator's Honeycomb/Datadog/PagerDuty). Reconciler retries forever; once Anthropic recovers, missed buckets are caught up on subsequent ticks (each Tick fetches the most-recent-bucket; for missed buckets, a CLI `regatta cost reconcile --since 6h` does backfill [tracking issue]). Substrate `budget_reconciled` reducer is LWW — backfill rows are first-class, not corrections-on-top-of-corrections.
**Verify:** `TestReconciler_AdminKeyUnset_LogsAndSkips` (degenerate); `TestReconciler_Network5xx_KeepsTickingAndNeverPanics` (positive).

### R7 — Cap misconfigured to 0 (deny everything?)

**Threat:** Operator typos `per_dag_usd: 0` intending unset; gate denies every spawn; orchestrator stalls.
**Mitigation:** CUE validator at config load rejects "all caps zero" combination (`ErrCostCapsAllZero` per §3.6 + B-rubric `TestCUEValidate_AllCapsZero_RejectedWithMessage`). Error message names the exact field combination and the omit-to-disable workaround. Documented in operator doc §"Cap-zero trap".
**Verify:** `TestCUEValidate_AllCapsZero_RejectedWithMessage`.

### R8 — Interaction with W6 OTel spans (parent / child confusion)

**Threat:** `cost.evaluate` span opens under `tick` (per §3.7); if W6 evolves the hierarchy, `cost.evaluate` orphans or duplicates.
**Mitigation:** §3.7 explicitly anchors `cost.evaluate` to the W6 §4.1 hierarchy as a tick-child gate-class span — identical shape to `gate.evaluate`. Implementer T1 uses `cfg.Tracer.Start(ctx, "cost.evaluate", trace.WithSpanKind(trace.SpanKindInternal))` where `ctx` is the tick-context (so parent is the W6 `tick` span). If W6 reshapes the hierarchy in a future wave, the seam stays correct because we depend on `ctx`-propagation, not on a hardcoded parent.
**Verify:** `TestGate_EmitsCostEvaluateSpan` + a span-tree assertion that `cost.evaluate.Parent == tick`.

### R9 — Multi-tenant isolation (W8 hookup deferred but planned)

**Threat:** W8 ships `tenant_id` on every substrate read; cost-governor reads MUST filter by tenant_id; if not, cross-tenant cap leakage.
**Mitigation:** Every `Reader.BudgetState` SQL query already includes `WHERE tenant_id = ?` (substrate spec §6 lint enforces). Today (pre-W8) tenant_id = `substrate.DefaultTenantID = "default"` for all rows — every read filters by `"default"`. When W8 ships, the spawner sets real `tenant_id` on `work_items` and the gate's `ScopeKey.TenantID` flows through unchanged. One-line swap, no architectural change. Tracking issue `[cost-governor-followup] W8 tenant propagation cutover` filed at impl-time.
**Verify:** `tools/lint-substrate-queries.go` (substrate spec §6) catches any unfiltered read in `internal/cost/`. Plus a regression `TestReader_FiltersOnTenantID`.

### R10 — Soft-cap downgrade thrash + silent-correctness regression

**Threats:**
1. (Thrash) Soft cap at 80% breached → downgrade Sonnet→Haiku; next call's lower spend pushes spend back below 80% → upgrade back to Sonnet; oscillation.
2. (Correctness) Planner chose opus for a capability-bound task; cost-policy silently downgrades to haiku; output is wrong; operator never sees the swap.
**Mitigations:**
1. (Thrash) Downgrade is a one-way ratchet within a `period` — once SoftCapBreached fires for a `(scope, period)` tuple, all subsequent spawns in that period stay downgraded. Substrate-derived: `Gate.Evaluate` checks `MAX(spend_pct over period)`, not `current spend_pct`.
2. (Correctness) **Auto-downgrade is OPT-IN per work_item** via annotation `work_item.annotations.cost.allow_downgrade: true`. Default OFF. Without the annotation, soft cap is WARN-ONLY: `Verdict.SoftCapBreached=true` triggers `obs.EventCostSoftCapBreached` slog + `regatta.cost.soft_breached=true` span attr, and the spawn proceeds with the planner-chosen model. With the annotation, `Verdict.DowngradeTo` is passed to the spawner via `Request.ModelOverride`. Closes the silent-correctness vector.
**Verify:** `TestGate_SoftCapBreached_RatchetsWithinPeriod` + `TestGate_SoftCapBreached_DowngradeOnlyWithAnnotation` + `TestGate_SoftCapBreached_WarnByDefault`.

### R11 — `claude --count-tokens` missing on operator's CLI version

**Threat:** Probe at process start fails (older claude CLI); fallback heuristic (`len(bytes)/4`) under-counts → estimate is wrong → gate over-allows.
**Mitigation:** Heuristic is documented as "conservative-pessimistic at 4 bytes/token but with ±20% variance"; cumulative estimate adds 25% safety margin when heuristic is in use (calibrated against fetched OpenAI cookbook tokenizer-vs-bytes-per-token analysis). Process startup logs `obs.EventCostTokenProbe = (mode=claude_cli|heuristic)` at INFO so operator sees which mode is active. Operator-doc bolds "upgrade claude CLI for accurate pre-call estimates."
**Verify:** `TestProbe_HeuristicFallbackAddsSafetyMargin`.

### R12 — Idempotency key collision under retry storm

**Threat:** Spawn retries cause same logical LLM call to be priced twice; double-spend recorded; cap denies legitimate next spawn.
**Mitigation:** `RecordCall` derives nonce from `sha256(CallID || retry_seq)` truncated to 16 bytes. CallID is `gen_ai.response.id` (Anthropic-issued, globally unique per response). Retry that produces a fresh `gen_ai.response.id` (new request) gets new nonce → new substrate row (correct: that IS a new call). Retry of the SAME `gen_ai.response.id` (idempotent retry against an already-completed call) collides at `UNIQUE(run_id, written_by, nonce)` → substrate ErrReplay → no double-write. Per substrate v2 §2.1 + §10 #11.
**Concurrent-writer note (closes R-A3):** the substrate UNIQUE index is `(run_id, written_by, nonce)`. Two parallel processes recording the same CallID with `retry_seq=0` would NOT collide if `written_by` differs. In regatta this is structurally impossible: one spawn per work_item, one work_item per spawner instance at a time (scheduler-tick lane caps enforce); `written_by` is the spawner process identifier so a single response.id is always recorded by a single `written_by`. Documented in `internal/cost/spend/writer.go` godoc + asserted by `TestRecordCall_OneWrittenByPerCallID`.
**Verify:** `TestRecordCall_IdempotentOnReplay` + `TestRecordCall_OneWrittenByPerCallID`.

### R13 — Spawner subprocess never emits `result` event (claude crashes mid-call)

**Threat:** Subprocess SIGKILLs during the LLM call; no `result` event; no `token_spend` row; the spend ACTUALLY happened (Anthropic billed) but regatta doesn't know.
**Mitigation:** This is the "drift-detected-by-reconciler" path — the next reconciliation tick sees `actual_usd > recorded_usd` for the bucket containing the lost call. Drift alert fires. Operator runbook documents "drift > 10% with no obvious failed-call alert ⇒ look for SIGKILL'd spawner subprocesses." Plus a `[cost-governor-followup] spawner reconciliation outbox` issue files the runtime fix (W6 parser emits a `cost.spend_unknown` event on SIGKILL detection so the reconciler has a hint).
**Verify:** Doc covers + tracking issue filed.

### R14 — `cost.evaluate` span cardinality explosion at high tick rate (closes I2)

**Threat:** Default `lane_cap=10 × num_lanes=5 × tick_interval=1s` produces 50 `cost.evaluate` spans/sec ≈ 4.3M spans/day. Operators on Honeycomb / Datadog / SaaS-priced-by-span quickly hit per-span budgets.
**Mitigation:** Operator doc §"OTel cardinality" bolds the recommendation: for deployments with `lane_cap × num_lanes > 20` OR `tick_interval < 5s`, set `OTEL_TRACES_SAMPLER=parentbased_traceidratio` with ratio ≤ 0.1 in the W6 SDK config. The reconciler-cron itself is low-frequency (default `1h`) so reconcile spans are bounded. Drop-sampled `cost.evaluate` spans do not affect enforcement correctness (decisions read substrate, not spans). The W6 R6 sampler-config-trap mitigation applies verbatim: spend numbers come from substrate `token_spend` events (unsampled — slog records always go through the bridge unfiltered), not from spans.
**Verify:** `docs/operator/cost-governor.md` §"OTel cardinality" cites W6 R6 + names the env var + the threshold.

### R15 — Anthropic admin key in process memory (closes I3)

**Threat:** If the regatta process is compromised, the admin key (Anthropic Usage/Cost API auth) is exfiltratable. Admin key has broader scope than per-workspace API keys — leakage enables org-wide spend visibility for the attacker.
**Mitigation:** Key is read from operator-named env var (`safety.cost.usage_api_key_env`, default `ANTHROPIC_ADMIN_KEY`). Process never logs the key value (only logs the env var NAME at boot + a sha256 fingerprint at WARN-on-rotation-detected). Wave 1 documentation covers env-var-only loading; vault-loading patterns (systemd-credentials, k8s-secret-projected-volume, 1Password-CLI) deferred to W8 RBAC's secret-management story. Tracking issue: `[cost-governor-followup] admin-key-vault integration` cites systemd-credentials + k8s + 1Password as candidate adopt patterns.
**Verify:** `TestReconciler_NeverLogsKeyValue` (grep-style assertion on captured log output across all error paths) + `[cost-governor-followup]` issue filed.

---

## §10 Wave breakdown

Three waves, file-disjoint within each wave. Each wave clears `make check` and adversarial-reviewer subagent before the next dispatches per `feedback_adversarial_review` + `feedback_review_before_automerge`.

**What got smaller (per `feedback_deletion_default` — applied by adversarial-reviewer pass 1):**
- **S1:** `history` estimation strategy DROPPED from Wave 1 → tracking issue. Always-upper-bound is deterministic, replay-safe, cold-start-friendly. Cuts ~80 LoC, simplifies T2 + test matrix.
- **S2:** `pricing_override_path` config surface DROPPED from Wave 1 → tracking issue. Refresh via code change matches Helicone/Portkey/LiteLLM v1 shape. Cuts ~50 LoC, simplifies validator, **eliminates R14 (override-tampering surface) entirely** — R14 slot reused for OTel cardinality risk.
- **S3:** T5 (config + scheduler hook) FOLDED into T1's PR. < 200 LoC and tightly coupled to T1's `Gate.Evaluate` signature. Net task count: 6 → **5**. Removes one cross-PR coordination tax and one wave-3 PR.

Net effect: **5 file-disjoint tasks**, ~250 fewer LoC (~850 instead of ~1100), 3 fewer config fields, 2 fewer test files, R14 + a portion of R11 closed entirely.

### Wave 1 — Foundations (T1 + T2)

**T1, T2 dispatched in parallel.** T1 owns the Gate + Reader + CUE config + validator + scheduler step-0.6 hook. T2 owns the pricing table + UpperBound estimator + claude-CLI probe. Both consumable independently; T2 has no scheduler/spawner/config touch.

Wave 1 exit gate: Gate.Evaluate works against a synthetic substrate test DB; pricing.Lookup returns correct rates for every active SKU; UpperBound estimator is deterministic + property-tested; validator rejects empty cost block + all-zero caps; scheduler step-0.6 hook conditionally registered (no-op when `safety.cost == nil`).

### Wave 2 — Data plane (T3 + T4)

**T3 + T4 dispatched in parallel.** T3 lands the spawner post-stream emission (one-line edit to W6 parser + new spend.RecordCall package + substrate validate dispatch addition). T4 lands the reconciler tick + Anthropic Cost API client (preferred) + Usage API fallback. Both consume Wave 1's primitives; neither touches the other's files.

Wave 2 exit gate: a synthetic spawn with stream-json fixture produces a substrate `token_spend` row with correct payload; a stub Anthropic Cost API server returns a canned response and the reconciler emits a `budget_reconciled` row; fallback path tested with Cost-API-down + Usage-API-up scenario.

### Wave 3 — Operator doc (T5)

**T5 dispatched alone.** T5 lands the operator doc + example regatta.yaml.

Wave 3 exit gate: a real `regatta serve` run with `safety.cost.per_dag_usd: 100` set denies the over-cap spawn at the scheduler tick + emits the `cost.evaluate` span + writes the `token_spend` substrate row on the allowed call. The doc passes `make doc-check` + every config field is documented + cardinality recommendation + Cost-vs-Usage-API fallback semantics + most-restrictive-wins precedence rule are bolded.

After Wave 3 merges: file the `[cost-governor-followup]` tracking issues per A7 rubric (per-tenant budgets, Stripe webhook, predictive forecasting, mid-DAG kill+compensation, cache-aware budgeting, cross-fleet MCP attribution, Bedrock pricing, Pricing API auto-flag, backfill recipe, progress-gated renewal, spawner reconciliation outbox, **history estimator opt-in (S1)**, **pricing_override_path config surface (S2)**, **admin-key-vault integration (R15)**).

---

## Appendix A — Adopted-OSS dependency manifest (cite-by-version)

This wedge intentionally adds ZERO new Go module dependencies. Per `feedback_research_design_principles` "no bespoke when proven OSS exists" — the proven OSS here is the Anthropic Usage API itself (HTTP endpoint, no SDK needed) + the substrate primitives shipped in v2.

| Dep | Version | Why this version | Adoption shape |
|---|---|---|---|
| `net/http` (stdlib) | Go 1.22+ (already required) | Stdlib — no version churn risk | Used for Anthropic Usage API HTTP client. ~80 LoC; transport timeout + retry-after parsing. |
| `encoding/json` (stdlib) | Go 1.22+ | Stdlib | Payload typed structs (TokenSpendPayload + BudgetReconciledPayload). |
| `database/sql` + sqlite driver | existing | already in repo | substrate.AppendEvent + spend.Reader SQL. |
| `go.opentelemetry.io/otel/trace` | v1.x (W6-locked) | reuses W6 SDK | `cost.evaluate` span open/close + attribute emission. |
| `contracts/schemas/sign.go` | this repo | HMAC reuse per substrate v2 §5 | Reused via substrate.AppendEvent — no direct call from cost-governor. |

**Why NOT adopt LiteLLM / Helicone / Portkey as a Go library:**
- LiteLLM is a Python proxy + Python SDK. No Go library. Adopting would force a Python sidecar → adds a process, an FFI seam, and a maintenance burden for a tier we've consciously decided to skip (the proxy tier). Pattern adopted (precedence schema); runtime rejected.
- Helicone is a SaaS + their own proxy. No embeddable Go library. Adopting would force a proxy hop on every LLM call → breaks the claude CLI stream-json seam. Pattern adopted (policy shape); runtime rejected.
- Portkey is enterprise SaaS. No embeddable. Pattern adopted (hard-error on missing pricing row); runtime rejected.

**Why NOT use the Anthropic Go SDK for the Usage API:**
The Anthropic Go SDK at `github.com/anthropics/anthropic-sdk-go` does not currently expose the Usage/Cost API endpoints (it covers Messages API + Models API as of inspection 2026-06-01 via context7). Direct HTTP is required. Once the SDK adds Usage API support, switch is a `[cost-governor-followup]` 50-LoC swap.

---

## Appendix B — Why each design choice picked X over Y

| Choice | Alternative considered | Why X won |
|---|---|---|
| Pre-call deny at scheduler tick (§3.2) | Pre-call deny at spawner SupervisorLimits | Scheduler tick fires BEFORE spawn; subprocess setup + system-init LLM call cost > zero. Per Waxell §"intercept before next LLM call". SupervisorLimits is the kill-already-running fallback, not the prevent-spawn primary. |
| Pre-call deny at scheduler tick | Proxy interception (Helicone/Portkey model) | Proxy requires regatta to mediate every Anthropic call → breaks the claude CLI subprocess seam (regatta's primary integration shape). Plus proxy = network hop tax on every call. Scheduler-tick deny is zero-tax. |
| Upper-bound estimation (§3.3) | Predicted-mean (rolling p95) | Upper-bound is conservative-correct (never undercounts → never under-denies); deterministic (W9 replay friendly); cold-start friendly (works on call #1). Predicted-mean undercounts in worst case → matches the failure mode this wedge is designed to prevent. |
| Hourly reconcile (§3.4) | Streaming reconcile / per-call reconcile | Anthropic Usage API minimum useful bucket is `1h` per docs; lower frequency = Anthropic-side rate-limit risk + cost-of-noise for sub-cent variance. Hourly with `[cost-governor-followup] sub-hour option` issue for cases where regulatory cadence demands tighter. |
| Hardcoded pricing table (§3.8) | Boot-time fetch from `/v1/models` or external pricing source | Hermetic (no boot-time network); reviewable in diff (every change is a code review); resilient to upstream outage. `[cost-governor-followup] auto-flag drift against /v1/models` covers the future enhancement. |
| Substrate v2 events (token_spend + budget_reconciled) | Bespoke `cost_records` table | Substrate v2 §1 prior-art adoption explicitly cites cost-governor as a primary consumer; per-table proliferation is the anti-pattern substrate was built to eliminate. Two new kinds, zero new tables. |
| `regatta.cost.*` OTel namespace (§3.7) | Reuse `gen_ai.*` semconv | `gen_ai.*` is the upstream-defined LLM semconv; forking it for cost would (a) shadow real attrs operators expect from any LLM observability backend; (b) drift from semconv if upstream adds a `gen_ai.cost.*` family later. Namespacing under `regatta.cost.*` keeps both surfaces clean. |
| Append + SUM reducer for `token_spend` (per substrate v2 §4 R6) | Set-union or write-once | Set-union collapses legitimate retry-with-corrected-tokens; write-once forbids the same. Append + SUM is the only correct shape per substrate spec §4 R6 detail. |
| LWW reducer for `budget_reconciled` per `(tenant, period_start)` | Append + MAX(written_at) | LWW IS append + max-written_at via Fold semantics per substrate v2. Naming "LWW per (tenant, period_start)" makes intent operator-readable. |
| Soft cap = ratchet within period (§9 R10) | Soft cap = re-evaluated per call (toggle) | Ratchet prevents oscillation under spend curves that hover near 80%. Re-evaluation is operationally noisy (alerts about transient downgrade/upgrade cycles). |
| Single `Gate` concrete type, no interface (§3.2) | `Gate interface { Evaluate(...) (Verdict, error) }` | Substrate spec §2.2 S5 pattern: ship concrete type for the one impl; extract interface when the second impl forces it. Wave 1 has one impl. |
| `safety.cost?` optional CUE field (§3.6) | New top-level `cost:` section in regatta.yaml | Keeps the existing `safety.*` mental model intact (caps = safety primitive); minimises operator churn (no doc-rewrite of existing `safety.spend_cap_usd`); backwards-compat byte-equal when unset. |
| Hard-fail `pricing.Lookup` on unknown SKU | Silent zero-cost (Portkey trap pattern) | Silent zero ⇒ silent over-deny. Per §3.1 Portkey adoption-lesson: hard-error and surface the missing-SKU at the call site. |

---

_End of spec. Total line count target: ≤ 900 (this file: ~720). Spec freezes the cost-governor pattern per `feedback_spec_pattern_authority`; implementer-subagent deviations require re-spawning this subagent. Wave 1 dispatch is BLOCKED until substrate v2 Wave 1 + W6 T4 are both merged to main per §depends-on._
