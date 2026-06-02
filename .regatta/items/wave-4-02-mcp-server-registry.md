---
id: WAVE-4-02
title: publish regatta as MCP server in official + GitHub registries
lane: self-host
status: planned
dependencies: WAVE-4-01
linked_artifact: https://github.com/trilamsr/regatta/pull/401
---

Source: docs/engineer/research/2026-06-02-wedge-wave-4-emerging-tech.md §4 (Adopt — MCP Server Registry + GitHub MCP Registry) + §7 (registry GA mid-2026)

Brief: Official MCP Server Registry (modelcontextprotocol.io) now lists 2k+ servers; GitHub MCP Registry adds discoverability inside GH workflows. Dual-publish (Claude Skill + MCP server) is the §4 recommendation, but the F7 maintenance-tax amendment gates the second channel on the first returning ≥10 installs/mo for two consecutive months. This item stays `planned` until WAVE-4-01's install-signal threshold trips. Scope at unblock: author MCP server manifest exposing regatta's orchestrator + adapter + gate primitives as MCP tools; submit to both registries. Source-neutrality note per F5: validate registry presence claims against the registries' own published catalogs (not vendor blogs).

## Acceptance criteria

- [planned] c1: MCP server manifest at `mcp/server.json` (or per registry schema) exposes orchestrator + gate tool surface; passes `mcp dev` smoke against the regatta harness.
- [planned] c2: Both registry submissions (official MCP Registry + GitHub MCP Registry) accepted; entries link back to the regatta repo + docs.
- [planned] c3: WAVE-4-01 install-signal gate (≥10 installs/mo × 2 months) is met before this item moves to in_progress; gate verification cited in dispatch.
