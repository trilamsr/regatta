# Day 7 - orchestrator on, one lane

Reader: customer-operator a week in.
Read time: 5 minutes.
Expires when: `regatta serve` flag surface changes.

## Goal

Run ~5-10 work items through the full L0-L6 stack with a single
agent, one lane at a time. Builds the trust loop before any
parallelism turns on.

## Prerequisites

Day 1 finished:
- `regatta validate-config` green.
- `regatta validate-spec --dry-run` lists ready items.
- `regatta verify-repo-config` green (or `--accept-degraded` with
  named gap logged to audit sink).

## Day 2 - calibrate the gates

Before turning on `regatta serve`, calibrate against 3 already-merged
PRs + the canary corpus:

```sh
regatta gate-calibrate --pr 95,87,79
regatta gate-calibrate --canary-corpus
```

`gate-calibrate` runs L0-L5 against the chosen PRs and emits a
per-gate confusion matrix. The canary archetypes (see
[`testdata/gates/canary/README.md`](../../testdata/gates/canary/README.md))
have known-expected verdicts. Tune `gates[*].severity_block` until
calibration is clean. Typical fixes: raise L4 threshold to
`critical` only when over-cautious; add path filters when L5
false-drifts on auto-generated files.

## Day 3 - single pilot PR (human-spawned)

```sh
regatta pilot --work-item 101 --no-orchestrator --interactive
```

Spawns one agent in `.regatta/worktrees/work-101-<slug>/`; you
watch it live. Once the PR opens:

```sh
regatta gate-run --pr 256
```

If gates accept and a synthetic-bad variant rejects, you are ready
for Day 7.

## Day 7 - turn on orchestrator (one lane)

```sh
regatta serve --lane server --max-concurrency 1
```

Watches one lane only; spawns one agent at a time.

## Monitoring

```sh
regatta status              # one line per agent + last gate
regatta digest --since 1w   # markdown digest
regatta canary-report       # catch-rate + recent canary PRs
```

## Next

- [day30.md](day30.md): promote remaining lanes once trust is
  established.
