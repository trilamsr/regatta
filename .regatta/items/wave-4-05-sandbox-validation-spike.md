---
id: WAVE-4-05
title: E2B + Daytona + Fly.io sandbox cost/latency validation spike
lane: self-host
status: planned
linked_artifact: https://github.com/trilamsr/regatta/pull/401
---

Source: docs/engineer/specs/2026-06-02-wave-4-amendments.md F5 (Source neutrality — vendor-affiliated comparisons; Adopt downgraded to "Adopt — pending validation spike") + docs/engineer/research/2026-06-02-wedge-wave-4-emerging-tech.md §3

Brief: §3 sandbox decision (default E2B, secondary Daytona, fallback Fly.io) was downgraded from `Adopt` to `Adopt — pending validation spike` because cited cost + cold-start numbers come from vendor-affiliated comparisons (Northflank blog; single Superagent benchmark row). Scope: independently benchmark E2B (~150ms claim, $0.05/vCPU-hr), Daytona (27–90ms claim, $0.067), and Fly.io (<1s, $0.024) on the regatta hello-world workload (single agent turn, no caching). Capture: cold-start P50/P95, per-turn cost, error rate. Write up under `docs/engineer/research/sandbox-validation-2026Q3.md`. Result feeds the final §3 commit decision (keep Adopt vs revise).

## Acceptance criteria

- [planned] c1: Benchmark harness at `scripts/bench/sandbox/` (or equivalent) reproducible against E2B + Daytona + Fly.io with one command; harness pins regatta hello-world workload + agent-turn shape.
- [planned] c2: Results captured at `docs/engineer/research/sandbox-validation-2026Q3.md` with raw P50/P95 cold-start + per-turn cost + error-rate tables and one-paragraph verdict per vendor.
- [planned] c3: §3 "Adopt — pending validation spike" qualifier resolved one way or the other in a follow-up brief amendment; tracking issue (`feedback_unaddressed_load_bearing`) closes when the verdict lands.
