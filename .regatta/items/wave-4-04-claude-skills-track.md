---
id: WAVE-4-04
title: track Claude Skills + Anthropic Claude Agent SDK releases
lane: self-host
status: planned
linked_artifact: https://github.com/trilamsr/regatta/pull/401
---

Source: docs/engineer/research/2026-06-02-wedge-wave-4-emerging-tech.md §4 (Adopt — primary distribution) + §7 (Apr 2026 Claude Agent SDK + Claude 4.6; May 2026 v2.1.137+ `/skills`)

Brief: Anthropic's Claude Skills catalog + Claude Agent SDK are the regatta substrate; staying current is a standing concern, not a one-shot dispatch. Scope: lightweight standing-task — quarterly check on Anthropic catalog policy changes, SDK breaking changes, and `/skills` UX shifts that affect WAVE-4-01 manifest validity. Bet-against signal per §0 6-mo: <50 combined installs/mo or Anthropic narrows Skills to first-party only by 2026-12-01 — both observable signals to bake into the standing check. Cadence: re-evaluate every Anthropic minor-version bump or every 90 days, whichever first.

## Acceptance criteria

- [planned] c1: Standing-check checklist at `docs/engineer/standing/claude-skills-watch.md` (or sibling) listing the catalog policy URL, SDK changelog URL, install-count source, 90-day review cadence.
- [planned] c2: First review iteration logged in the standing-check file with date + observed signals against the §0 6-mo bet-against thresholds.
- [planned] c3: Item resolves to closed-but-recurring (not closed-done) so the next horizon refresh can re-cite it without a new dispatch.
