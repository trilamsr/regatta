---
id: OBS-WAVE-B-T2
title: substrate chain-break counter + critical alarm + dashboard tile (item #6)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0b
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 Tier-1 row item #6, §7 Wave-B table row B-T2, §2.5 trace head-sampling (chain-verify always-on override), §10 R8.

## Task

Instrument `internal/orchestrator/state/substrate/sign.go` chain-verify path. On every HMAC-chain verify, emit:

```go
meter.Int64Counter("regatta.substrate.chain.break").Add(ctx, n,
    attribute.String("layer", layer))
```

Where `n = 1` on verify failure; on success the counter is NOT incremented (Prom convention: `rate()` over an unchanged counter returns zero, so no `n = 0` write is needed to keep the series alive). The companion `regatta.substrate.chain.verify` counter (also emitted from this path, `n = 1` per verify regardless of outcome) provides the denominator for failure-rate alerts and guarantees series presence so `absent()` alarms fire correctly if the verify path goes silent. Resolve meter from substrate `Config.Meter` field (A-T0b retrofit). Nil falls back to `otel.Meter("orchestrator/state/substrate")` (covered by `TestChainBreakCounter_NilMeterFallback`).

Tag set: `layer` (≤ 5 enums, mirrors B-T1). Cardinality safe.

Per spec §2.5, the chain-verify package is on the always-sample override list — A-T0a's `ErrorOverride` sampler captures every chain-break trace regardless of head-sampling ratio. Verify the override fires by emitting a span carrying `error.type=chain_break` on every non-zero increment.

Critical-tier alarm rule (spec §3 row 6): fires on any non-zero increment (`increase(regatta_substrate_chain_break_total[5m]) > 0`). Lives in `slo/substrate-chain-break.yaml` (alarm-only, no SLO). Operator runbook `docs/operator/runbooks/substrate-chain-break.md` covers triage + recovery.

A chain break is a tamper signal — any non-zero increment is load-bearing. The dedup safeguard against alert-fatigue is at the notifier (Alertmanager `group_wait` / `repeat_interval`), NOT at the rule (do not soften to `> N` thresholds; that hides the first incident). Document the Alertmanager group config in the runbook.

Add `dashboards/grafana/substrate-chain.json`:

1. Stat panel "Chain breaks (last 24h)" — `sum(increase(regatta_substrate_chain_break_total[24h]))`.
2. Time-series panel "Breaks by layer" — `sum by (layer) (rate(regatta_substrate_chain_break_total[5m]))`.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; AST-walk lint passes; dashboard JSON + alarm rule + runbook all check in; B1+B2+B3+B4+B5 from spec §8.
- **A (target):** B + adversarial reviewer clears; A1 + A2 + A3 + A4 from spec §8. Synthetic chain-break test fixture (`TestSubstrateChainBreak_FiresCriticalAlarm`) flips one byte mid-chain → counter increments + critical alarm fires + trace carries `error.type=chain_break` + sampler captures it (spec §2.5 + §7 Wave-B exit gate).
- **A+ (stretch):** A + `TestDashboardJSON_LintsAgainstSchema` exit 0; property test sweeps 100 synthetic chain corruption positions and asserts counter increments on every break.

## Acceptance criteria

- [planned] c1: `internal/orchestrator/state/substrate/sign.go` emits `regatta.substrate.chain.break` on every verify failure (n=1 on fail; no write on success) AND `regatta.substrate.chain.verify` (n=1 per verify) as the denominator counter (spec §3 item #6).
- [planned] c2: Tag set strictly `layer`; AST-walk lint stays green (spec §2.2).
- [planned] c3: Critical-tier alarm rule fires on any non-zero increment; synthetic-break test fixture proves it (spec §7 Wave-B exit gate).
- [planned] c4: Operator runbook `docs/operator/runbooks/substrate-chain-break.md` covers triage (spec §8 A3).
- [planned] c5: Trace span on break carries `error.type=chain_break` so A-T0a's always-sample override captures it (spec §2.5).
- [planned] c6: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
