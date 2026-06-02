---
id: WAVE-4-NIT-5
title: tier-2 vendor-signal caveat — RESOLVED (row not retained in unified brief)
lane: self-host
status: closed-resolved
linked_artifact: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md
source_ref: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md §7.4 (Bet against)
---

Source: unified next-horizon roadmap §7.4. The G5 nit targeted a "§0 6-mo failure-signal row" in the superseded wave-4 brief; the unified brief's §7.4 "Bet against" table does NOT carry the vendor-announcement-as-failure-signal row that needed the tier-2 caveat. The principle (vendor announcements are observable but lower-confidence than metrics) carries through into WAVE-4-01 acceptance criterion c2 ("install-count telemetry source identified... so the F7 gate is measurable").

Status: closed-resolved (row not retained; caveat is moot).

## Acceptance criteria

- [closed] c1: Verified — unified brief §7.4 carries no vendor-announcement-as-failure-signal row needing tier-2 annotation.
- [closed] c2: WAVE-4-01 c2 captures the measurement-discipline equivalent (install-count over vendor-announcement).
