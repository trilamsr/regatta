# OBS-C T2 — PR-watch collector wedge

Status: **design** (deferred T2 from #599; closes #633).
Source spec: `docs/engineer/specs/phase-x/2026-06-02-obs-wave-c-agent-loop-telemetry.md` §3.

## §1 Problem

OBS-C T1 emits one OTel span per substrate event chain (T1: substrate
event spans). OBS-C T3 emits per-agent-call cost in a separate log
warehouse stream. GitHub PR lifecycle events (open / review / merge /
close) live in a third store — `prs` table in the same sqlite the
substrate writes to, populated by `regatta serve`'s gh-pr-poller.

These three stores share no join key. To answer the operator question
"for PR #N, what was the substrate span tree, the cost roll-up, and
the final outcome", an analyst must pull three queries by hand and
correlate them in a notebook. That is the wrong UX for a tool whose
promise is "see your fleet from the terminal".

## §2 Wedge: PR-watch collector

A separate process (or in-process goroutine inside `regatta serve`)
that:

1. Subscribes to the substrate event stream (the same one T1 spans
   read).
2. Watches the `prs` table for state-transition events
   (`opened → review_requested → ci_green → merged | closed`).
3. Joins on `(pr_number, repo)`, with a 24-hour tail window.
4. Emits a single derived metric — `regatta.pr.span_summary` — per PR
   lifecycle, carrying: total span count, total error span count,
   total duration, cost-USD-micro (from the log warehouse), final
   outcome (merged / closed-unmerged / abandoned).

## §3 Cardinality

`regatta.pr.span_summary` ships with:

- `repo` — bounded by operator's repo list (typically 1; cap at 10 for
  the single-tenant self-host filter).
- `outcome` — closed enum (`merged | closed_unmerged | abandoned |
  in_flight`).
- NO `pr_number` label. `pr_number` rides as a span attribute on the
  per-PR span this collector also emits; the metric is a roll-up.

Steady-state cardinality: `repos × 4 outcomes` cells. Per-PR drill via
exemplar trace_id → span attributes (same operator drill pattern as
OBS-C T1).

## §4 Join window

24 hours. PRs that take longer than 24h to land cross the window
boundary; the collector emits a `tail_window_exceeded` event and
defers the join to the log warehouse for offline correlation
(operator-actionable: a PR sitting open > 24h is itself a signal — see
SLO-6).

## §5 Source pipeline

```text
substrate event stream  ─┐
                         ├── join on (pr_number, repo)  ─→ pr.span_summary metric
gh-pr-poller events    ─┘                                ─→ pr.* span (with pr_number attr)
log-warehouse cost stream  ─┘  (24h tail window)         ─→ pr:cost roll-up via #634 rule
```

## §6 Trigger / reopen condition

- A debugging session needs to correlate substrate spans to PR
  outcomes (operator pain signal).
- Wave-D dashboards (#635 cost-vs-failure scatter) need the join — the
  scatter panel currently joins on PromQL `on(pr_number)` against the
  log-warehouse projection, which works for cost-vs-failure but does
  NOT carry the span-tree drill. Adding the collector deepens the
  drill.

Per the self-host filter (CLAUDE.md): does the sole internal operator
need this to dispatch regatta-the-binary at this repo unattended? Today:
NO — the manual three-query correlation, while clunky, is feasible at
the single-operator throughput. **Defer to first operator pain
signal.**

## §7 Implementation seam (when triggered)

The collector lives at `internal/obs/prwatch/` as a separate package
with a `Run(ctx, Config) error` entry point. `Config` carries:

- substrate event source (DB conn).
- gh-pr-poller event source (same DB conn — different table).
- log-warehouse source (HTTP client).
- meter handle for `regatta.pr.span_summary`.

Wire from `cmd/regatta/serve.go` as one of the optional goroutines per
the composition root pattern (see `cmd/regatta/wire_reconcile.go` for
the existing optional-goroutine shape).

## §8 Tests (when triggered)

- `TestPRWatchCollector_JoinsOnPRNumber` — 3 synthetic streams, assert
  one span_summary emit per PR.
- `TestPRWatchCollector_TailWindowExceeded` — PR open > 24h emits
  `tail_window_exceeded` event.
- `TestPRWatchCollector_NoCardinalityLeak` — AST-walk the emit site
  for `pr_number` on metric instruments (mirror
  `TestDispatchSpan_NoUnboundedLabel_PRNumber` from `internal/obs/dispatch`).
- `TestPRWatchCollector_BoundedRepoLabel` — fuzz the repo label, assert
  rejection of values not in the operator's configured allow-list.

## §9 What got smaller (per `feedback_deletion_default`)

The collector REPLACES the three-query notebook correlation. Net: one
metric series + one span replaces three ad-hoc queries the operator
runs by hand. The notebook lives outside the repo so the deletion is
"the operator's workflow", not "lines of code".

## §10 Closes

- #633 (this design dossier).

The implementation issue should be filed when the trigger fires — see
§6.
