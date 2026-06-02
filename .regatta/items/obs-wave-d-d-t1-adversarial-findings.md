---
id: OBS-WAVE-D-T1
title: adversarial-findings counter + dashboard tile + A-T4 placeholder removal (item #3)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0a
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 Tier-1 row item #3, §7 Wave-D table row D-T1, §6.2 first-digest degraded contract (placeholder removal), §7 Wave-D exit gate (dismissal-rate alarm).

## Task

Create new file `internal/orchestrator/followup/triage.go` (path-exclusive new surface). Instruments follow-up triage decisions on adversarial reviewer findings:

```go
meter.Int64Counter("regatta.adversarial.findings").Add(ctx, 1,
    attribute.String("fate", fate),       // filed | dismissed | auto_fixed | superseded
    attribute.String("severity", severity)) // critical | major | minor
```

Resolve meter from followup `Config.Meter` field (A-T0b retrofit lands this Config struct — spec §7 Wave-A table row A-T0b explicitly includes `orchestrator/followup`). Nil falls back to `otel.Meter("orchestrator/followup")`.

Tag set: `fate` (4 enums), `severity` (3 enums). Cardinality safe (≤ 12 cells).

Add `dashboards/grafana/adversarial.json`:

1. Stacked-bar panel "Findings by fate (7d)" — `sum by (fate) (increase(regatta_adversarial_findings_total[7d]))`.
2. Stat panel "Dismissal rate (7d)" — `sum(increase(regatta_adversarial_findings_total{fate="dismissed"}[7d])) / sum(increase(regatta_adversarial_findings_total[7d]))`.
3. Time-series panel "Findings by severity" — `sum by (severity) (rate(regatta_adversarial_findings_total[1h]))`.

**Dismissal-rate alarm** (per spec §7 Wave-D exit gate): warn-tier alarm fires when dismissal rate > 50% over 7-d trailing window AND finding count > 20 (avoids low-sample noise). Lives in `slo/adversarial-dismissal.yaml` (alarm-only). Operator runbook `docs/operator/runbooks/adversarial-dismissal.md` covers triage (recalibrate reviewer prompt? reviewer disagreement with house style? real false-positive surge?).

**A-T4 placeholder removal (per spec §6.2 first-digest degraded contract):** This PR also removes the placeholder line for the "Adversarial-findings" section in A-T4's `cmd/regatta/digest.go` so the digest renders live finding-counter data. Cite the contract handoff in the PR body.

Per `feedback_research_design_principles`: lean on OTel SDK + existing followup-triage hooks; no new triage decision surface.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; AST-walk lint passes; dashboard JSON + alarm rule + runbook all check in; B1+B2+B3+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer clears; A1 + A2 + A3 + A4 from spec §8. Synthetic dismissal-burst test fixture (15 dismissals in 5 min) fires warn-tier alarm on the 7-d window (Wave-D exit gate).
- **A+ (stretch):** A + `TestDashboardJSON_LintsAgainstSchema` exit 0; `TestSLOBurnRate_FiresOnSyntheticBreach` covers dismissal-rate alarm.

## Acceptance criteria

- [planned] c1: New `internal/orchestrator/followup/triage.go` emits `regatta.adversarial.findings` Int64Counter (spec §3 item #3).
- [planned] c2: Tag set strictly `fate` + `severity`; AST-walk lint stays green (spec §2.2).
- [planned] c3: `dashboards/grafana/adversarial.json` checked in with three panels (spec §9 R2).
- [planned] c4: Dismissal-rate warn-tier alarm fires on synthetic dismissal-burst test fixture (spec §7 Wave-D exit gate).
- [planned] c5: Operator runbook `docs/operator/runbooks/adversarial-dismissal.md` covers triage (spec §8 A3).
- [planned] c6: A-T4 placeholder line for "Adversarial-findings" digest section removed (spec §6.2 first-digest degraded contract).
- [planned] c7: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
