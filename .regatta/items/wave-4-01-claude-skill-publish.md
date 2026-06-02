---
id: WAVE-4-01
title: publish regatta capabilities as Claude Skill bundle
lane: self-host
status: planned
linked_artifact: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md
source_ref: docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md §7.2 (Adopt — Claude Skills + MCP Registry) + §11 MVR-2 dispatch list
---

Source: unified next-horizon roadmap §7.2 (Adopt) + §7.5 G1 (4 Adopt rows wired into MVR-2 scope). Original research preserved as background context only: `docs/engineer/research/2026-06-02-wedge-wave-4-emerging-tech.md` §4 + §7.

Brief: Anthropic Claude Skills + Plugins catalog now carries 101 official + 68 partner + 132 community skills; v2.1.137+ slash-prompts (May 2026) make `/skills` discoverable inside the harness. Single-channel absence = invisibility risk by 2026-12-01 per §0 bet-against signal. Scope: author a Skill manifest covering the regatta capabilities the harness can invoke (dispatch templates, gate prompts, autonomous-session boot prompt), submit to the official Anthropic directory. Defer dual-publish (MCP server, see WAVE-4-02) until this channel returns ≥10 installs/mo for two consecutive months — F7 maintenance-tax gate, ≈4 hr/mo overhead per channel. UX-first decision priority (single install path beats two until signal arrives).

## Acceptance criteria

- [planned] c1: Skill manifest authored under `skills/regatta/` (or repo-root `.skill.json` per Anthropic schema) with name, description, version, capabilities, install instructions; passes Anthropic vetting submission.
- [planned] c2: Install-count telemetry source identified (Anthropic catalog page or vendor-published metric) so the F7 gate (≥10 installs/mo × 2 months) is measurable before WAVE-4-02 unblocks.
- [planned] c3: Dispatch brief cites this item's `linked_artifact` (`docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md`) and the §7.2 maintenance-tax framing; implementer PR posts B/A/A+ scorecard verbatim per `feedback_grade_rubric`.
