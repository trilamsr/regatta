---
title: Dashboard evolution framework — feedback loop for single-operator self-host
date: 2026-06-09
status: draft
phase: self-host
---

# Dashboard evolution framework — feedback loop for single-operator self-host

## Problem

This session shipped 14 dashboard PRs. The feedback channel was a single
operator voice: "show details, not numbers", "click to expand", "stop padding
empty cards". Each comment was actioned in isolation. There is no telemetry
backend, no analytics SDK, no A/B harness, no funnel — the binary runs against
one repo on one machine for one operator. Standard SaaS feedback playbooks
(`PostHog` surveys, `Sentry` user-feedback widget, `Grafana` Dashboard Insights)
all assume N>1 users and a metrics pipeline. None of those preconditions hold
here.

The risk: the dashboard drifts into "whatever the operator complained about
this week" — a frustration-mover, not a north-star-mover. The
`operator-minimal-input` mandate is invisible until violated; once violated,
the panel grows a knob, the knob grows a tooltip, the tooltip grows a doc link,
and the dashboard is no longer minimal. We need a lightweight, friction-free
loop that lets a single operator evolve the dashboard rigorously — without
inventing infrastructure that doesn't pay rent.

## Prior art

Three reference implementations that are self-host-shippable and OSI-licensed
or close to it. Versions verified 2026-06-09; license SPDX from upstream
LICENSE file.

- **Grafana — Dashboard Insights / Usage Insights** —
  `https://github.com/grafana/grafana` (AGPL-3.0; OSS core relicensed from
  Apache-2.0 in 2024 mirror of Elastic move). Insights panel surfaces per-
  dashboard view count, error count, last viewed, recent users — gated on
  Enterprise license at runtime. Useful as a UI pattern (insights icon in the
  toolbar opens a side-drawer), but the data pipeline requires a logs
  database. Free tier of self-hosted Grafana ships the icon but no data — a
  cautionary pattern for "feature visible, data behind license".

- **Backstage — TechDocs + plugin "report issue" patterns** —
  `https://github.com/backstage/backstage` (Apache-2.0). Backstage TechDocs
  pages render a "Report an issue" link in the page chrome that opens a
  GitHub issue draft pre-populated with the page URL, plugin name, and a
  template body. The pattern is a few lines of TypeScript per plugin — no
  backend. Internal Developer Platform teams use this as their primary
  feedback channel at company-scale; the mechanism is identical at solo
  scale.

- **Sentry — User Feedback widget (self-hosted)** —
  `https://github.com/getsentry/self-hosted` (FSL-1.1-Apache-2.0; converts to
  Apache-2.0 after 2y). Embeddable JS widget triggers a modal where the user
  describes a frustration; widget POSTs to the Sentry backend (or in our
  case, to a stub that writes a `gh issue create` payload). Requires
  self-hosted Sentry >= 24.4.2 for the full feature. The widget UX is the
  reference; the backend is the part we cut.

Honorable mention: **PostHog Surveys** (MIT, `github.com/PostHog/posthog`) —
no-code in-app survey widgets. Overkill at N=1, but the template structure
(NPS, CSAT, open-text, panel-scoped) is reusable prose-only.

## What is measurable single-operator

A single operator is not zero signal — they are low-volume, high-fidelity
signal. The naive read ("no telemetry → no measurement") gives up too early.
Practical signals available without a metrics backend:

- **localStorage panel-hide events** — when the operator hides a panel via
  the existing collapse/dismiss control, persist the panel key + timestamp +
  reason-string (free-text). Reading the bag at session boot answers "which
  panels the operator has chosen to hide" and "how recently". No network.
- **Keyboard shortcut usage** — every dashboard action bound to a shortcut
  writes `localStorage.shortcutCount[<key>]++`. A monthly inspection answers
  "which shortcuts the operator actually uses" — unused shortcuts are
  candidates for removal, not promotion.
- **Time-on-panel via `mouseenter`/`mouseleave`** — accumulate per-panel
  hover time into `localStorage`. Noisy at the second-level but at the hour-
  level reveals "operator stares at the queue depth panel 4x more than the
  run-history panel" — a relative ranking, not an absolute one.
- **Page navigation counts** — same pattern; `localStorage.pageVisits[<key>]`
  incremented on `history.pushState`. Answers "the audit log page got 0
  visits this month — kill it".
- **Operator-voice tickets, time-stamped** — covered in next section. The
  highest-fidelity signal because it carries the operator's reasoning.

The unifying property: every signal lives in `localStorage` on the operator's
own machine. There is no backend, no fleet aggregation, no privacy review, no
opt-in dialog. The operator owns the data; the dashboard reads it at boot;
the operator-or-an-agent can dump it via a single "Export feedback" button
that writes JSON to clipboard for paste-into-issue.

## Operator-voice tickets

The richest signal is what the operator says when they hit friction. Encode
the panel into the ticket itself so a pivot table answers "which panel gets
most complaints" without a data warehouse.

Convention:

- **Label per panel**: `dashboard/<panel-key>` — e.g. `dashboard/queue-depth`,
  `dashboard/run-history`, `dashboard/agent-status`. Created lazily on first
  use via `gh label create`.
- **Issue title prefix**: `[UX] dashboard/<panel-key>: <one-line friction>`.
- **Body template**: panel key (machine-readable), current behaviour
  observed, expected behaviour, screenshot path (optional), session retro
  context (optional).
- **Aggregation query**: `gh issue list --label dashboard --state all --json
  number,title,labels,createdAt,closedAt -L 200` piped through `jq` group-by
  label answers monthly "which panel has the most tickets" and "which
  tickets are still open". Zero infrastructure.

The in-page entry point (Recommendation §below) writes a draft body matching
this template so the operator never has to remember the format.

## Failure modes

Single-operator feedback loops fail in four specific shapes; each has a
counter-measure that lives in the loop itself, not in operator discipline.

- **Feature creep** — every frustration becomes a panel. After 6 months the
  dashboard has 40 panels and the operator scrolls past the one they
  actually wanted. Counter: hard cap on panel count enforced at render; new
  panel requires deleting an existing one. The cap forces explicit
  prioritisation in the ticket body ("this replaces dashboard/run-history").
- **No-honest-no-from-operator** — the operator says "would be nice to have"
  about every idea because they wrote the codebase and feel obligated to
  validate every suggestion. Counter: every ticket older than 30 days
  without a green CI auto-asks "still wanted? closes itself in 7d". The bot
  forces a yes/no by default-closing.
- **Drift from north-star** — the operator solves the immediate frustration
  by adding a knob, not by removing the cause. Counter: every
  `dashboard/<panel>` ticket body has a required line "what gets smaller if
  we fix this?". Empty answer auto-closes with the operator-minimal-input
  citation. The deletion-default rule from `CLAUDE.md` is mechanically
  enforced at the ticket boundary, not just the PR boundary.
- **Cargo-cult prior art** — the operator copies a Grafana / Backstage
  pattern that assumes N>1 users and pays the complexity tax for a feature
  that gives no benefit at N=1. Counter: every PR that adds a new feedback
  primitive answers "what's the N=1 read?" in the body. If the answer is
  "this only pays off when we have more operators", defer to Phase X with a
  reopen-trigger.

## Recommendation

Two mechanisms; both ship-this-week scope:

1. **In-page "Report a frustration" link** — small footer link on every
   dashboard page that opens a pre-filled GitHub issue URL in a new tab.
   URL shape:
   `https://github.com/<owner>/<repo>/issues/new?labels=dashboard/<panel-key>,UX&title=%5BUX%5D+dashboard%2F<panel-key>%3A+&body=<urlencoded-template>`.
   Template includes panel key, current view state (collapsed/expanded),
   page URL, operator-minimal-input checklist. No backend; GitHub stores
   the ticket; the panel key is in the label and the title. Pattern lifted
   directly from Backstage's TechDocs "Report an issue" link — single-file
   change per panel.

2. **Quarterly self-retro reading dashboard-labeled tickets** — recurring
   calendar item on the first Monday of every quarter. The retro reads
   `gh issue list --label dashboard --state all` from the last 90 days,
   groups by `dashboard/<panel-key>`, and produces a one-page write-up in
   `docs/engineer/retros/<YYYY-MM-DD>-dashboard.md` answering: which panel
   churned most, which panel got zero tickets (candidate for deletion),
   which frustrations recurred (candidate for structural fix vs. patch),
   what got smaller this quarter. The retro is the forcing function that
   converts low-volume ticket data into structural decisions. Optional:
   the retro dispatches a designer subagent to propose simplification PRs
   for the top-3 panels by ticket count.

This pair captures the per-incident frustration and the per-quarter pattern
without inventing a metrics pipeline. Both pieces are <50 LoC of glue.

## Open questions

- Should panel-hide events in `localStorage` migrate to a tiny on-disk
  JSON file so a fresh browser profile doesn't lose history? Worth it
  only when operator switches browsers regularly.
- Does the quarterly retro need a CI gate to enforce the cadence, or is a
  calendar reminder enough at N=1?
- When the operator dispatches an agent to triage the dashboard backlog,
  should the agent be allowed to auto-close tickets older than 90 days
  with no replies, or only propose closures? Lean: propose-only —
  `operator-minimal-input` does not mean operator-zero-decision on
  destructive ops.
- Is the in-page link the right primitive at all, or should the operator
  just type the issue directly because they are the only user? Lean:
  ship the link because the panel-key auto-fill prevents miscategorisation
  retroactively.

## References

- Grafana Dashboard Insights:
  `grafana.com/docs/grafana/latest/visualizations/dashboards/assess-dashboard-usage/`
- Grafana repo (license + version):
  `github.com/grafana/grafana`
- Backstage TechDocs:
  `backstage.io/docs/features/techdocs/`
- Backstage repo (Apache-2.0):
  `github.com/backstage/backstage/blob/master/LICENSE`
- Sentry User Feedback architecture:
  `develop.sentry.dev/application-architecture/feedback-architecture/`
- Sentry self-hosted (FSL-1.1-Apache-2.0):
  `github.com/getsentry/self-hosted`
- PostHog Surveys (MIT):
  `github.com/PostHog/posthog`
- `CLAUDE.md` (this repo) — `operator-minimal-input`,
  `deletion-default`, `default-simpler`, `recognize-session-end`.
