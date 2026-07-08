# Autonomous loop — fleet-scope operating rules

Rules below apply ONLY when running the multi-agent autonomous loop (≥2 parallel implementer / reviewer subagents dispatched from a single main-thread session). They were culled from `CLAUDE.md` because they cost solo-operator sessions context budget without firing.

Read this file at session start if you are dispatching parallel implementers, reviewer waves, or the audit-session loop. Skip it entirely if you are running a single-implementer session or an operator-only observation session.

Cross-refs: `CLAUDE.md` remains authoritative for universal rules (root cause, deletion sweep, TDD ordering, worktree race, no self-tagged APPROVE, no implementer-enabled automerge, stop-at-pr-ready, reviewer-verdict gate).

## Parallel dispatch

- **Cap parallel implementers at 3-4** — shared API quota dies at 5+; heavy-context sessions cap at 2-3.
- **File-disjoint only** in parallel; sequence chained-output work.
- **Shared-primitive owner**: scan composition roots (`cmd/regatta/serve.go`, `internal/orchestrator/state/machine.go`, `Makefile`) before dispatch; name OWNER for each shared primitive. (`feedback_parallel_safety`, `feedback_conflict_anticipation`)
- **Pre-file shared followups** for cross-cutting items; pre-merge collision rebase.
- **Cascade-rebase = design defect**: when ≥3 PRs go DIRTY simultaneously on shared-anchor changes, treat as design defect, not "normal merge math". Investigate the shared anchor (god-file, large composition root) — fix structurally (split files per #737 pattern) rather than absorbing rebase churn N times. Act-threshold: ≥2/session OR ≥3 DIRTY same-anchor. (`feedback_cascade_rebase_root_cause`)
- **Free-headroom backfill**: when parallel-implementer cap has open slots AND critical-path subagents are running, do NOT idle. Backfill with safe, file-disjoint, easy issues from open followups. Candidates must be file-disjoint w/ active scopes, doc/scripts/single-file-bounded, not trigger-gated, <30 min effort. (`feedback_free_headroom_backfill`)
- **Backfill-on-idle (operator invariant)**: every operator turn while ≥1 subagent is in flight MUST either (a) dispatch new file-disjoint work, OR (b) explicitly declare queue-exhausted. Idle-waiting on subagent return wastes the parallel-cap (6 slots) — session 2026-06-21 burned ~6hr of dead-air this way. If no wedge fits, declare exhaustion + offer session wrap rather than fill the turn with polling. (`feedback_backfill_on_idle`)
- **Fire-and-forget dispatch** — at session start, file 4-6 file-disjoint wedges in ONE turn (within the parallel-implementer cap). Do NOT poll between dispatches. Operator returns to merge ready PRs via the harness completion-notification, not a poll loop. The poll-loop antipattern wastes ~6hr/session of operator wall-clock. (`feedback_fire_and_forget_dispatch`)

## Reviewer wave prefetch (main-thread caching)

- **PR body cache per phase**: ONE `gh pr view N --json number,title,body,comments,reviews` per review phase; pass as text to phase subagents. Re-fetch only on phase boundary. (`feedback_pr_body_cache_per_phase`)
- **Main-thread reviewer prefetch (dispatch-side)**: BEFORE dispatching any reviewer subagent, main thread MUST run `gh pr view <N> --json title,body,comments,reviews` AND `gh pr diff <N>` ONCE and paste BOTH into the reviewer dispatch prompt preamble (one fenced block per artifact). Reviewer never re-fetches. Reviewer dedup audit 2026-06-21 found ~52 redundant round-trips per session across 21 reviewer dispatches. (`feedback_reviewer_prefetch_diff_and_body`)
- **Reviewer template inline**: main thread pastes the `reviewer.md` 5-lens skeleton ONCE in the dispatch-prompt preamble (single fenced block), NOT re-embedded per task sentence. Reviewer never `Read`s `docs/engineer/dispatch-templates/reviewer.md`. Full template stays canonical at that path for operator reference + audit-session diffing. Reviewer dedup audit 2026-06-21 found ~11 redundant template Reads / ~40KB per session. (`feedback_reviewer_template_inline`)
- **Survey-level adversarial pass** — when ≥2 parallel research / audit subagents feed a single load-bearing artifact (brief, spec, dispatch-template), spawn an N+1 reviewer per survey BEFORE the synthesis step. Brief-level review catches downstream symptoms; survey-level catches upstream defects (broken cross-refs, unverified deps) at lower review-round cost. (`feedback_subagent_survey_adversarial_pass`)

## gh polling optimization

- **gh minimal fields**: every `gh pr list/view/issue list` MUST pass explicit `--json` allowlist (default: `number,state,mergeStateStatus,statusCheckRollup,isDraft,headRefName`) + `-L 20`. Never bare `--json`. For high-frequency polling (CI watch loops, status sweeps), prefer REST-backed `gh pr checks <N> --json name,state,bucket` over GraphQL `gh pr view --json statusCheckRollup` — REST costs ~5× less rate-limit per call. Use `gh pr checks`, NOT the raw `gh api repos/.../commits/<sha>/check-runs` endpoint: the raw endpoint returns only GitHub Actions check_runs and silently omits commit-status entries. `gh pr checks` unifies both surfaces into one `bucket` field (`pass`/`fail`/`pending`/`skipping`/`cancel`) matching the PR UI. GraphQL acceptable for single-shot reads (PR body, `mergeStateStatus` terminal check). 2026-06-10 session depleted 5000/hr GraphQL quota in 1 session → 26-min `ScheduleWakeup` wait. (`feedback_gh_minimal_fields`)
- **Batch gh queries via `& + wait`** — when polling ≥2 PRs, run the `gh` calls concurrently rather than sequentially: `for n in 1316 1317; do gh pr view "$n" --json mergeStateStatus,statusCheckRollup & done; wait`. Saves ~50% wall vs the equivalent for-loop. Pairs with `feedback_gh_minimal_fields`: each parallel call still needs the explicit `--json` allowlist. (`feedback_gh_batch_queries`)

## Research capture

- **ctx capture dedupe**: `ctx_search` before `ctx_batch_execute` on research/spec; skip batch if recent (<24h) hit covers same content. (`feedback_ctx_capture_dedupe`)

## Wave-completion audit

- **Test-coverage audit per wave**: end every parallel-dispatch wave with explicit test-coverage audit BEFORE next wave. Audit unit / integration / E2E + TDD-order-verification (`git log --reverse <branch>` shows RED commit first) + RED-output-in-PR-body + mock-vs-real ratio. Catches subagent over-claims + integration gaps unit tests don't see. Gap → tracker issue before next wave. (`feedback_test_coverage_audit_per_wave`)
- **Trap projection across loop closure**: when a recurring trap trips the operator ≥2 times in a session, project whether autonomous workers will hit the same trap post-loop-closure. If yes, fix BOTH operator-side AND worker-side BEFORE loop closure. Three boundaries — pick by root cause: (1) gate enforcement (`scripts/check-*.sh` too strict), (2) prompt authorship (`internal/orchestrator/spawner/claude.go::defaultPromptBuilder` doesn't teach the rule), (3) operator knowledge (CLAUDE.md / dispatch templates drift from spec). (`feedback_trap_projection`)
