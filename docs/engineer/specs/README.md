# Engineer specs

Locked design for the ACTIVE or NEXT wave. One spec per wedge. Specs here are the canonical design that implementer subagents follow; deviations require re-spawning the design subagent (per `memory/feedback_spec_pattern_authority`).

## Active / next-wave specs

- [`2026-05-31-mvp-3-w6-otel-backbone.md`](2026-05-31-mvp-3-w6-otel-backbone.md) — W6 OTel observability backbone: SDK + slog bridge + scheduler/spawner/gate spans + Jaeger E2E. Wave 1 partial shipped (#172, #169, #168); T3+T4+T5+T6+T7 remain.
- [`2026-06-01-unified-substrate-design.md`](2026-06-01-unified-substrate-design.md) — Unified substrate v2 (events log only in Wave 1). 3 tasks. Ships AFTER W6 T1+T2+T5 merge. Policies primitive deferred to W8; blobs primitive deferred to W11. Phased read-side cutover; no atomic dual-write.
- [`2026-06-01-w7-operator-web-ui-design.md`](2026-06-01-w7-operator-web-ui-design.md) — W7 operator web UI v2: server-rendered approval flow + read-only DAG + cost panel; Go embed.FS + htmx + Tailwind CDN. 14 tasks across 4 waves (W7.0 listener prereq + 3 build waves). Authorizer interface seam designed pre-W8.
- [`2026-06-01-w9-temporal-vs-bespoke-redteam.md`](2026-06-01-w9-temporal-vs-bespoke-redteam.md) — W9 replay+diff harness, option C (hybrid): `DurableHistory` Go interface, substrate-default impl, Temporal-backed impl behind refined P2.5 trigger. Ships AFTER W6/W7/W8 land.
- [`2026-06-01-adapter-contracts-design.md`](2026-06-01-adapter-contracts-design.md) — P3.8 swap-out adapter contracts: 5 adapters (OTel exporter, OPA RBAC, Sigstore signer, Stripe metered billing, LLM gateway) behind `internal/adapters/<name>/` `sql.Register`-style pattern. Trigger = first customer ask for hosted backend.
- [`2026-06-02-orchestrator-pr-watcher.md`](2026-06-02-orchestrator-pr-watcher.md) — orchestrator PR-watch wedge: drives `running → pr_open` by polling GitHub head SHA via `gh pr list --head regatta/agent-{id}`. New `internal/orchestrator/prwatch` package; tick-driven Sweep; emits `agent_pr_opened` + `agent_pr_head_changed` substrate events so gate runner (#33) and rejection router (#16) consume via the existing event seam. Supersedes issue #15.

## Dependency graph

```
              W6 (OTel)
                 |
             substrate           (substrate ships after W6 T1+T2+T5)
                 |
        +--------+--------+
        |                 |
        W7              [adapters]    (W7 and adapters are orthogonal)
        |
        W8 (OPA RBAC + tenant_id)
        |
        W9 (replay + diff)
```

`W6 -> substrate -> W7 + W8 -> W9`. Adapters (P3.8) ship orthogonally on first hosted-backend customer ask. Substrate is load-bearing for every later wedge that needs the events log; W7's Authorizer interface seam is the W8 plug-in point with no W7 re-architecture; W9 reads W6's events shape, renders through W7's UI, and scopes per W8's `tenant_id` column.

## Related RFCs (shipped milestone decisions)

- [`../../rfcs/0001-mvp-1-program-publish.md`](../../rfcs/0001-mvp-1-program-publish.md) — MVP-1 program publish via sqlite, not SpecAdapter.
- [`../../rfcs/0002-mvp-2-conditional-dag.md`](../../rfcs/0002-mvp-2-conditional-dag.md) — MVP-2 W1 outcome-conditional DAG (CEL edges + journal).
- [`../../rfcs/0003-mvp-2-approval-gates.md`](../../rfcs/0003-mvp-2-approval-gates.md) — MVP-2 approval gates (HMAC token + reaper + fold).

## What goes here

- Locked design for the ACTIVE or NEXT wave.
- One spec per wedge; each ends with a B/A/A+ grade rubric.
- Schema, migration, reducer, interface boundaries, file layout, parallel-dispatch plan.

## What does NOT go here

- Strategic vision (goes in `../briefs/`).
- Accepted decisions for shipped milestones (goes in `../../rfcs/`).
- Per-iteration drafts, plans, dispatch prompts, working reviews, superseded specs (stay under `docs/superpowers/`, gitignored, one-shot).

Per-iteration scratch lives in `docs/superpowers/` and is gitignored on purpose; the final-state, tracked design lives here.
