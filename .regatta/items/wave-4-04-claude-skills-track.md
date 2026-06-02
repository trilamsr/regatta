---
id: WAVE-4-04
title: track Claude Skills + Anthropic Claude Agent SDK releases
lane: self-host
status: planned
linked_artifact: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md
source_ref: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md §7.2 (Adopt — Anthropic Claude Agent SDK + Claude 4.6) + §7.3 (Track) + §11 MVR-2 dispatch list
---

Source: unified next-horizon roadmap §7.2 (Adopt — Anthropic Claude Agent SDK substrate) + §7.3 (Track — quarterly cadence). Background context: `docs/engineer/research/2026-06-02-wedge-wave-4-emerging-tech.md` §4 + §7.

Brief: Anthropic's Claude Skills catalog + Claude Agent SDK are the regatta substrate; staying current is a standing concern, not a one-shot dispatch. Scope: lightweight standing-task — quarterly check on Anthropic catalog policy changes, SDK breaking changes, and `/skills` UX shifts that affect WAVE-4-01 manifest validity. Bet-against signal per §0 6-mo: <50 combined installs/mo or Anthropic narrows Skills to first-party only by 2026-12-01 — both observable signals to bake into the standing check. Cadence: re-evaluate every Anthropic minor-version bump or every 90 days, whichever first.

## Acceptance criteria

- [planned] c1: Standing-check checklist at `docs/engineer/standing/claude-skills-watch.md` (or sibling) listing the catalog policy URL, SDK changelog URL, install-count source, 90-day review cadence.
- [planned] c2: First review iteration logged in the standing-check file with date + observed signals against the §0 6-mo bet-against thresholds.
- [planned] c3: Item resolves to closed-but-recurring (not closed-done) so the next horizon refresh can re-cite it without a new dispatch.
