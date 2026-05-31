# Wedge: cost / budget governor

Prospective. Not on the milestone path. See
[`README.md`](./README.md) for ranking and the adopt-when-needed
gate.

## Thesis

Pre-call budget enforcement scoped per operator / DAG / work item,
reconciled post-hoc against the Anthropic Usage API. Vendors cap
at the org tier; gateways like Portkey and Helicone do enforce
pre-call deny at the credential layer; what nobody owns is
**DAG-context attribution and journal-grade replay** of every
spend decision. The scheduler is the natural owner -- it already
sees the dependency graph, the work-item kind, and the journal
entries that make a spend event reproducible.

Maps to **Trap Catalog P8** -- spend / iteration brakes with
mandatory re-approval, load-bearing, three documented incidents.

### Defensibility under Dynamic Workflows

Claude Code Dynamic Workflows (2026-05-28) hold orchestration
state outside the model context but still bill against a single
session bucket. They do not attribute spend to a journaled DAG
node, do not separate the Claude Agent SDK credit pool from the
seat subscription (split effective 2026-06-15, $200/seat
Enterprise), and do not survive a process restart. That is the
crack this wedge widens.

## Prior art

| Source | Pattern worth stealing |
|---|---|
| [Helicone — custom rate limits](https://docs.helicone.ai/features/advanced-usage/custom-rate-limits) | Cost-as-policy-header: `Helicone-RateLimit-Policy: 500;w=3600;u=cents;s=user` — composable, declarative, copy-pasteable into a work item annotation. |
| [Portkey — virtual key budget limits](https://portkey.ai/docs/product/ai-gateway/virtual-keys/budget-limits) | Hard budget cap on a virtual key (non-retroactive); the gateway returns 429 once the counter trips. Pre-call deny at the credential layer is the cheapest enforcement point. Mint short-lived per-DAG keys; pair with the project's `Helicone-User-Id` companion header for attribution. |
| [LiteLLM — budgets](https://docs.litellm.ai/docs/proxy/users) and [bug #12905](https://github.com/BerriAI/litellm/issues/12905) | Hierarchy with explicit precedence (org → workspace → key → tag). The bug is the warning: user budgets get silently ignored when keys have a `team_id`. Define precedence in regatta's schema — do not inherit by accident. |
| [Anthropic — rate limits & spend limits](https://docs.anthropic.com/en/api/rate-limits) | Tiered ceiling: platform cap stacked above user cap, with workspace sub-caps and fractional notification thresholds. |
| [Anthropic Usage & Cost API](https://docs.anthropic.com/en/api/usage-cost-api) | Authoritative post-hoc usage at `/v1/organizations/usage_report/messages`, broken down by uncached / cached / cache-write / output and grouped by `api_key` / `workspace` / `model`. In-process meters drift; reconcile against this on a cron and emit `budget.reconciled` events. |
| [AWS Budgets Actions](https://docs.aws.amazon.com/cost-management/latest/userguide/budgets-controls.html) | Graduated SCP attachment: at 80 % soft action (downgrade), at 100 % hard action (deny). Maps to model downgrade (Sonnet → Haiku) then DAG pause. |
| [Kubernetes ResourceQuota](https://kubernetes.io/docs/concepts/policy/resource-quotas/) and [LimitRanger](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) | Admission-time deny with `LimitRange` defaulting. Every unannotated work item gets a defaulted budget — otherwise unbounded ones become the exfiltration path. |
| [The $47,000 Agent Loop](https://dev.to/waxell/the-47000-agent-loop-why-token-budget-alerts-arent-budget-enforcement-389i) | Pre-call session ceiling that intercepts *before* the next LLM call, records the agent's intent at the trigger point, supports soft (downgrade) and hard (kill) modes. The gap LangSmith / Helicone / Braintrust admit they do not fill. |
| [Mavvrik + Ingram Micro AI Cost Governance](https://www.mavvrik.ai/press-releases/ingram-micro-ai-cost-governance/) | Channel-partner FinOps allocation with bill-tenant-back. Names the multi-tenant axis the wedge needs: every spend event carries a `tenant_id` so agencies can attribute fleet runs to end customers. |
| [zop.dev — LLM FinOps per-feature token budget](https://zop.dev/resources/blogs/llm-finops-per-feature-token-budget/) | Redis-tracked `feature_id` budget at the gateway, 429 on overrun. Cleaner pre-call topology than scheduler-level deny; useful when regatta sits behind an existing AI gateway rather than in front of it. |
| [AgentOps -- agentic monitoring](https://research.aimultiple.com/agentic-monitoring/) | Recursive-loop detection feeds the brake signal. The dossier's P8 mapping wants this as the upstream detector that closes the loop on infinite-token-burn. |

## Proposed minimal data model

Substrate note: this section's bespoke `budget` table is
**superseded** by the [unified substrate](./unified-substrate.md).
Ship the policy as `policies WHERE kind='budget'` and the spend
view over `events WHERE kind='token_spend'`. The fields below are
the `spec_json` for the policy row.

`budget` holds *policy* only. Spend is **not** stored here -- it
derives from existing `events` rows (which already record token
usage per call), surfaced through a materialized view for cheap
reads.

```
budget (
  id           uuid pk,
  scope_kind   enum('operator', 'tenant', 'dag', 'work_item'),
  scope_id     uuid,
  meter_pool   enum('subscription', 'agent_sdk_credits', 'gateway'),
  limit_usd    numeric(10, 4),       -- micro-cent precision; null = inherit
  soft_pct     smallint default 80,  -- warn / downgrade threshold
  period       interval               -- null = lifetime of scope
)

-- read-side
budget_state(budget_id, spent_usd, period_start)  -- materialized view
```

The `tenant` scope and the `meter_pool` column are deliberate:
without them, agency operators cannot bill fleets to end
customers, and Anthropic's 2026-06-15 split between subscription
bucket and Agent SDK credits silently double-counts.

Precedence: most-specific scope wins; missing budgets inherit
upward; an `operator`-scoped default is **mandatory** (LimitRange
pattern). Encode precedence in the schema -- never let it fall
out of join order, or you reproduce the LiteLLM team-vs-user bug.

## Regatta extension points (no schema breaks)

- `regatta.yaml:safety.spend_cap_usd` already exists; widen its
  schema to accept a scope and a soft / hard pair.
- `SupervisorLimits` (per `design.md` §Programs P2.7) tracks
  cumulative cost per agent and per lane. Same hook fires on
  cost overruns.
- `RejectionRouter` (Phase 2): if `cumulative_spend > cap`,
  escalate immediately rather than after `K = 3` rounds.
- Reconciliation cron writes `budget.reconciled` rows into
  `events`; the existing audit pipeline picks them up.

## Open problems regatta could lead

- **Progress-gated renewal.** Remaining budget without verifier
  progress is permission to keep being wrong. Gate child spawns
  on a parent-progress signal, not raw budget remaining.
- **Mid-DAG kill semantics.** Reversibility tag per work-item
  kind (idempotent retry vs. side-effecting commit) plus a
  compensation hook (`on_budget_kill`).
- **Cache-aware budgeting.** Anthropic prices cache writes at
  1.25x (5m) or 2x (1h) and reads at 0.1x. Most budget systems
  quote tokens or dollars and never model the break-even. A
  budget that *rewards* cache-friendly prompt structure is open
  territory.
- **Cross-tenant attribution.** When one operator's DAG triggers
  a subagent that uses a shared MCP key, whose budget burns?
  The `tenant` scope above is the schema answer; the policy
  question (always charge the calling tenant? proportional to
  output tokens?) is still open.

## Failure modes

- **Partial commits.** A work item that pushed an external
  side-effect (git push, API write) before the budget tripped
  leaves the DAG inconsistent. Tag kinds with `reversibility` and
  register a compensation hook.
- **Sunk cost.** Killing at 99% wastes the 99%. Soft cap allows
  a single grace step iff `progress_score` improved.
- **Cascading dependents.** Either auto-fail downstream loudly or
  emit `kind = blocked_by_budget` events for replay.
- **Reconciliation drift.** In-process meter undercounts
  `cache_creation`; vendor invoice arrives 24h late showing 15%
  overrun the gateway already authorised. `budget.reconciled`
  events must be able to retroactively pause an operator even if
  no individual DAG tripped.
- **Admin-exempt escapes.** LiteLLM's "rate limits do not apply
  to admins" pattern is a real production bug. Admins are
  metered, never exempted; overrides emit an explicit event.

## Trigger metric (when to adopt)

- First customer running ≥3 concurrent DAGs.
- OR first incident where the spend-cap warning fired but a DAG
  continued past the cap.
- OR procurement ask citing FinOps controls.

## Grade rubric

| Tier | Criterion |
|---|---|
| **B** | Pre-call deny on hard cap; `budget` table + materialized view; soft-cap warning emitted to events. |
| **A** | All B + post-hoc reconciliation against Anthropic Usage API on hourly cron; soft cap triggers model downgrade. |
| **A+** | All A + reversibility-tagged work items + compensation hook + progress-gated renewal + zero admin-exempt paths (mutation-tested). |

## References to existing repo state

- `contracts/schemas/work_item.schema.json` — extend with
  optional `budget_scope` annotation.
- `docs/design.md` §Programs P2.3 — depth caps are an adjacent
  primitive; budget caps reuse the same enforcement boundary.
- `docs/incidents.md` P8 — spend / iteration brakes.
