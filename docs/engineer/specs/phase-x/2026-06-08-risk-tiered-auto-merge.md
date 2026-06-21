---
title: "L6.1 risk-tiered auto-merge for low-risk PRs (closes #1080)"
status: phase-x-deferred
phase: self-host
summary: "Add an opt-in `gates::low_risk_automerge` gate that lets the orchestrator click-merge provably low-risk PRs (doc-only / dep-bump / comment-sweep) after a soak window, while leaving load-bearing PRs on the existing `human_merge` path. Reuses the existing reviewer-verdict path classifier as the negative test; gates merge eligibility on adversarial review + LoC cap + soak window. Default-disabled; opt-in twice (yaml + CLI flag)."
date: 2026-06-08
phase: self-host
deferred_on: 2026-06-10
---

# L6.1 risk-tiered auto-merge — Spec

Closes: [#1080](https://github.com/trilamsr/regatta/issues/1080) — `BUG-1080: introduce L6.1 risk-tiered auto-merge for low-risk PRs`.

Memory rules in force: `feedback_default_simpler`, `feedback_decision_priority`, `feedback_no_self_tagged_approve`, `feedback_no_implementer_automerge`, `feedback_review_proportional`, `feedback_watch_pr_until_merged`, `feedback_reviewer_findings_to_issues`, `feedback_root_cause`, `feedback_no_signatures`.

```release-notes
[DOCS] Spec for #1080 — opt-in `gates::low_risk_automerge` that lets the
orchestrator click-merge provably low-risk PRs (doc / dep / comment) after
a soak window. Path classifier + LoC cap + adversarial-review token +
soak window + operator override. Default disabled; load-bearing PRs stay
on the existing `human_merge` gate. Pure spec; no prod code; implementer
task tracked separately.
```

## §0 Closing trigger

`#1080` closes when ALL of:

1. This spec lands on `main` with `closes #1080`.
2. Acceptance criteria c1–c4 from the issue body have implementer-task issues filed (one per criterion) and at least one regression test in `internal/orchestrator/merge/low_risk_test.go` demonstrates a load-bearing change is REJECTED by the classifier.
3. Operator has run `regatta serve --auto-merge=true` once against a synthetic doc-only PR fixture (≤50 LoC, `[DOCS]` release-notes, no load-bearing paths) and observed `merge.low_risk_decision eligible=true` followed by `mergedAt != null` in state machine. Recorded in a comment on #1080.

The spec PR alone is NOT sufficient — the wire-up + first synthetic-PR pass is the acceptance signal.

## §1 Problem

Every PR in the 2026-06-08 dogfood session required manual operator merge:

```
gh pr merge --squash --delete-branch --admin <N>
```

The session's 7 PRs were ALL load-bearing (touched `internal/orchestrator/*`, `scripts/check-*.sh`, or `CLAUDE.md`) — so manual click was correct. But in steady-state operation, the same `human_merge` gate fires on:

- Dependency bumps (Dependabot / Renovate) with green CI and zero functional change.
- Documentation typo fixes under `docs/` that touch zero code.
- Comment-only sweeps (per `feedback_comments_discipline` enforcement) with zero behavior change.
- CI-noise cleanups under `.github/workflows/` that change only descriptive YAML keys.

For these PRs, operator-attention is pure burden: there is no real safety win from a human clicking merge on a `[DEP] bump foo from 1.2.3 to 1.2.4` PR after CI passes. The `CLAUDE.md::feedback_review_proportional` rule already documents the "low-risk auto-skip reviewer" pattern at the review tier; this spec generalizes it to the merge tier.

Self-host-phase constraint (`docs/engineer/briefs/2026-06-01-self-host-first.md` §1): single operator, deterministic CI, human-merge enforced by branch protection. We do NOT relax that for load-bearing PRs. We carve out a **provably low-risk** sub-class where the classifier rejects-by-default and a positive eligibility decision requires every guardrail to fire green.

Goal: ~80% of typical-repo PRs (deps + doc + comment sweeps) merge hands-off; the ~20% that are load-bearing stay on the existing `human_merge` path; an adversarial reviewer subagent still runs on every PR regardless of tier.

## §2 Non-goals (Out of scope)

- **NOT replacing `human_merge`.** Load-bearing PRs stay on the operator-click path. This gate is additive, lives at a strictly lower risk tier, and is rejected-by-default.
- **NOT enabling auto-merge for any path on the existing load-bearing list.** The reviewer-verdict path classifier (`scripts/lib/reviewer-verdict/path-classifier.sh`) is the authoritative source. If that classifier flags load-bearing, this gate fails closed.
- **NOT skipping adversarial review.** Every tier including the lowest still requires `Reviewer-recommendation: APPROVE` from an independent-reviewer allowlist agent-id. The eligibility check is ADDITIVE to the existing reviewer-verdict gate, not a bypass.
- **NOT a multi-tier permission system across operators.** Self-host phase = one operator. Risk tiers are PR-shape buckets, not user-role buckets. `RBAC` is Phase X (`docs/engineer/briefs/2026-06-01-self-host-first.md` §4).
- **NOT changing the existing `gh pr merge --auto` story.** GitHub's native auto-merge already fires when CI passes after operator click; this gate REPLACES the operator click for low-risk PRs only.

## §3 Design (Risk tier matrix)

Four tiers. Lowest tier eligible for auto-merge; highest tier always requires operator click. A PR lands in the LOWEST tier that ALL its predicates satisfy; failing any predicate at tier N promotes to tier N+1.

| Tier | Name | Eligible for auto-merge | Path predicate | LoC delta cap | Release-notes prefix | Adversarial review | Soak window |
|---|---|---|---|---|---|---|---|
| **T0** | trivial | YES | `^(\.github/|docs/|.*\.md$)` AND NOT load-bearing classifier | ≤20 | `[DOCS]\|[CHORE]` | required (APPROVE) | 15 min |
| **T1** | low-risk | YES (opt-in) | `^(docs/|\.github/|scripts/(?!check-.*\.sh)|.*\.md$)` AND NOT load-bearing classifier | ≤50 | `[DOCS]\|[CHORE]\|[CI]\|[DEP]` | required (APPROVE) | 15 min |
| **T2** | standard | NO | not load-bearing, no path / LoC restriction | any | any | required (APPROVE) | n/a — manual |
| **T3** | load-bearing | NO | hits load-bearing classifier (CLAUDE.md, Makefile, Makefile.d/*, .github/workflows/*, scripts/check-*.sh, docs/engineer/dispatch-templates/*, docs/engineer/specs/*.md, docs/engineer/briefs/*.md, internal/orchestrator/*, cmd/*, contracts/schemas/*) | any | any | required (APPROVE, non-self-tagged) | n/a — manual |

**Tier resolution rule**: a PR is T0 only if EVERY T0 predicate passes. Any failure promotes to T1 with the broader T1 predicates re-evaluated. Any T1 failure promotes to T2. Any load-bearing path match promotes to T3 regardless of LoC or release-notes prefix.

**Why two auto-merge tiers (T0 + T1)?** T0 is the "boring default" (markdown / GH workflow tweak) that is intuitively safe even to an operator skim-reading. T1 widens to scripts and deps where the operator wants opt-in granularity (e.g. "auto-merge deps but not arbitrary scripts"). The implementation collapses the two tiers if the operator never sets per-tier knobs; the YAML schema allows both.

**Why does T2 exist if it's not auto-mergeable?** It distinguishes "needs operator attention because we couldn't prove low-risk" from "needs operator attention because we KNOW it's load-bearing". Slog event `merge.low_risk_decision` surfaces the tier so the operator can see WHY a PR landed in manual-merge.

## §4 Path classifier extension

The existing `scripts/lib/reviewer-verdict/path-classifier.sh::rv_classify_paths` is reused as the **negative test** (load-bearing detection). For the **positive test** (low-risk eligibility) we add a sibling library file `scripts/lib/low-risk-automerge/path-classifier.sh` exposing:

```sh
lr_classify_paths   # sets LOW_RISK_TIER ∈ {T0,T1,T2,T3}
                    # sets LOW_RISK_PATH_REASON for slog emission
```

Rules:

1. Read changed paths from the same `--changed-paths-file` flag the reviewer-verdict gate accepts.
2. If `rv_classify_paths` has already set `LOAD_BEARING_BY_PATH=1`, force `LOW_RISK_TIER=T3` and return.
3. For each remaining path, walk the tier predicates top-down. The PR's tier is the MAX (most restrictive) of its per-path tiers.
4. Reasons accumulated into `LOW_RISK_PATH_REASON` MUST be human-readable: `"vendor/foo/x.go: not on low-risk allowlist → T2"`.

We do NOT modify `path-classifier.sh` in-place — load-bearing detection is widely consumed and we want byte-equivalent behavior there (per `feedback_byte_equal_refactor_pin` in CLAUDE.md). The new file lives alongside it.

A Go-side mirror lives in `internal/orchestrator/merge/lowrisk/classifier.go` exposing `Classify(paths []string) (Tier, Reason, error)`. The shell + Go classifiers MUST agree byte-for-byte on the same inputs — enforced by `scripts/check-low-risk-classifier-parity.sh` (template: `scripts/check-prompt-parity.sh`). Drift is rejected by reviewer-subagent dispatch (per `feedback_adversarial_review`; the dedicated byte-equal-pin pr-lint workflow was demoted in MAY-31).

## §5 LoC cap definition

**Definition**: LoC delta = `git diff --stat origin/main...HEAD | tail -1` insertions + deletions, EXCLUDING:

- Generated files: anything matching `*_gen.go`, `*.pb.go`, `mock_*.go`, `bundled/*`, `node_modules/*`, `vendor/*`, `*.lock`, `go.sum`, `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`.
- Whitespace-only changes (`git diff --ignore-all-space --shortstat`).
- Renames (counted as 1 line each, not full re-add).

**Caps**:

- T0 = 20 (matches the existing `feedback_review_proportional` "<20 LoC" auto-skip threshold so the two tiers are coherent).
- T1 = 50 (matches the issue body acceptance criterion c1).
- T2/T3 = unbounded; not auto-merge eligible regardless.

**Configurable**: `gates::low_risk_automerge::loc_cap_t0` (default 20) and `gates::low_risk_automerge::loc_cap_t1` (default 50). Operator can tighten but NOT loosen above 50 without a CLAUDE.md edit cited in PR body (load-bearing surface — gate self-protection).

**Why exclude generated files**: a `go mod tidy` after a small dep bump can balloon `go.sum` by hundreds of lines for a 1-line `go.mod` change. The semantic delta is 1 line, not 300. Counting the lockfile defeats the cap's purpose.

**Why not file count**: a 1-file 200-LoC refactor is riskier than a 5-file 20-LoC typo sweep. LoC is the better proxy.

## §6 Soak window

After CI goes green on the head commit, the PR enters a soak window. Auto-merge fires only after the window elapses without:

- New commits pushed to the head ref (resets timer).
- A new review with `Reviewer-recommendation: REVISE|BLOCK` landing.
- An issue label `merge-block` being applied.
- CI flipping to red (any required check failing).

**Defaults**:

- T0 soak = 15 min.
- T1 soak = 15 min.
- Configurable via `gates::low_risk_automerge::hold_window` (per-tier override permitted: `hold_window_t0`, `hold_window_t1`).

**Wall-clock vs CI-clock**: soak measured from the moment ALL required checks went green AND the reviewer-verdict gate's `Reviewer-recommendation: APPROVE` token landed in the PR body, whichever came LATER. Resets to the later of the two on any reset event.

**Why 15 min minimum**: gives the operator a chance to spot-check the PR over morning coffee / between meetings without forcing an explicit click on every routine bump. Closes the "automerge fires 19s after open" gap (`feedback_no_implementer_automerge` retro, #1046).

**Why not 0 min**: zero soak == agent writes APPROVE token + automerge fires before operator can revoke. We already enforce a similar rule at the `--load-bearing + automerge enabled + Reviewer-agent-id present` gate (closes #1046); this spec adds the soak signal as a general low-risk default.

Implementation note: the soak timer is checked at each orchestrator tick (existing scheduler cadence ~30s); we do NOT add a dedicated timer goroutine. Timer state lives in `substrate_events` so a crash mid-soak does not lose elapsed time (per `feedback_substrate_durability`).

## §7 Adversarial review requirement

Every tier including T0 requires `Reviewer-recommendation: APPROVE` from a canonical-allowlist agent-id, enforced by the existing `scripts/check-reviewer-verdict.sh` gate. This spec does NOT relax that gate.

**Tier-specific additions**:

- **T0** — reviewer scope is "diff-only adversarial pass for non-load-bearing-doc / config" (proportional to risk per `feedback_review_proportional`). Reviewer agent-id MUST be on the canonical allowlist (`^a[0-9a-f]{16}$` harness shape OR `^(cavecrew|designer|triage|implementer|reviewer)-<slug>$`).
- **T1** — same as T0; agent-id allowlist + author-mismatch check (`feedback_no_self_tagged_approve`). For dep bumps, reviewer's adversarial focus is "is this dep's CHANGELOG free of breaking changes between the old + new version?".
- **T2/T3** — existing full reviewer-verdict gate semantics (no change).

**Self-tag bypass**: `--pr-author <login>` flag continues to feed the author-mismatch check. If the reviewer agent-id equals the PR author login, the gate fails closed regardless of tier. This is the existing rule (closes #1004–#1007 + retro 2026-06-08) and we are NOT carving an exception for low-risk tiers.

**Reviewer escape token**: `<!-- reviewer-skip-justified: <reason> -->` is a HARD reject for T0/T1 auto-merge — even though the existing reviewer-verdict gate honors it for trivial doc/typo cases, the low-risk gate refuses to auto-merge any PR carrying that escape. The escape was designed for human-clicked merges; auto-merge requires the real reviewer signal.

## §8 Operator override / opt-out

**Default state**: gate is `enabled: false` in `regatta.yaml`. No PRs auto-merge until operator opts in.

**Opt-in (two-step)**:

1. Set `gates::low_risk_automerge::enabled: true` in `regatta.yaml`.
2. Run with `regatta serve --auto-merge=true` (CLI flag, default `false`).

Both signals MUST be set; either alone fails closed. The CLI flag is per-process (operator can flip without an editor), the YAML setting is per-deployment (operator decided this repo wants automerge as a class).

**Per-PR opt-out**:

- Add label `merge-block` to the PR. Classifier downgrades to T2/T3 regardless of path / LoC. Surfaces in `merge.low_risk_decision reason="merge-block label set"`.
- Include `<!-- low-risk-automerge-opt-out: <reason> -->` HTML comment in the PR body. Same effect as the label, useful when the agent author wants to mark their own PR for manual click.

**Per-author opt-out**: `gates::low_risk_automerge::block_authors: [user1, user2]` YAML list. PRs from those authors stay on manual merge.

**Per-path tightening**: `gates::low_risk_automerge::extra_load_bearing_paths: [path/glob]` adds operator-specified paths to the load-bearing list FOR THIS GATE ONLY (does NOT affect reviewer-verdict gate). Useful when the operator wants to keep a noisy non-Go directory on manual click without polluting global load-bearing-path classifier.

**Kill switch**: `regatta serve --auto-merge=false` (the default) is the kill switch. Operator can restart the orchestrator with `--auto-merge=false` and ALL PRs revert to manual. No state migration needed.

**Audit trail**: every auto-merge decision emits `merge.low_risk_decision` slog event with fields `pr=N tier=T0|T1|T2|T3 eligible=bool reason="..." classifier_paths_reason="..." loc_delta=N soak_remaining_sec=N reviewer_agent_id="..."`. Operator can `grep merge.low_risk_decision` to audit gate behavior. Decision also written to `substrate_events` (immutable audit).

## §9 Test plan (Acceptance)

### §9.1 TDD discipline (per `feedback_tdd_discipline`)

Failing tests land FIRST in the implementer PR — RED commit visible in `git log --reverse <branch>`. PR body captures the RED output. Implementer task issues are filed with the spec; each task ships its own failing-test-first commit.

### §9.2 Shell-side gate tests

`scripts/check-low-risk-automerge.sh_test.sh` (template: `scripts/check-reviewer-verdict_test.sh`). Fixtures under `scripts/lib/low-risk-automerge/testdata/`:

| Fixture | Expected tier | Eligible | Reason |
|---|---|---|---|
| `doc-only-15loc.txt` (`docs/foo.md` 15 lines) | T0 | YES | low-risk-classifier pass |
| `doc-only-25loc.txt` (`docs/foo.md` 25 lines) | T1 | YES | T0 cap busted, T1 cap intact |
| `doc-only-60loc.txt` | T2 | NO | over T1 cap |
| `dep-bump.txt` (`go.mod` + `go.sum`) | T1 | YES | lockfile excluded from LoC |
| `claude-md-edit.txt` (`CLAUDE.md`) | T3 | NO | load-bearing classifier hit |
| `mixed-doc-and-prod.txt` (`docs/x.md` + `internal/foo/bar.go`) | T3 | NO | load-bearing prod path hit |
| `comment-sweep.txt` (4 `*.go` files, 12-LoC removal of `// ` lines, whitespace-only ignored) | T2 | NO | prod-Go path not on allowlist |
| `merge-block-label.txt` (T0-shape + label set) | T2 | NO | merge-block label |
| `self-tagged-approve.txt` (T0-shape, author == reviewer agent-id) | T0 | NO | reviewer-verdict gate fails first |

### §9.3 Go-side classifier parity tests

`internal/orchestrator/merge/lowrisk/classifier_test.go` runs the SAME fixtures as §9.2 through the Go classifier and asserts byte-equal `tier + reason + eligible` output. Mechanically enforced by `scripts/check-low-risk-classifier-parity.sh` in `make check`.

### §9.4 Soak-window unit tests

`internal/orchestrator/merge/lowrisk/soak_test.go`:

- Soak elapsed → eligible.
- Soak not elapsed → defer (event surfaces `soak_remaining_sec`).
- New commit during soak → timer resets, event surfaces `soak_reset_reason="head_advanced"`.
- REVISE review during soak → defer permanently until new APPROVE; event surfaces `soak_reset_reason="revise_received"`.
- merge-block label applied during soak → tier-downgrade event; PR moves to T2.
- CI flips red during soak → defer; auto-resume when CI returns green AND fresh timer elapses.

### §9.5 Integration tests

`internal/orchestrator/merge/coordinator_lowrisk_integration_test.go` (template: `coordinator_concurrent_test.go`):

- Coordinator + classifier + executor end-to-end with mocked `gh` shim.
- Synthetic doc-only PR (T0) → coordinator opens, soak elapses, executor calls `gh pr merge --squash --delete-branch` and surfaces `mergedAt`.
- Synthetic load-bearing PR (T3) → coordinator opens, classifier rejects, executor does NOT call `gh pr merge`; PR sits at `awaiting_merge` until operator click (existing path).
- Race: two parallel low-risk PRs against same head ref → executor serializes; per-PR independent decisions.

### §9.6 E2E smoke

Operator-runnable harness under `scripts/smoke-low-risk-automerge.sh`:

1. Create a fresh branch with a single-line `docs/foo.md` edit.
2. Open PR with `[DOCS]` release-notes + `Reviewer-recommendation: APPROVE` + `Reviewer-agent-id: cavecrew-smoke-fixture`.
3. Run `regatta serve --tick-once --auto-merge=true` with `gates::low_risk_automerge::hold_window: 0s` (test override).
4. Assert PR `state == MERGED` and `mergedAt != null` within 60 s wall clock.
5. Repeat with a `CLAUDE.md` edit; assert PR stays OPEN (load-bearing classifier).

### §9.7 Mutation test coverage

Add `gates::low_risk_automerge` to `mutation-testing.yml` workflow targets. Mutation survivors > 5 % blocks the implementer PR (per CI gate config in `Makefile.d/ci.mk::check-mutation`).

### §9.8 Adversarial review of this spec

Per `feedback_adversarial_review_every_step` + `feedback_subagent_survey_adversarial_pass`: this spec PR carries `Reviewer-agent-id: <independent-subagent-id>` AND `Reviewer-recommendation: APPROVE` in its body footer. The reviewer's adversarial mandate:

1. Find a path predicate gap where a load-bearing change could land in T0/T1.
2. Find a LoC delta computation where the cap can be circumvented (e.g. binary asset that doesn't show in `git diff --stat`).
3. Find a soak-window reset event we forgot (force-push? rebase? merge-base advance?).
4. Find an operator-override hole where opt-out can't actually opt out.
5. Find a CI-clock vs wall-clock race that fires automerge before the soak signal is observable.

Reviewer findings filed inline OR as tracking issues before merge (per `feedback_reviewer_findings_to_issues`).

## §10 Open questions

1. **Should T0 require ZERO reviewer findings or just APPROVE?** Today the reviewer-verdict gate accepts APPROVE with attached suggestions. For T0 auto-merge we could tighten to "no MED+ findings filed against this PR" — but that requires the reviewer subagent to emit structured findings, not just an APPROVE token. **Tentative**: keep APPROVE-only for T0/T1 in v1; add structured-findings tightening as a follow-up tracker issue if a regression escapes.

2. **Should soak window restart on PR body edits?** A body edit can flip release-notes prefix from `[CHORE]` to `[CHANGE]`, which would alter tier classification. **Tentative**: yes — body edit triggers re-classification + soak restart. Symmetric with head-ref-advance behavior.

3. **Does this gate conflict with branch protection's "require linear history" / "require signed commits"?** Operator's GitHub branch-protection currently has `required_status_checks.strict: false` (per CLAUDE.md branch-protection-state section). The orchestrator's `gh pr merge --squash --delete-branch --admin` call already bypasses required reviewers — `--admin` is necessary because the orchestrator's GH App / PAT isn't a reviewer. **Tentative**: document that `--admin` is required for this flag to function; operator MUST grant the orchestrator's GH credential admin merge rights. Cross-reference with `feedback_branch_protection_strict`.

4. **What about `dependabot[bot]` / `renovate[bot]` PRs that have no Reviewer-agent-id?** They never carry a reviewer token because no subagent reviews them. **Tentative**: bot PRs are out of scope for v1 — operator must either click-merge OR dispatch a reviewer subagent against the bot PR before this gate considers it. Filed as a follow-up question (cross-ref Phase X "external author" surface).

5. **Should T0 and T1 collapse into one tier in v1?** YAGNI argument (per `feedback_default_simpler`): one tier with one set of knobs is simpler. Two tiers gives the operator opt-in granularity for "auto-merge doc but not dep bumps". **Tentative**: ship as one collapsed tier in v1 implementer PR; split into T0/T1 if operator hits a real case where they want different soak windows / LoC caps for doc vs dep. Spec keeps the matrix for future expansion.

6. **What happens during a substrate replay (W9)?** The auto-merge decision is recorded in `substrate_events`. On replay, do we re-fire the merge call? **Tentative**: no — merge is a non-idempotent external effect. Use the W9 external-effect-outbox primitive (`docs/engineer/specs/2026-06-02-external-effect-outbox-primitive.md`) to guarantee at-most-once merge per PR. Implementer task carries this dep.

7. **CLI flag name** — `--auto-merge=true` or `--low-risk-automerge=true`? Issue body uses the former. **Tentative**: ship `--auto-merge=true` for consistency with the issue + operator's existing mental model; the gate's YAML key is the more-specific `gates::low_risk_automerge` so the config layer is self-documenting.

8. **Per-tier YAML schema surface**: do we expose `hold_window_t0` / `hold_window_t1` / `loc_cap_t0` / `loc_cap_t1` in v1, or just `hold_window` / `loc_cap` collapsed? **Tentative**: collapsed in v1 (matches §10.5 collapse decision); add per-tier overrides only after the first real case for differing them appears (per `feedback_default_simpler`).

## §11 Implementer brief (Implementation task breakdown)

To be filed as separate issues (NOT in scope for this spec PR — spec ships acceptance only):

- **L6.1-T1**: `scripts/lib/low-risk-automerge/path-classifier.sh` + Go mirror in `internal/orchestrator/merge/lowrisk/classifier.go` + parity gate `scripts/check-low-risk-classifier-parity.sh`.
- **L6.1-T2**: `gates::low_risk_automerge` YAML schema in `contracts/schemas/regatta.cue` + CUE validation tests.
- **L6.1-T3**: `internal/orchestrator/merge/lowrisk/` package — `Classifier`, `SoakTimer`, integration with `coordinator.go::Merge`.
- **L6.1-T4**: `--auto-merge` CLI flag wired in `cmd/regatta/serve.go`; default `false`; double opt-in with YAML.
- **L6.1-T5**: slog event `merge.low_risk_decision` + substrate-event projection.
- **L6.1-T6**: E2E smoke harness `scripts/smoke-low-risk-automerge.sh` + recorded run on a synthetic PR in #1080.
- **L6.1-T7**: Documentation update in `docs/operator/quickstart.md` for opt-in workflow.

Total effort estimate: ~3-5 implementer subagent days (file-disjoint, parallelizable T1/T2 then T3/T4 then T5/T6).

## §12 Decision-priority self-check

Per CLAUDE.md `feedback_decision_priority` (UX → ease → performance → best-practices → speed → velocity; long-term > short-term):

- **UX** — operator gains hands-off merge for ~80% of PRs without giving up the human-merge guardrail on load-bearing changes. Net UX improvement.
- **Ease** — opt-in twice (YAML + CLI), default off. Operator can't accidentally enable; can't accidentally enlarge surface; can disable via `--auto-merge=false` restart.
- **Performance** — soak window is checked at scheduler tick cadence (no new timer goroutine). Zero new background work in the disabled-default state.
- **Best-practices** — adversarial review still required at every tier. Path classifier byte-equivalent to existing load-bearing gate's negative test. Substrate-event audit trail. Mutation testing required.
- **Speed** — implementer effort ~3-5 subagent days, mostly parallelizable.
- **Long-term** — gate is opt-in and per-PR-overridable; surface can grow (T0/T1 split) or shrink (collapse) without breaking the API. Failure mode is "PR sits at manual merge", not "PR merges incorrectly".

## §13 References

- `docs/engineer/briefs/2026-06-01-self-host-first.md` — phase constraints (§1: deterministic CI, human-merge via branch protection; §4: Phase X defers `RBAC`).
- `scripts/check-reviewer-verdict.sh` — existing reviewer-verdict gate (sibling to this gate; ALWAYS runs first).
- `scripts/lib/reviewer-verdict/path-classifier.sh` — authoritative load-bearing-path classifier (reused as negative test).
- `regatta.yaml::gates::human_merge` — existing approval gate (NOT replaced; T2/T3 PRs continue to use it).
- `CLAUDE.md::feedback_review_proportional` — pre-existing "low-risk auto-skip reviewer" pattern at the review tier; this spec generalizes to the merge tier.
- `CLAUDE.md::feedback_no_implementer_automerge` — closes #1046 (agent enabled automerge 19s after open). This spec preserves the rule by gating automerge on soak window + reviewer-verdict gate, both of which run BEFORE merge fires.
- `CLAUDE.md::feedback_no_self_tagged_approve` — author ≠ reviewer agent-id enforcement; preserved at every tier of this gate.
- `docs/engineer/specs/2026-06-02-external-effect-outbox-primitive.md` — at-most-once external-effect semantics for the merge call on substrate replay.

<!--regatta
lane: server
-->

<!-- TODO(#1080) — reviewer-agent-id + recommendation tokens land on the implementer PR that closes #1080, not on this spec PR. -->
