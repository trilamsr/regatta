---
title: "Regatta on arbitrary repo — generalization umbrella"
status: active
phase: mvr-1
summary: "Umbrella spec for the gap between today's posture (regatta works against one repo — itself — at full quality) and the target posture (regatta orchestrates ANY repo with minimal operator setup). Today `regatta serve` is repo-agnostic at runtime (see `docs/engineer/specs/2026-06-07-multi-target-repo.md` Option A; #933 supervisor --name), but quality drops sharply on a target without CLAUDE.md / dispatch templates / scorecard / tuned L4 prompts, and cross-repo features (work-item routing, dep cascade) are missing. Five sequential slices: L1 bundled-default prompt baseline → L2 adaptive enrichment from target conventions → L3 per-repo override loader → L4 per-repo quality-feedback meters → cross-repo work-item routing. Shared selfimprove learning + multi-tenant secrets stay Phase-X. Adversarial pass per layer. Default-simpler: 5 slices, deliberate sequencing."
date: 2026-06-08
---

# Regatta on arbitrary repo — generalization umbrella

_Author: design session, 2026-06-08. Source: operator question — "is regatta extensible to ANY codebase?". Companion specs: `docs/engineer/specs/2026-06-07-multi-target-repo.md` (#929 Option A landed via #933 — supervisor --name namespacing), `docs/engineer/specs/2026-06-07-bring-your-own-agent.md` (#930 BYOA spawner adapter), `docs/engineer/specs/2026-06-07-byom-model-providers.md` (#928 BYOM), `docs/engineer/specs/2026-06-08-self-host-smoke-harness.md` (#864), `docs/engineer/specs/2026-06-07-autotuner-closed-loop.md` (#955 live-outcome closed loop). Memory cites: `feedback_default_simpler`, `feedback_decision_priority`, `feedback_single_user_priority`, `feedback_no_signatures`, `feedback_root_cause`, `feedback_deletion_default`, `feedback_validate_before_ship`, `feedback_adversarial_review_every_step`, `feedback_audit_main_before_implementing`._

```release-notes
[DOCS] Spec umbrella for regatta-on-arbitrary-repo generalization.
Five sequential slices — L1 bundled prompt baseline, L2 adaptive
enrichment, L3 per-repo override, L4 quality-feedback meters,
cross-repo work-item routing. Cross-repo dep cascade + shared
selfimprove learning stay Phase-X.
```

## 1. Problem framing

`docs/engineer/specs/2026-06-07-multi-target-repo.md` proved the daemon runtime is per-target-clean: two `regatta serve` processes against two clone roots run side-by-side. Option A (one daemon per target) shipped via #933 (`regatta install-service --name <name>`). That is the **horizontal axis** — N targets, one operator.

The **vertical axis** is open: when the target ≠ this repo, regatta loses the implicit knowledge that lives in this repo's `CLAUDE.md`, `docs/engineer/dispatch-templates/`, scorecard convention, and the L4 prompt tuning observed via #852-style cache savings + observed merge rate. Quality drops:

1. Worker prompt is the bundled default — no per-repo style guide, no banned-phrase list, no decision-priority rule, no comment-discipline rule.
2. Dispatch templates fall back to defaults — reviewer pass is weaker (no lens taxonomy, no project-specific risk list).
3. No scorecard convention on target → grade variance higher; `scripts/check-scorecard.sh` is regatta-specific.
4. L4 prompt tuning unverified — verdict accuracy + first-PR-merged rate measured only on this repo.
5. Roadmap discovery — operator pre-files issues by hand against the new target.

And cross-repo features are absent entirely:

6. Work-item routing — one issue (e.g. "bump shared lib X to v2.0") needs PRs in N repos; today the operator opens N issues by hand.
7. Cross-repo dep cascade — a lib bump in repo-foo doesn't auto-file a consume-it issue in repo-bar.
8. Shared selfimprove learning — patterns detected by `internal/selfimprove/detector.go` on repo-foo don't carry to repo-bar.
9. Multi-tenant secrets — Phase-X W8.

This spec covers items 1-7. Items 8-9 stay Phase-X with named reopen-triggers per `CLAUDE.md` self-host filter.

## 2. Goal

Specify the path from "works on this repo at full quality, works on other repos at degraded quality" → "works on any repo at quality ≥ today's baseline, with cross-repo work-item routing as a new capability". NOT shared selfimprove learning (Phase-X, item 8). NOT multi-tenant (Phase-X, item 9 — DIFFERENT axis from this spec: tenant = customer-of-regatta, target = repo-of-operator).

Operator north-star: clone a new repo, run `regatta install-service --name <repo>`, `regatta init --no-claude-md`, and get ≥ baseline merge-rate on the first PR within the live-outcome smoke window (#864 §5).

## 3. Generalization layers — L1 → L4

Four sequential layers. Each layer is independently shippable; each is a strict superset of the prior.

### L1 — Repo-agnostic prompt baseline

**Problem.** Today `internal/orchestrator/spawner/claude.go::defaultPromptBuilder` injects a prompt that assumes the target repo has the rules `CLAUDE.md` ships. On a target without `CLAUDE.md` the bundled prompt either (a) injects this-repo-specific guidance the worker can't satisfy ("see `feedback_*` slugs" — slugs don't exist), or (b) injects nothing and the worker drifts.

**Design.** Two bundled-default prompt fragments shipped inside the regatta binary at `assets/prompts/`:

```
assets/prompts/
  CLAUDE.md.default              # universal decision-priority, TDD, no-signatures, deletion-default
  dispatch-templates/
    implementer.default.md
    reviewer.default.md
    designer.default.md
    triage.default.md
```

`defaultPromptBuilder` resolution order:

1. If target repo has `CLAUDE.md` at the worktree root → use it verbatim.
2. Else → inject `assets/prompts/CLAUDE.md.default`.
3. Same fallback for `docs/engineer/dispatch-templates/*.md`.

The defaults distill the universal rules from this repo's `CLAUDE.md` (decision priority, TDD-first, no AI signatures, deletion default, root cause only, comments WHY not WHAT). They drop repo-specific items (`feedback_*` slugs, `scripts/check-*.sh` gates, the self-host filter narrative, regatta-only paths).

**Operator override.** Target repo with its own `CLAUDE.md` overrides verbatim. No merge, no patch — operator-authored copy wins. This matches the principle of least surprise: the operator's file on disk is authoritative.

**Acceptance.**
- L1.1: `regatta serve` against a target with NO `CLAUDE.md` injects `assets/prompts/CLAUDE.md.default` into the worker.
- L1.2: `regatta serve` against a target WITH `CLAUDE.md` injects that file verbatim; bundled default is unused.
- L1.3: Bundled default carries zero `feedback_*` slug references, zero `scripts/check-*.sh` references, zero `regatta`-specific paths.
- L1.4: First-PR-merged rate on a clean target ≥ baseline (smoke harness #864 measures).

### L2 — Adaptive prompt enrichment

**Problem.** L1 gives a baseline. Targets vary — a Go monorepo wants different prompt hints than a Rust crate or a TypeScript SPA. The bundled default is language-agnostic by design, so quality is bounded by the lowest common denominator.

**Design.** Best-effort scanner reads target repo conventions at `serve` startup and enriches the worker prompt:

- `CONTRIBUTING.md` → append to worker prompt verbatim (capped at 200 lines).
- `AGENTS.md` → append verbatim (this is becoming a convention — `agentsmd.org`, observed in 2025-2026 OSS).
- `.editorconfig` → derive indent style + line-ending rules.
- `go.mod` / `package.json` / `Cargo.toml` / `pyproject.toml` → derive primary language + test command hint.
- `Makefile` top-level targets → derive test / build / lint commands.
- `.github/PULL_REQUEST_TEMPLATE.md` → derive PR-body structure hint.

Enrichment is **append-only**. Bundled default + enrichment fragments are concatenated; later wins on conflict. Enrichment fragments live in the worker context window, not on disk in the target repo.

**Operator override.** L2 enrichment runs by default; operator can disable per target with `regatta.yaml::prompts.adaptive_enrichment: false`.

**Acceptance.**
- L2.1: Polyglot repo with both `go.mod` and `package.json` injects BOTH language hints (no race, no silent drop).
- L2.2: Repo with no convention files yields L1 baseline only — no fabricated hints.
- L2.3: Enrichment scanner timeout ≤ 200ms wall (file-system reads only; no network). Timeout → fall back to L1 baseline.
- L2.4: Enrichment fragment cap 5KB per source file; total enrichment cap 20KB. Larger → truncate w/ marker `...[truncated by L2 enrichment cap]`.

### L3 — Repo-specific prompt tuning

**Problem.** L1 + L2 are read-only; the operator can't say "for repo-foo, always remind the worker about our weird build flag". A clone-and-go target has no place to stash repo-specific worker hints.

**Design.** Optional `regatta-prompts/` directory in the target repo:

```
regatta-prompts/
  CLAUDE.md         # full override (replaces, doesn't append, the root CLAUDE.md)
  implementer.md    # appended after dispatch-template/implementer.md
  reviewer.md       # appended after dispatch-template/reviewer.md
  designer.md       # appended after dispatch-template/designer.md
```

Resolution order per prompt slot:
1. `regatta-prompts/<role>.md` if present → append to L1 baseline + L2 enrichment.
2. Root `CLAUDE.md` if `regatta-prompts/CLAUDE.md` absent → use root version.
3. Bundled default otherwise.

`regatta-prompts/` is committed to the target repo — the operator manages it like any other source file. Regatta does not write to it (`internal/selfimprove/detector.go` proposals fire as PRs against `regatta-prompts/`, not direct writes).

**Operator override.** `regatta-prompts/CLAUDE.md` overrides the root `CLAUDE.md` to allow per-target divergence (e.g. one branch / one project subdir treats decisions differently). Default: absent ⇒ root `CLAUDE.md` wins ⇒ falls back to L1 default if root absent.

**Acceptance.**
- L3.1: Target with `regatta-prompts/implementer.md` injects bundled default + L2 enrichment + this file (in that order).
- L3.2: Target with `regatta-prompts/CLAUDE.md` and root `CLAUDE.md` → `regatta-prompts/CLAUDE.md` wins.
- L3.3: `regatta-prompts/` is treated as a normal source dir — `internal/orchestrator/spawner` cannot write to it directly. Detector proposals route via the autotuner PR path (`docs/engineer/specs/2026-06-07-autotuner-closed-loop.md`).
- L3.4: File-size cap 50KB per file in `regatta-prompts/`; oversize → log warning + skip that file (do not crash).

### L4 — Quality-feedback loop per target

**Problem.** L1 + L2 + L3 widen the prompt surface; without measurement we don't know if quality on target-foo regressed or held. The autotuner closed-loop spec (#955) measures THIS repo via live-outcome rates; no per-target dimension today.

**Design.** Two per-target meters emitted to OTEL:

- `regatta.target.l4_verdict_accuracy{target=<name>}` — sample of L4 verdicts vs subsequent merge / revert outcome. Daily rolling window. Computed by `internal/observability/metrics/target_quality.go` (new file).
- `regatta.target.first_pr_merge_rate{target=<name>}` — fraction of issues that produced a `mergedAt != null` PR within 24h of issue creation. Daily window.

When either meter drops below a configurable threshold (default 0.7 for verdict accuracy, 0.5 for merge rate) for ≥ 3 consecutive days, regatta files a follow-up issue against the target (`[autonomous] L4 quality regression on <target>`) with the meter trace attached. Operator triages.

Threshold + window configurable via `regatta.yaml::quality.target_thresholds:`. Zero-value default: thresholds above, window 3d.

**Acceptance.**
- L4.1: Per-target verdict-accuracy meter emits with `target=<name>` label after a single L4 verdict pass.
- L4.2: Per-target merge-rate meter emits with `target=<name>` label after a single issue closes (merged or abandoned).
- L4.3: Sub-threshold trigger files exactly ONE follow-up issue per (target, threshold-breach-window); de-duped via state-machine substrate per `internal/orchestrator/state/machine.go`.
- L4.4: Meter trace attaches as a code-fence block to the follow-up issue body for operator triage.

## 4. Cross-repo work-item routing (new capability)

**Problem.** A single conceptual change ("bump shared lib X to v2.0", "rename API symbol Y") today requires the operator to file N issues across N repos. The operator's intent is one work-item; the system requires N.

**Design.** Issue body markup parsed by `internal/orchestrator/adapter/githubissues/adapter.go`:

```markdown
<!--regatta: routes: [owner/repo-bar, owner/repo-baz] -->
```

When regatta consumes an `[autonomous]` issue carrying the routes marker, it:

1. Opens the local PR in the source repo as today.
2. For each listed `<owner>/<repo>`, creates a sibling work-item in that repo's regatta daemon (Option A multi-target) with the same brief, an explicit `<!--regatta: routed_from: <source-repo>#<source-issue> -->` back-pointer, and a `routed-from-N` label.
3. Each sibling work-item flows through that target's independent scheduler / spawner / orchestrator — no shared branch, no shared commit, no shared PR. Per-repo branch (`autonomous/<source-repo>-<source-issue>`), per-repo commit, per-repo PR.

**Coordinate-merge (optional).** Issue body may carry:

```markdown
<!--regatta: coordinate_merge: true -->
```

If set, each PR gets a `coordinate-merge` label and a status check that fires only when ALL sibling PRs have `mergeStateStatus = CLEAN`. The check fails closed until all are ready. Operator must explicitly merge in dependency order (regatta does not auto-merge coordinated PRs — too risky on a cycle).

**Operator override.** `coordinate_merge` default = false. `routes:` default = empty (no routing). Sibling daemons must be installed via `regatta install-service --name <repo>` independently — regatta does not auto-install daemons on demand.

**Acceptance.**
- R.1: Issue with `routes: [a, b]` opens local PR + spawns sibling work-items in repo-a and repo-b within the local adapter poll cycle.
- R.2: Sibling work-items carry back-pointer + `routed-from-N` label.
- R.3: `coordinate_merge: true` blocks merge on all siblings until each is CLEAN. Plain merge is allowed (false default).
- R.4: Missing sibling daemon (e.g. routes: [unknown-repo]) → log warn + skip that route; local PR proceeds. Do not crash.
- R.5: Routes parser charset whitelist `[a-zA-Z0-9._\-/]{3,80}` to reject path injection (mirrors #933 supervisor --name validation).

## 5. Cross-repo dep cascade (deferred Phase-X)

**Problem.** A merged PR in repo-foo bumps shared-lib X from v1 to v2. Consumers in repo-bar / repo-baz need follow-up PRs to consume v2. Today the operator files them by hand.

**Design (sketch).** When a PR merges with a `dep-cascade:` block in the body (or a `regatta.yaml::cascades:` declaration matching the diff), regatta files follow-up issues in listed downstream repos. Driven by per-repo `regatta.yaml::cascades:` config:

```yaml
cascades:
  - on_change: ["go.mod"]
    if_diff_contains: "shared-lib"
    file_issue_in: ["owner/repo-bar", "owner/repo-baz"]
    title_template: "consume shared-lib bump from {{source_repo}}#{{source_pr}}"
```

**Phase-X gate.** Defers behind two triggers:
1. Operator has ≥ 2 dependent repos under regatta orchestration with a known shared-lib relationship.
2. Cycle detection design lands first (foo → bar → foo) — without cycle detection a circular cascade storms the issue tracker.

`status: x-prefetch` if/when this slice graduates. NOT in scope for the umbrella's 5 active slices.

## 6. Shared selfimprove learning (Phase-X)

**Problem.** `internal/selfimprove/detector.go` patterns detected on repo-foo (e.g. "tests using bare `time.Sleep` keep slipping in") don't carry to repo-bar. Each target re-discovers the same rule.

**Design (deferred).** Detector findings could publish to a cross-repo store (per-operator, single-tenant — NOT cross-customer). Patterns include rule-id + occurrences + suggested fix. Per-target opt-in consume.

**Phase-X gate.** Defers behind:
1. Privacy / tenant-isolation triggers — even single-operator, code snippets in detector findings may carry secrets / proprietary patterns. Per-repo isolation by default.
2. The autotuner closed-loop (#955) lands first — it's the per-repo precursor; cross-repo amplification only makes sense once per-repo signal is proven.
3. ≥ 3 active targets where the same selfimprove rule fires independently (the "I keep re-learning this" trigger).

NOT in scope.

## 7. Out of scope

- 7.1 Multi-tenant `tenant_id` propagation (Phase-X W8, external-customer trigger).
- 7.2 Cloud-hosted regatta-as-a-service (Phase-X W12).
- 7.3 Cross-repo selfimprove sharing (deferred per §6).
- 7.4 Cross-repo dep cascade implementation (deferred per §5; sketch retained for forward-fit).
- 7.5 Auto-installing sibling daemons on demand from cross-repo routing (R.4 forces operator to install). Hot-add daemon is Option C of #929 — separately deferred indefinitely.
- 7.6 Auto-roadmap parser — covered by the cross-repo roadmap-discovery spec in flight; this umbrella does not duplicate.
- 7.7 Centralized cross-target dashboard — Option B of #929 prereq; defer until pain.

## 8. Adversarial pass per layer

- **L1 baseline mismatch.** Risk: bundled `CLAUDE.md.default` is so generic the worker treats it as no-op. Mitigation: smoke harness #864 measures first-PR-merged rate on a clean target. If < baseline by ≥ 20%, baseline tuning becomes a tracking issue (file before L1 ships). Counter-risk: bundled default duplicates this-repo content silently — `scripts/check-no-repo-specific-slugs.sh` (Slice 1 deliverable) fails closed on `feedback_*` slugs / `scripts/check-*.sh` paths in `assets/prompts/`.

- **L2 detection wrong on polyglot.** Risk: language detection on a polyglot repo (e.g. Go + TypeScript) picks one and drops the other. Mitigation: L2.1 acceptance requires BOTH hints injected (additive, not exclusive). Counter-risk: enrichment fragment from a stale `Makefile` target steers the worker into a dead command. Mitigation: L2.3 timeout + L2.4 size cap bound the blast radius; bad enrichment is bounded noise, not a crash.

- **L3 operator override = prompt-injection.** Risk: target repo with malicious `regatta-prompts/CLAUDE.md` injects "ignore previous instructions" into the worker. Mitigation: target repo trust is **operator-asserted** — the operator chose to install regatta against this repo, so the repo's authored content is in the trust boundary. This matches the principle that a human-merged commit in a target repo carries operator-vouched trust (gh branch protection). Counter-risk: a malicious PR adds `regatta-prompts/CLAUDE.md` and a worker on a subsequent issue picks it up. Mitigation: L3 reads `regatta-prompts/` only from the worktree's checked-out commit on the issue's base branch, not the PR's head branch. The operator gates merge to base via gh branch protection; this defers the trust boundary to the same place all other authored code lives.

- **L4 feedback feeds wrong autotune.** Risk: per-target meters are noisy on a low-volume repo (1 issue/week → meter is a 1-bit signal). Mitigation: L4 trigger requires ≥ 3 consecutive days BELOW threshold AND a configurable min-sample-size (default 5 issues in the window). Below min-sample → meter emits "insufficient-data", no follow-up issue. Counter-risk: thresholds too tight → false-positive issue storm. Mitigation: zero-value defaults (0.7 / 0.5) tuned to be lax; operator tightens via `regatta.yaml`. Default state is "no signal" not "storm".

- **Cross-repo routing PR storm.** Risk: an issue with `routes: [a, b, c, d, e, ...]` spawns N PRs all stuck on a common bug, eating quota. Mitigation: route count cap (default 5) configurable via `regatta.yaml::routes.max_per_issue:`. Cap exceeded → log warn + open the first N + skip the rest. Counter-risk: typo in route (`owner/repo-bra` instead of `repo-bar`) opens an issue in the wrong target. Mitigation: R.5 charset check is necessary-not-sufficient; routing also verifies the sibling daemon exists locally (`regatta install-service --name <repo>` must have run). Unknown repo → R.4 warn + skip; no remote auto-create.

- **Cross-repo dep cascade cycle.** Risk (Phase-X §5): foo bumps lib → cascade opens issue in bar → bar bumps another lib → cascade opens issue in foo → loop. Mitigation (Phase-X spec): cascade engine carries a back-pointer chain; if the new issue's chain already names the target repo, refuse. Cycle detection is a Phase-X §5 acceptance gate, not optional.

## 9. Acceptance (umbrella)

- 9.1 `regatta init --no-claude-md` bootstraps a target repo without an operator-authored `CLAUDE.md`. Worker invocation against this target uses L1 baseline + L2 enrichment + (if present) L3 override.
- 9.2 First-PR-merged rate on a test target repo (e.g. a fork of `golang/example` or similar small OSS project) ≥ this repo's baseline measured via the live-outcome harness over a 7-day window.
- 9.3 Multi-repo work-item with `routes: [a, b]` opens PRs in all listed targets within a single adapter-poll cycle deadline (default 5min).
- 9.4 L4 meters emit per-target labels and trigger follow-up issues only when both ≥ min-sample-size AND ≥ 3-day below-threshold.
- 9.5 Smoke harness #864 extends to cover a non-regatta target repo end-to-end.

## 10. Risk + adversarial summary

| Layer | Top risk | Mitigation cite |
|---|---|---|
| L1 | Baseline too generic | §8 L1 + smoke harness #864 measurement |
| L2 | Polyglot drops a language | §8 L2.1 acceptance + timeout/size caps |
| L3 | Prompt injection via target repo | §8 L3 trust-boundary alignment with gh branch protection |
| L4 | Noisy meters → false-pos storm | §8 L4 min-sample-size + lax defaults |
| Routes | PR storm on mis-targeted routes | §8 routes cap + R.5 charset + R.4 daemon-existence check |
| Cascade (Phase-X) | Cycle foo → bar → foo | §8 cascade back-pointer chain + Phase-X gate |

## 11. Sequencing + dependencies

L1 → L2 → L3 → L4 → cross-repo routing. L1 ships standalone (no prereq beyond #933 supervisor --name on main). L2 depends on L1's prompt-resolution refactor. L3 depends on L1 + L2. L4 depends on L1 + L2 + L3 shipped (otherwise meters measure a moving baseline). Cross-repo routing depends on #933 + L1 (so sibling targets have a non-degraded prompt baseline). Cascade + shared selfimprove stay Phase-X.

Suggested dispatch wave shape: L1 + L2 in parallel (file-disjoint — L1 is `assets/prompts/` + `internal/orchestrator/spawner/claude.go`; L2 is `internal/orchestrator/prompt/enrich.go` new file). L3 + L4 in a second wave (also file-disjoint). Cross-repo routing last (touches `internal/orchestrator/adapter/githubissues/adapter.go` — large file, sequence solo).

## 12. Implementer brief (§12 style) — 5 slices

Each slice files as a separate `[autonomous]` + `regatta-on-arbitrary-repo` labeled issue. Dispatch one implementer per slice; cap at 3 in parallel per `CLAUDE.md` dispatch rules.

### Slice 1 — Bundled-default prompt baseline (L1)

- Add `assets/prompts/CLAUDE.md.default` + `assets/prompts/dispatch-templates/{implementer,reviewer,designer,triage}.default.md`. Embed via `//go:embed` in a new `internal/orchestrator/prompt/embed.go`.
- Refactor `internal/orchestrator/spawner/claude.go::defaultPromptBuilder` to resolve in order: target-repo `CLAUDE.md` → bundled default. Same for dispatch templates.
- Lint gate: `scripts/check-no-repo-specific-slugs.sh` fails closed if `assets/prompts/` mentions `feedback_*` slugs or `scripts/check-*.sh` paths.
- Tests: `TestPromptResolver_TargetHasClaudeMd_UsesItVerbatim`, `TestPromptResolver_NoClaudeMd_UsesBundledDefault`, `TestPromptEmbed_NoRepoSpecificSlugs`.
- Acceptance: L1.1 through L1.4.

### Slice 2 — Target-repo convention scanner (L2)

- New `internal/orchestrator/prompt/enrich.go` reads `CONTRIBUTING.md` / `AGENTS.md` / `.editorconfig` / `go.mod` / `package.json` / `Cargo.toml` / `pyproject.toml` / `Makefile` / `.github/PULL_REQUEST_TEMPLATE.md`. Best-effort; missing files are silent.
- Wall-clock timeout 200ms; per-file 5KB cap; total 20KB cap with truncation marker.
- Hook into `defaultPromptBuilder` after L1 resolution; append-only.
- Config gate `regatta.yaml::prompts.adaptive_enrichment: false` (default true) disables.
- Tests: `TestEnrich_Polyglot_InjectsBothLanguages`, `TestEnrich_NoConventions_YieldsBaseline`, `TestEnrich_Timeout_FallsBackToBaseline`, `TestEnrich_SizeCap_Truncates`.
- Acceptance: L2.1 through L2.4.

### Slice 3 — Per-repo prompt override loader (L3)

- Extend `internal/orchestrator/prompt/` to read `regatta-prompts/{CLAUDE.md,implementer.md,reviewer.md,designer.md}` from the target repo worktree (issue base branch, NOT PR head branch).
- Resolution order: L1 baseline + L2 enrichment + L3 override appended at end.
- File-size cap 50KB per file; oversize → log warn + skip.
- Workflow: implement the read path only; `internal/selfimprove/detector.go` write-back stays on the autotuner spec path (#955).
- Tests: `TestOverride_RegattaPromptsDir_AppendsToBaseline`, `TestOverride_RegattaPromptsClaudeMd_OverridesRoot`, `TestOverride_ReadsBaseBranchNotHead`, `TestOverride_OversizeFile_LogsAndSkips`.
- Acceptance: L3.1 through L3.4.

### Slice 4 — Per-repo quality-feedback meters (L4)

- New `internal/observability/metrics/target_quality.go` emits `regatta.target.l4_verdict_accuracy` and `regatta.target.first_pr_merge_rate` with `target=<name>` label.
- Window: 24h rolling for merge-rate; daily aggregate for verdict-accuracy. Min-sample-size default 5.
- Threshold-breach detector in `internal/selfimprove/target_quality.go`: ≥ 3 consecutive days below threshold AND ≥ min-sample → file ONE follow-up issue (de-duped via state-machine substrate).
- Config: `regatta.yaml::quality.target_thresholds: {verdict_accuracy: 0.7, merge_rate: 0.5, window_days: 3, min_samples: 5}`.
- Tests: `TestTargetQuality_MetersEmitPerTargetLabel`, `TestTargetQuality_BelowThreshold_FilesOneIssue`, `TestTargetQuality_InsufficientSample_NoIssue`, `TestTargetQuality_DupeBreachWindow_NoSecondIssue`.
- Acceptance: L4.1 through L4.4.

### Slice 5 — Cross-repo work-item routing

- Extend `internal/orchestrator/adapter/githubissues/adapter.go` to parse `<!--regatta: routes: [...] -->` + `<!--regatta: coordinate_merge: true -->` from issue bodies.
- For each route, open a sibling work-item in that target's regatta daemon via the local state-machine substrate (sibling daemon process must be running — verify via filesystem ping on its DB path).
- Carry back-pointer `<!--regatta: routed_from: <source>#<n> -->` + `routed-from-N` label on each sibling work-item.
- Routes cap 5 (configurable via `regatta.yaml::routes.max_per_issue:`); over-cap → log warn + open first N.
- Charset check `[a-zA-Z0-9._\-/]{3,80}` on each route before dispatch.
- `coordinate_merge: true` → add `coordinate-merge` label + register a status check that fires only when ALL siblings reach `mergeStateStatus = CLEAN`.
- Tests: `TestRoutes_MultiTarget_SpawnsSiblings`, `TestRoutes_BackPointerLabel`, `TestRoutes_CoordinateMerge_BlocksUntilAllClean`, `TestRoutes_UnknownTarget_LogsAndSkips`, `TestRoutes_CharsetReject`, `TestRoutes_CapEnforced`.
- Acceptance: R.1 through R.5.

### Phase-X holdouts (not slices — track only)

- Cross-repo dep cascade (§5).
- Shared selfimprove learning (§6).

## 13. Out-of-band followups (file as separate issues if approved)

- F1: Auto-installing sibling daemons from cross-repo routing (Option C of #929; defer indefinitely).
- F2: Centralized cross-target dashboard (Option B of #929 prereq).
- F3: Cycle detection for dep cascade (Phase-X §5 prereq).
- F4: Shared selfimprove tenant-isolation design (Phase-X §6 prereq).
- F5: Cross-repo coordinate-merge auto-resolver (after operator pain hits on §4 manual merge ordering).
