---
id: MVR-1-T2
title: regatta init bundle - clone-to-first-PR ergonomics (init wizard + GoReleaser + GH-issue adapter)
lane: customer
kind: program
status: planned
gate: mvr-1-entry (30-day-self-host-green OR named persona-A inbound)
source_ref: docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md §6 MVR-1-T2/T3/T4 (PR #399) - bundled into one program per dispatch
dependencies:
linked_artifact: docs/engineer/briefs/2026-06-02-next-horizon-customer-roadmap.md
---

Source briefs: #399 §3 (NEW wedges: init wizard / release pipeline / GH-issue adapter) + §6 MVR-1-T2/T3/T4 + #408 §6 cross-phase budget table.

Phase-MVR-1 wedge 2 of 4 per #421 ADOPT-WITH-AMENDMENTS verdict. Closes G1 (init wizard) + G3 (GH-issue adapter) + G4 (binary release) - the three adoption-cost blockers that bounce persona A at minute 5.

## Scope

Bundle three small adoptions into one program so persona A's first 30 minutes is `go install` → `regatta init` → first PR dispatched against a GH-issue labelled `[autonomous]`.

Three child tasks under this program item:

- Init wizard - `regatta init` runs an AlecAivazis/survey TUI; probes `gh auth status` + asks for repo path; writes `regatta.yaml` + `.regatta/items/` scaffold.
- GoReleaser pipeline - single `.goreleaser.yaml`; builds darwin/linux/windows binaries on tag push; uploads to GH Release.
- GH-issue adapter - implements `schemas.SpecAdapter` against `[autonomous]`-labelled issues via go-github (already a runtime dep). Round-trip schema parallel to markdown adapter.

## Approach

- AlecAivazis/survey/v2 (MIT) - adopted per #399 §3 init-wizard score table; pure-Go TUI, no runtime dep beyond stdlib.
- GoReleaser (Apache 2.0) - adopted per #399 §3 release-pipeline score table; single YAML config.
- go-github (BSD-3) - already a runtime dep; the GH-issue adapter reuses the same client the cost-governor PR-watcher uses.
- All three sub-adoptions ship as one PR program; sub-PRs land independently but the program closes when persona A can complete the install → init → first-PR flow without reading source.

## Acceptance criteria

- [planned] c1: `regatta init` runs interactively, writes a valid `regatta.yaml` + `.regatta/items/_template.md`, round-trips through the markdown adapter on first poll.
- [planned] c2: `git tag v0.x.0 && git push --tags` triggers GoReleaser; darwin-arm64 + linux-amd64 binaries appear on GH Release within 10 min.
- [planned] c3: GH-issue adapter implements `schemas.SpecAdapter` (List + Get + UpdateStatus + Capabilities); `[autonomous]`-labelled issues round-trip to `schemas.WorkItem` with stable `SourceRef`.
- [planned] c4: GH-issue adapter shares a second-consumer contract proof against `internal/orchestrator/adapter/markdown.go` per `feedback_research_design_principles` "no proven equivalent for exact shape".
- [planned] c5: README install snippet works on a fresh macOS/Linux box - one `go install` line OR one `curl | tar` line; persona A reaches first PR under 30 min on a clean repo.
- [planned] c6: Reviewer subagent spawned + cleared per `feedback_agent_pr_review`.

## B/A/A+ rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) `regatta init` writes a working `regatta.yaml`. (b) GoReleaser config builds at least darwin-arm64 + linux-amd64. (c) GH-issue adapter round-trips one label. (d) Release-notes fence in PR body. |
| A (target) | B + (e) Init wizard probes `gh auth status` + `ANTHROPIC_API_KEY` + offers a copy-paste recovery if either is missing. (f) GoReleaser also publishes a `brew tap` formula. (g) GH-issue adapter handles status transitions (`[in-progress]` / `[done]` labels) bidirectionally. (h) E2E test runs `regatta init` against a tmpdir + verifies the markdown adapter loads the output. (i) `go install` works against a public tag. |
| A+ (stretch) | A + (j) Persona-A demo video - 30-min recording on a clean box reaches first merged PR; posted to `docs/demos/`. (k) Init wizard surfaces a known persona-A example repo (langchain / prefect / dagster / temporal / n8n / langflow per #399 §1 table) as a try-it-now suggestion. (l) Adversarial reviewer subagent re-scores against this rubric. (m) Sub-PR program lands within 1-2 wks total per #399 §6 MVR-1-T2/T3/T4 effort. |

## Cites

- #399 §3 NEW wedges (init wizard / release pipeline / GH-issue adapter score tables)
- #399 §6 MVR-1-T2/T3/T4
- #408 §6 cross-phase budget table (MVR-1 OSS adoptions: survey + GoReleaser + go-github)
- `feedback_research_design_principles` - adopt-over-build for all three sub-deliveries
- `feedback_decision_priority` - first-30-min-friction is the customer-0 conversion funnel
