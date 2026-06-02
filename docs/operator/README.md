# Operator docs

Reader: operator standing up Regatta against a real repo for the
first time, or running a deployed instance day-to-day.
Read time: 1 minute (this index).

## Start here

1. [`getting-started.md`](getting-started.md) — end-to-end walkthrough
   from `git clone` to first agent-opened PR (15 minutes; covers
   cost caps, key rotation, L4 gate, troubleshooting).
2. [`quickstart.md`](quickstart.md) — 5-minute contract drill:
   validated `regatta.yaml` + first agent spawn.
3. [`install.md`](install.md) — pin a release, verify the artifact
   provenance, install the binary.
4. [`configure.md`](configure.md) — full `regatta.yaml` schema with
   per-field semantics and defaults.

## Operate

5. [`day1.md`](day1.md) — first day on a new deployment: what to
   watch, what's normal, when to halt.
6. [`day7.md`](day7.md) — calibration window (covers Day 2, 3, 7
   checkpoints).
7. [`day30.md`](day30.md) — month-one steady state + halt criteria.
8. [`approval-gates.md`](approval-gates.md) — HITL gate operator
   surface (decide, escalate, timeout, MTTD runbook).
9. [`cost-governor.md`](cost-governor.md) — per-DAG / per-operator
   USD caps, reconciliation, drift alerts, pricing refresh.
10. [`cost-governor-dashboards.md`](cost-governor-dashboards.md) —
    Honeycomb + Grafana + Jaeger dashboards for cost-governor spans.
11. [`observability.md`](observability.md) — OTel spans, slog events,
    audit-sink wiring.
12. [`rbac-onboarding.md`](rbac-onboarding.md) — multi-tenant policy
    revisions and OPA policy hot-reload.

## Maintain

13. [`upgrade.md`](upgrade.md) — version bumps, schema migrations,
    rollback.
</content>
</invoke>