---
status: active
phase: standard-operating-procedure
title: Label SOP — canonical 25-label set portable across any repo
---

# Label SOP

Universal label vocabulary for any repo running the same operating model as regatta. The set is intentionally tight — 25 labels across 5 dimensions — so any operator (or autonomous agent) can triage a new issue in one pass without spelunking the label catalog.

## §1 Goal

One label answers one question. Five dimensions cover every question worth asking at triage time. Per-feature, per-wave, per-umbrella scoping lives in the issue body (cross-link to the umbrella issue), NOT in a label. The number of labels stays constant as the repo grows.

## §2 The five dimensions

Every issue or PR carries AT LEAST `kind:*` + `severity:*` (or `priority:*` for non-finding work). `state:*` and `scope:*` are optional.

### kind (8) — what type of work is this?

| Label | Meaning |
|---|---|
| `kind:bug` | Defect: behavior deviates from spec or doc. |
| `kind:feat` | New capability. |
| `kind:chore` | Tooling, dependency bump, repo housekeeping. No user-visible change. |
| `kind:docs` | Documentation-only change (specs, briefs, README). |
| `kind:refactor` | Internal restructure; no behavior change. |
| `kind:test` | Test-only addition or fix. |
| `kind:wedge` | Umbrella tracking issue holding N slices. |
| `kind:slice` | One implementation slice under an umbrella; body links the parent. |

### severity (4) — for findings + bugs

| Label | Meaning |
|---|---|
| `severity:critical` | Production outage, security exploit, data loss; drop everything. |
| `severity:high` | Real defect, no workaround, must fix before next release. |
| `severity:medium` | Real defect with workaround, OR latent risk in a load-bearing surface. |
| `severity:low` | Cosmetic, edge case, deferred fine-tuning. |

Pair `kind:bug` + `severity:*` or `kind:slice` (reviewer-finding) + `severity:*`. Features and chores use `priority:*` below instead.

### priority (3) — for feature / chore work where severity is N/A

| Label | Meaning |
|---|---|
| `priority:p0` | This-sprint critical-path. Blocks other work. |
| `priority:p1` | Next-sprint candidate. Ships when p0 clears. |
| `priority:p2` | Backlog. Picked up at operator discretion. |

Anything more granular (p3, p4) lives in the body or umbrella ranking — not a label.

### state (4) — workflow status, NOT priority

| Label | Meaning |
|---|---|
| `state:blocked` | Cannot start until a named prerequisite closes. Body MUST name the blocker. |
| `state:followup` | Deferred during PR review; reopen-trigger documented in body. |
| `state:parking` | Forward-fit deferral (= regatta's "phase-x"). Out of current scope; reopen-trigger required. |
| `state:in-review` | Has an open PR or is under reviewer-subagent pass. |

### scope (4) — orthogonal cross-cuts that trigger extra review

| Label | Meaning |
|---|---|
| `scope:security` | Auth, secrets, sandbox, supply chain, injection — triggers security review. |
| `scope:ci` | CI gates, GitHub Actions, lint, test infrastructure. |
| `scope:perf` | Latency, throughput, memory, cost — change must show measurement. |
| `scope:ux` | Operator-facing CLI, docs, error messages — change must include the operator's eyeball. |

### special (2)

| Label | Meaning |
|---|---|
| `good-first-issue` | Self-contained, well-scoped, documented; safe for a new contributor. |
| `duplicate` | Closing as duplicate of another tracked issue. Body MUST link the parent. |

**Total: 25.**

## §3 Where per-feature / per-wave / per-umbrella tracking lives

Drop the legacy `mvp-2`, `mvp-3`, `W6-followup`, `cost-governor-followup`, `OBS-followup`, `billing-followup`, `dw-superset`, `regatta-on-arbitrary-repo`, `phase-x`, `autonomous`, `tdd-justified` labels. Replace with:

- **Umbrella issue per scope**: one `kind:wedge`-labeled issue holds the roadmap (e.g. "[wedge] cost-governor #727", "[wedge] phase-x parking #NNNN"). The umbrella body lists open child issue numbers.
- **Body cross-link in children**: every child issue body opens with `Umbrella: #NNNN`. Tooling and operators find siblings via `gh issue view <umbrella>`.
- **Discoverability**: `gh issue list --label kind:wedge --state open` lists every active umbrella. Per-feature dashboards live IN the umbrella body, not in labels.

This trades label-name discoverability (no `--label mvp-3`) for label-set stability (no churn as waves close).

## §4 Migration from the current 33-label set

| Drop / archive | Replacement |
|---|---|
| `mvp-2`, `mvp-3` | Umbrella issue per wave; close labels when waves ship. |
| `W6-followup`, `OBS-followup`, `cost-governor-followup`, `billing-followup` | Per-wave umbrella issue + `Umbrella: #N` body link. `state:followup` covers the workflow state. |
| `dw-superset`, `regatta-on-arbitrary-repo` | Same: umbrella issue + body link. |
| `phase-x` | `state:parking` (universal name). |
| `autonomous` | Per-repo runtime concern; if needed, separate machine-readable label `auto:claim` lives in a private file, NOT GitHub. |
| `tdd-justified` | Per-PR body marker `<!-- tdd-justified: <reason> -->`, NOT a label. |
| `bug`, `documentation`, `enhancement` | `kind:bug`, `kind:docs`, `kind:feat`. |
| `tech-debt` | `kind:refactor` + `priority:p2`. |
| `help wanted`, `question`, `invalid`, `wontfix`, `good first issue` | Drop except `good-first-issue` (with hyphen, lowercase). Wontfix → close issue. Question → discussions. Invalid → close. Help-wanted → `priority:p2`. |
| `blocked-by` | `state:blocked`. |
| `kind:reviewer-finding` | `kind:bug` + `severity:*`. Reviewer findings ARE bugs. |
| `followup` | `state:followup`. |

## §5 Color palette (deterministic + WCAG AA)

| Label class | Color | Hex |
|---|---|---|
| `kind:bug` / `severity:high` / `state:blocked` | red | `D93F0B` |
| `severity:critical` | dark red | `B60205` |
| `kind:feat` / `severity:low` | green | `0E8A16` |
| `kind:chore` / `kind:refactor` | grey | `BFD4F2` |
| `kind:docs` | blue | `0075CA` |
| `kind:test` | cyan | `1D76DB` |
| `kind:wedge` / `kind:slice` | light blue | `C5DEF5` |
| `severity:medium` / `priority:p1` | yellow | `FBCA04` |
| `priority:p0` | orange | `D93F0B` |
| `priority:p2` | light grey | `EEEEEE` |
| `state:followup` / `state:in-review` | light blue | `C5DEF5` |
| `state:parking` | light grey | `EEEEEE` |
| `scope:security` | purple | `5319E7` |
| `scope:ci` | dark blue | `0052CC` |
| `scope:perf` / `scope:ux` | green-blue | `006B75` |
| `good-first-issue` | indigo | `7057FF` |
| `duplicate` | white-grey | `CFD3D7` |

## §6 Apply across repos

`scripts/regatta-labels.sh` applies this canonical set to any repo via `gh label create` / `gh label edit` / `gh label delete`. Drift-detection mode (`--check`) fails non-zero when the repo's label set diverges. Wire into per-repo nightly CI for fleet-wide consistency.

## §7 Reopen-trigger to revisit the SOP

- Three+ operators report the 25-label set forces "label gymnastics" on real triage (e.g. a defect needing two `scope:*` labels).
- A new dimension emerges that none of the 5 covers (e.g. a `compliance:*` axis for repos handling regulated data).
- Total active labels under SOP exceed 30 in any single repo for >30 days — signals the per-feature-in-body discipline is breaking.

Until then, the SOP is the standing rule.
