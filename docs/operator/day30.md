# Day 30 - all lanes, concurrency 1 each

Reader: customer-operator a month in.
Read time: 3 minutes.
Expires when: promotion criteria for concurrency >1 land.

## Goal

All lanes from `regatta.yaml` active. Each at concurrency 1 until
the per-lane promotion criteria below are met.

```sh
regatta serve
```

## Promotion criteria (concurrency 1 -> 2)

Per-lane gates that must clear before raising `max_concurrency`:

- >=20 PRs merged in the lane via the fleet.
- Canary human-catch-rate >=85% over a rolling 20-canary window.
- Net-helpfulness >=70% (see `docs/design.md` §Test harness).

Each criterion is empirical; do not raise concurrency on intuition.
The criteria themselves are tracked in `docs/design.md` §Stop
conditions; this doc is the operational quick reference.

## Monitoring at Day 30 and beyond

```sh
regatta digest --since 1w        # weekly rollup
regatta canary-report --window 20
regatta cost --since 1d          # spend cap monitoring
```

## When to halt the fleet

If any of:

- Canary catch-rate dips below 75% over a 20-window.
- Daily spend cap trips repeatedly (>3 days in a week).
- Audit-sink integrity check fails (out-of-band).
- Any L0 fixture regresses in CI.

Halt with `regatta serve --halt`; investigate via
[`docs/engineer/post-mortems/`](../engineer/post-mortems/) using the
template documented in that directory's README.
