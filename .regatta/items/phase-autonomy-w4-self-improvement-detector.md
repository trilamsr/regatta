---
id: PHASE-AUTONOMY-W4
title: self-improvement detector — recurring-failure → self-improvement GH issue
lane: self-host
kind: feature
status: planned
gate: phase-autonomy-landing-3 (W1+W2+W3 merged)
source_ref: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md §11 W4
dependencies: PHASE-AUTONOMY-W1, PHASE-AUTONOMY-W2, PHASE-AUTONOMY-W3
linked_artifact: docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md
---

Source brief: PHASE AUTONOMY amendment §11 W4 (Landing 3, soft-depends on W5 to avoid blaming agents for cost-pause halts).

## Scope

Build `cmd/regatta-self-improve`: a daemon-side analyzer that reads substrate events for recurring failure patterns and files `[self-improvement]` GH issues with a root-cause hypothesis. The issue is auto-picked by the regatta loop → subagent writes a `feedback_*.md` memory file + amends the boot prompt + opens a dispatch-template PR. Loop closes itself.

Four triggers:

- Same gate-fail ≥3× in 7 days.
- Same banned-phrase token tripped ≥2× across distinct PRs.
- Same agent-failure-mode (e.g., "subagent claimed `make check` clean but CI tripped") ≥3× in 7 days.
- Same load-bearing-leftover pattern in ≥2 PR bodies.

## Approach

- Adopt the rolling-window count primitive from `hashicorp/go-set` (MPL-2) — already in `go.mod` family.
- Adopt the fingerprint shape from Sentry's grouping algorithm (reference; not imported).
- Build heuristic suite (~250 LoC) + per-heuristic YAML config at `internal/selfimprove/heuristics.yaml` (5 named heuristics initially; adding a 6th is one-file-PR).
- Build issue-body templating: detected pattern + 3+ source-event substrate links + root-cause hypothesis + one suggested edit (memory file path, boot prompt section, OR dispatch template).

## Acceptance criteria

- [planned] c1: Each of the four triggers fires a `[self-improvement]` issue with: detected pattern + 3+ source-event links + root-cause hypothesis + one suggested edit.
- [planned] c2: Dedup — re-firing the same pattern within 7 days comments on the open issue, no new issue.
- [planned] c3: Heuristic suite lives in `internal/selfimprove/heuristics.yaml`; adding a 6th heuristic is a single-file PR + a single Go test.
- [planned] c4: Subagent picking the issue can resolve it by writing a `feedback_*.md` file + opening a PR; the loop closes.
- [planned] c5: Adversarial reviewer subagent posts.

## B/A/A+ rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) c1 ships. (b) ≥3 of the 4 triggers wired. (c) Release-notes fence. |
| A (target) | B + (d) c2+c3+c4+c5. (e) Heuristics-coverage table in the PR body shows which substrate-event kinds each heuristic reads. |
| A+ (stretch) | A + (f) Each issue carries an estimated-time-saved number (operator hours/week if pattern eliminated). (g) Mutation test: each heuristic survives mutation of every other heuristic's threshold by ±50%. (h) Replay harness re-runs the substrate window deterministically and reproduces every filed issue. |

## Cites

- `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W4
- Sentry issue-grouping fingerprint — algorithm shape reference
- `grafana/oncall` (AGPL-3) — event-aggregation pattern
- `hashicorp/go-set` (MPL-2) — bounded-window count primitive (adopted)
- `feedback_decision_priority` — operator UX: self-improving loop = highest-leverage UX win
- `feedback_research_design_principles` — adopt-first; window primitive adopted, heuristics built
- `feedback_unaddressed_load_bearing` — one trigger directly closes the load-bearing-leftover pattern
