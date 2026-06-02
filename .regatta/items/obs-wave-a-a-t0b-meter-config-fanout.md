---
id: OBS-WAVE-A-T0b
title: Config.Meter retrofit fan-out — 6 remaining Config structs (scheduler, spawner, substrate, history, followup, +1)
lane: observability
status: planned
dependencies: OBS-WAVE-A-T0a
linked_artifact: docs/engineer/specs/2026-06-02-observability-roadmap.md
---

Source spec: `docs/engineer/specs/2026-06-02-observability-roadmap.md` §2.4 (impl seam), §7 Wave-A table row A-T0b (post-amendment split).
Amendment ref: review of PR #410 §3 (A-T0a/A-T0b split rationale — same `setup.go` file forces serial after A-T0a).

## Task

Add `Config.Meter metric.Meter` field to the 6 remaining Config structs that already carry `Config.Tracer` from W6 (#159). Mirror the W6 Tracer DI pattern verbatim per `feedback_spec_pattern_authority`: nil falls back to `otel.Meter("<component-name>")`.

Target Config structs (cross-reference the W6 Tracer-DI fan-out PR list to confirm exact set):

1. `internal/orchestrator/scheduler` Config (unblocks A-T3 — scheduler-tick histogram).
2. `internal/orchestrator/spawner` Config (unblocks C-T1 + C-T4).
3. `internal/orchestrator/state/substrate` Config (unblocks B-T1 + B-T2 + B-T3).
4. `internal/history` Config (unblocks B-T4 — replay-latency histogram).
5. `internal/orchestrator/followup` Config (unblocks D-T1 — adversarial findings).
6. Sixth Config — confirm from the W6 Tracer-DI fan-out (likely `internal/obs/triggers` or the adapter Config touched in #159).

If a target package does not yet have a `Config.Tracer` field, file a tracking issue rather than retrofitting both — that is W6 scope, not this PR's.

**File-ownership fences (carry-forward from A-T0a per `feedback_shared_primitive_owner`):**

- Substrate retrofit edits the Config-bearing file ONLY. Do NOT touch `event.go` (B-T1 owner), `sign.go` (B-T2 owner), or `divergence_emit.go` (B-T3 owner — new file anyway).
- Scheduler retrofit edits Config struct file ONLY. Do NOT touch the tick path (A-T3 owner).
- Spawner retrofit edits Config struct file ONLY. Do NOT touch the spawn path (C-T1 owner) or wrap-CI-classification (C-T4 owner).
- History retrofit edits Config struct file ONLY. Do NOT touch `substrate_impl.go` Replay path (B-T4 owner).
- Followup retrofit creates `internal/orchestrator/followup/config.go` if no Config struct exists yet (D-T1 owner adds the triage logic in a sibling file).

Per `feedback_research_design_principles`: copy the W6 Tracer-DI fan-out diff verbatim, swap `Tracer` for `Meter` and `trace.Tracer` for `metric.Meter`. Touch no logic.

## B/A/A+ rubric (cite spec §8)

- **B (floor):** `make check` clean; all 6 Config structs carry `Meter metric.Meter` with the documented nil-fallback (spec §2.4); each Config's existing test exercises both the nil-fallback and an explicit-meter path.
- **A (target):** B + adversarial reviewer subagent clears; A1 + A4 from spec §8. Zero edits to the non-Config files in each of the 6 packages (mirrors A-T0a file-ownership fence).
- **A+ (stretch):** A + grep-test `TestEveryConfig_TracerHasMatchingMeter` asserts every Config that carries `Tracer` also carries `Meter` (prevents future drift); landed in this PR.

## Acceptance criteria

- [planned] c1: All 6 Config structs gain `Meter metric.Meter` field with nil-fallback to `otel.Meter("<component>")` (spec §2.4).
- [planned] c2: Each retrofit is scoped to the Config-bearing file; no logic edits in scheduler/spawner/substrate/history/followup paths (amendment §7 file-ownership fence carry-forward).
- [planned] c3: `make check` clean; existing tests still pass; nil-meter callers see the no-op fallback.
- [planned] c4: PR body carries A+ rubric scorecard + release-notes fence; submitted via `--body-file`.
