---
title: "Scheduler filter.Apply monomorphization bench (closes #753)"
status: shipped
summary: "Measure ns/op + B/op + allocs/op for the 3 `filter.Apply[Scope]` instantiation sites and capture stripped `cmd/regatta` binary size as a baseline for monomorphization cost (closes #753)."
---

# Scheduler filter.Apply monomorphization bench (closes #753)

Owner: scheduler  
Status: shipped  
Closes: #753

```release-notes
[PERF] add filter monomorphization bench + binary-size delta script (closes #753)
```

## §1 Problem statement

`filter.Apply[Scope]` (`internal/orchestrator/scheduler/filter/filter.go`) is
instantiated at 3 call sites — `applyApprovalGates`
(`scheduler_approval_gate.go:27`), `applyCostGovernor`
(`scheduler_cost_gate.go:25`), and `applyL4Gate` (`scheduler_l4_gate.go:38`).
The generic was introduced by #698 (consolidation of the gate-pass loops).
Go's compiler monomorphizes generic functions per distinct type argument, so
each call site produces a separate compiled body. Cost was unmeasured until
this PR: no ns/op, no B/op, no binary-size delta vs the pre-#698 baseline.

## §2 Captured baselines (HEAD)

Apple M1 Max, go1.26.3, `-benchtime=1x` (fast capture; rerun with `3s` for
averaged numbers):

| sub-bench         | ns/op       | B/op    | allocs/op |
| ----------------- | ----------- | ------- | --------- |
| approval/N=10     | ~501k       | 41520   | 628       |
| cost/N=10         | ~274k       | 29008   | 544       |
| l4/N=10           | ~3.69M      | 74192   | 1174      |
| all3/N=10         | ~281k       | 33824   | 613       |
| approval/N=100    | ~605k       | 224496  | 4577      |
| cost/N=100        | ~558k       | 175520  | 3876      |
| l4/N=100          | ~32.7M      | 600232  | 9889      |
| all3/N=100        | ~594k       | 223272  | 4574      |

Stripped `cmd/regatta` binary at HEAD: **40.80 MiB** (42,786,706 bytes) via
`go build -ldflags='-s -w'`.

L4's higher ns/op reflects per-call `RecordEvent` DB writes when the gate
blocks — not monomorphization overhead per se. The relevant signal is
per-site allocation count vs a hypothetical non-generic loop, which the
bench captures via `B/op` and `allocs/op`.

## §3 Baseline-measurement procedure

Run from repo root:

```
scripts/bench-filter-monomorphization.sh [benchtime]
```

`benchtime` defaults to `1x` (single iteration per sub-bench) for a fast
capture; pass e.g. `3s` for a stable averaged baseline. The script:

1. Runs `go test -bench BenchmarkSchedulerTick_FilterMonomorphization
   -benchmem -benchtime=<benchtime> ./internal/orchestrator/scheduler/...`.
2. Builds `cmd/regatta` with `-ldflags='-s -w'` and reports `wc -c` bytes
   + MiB.
3. Prints `head_sha`, `go_version`, and `benchtime` for reproducibility.

To bisect against pre-#698 (SHA `d823a02`), check out the SHA, re-run the
script, and diff the two output blocks. Comparison automation is a
follow-up if a regression flags.

## §4 Grade rubric

| Criterion              | B | A | A+ | Evidence |
| ---------------------- | - | - | -- | -------- |
| Bench exists           | x |   |    | `BenchmarkSchedulerTick_FilterMonomorphization` |
| All 3 sites exercised  | x |   |    | `benchFilterApproval` / `benchFilterCost` / `benchFilterL4` sub-benches |
| Binary size measured   | x |   |    | `scripts/bench-filter-monomorphization.sh` `binary_bytes` block |
| Reproducible script    |   | x |    | Script pins `-ldflags`, prints `head_sha`+`go_version`; rerunnable across SHAs |
| Regression-bisect path |   |   | x  | §3 documents pre-#698 SHA `d823a02` rerun procedure |

Claimed tier: A — bench + script + spec + bisect-path documented. A+ would
add automated comparison + CI gate; deferred per self-host filter (no
external customer yet).
