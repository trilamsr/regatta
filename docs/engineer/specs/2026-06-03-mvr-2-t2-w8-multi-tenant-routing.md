---
title: "MVR-2 T2 — W8 multi-tenant tenant_id routing"
status: skeleton-prefetch
summary: "Pre-fetch skeleton for MVR-2 T2. Carries the W8-OPA design's `Principal.Tenant` field into every storage read path: substrate events, cost spend, orchestrator state, prwatch, rejectionrouter. One read-side audit + one migration to add `tenant_id` columns. Default tenant = `default` (backwards-compatible single-tenant operators). 2-3 wk effort band. SKELETON."
---

# MVR-2 T2 — W8 multi-tenant `tenant_id` routing — skeleton spec

_Pre-fetch skeleton, 2026-06-03. Material elaboration deferred to MVR-2 dispatch. Source-of-truth: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 MVR-2-T2. Prior W8 design: `docs/engineer/specs/2026-06-01-w8-opa-rbac-design.md` (covers Authorizer + Principal type; this spec carries the `Tenant` field into storage). Cost-cap forward-fit reference: `docs/engineer/specs/phase-x/2026-06-02-phase-autonomy-w5-cost-cap-autonomic-enforcement.md` (cost-cap already has per-tenant scope; verify forward-fit holds)._

## 1. Scope

### 1.1 In scope

End-to-end carry of `Principal.Tenant` (declared in `internal/web/auth.go` per W7 §3.6.4) from inbound request through every storage read into substrate, cost, orchestrator state, prwatch, and rejectionrouter. **Read path first**; write paths follow once the read path verifies (TDD per `feedback_tdd_discipline`).

Surfaces gaining tenant filtering:
- `substrate.Fold(ctx, db, kind, tenant)` — adds `WHERE tenant_id = ?` to existing folder fns.
- `cost.Spend(ctx, db, window, tenant)` — already has tenant scope per W5 cost-cap; verify forward-fit.
- `internal/orchestrator/state` agent table — adds `tenant_id` column (one migration: `015_add_tenant_id.sql`).
- `internal/orchestrator/prwatch` — `gh pr list` results joined to local `prs` table by `(tenant_id, pr_number)`.
- `internal/orchestrator/rejectionrouter` — `(tenant_id, repo, pr_number)` becomes the routing key.

### 1.2 Out of scope

- Hosted IdP / SSO — covered by W8 spec deferral (no change).
- Per-tenant rate-limit / quotas — followup wedge (post-MVR-2).
- Cross-tenant read for admin (e.g., `regatta admin dump --all-tenants`) — followup CLI flag, not Wave-2 surface.
- Tenant-bound encryption keys at rest (crypto-shred per-tenant) — `docs/engineer/specs/phase-x/2026-06-02-crypto-shredding-design.md` owns that, defer.
- UI per-tenant filter dropdown — Wave-3 (T5) polish item.

## 2. Architecture (high-level)

### 2.1 Storage layer changes

One forward-only migration: `internal/migrations/sqlite/015_add_tenant_id.sql` adds `tenant_id TEXT NOT NULL DEFAULT 'default'` to the four tables that store per-run state today: `agents`, `prs`, `events` (substrate), `cost_ledger`. Default value `'default'` keeps single-tenant operators byte-equal after migration.

Migration ordering: pinned at `015` per `feedback_migration_number_lock` — dispatch prompt MUST specify. Implementer never picks.

### 2.2 Read-path threading

A single new helper `tenant.FromContext(ctx) string` wraps the W7/W8 `Principal` lookup with a fallback to `'default'`. Every read fn signature changes from `(ctx, db)` to `(ctx, db, tenant)`, with `tenant` resolved at the handler boundary (web), CLI boundary (`cmd/regatta`), or orchestrator-loop boundary (state machine). No `ctx.Value` keys for tenant in storage layer — explicit param, per `feedback_research_design_principles` (no hidden state).

### 2.3 Write-path threading

Phase 2 of the spec (lands AFTER read-path PR is merged + soaked 48 h). Adds `tenant_id` to every `INSERT` site. Substrate event `kind` validators (per W1 `RegisterPayloadValidator` pattern) gain a tenant-presence assert.

## 3. Key risks (named, ≥6)

| # | Risk | Mitigation seed |
|---|---|---|
| R1 | Forgotten read site silently leaks cross-tenant data | Static analysis gate: `grep -RE "db\.Query|db\.Exec" internal/` cross-referenced against an allow-list of tenant-aware callers. `make check` runs the linter; CI red on drift |
| R2 | `tenant_id` migration on a 10-GB substrate.db locks WAL for >30 s | Default value is constant — sqlite ALTER TABLE ADD COLUMN with literal default is O(1) (no row rewrite). Verify on a 100k-event fixture before merge |
| R3 | Single-tenant operators see no behavior change (acceptance), but their old `events` rows show `tenant_id='default'` AND fail policy bundle that excludes `'default'` | Default policy bundle in W8 default-deny excludes `'default'` from deny path; explicit allow-row for `tenant='default'` baseline |
| R4 | Cost-cap's existing per-tenant scope drifts from W8 Principal.Tenant naming | Verify in §5 dep-order: cost-cap merges first, W8 OPA merges second, this spec lands third + uses cost-cap's exact `tenant` string |
| R5 | Test fixtures across the repo use synthetic tenant strings (`acme`, `customer-0`) that fail W8 policy | Fixture-level helper `tenant.TestDefault()` returns `'default'`; sweep replaces ad-hoc strings |
| R6 | Substrate fold cache (existing) keyed on `(kind)` — now must be `(kind, tenant)`; stale entries leak | Cache key change ships in same PR as the fold-fn signature change; `TestFold_CacheKeyIncludesTenant` regression test |
| R7 | `regatta serve` boot-time fixture seed runs against tenant `'default'` and a new operator's first tenant is also `'default'` — collision risk | Boot-seed is idempotent + tenant-aware; if tenant existed pre-boot, seed is no-op |
| R8 | Cross-tenant followup queries (e.g., total cost across customers for billing) require deliberate fan-out, easy to forget | Tracked followup issue at PR merge: `regatta admin cost --all-tenants` (post-Wave-2) |

## 4. Test plan (≥8)

- `TestTenant_FromContext_Default` — ctx absent → `'default'`
- `TestTenant_FromContext_PrincipalSet` — W7 Principal set → tenant carried
- `TestSubstrate_FoldFiltersOnTenant` — multi-tenant fixture, fold returns only the asked tenant
- `TestCost_SpendForwardFitsTenantString` — cost-cap tenant string matches Principal.Tenant
- `TestMigration_015_DefaultsToDefault` — pre-existing rows get `'default'` post-migration
- `TestMigration_015_O1OnLargeDB` — 100k-row fixture < 5s ALTER (sqlite literal-default fast path)
- `TestPRWatch_ListFiltersOnTenant` — two-tenant fixture, each sees only own PRs
- `TestRejectionRouter_RoutingKeyIncludesTenant` — same `(repo, pr_number)` across tenants does not collide
- `TestStaticAnalysis_AllDBQueriesTenantAware` — drift linter green
- `TestDefaultPolicyBundle_AllowsDefaultTenant` — single-tenant backwards-compat acceptance

## 5. Dependency order

W7 (#318/303/307 — shipped) declared the `Principal.Tenant` field → `cost-cap` (#596, shipped) writes tenant-scoped spend → `W8 OPA RBAC` (spec: 2026-06-01) lands as `MVR-2-T2-prep` immediately before this spec → this spec ships the read-path migration → write-path PR follows after 48 h soak → T1 (W7 Wave-2 DAG UI) consumes tenant filter for free.

## 6. Deferred to dispatch-time elaboration

- Exact column type (`TEXT` vs `INTEGER` tenant_id; W7 uses string — keep string)
- Index strategy: `CREATE INDEX events_tenant_kind ON events(tenant_id, kind)` partial vs full
- W8 policy-bundle path resolution under multi-tenant (already in W8 spec §3.5 — re-check before dispatch)
- CLI flag surface: `regatta --tenant <id>` global flag vs per-subcommand

```release-notes
none (internal — design spec skeleton, pre-fetched for MVR-2)
```
