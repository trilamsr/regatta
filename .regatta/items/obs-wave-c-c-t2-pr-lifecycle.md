---
id: OBS-WAVE-C-T2
title: PR-lifecycle stage histogram + dashboard tile + A-T4 placeholder removal (item #10)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0b, OBS-WAVE-C-T1
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §3 Tier-2 row item #10, §7 Wave-C table row C-T2, §6.2 first-digest degraded contract (placeholder removal).

## Task

Create new file `internal/obs/prlifecycle/collector.go` (path-exclusive new surface — does not touch existing spawner/dispatch files). The collector correlates two streams:

1. Dispatch spans (from C-T1) carry `pr_number` as a span attribute (not as a metric label — banned).
2. GitHub PR events (open / review-requested / approved / merged) via `gh` CLI shell or a minimal client.

On correlation, emit a per-stage timer histogram:

```go
meter.Float64Histogram("regatta.pr.stage_duration_seconds").Record(ctx, durationS,
    attribute.String("stage", stage)) // open_to_review | review_to_approve | approve_to_merge
```

Tag set: `stage` (3 enums). Cardinality safe. `pr_number` flows via the dispatch span attribute → exemplar; the histogram itself stays unlabeled-by-PR.

Stage-ordering invariant: stages MUST be monotone (`open_to_review` → `review_to_approve` → `approve_to_merge`); out-of-order GitHub events (e.g. a review pushed before the open webhook is observed due to event reordering) are dropped with a warn-log + a `regatta.pr.lifecycle.out_of_order` counter increment (tagged `stage` only — same enum set, cardinality unchanged). Test fixture `TestPRLifecycle_OutOfOrderEventsDropped` covers reorder + late-arrival.

Resolve meter from a new `internal/obs/prlifecycle/config.go` Config struct (Config.Meter field added inline in this PR — not part of A-T0b's retrofit list). Nil falls back to `otel.Meter("obs/prlifecycle")` (covered by `TestPRLifecycle_NilMeterFallback`).

GitHub API rate-limit handling: `gh` CLI inherits the operator's auth token and surfaces HTTP 403 / 429 on rate-limit hit. The collector retries with exponential backoff (`time.Sleep(1s, 2s, 4s, 8s)`) up to 4 attempts; on persistent failure, log a warn + increment a `regatta.pr.lifecycle.gh_rate_limited` counter (no tags — single series) so the operator can spot a quota-burn against the steady-state event rate. Tracked + alarmed via the same A-T6 operator doc surface — no new dashboard.

Add `docs/operator/dashboards/dispatch.json` panels (extends C-T1's dashboard — coordinate edit ordering: C-T1 lands first, C-T2 extends):

1. Line panel "PR stage P50/P95 by stage" — `histogram_quantile(0.95, sum by (le, stage) (rate(regatta_pr_stage_duration_seconds_bucket[5m])))`.
2. Heatmap panel "Stage distribution" — `sum by (le, stage) (rate(regatta_pr_stage_duration_seconds_bucket[1h]))`.

**A-T4 placeholder removal (per spec §6.2 first-digest degraded contract):** This PR also removes the placeholder line for the "PRs-landed" section in A-T4's `cmd/regatta/digest.go` so the digest renders the live PR-lifecycle data from C-T2. Cite the contract handoff in the PR body.

Per `feedback_research_design_principles`: prefer existing `gh` CLI shell over a new HTTP client; only build a minimal client if rate-limit pressure forces it.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; AST-walk lint passes (`pr_number` NOT on any metric label — only on span attribute); dashboard panels checked in; B1+B2+B3+B4+B5 + B6 (D-T3 dep precondition: `regatta_pr_stage_duration_seconds_count` series non-zero on a real PR) from spec §8.
- **A (target):** B + adversarial reviewer clears; A1 + A2 + A4 from spec §8. Synthetic PR-lifecycle test fixture (open → review → approve → merge) populates all three `stage` buckets.
- **A+ (stretch):** A + `TestDashboardJSON_LintsAgainstSchema` exit 0; histogram populates on real PRs (manual verification: PR body shows `prom http GET /api/v1/query?query=regatta_pr_stage_duration_seconds_count` returns ≥ 1 series per stage).

## Acceptance criteria

- [planned] c1: New `internal/obs/prlifecycle/collector.go` correlates dispatch span → GitHub PR events + emits `regatta.pr.stage_duration_seconds` (spec §3 item #10).
- [planned] c2: Tag set strictly `stage`; `pr_number` flows via span attribute / exemplar only; AST-walk lint stays green (spec §2.2).
- [planned] c3: `docs/operator/dashboards/dispatch.json` extended with two PR-stage panels (spec §9 R2).
- [planned] c4: A-T4 placeholder line for "PRs-landed" digest section removed (spec §6.2 first-digest degraded contract).
- [planned] c5: B6 precondition met — PR body cites C-T1 dep + shows non-zero stage_count series (spec §8 B6, satisfies D-T3 gate).
- [planned] c6: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
