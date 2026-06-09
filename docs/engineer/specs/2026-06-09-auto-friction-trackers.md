---
title: "Auto-file friction trackers from observed signals (#1077)"
status: design
phase: self-host-s2
issue: 1077
summary: "Wire the existing W4 self-improve detector (`internal/selfimprove`) into `regatta serve` on a 5-minute cadence, add three substrate event-kinds (`agent_non_completed_exit`, `spawn_failed_retry`, `tick_slow_repeat`) sourced from the slog events already emitted by `internal/orchestrator/spawner` and `internal/orchestrator/scheduler`, and ship rule R12-friction that files a `state:auto-improve` tracker issue when those kinds cross a count threshold. Dedup uses the existing `Finding.DedupKey` sha256 fingerprint persisted in a new `filed_friction_trackers` side table; throttle hard-caps at 5/day and 50/week; operator override is the `do-not-auto-file:<rule-id>` label on any open tracker. Spec only; no code in this PR."
---

# Auto-file friction trackers from observed signals — Spec

Memory rules in force: `feedback_default_simpler`, `feedback_no_signatures`, `feedback_cite_origin_main_not_local`, `feedback_root_cause`, `feedback_deletion_default`, `feedback_unaddressed_load_bearing`, `feedback_meta_codify_repeat_directives`.

```release-notes
[DOCS] specs: auto-friction tracker design (#1077)
```

## §1 Problem

The 2026-06-08 / 2026-06-09 dogfood sessions left a clear pattern: the operator filed every friction tracker by hand, while the orchestrator emitted the triggering signal as a structured slog event in every case. Concrete samples from this session (each filed manually by the operator after reading logs), reproducible via `gh issue list --state all --search "BUG-DOCKER OR BUG-WORKTREE OR BUG-1116 OR BUG-1117 OR BUG-1119 OR BUG-1123" --json number,title`:

- `#1116` — Docker quickstart: `claude --mcp-config=/dev/null` fails with "not valid JSON". The fix in `#1086` was incomplete. The orchestrator emitted `agent.exited` with `exit_reason=tool_denied` repeatedly; nothing turned that into a tracker.
- `#1117` — `github_issues` adapter silently skips issues without a `lane:` label, emitting an `empty_lane` WARN. Repeated WARN; zero auto-issue.
- `#1119` — dashboard events panel renders verb-only for non-exit events. Discovered visually; not auto-detected.
- `#1123` — parallel subagents shared a single worktree (`operator-docker-soak/`), causing branch-HEAD clobbering. Manifested as `prwatch.branch_renamed_by_agent` WARN + downstream `agent.exited` non-completed.

The substrate already records discrete `EventKind*` rows that W4's `internal/selfimprove/rules.go` (R1-R5) scans on demand via `regatta self-improve scan --apply`. Two gaps prevent that pipeline from closing on the friction patterns above:

1. **Slog → substrate gap.** The slog event names `agent.exited`, `spawn.failed`, `tick.slow` (declared in `internal/obs/events.go:23,38,44`) are NOT mirrored as substrate `EventKind*` constants in `internal/selfimprove/rules.go:28-34`. The detector reads substrate rows only, so it never sees these signals.
2. **Cadence gap.** `cmd/regatta/selfimprove.go` is a one-shot CLI verb (`runSelfImprove` at `cmd/regatta/selfimprove.go:28`). Nothing in `cmd/regatta/serve.go` or `internal/orchestrator/scheduler/scheduler.go:306` (`Tick`) drives a periodic scan, so the rule suite fires only when the operator manually invokes it — i.e. never, in practice, during a long autonomous loop.

Per `feedback_root_cause`: the primary failure mode is not "operator is slow at filing issues"; it is "the orchestrator's own friction signals never reach its own detector." This spec closes both gaps with the minimum surface that lets the existing W4 pipeline absorb the three concrete event sources, and adds the dedup + throttle + operator-override hygiene needed before issue-filing can run unattended.

## §2 Design

Per `feedback_default_simpler` — extend the W4 surface, do not invent a parallel pipeline.

### 2.1 Three new substrate event-kinds

Add three constants to `internal/selfimprove/rules.go` (alongside the existing six at `internal/selfimprove/rules.go:28-34`):

```
EventKindAgentNonCompletedExit = "agent_non_completed_exit"   // mirrors slog agent.exited where exit_reason != completed
EventKindSpawnFailedRetry      = "spawn_failed_retry"          // mirrors slog spawn.failed when retry_count >= threshold
EventKindTickSlowRepeat        = "tick_slow_repeat"            // mirrors slog tick.slow when same lane recurs in window
```

Each gets an emitter in the package that owns the slog event:

| Substrate kind | slog source | Emitter package | Payload fields |
|---|---|---|---|
| `agent_non_completed_exit` | `internal/obs/events.go::EventAgentExited` ("agent.exited") | `internal/orchestrator/spawner` (already calls `db.RecordEvent` for other kinds; see `internal/orchestrator/state/events.go:27`) | `exit_reason`, `agent_id`, `last_text_hash` |
| `spawn_failed_retry` | `internal/obs/events.go::EventSpawnFailed` ("spawn.failed") | `internal/orchestrator/spawner` | `retry_count`, `agent_id`, `error_class` |
| `tick_slow_repeat` | `internal/obs/events.go::EventTickSlow` ("tick.slow") | `internal/orchestrator/scheduler` (already emits via `obs` near `scheduler.go:281`; mirror to `RecordEvent`) | `duration_ms`, `lane`, `tick_id` |

The emitter pattern is the existing one: every slog call that already emits these names gets paired with a `db.RecordEvent(ctx, agentID, EventKind…, payloadJSON)`. No new event surface is invented; this is mirror-on-emit, gated by the same `if err == nil` branch the existing kinds use. Per `feedback_deletion_default`: zero new tables, zero new packages.

### 2.2 R12-friction — one detector rule covers the three kinds

The sibling spec `docs/engineer/specs/2026-06-08-w45-detector-rules-r6-r11.md` claims R6 for "latency-outlier" (skeleton-prefetch, baseline-gated by 30-day soak). To avoid a numbering collision, this spec **resolves NOW** (per `feedback_spec_pattern_authority` — no impl-time renaming): the new rule numbers as **R12-friction** with rule registry name `friction_recurrence`. R12 is the next free slot above the W4.5 reservation block R6-R11. If W4.5 ships first, R12 stays R12; if this spec ships first, R6-R11 remain reserved for W4.5. No rename games at impl time. The rule MECHANICS in this spec are independent of the numbering.

R12-friction reuses the existing `streakRule` primitive at `internal/selfimprove/rules.go:38-46`. The rule registry stores a single `friction_recurrence` entry with three sub-rule names (one per kind) — `Rule.Name()` returns the sub-name so `Finding.DedupKey` salts on the same string per kind. The W4 mute surface (`regatta self-improvement mute <name>`) accepts either the root `friction_recurrence` (silences all three sub-rules) or any of the three sub-names individually.

| Sub-rule | Window | Threshold | group_by | Severity |
|---|---|---|---|---|
| `friction_recurrence_agent_exit` | 24h | 3 events same `exit_reason` (last_text_hash NOT included — see §2.3) | `exit_reason` | medium |
| `friction_recurrence_spawn_failed` | 24h | 2 events same `error_class` with `retry_count >= 2` | `error_class` | medium |
| `friction_recurrence_tick_slow` | 6h | 5 events same `lane` with `duration_ms >= 1000` | `lane` | low |

Thresholds are conservative defaults — no analytical baseline exists pre-soak. Per `feedback_default_simpler` we do NOT pre-build a YAML tuning surface. Calibration loop: if a default mis-fires twice in one operator session, file a tracker against the default; tightening / loosening happens in a follow-up PR. The §7 reopen-trigger commits to re-opening on first mis-fire so this is not an indefinite punt.

### 2.3 Dedup — fingerprint persisted in a new side table

Re-firing the same finding into a fresh issue every 5 minutes is unacceptable. The existing `selfimprove.Finding.DedupKey` (computed by `ComputeDedupKey` at `internal/selfimprove/rule.go:105`) is sha256(rule + sorted group_by + schema). The CLI verb today computes the key but does not persist it because dry-run / one-shot scans rely on GH-side dedup (existing-issue label search per W4 spec §6.1 step 4). The serve loop cannot afford a `gh issue list --label self-improvement` round-trip per scan — and at 5-min cadence the label query would also be racy against the same loop's own writes.

Solution: add one sqlite table colocated with `substrate_events`:

```
CREATE TABLE IF NOT EXISTS filed_friction_trackers (
    dedup_key    TEXT PRIMARY KEY,
    rule_name    TEXT NOT NULL,
    issue_number INTEGER NOT NULL,
    filed_at     INTEGER NOT NULL,   -- unix seconds
    closed_at    INTEGER             -- nullable; populated on subsequent scan when GH state=CLOSED
);
CREATE INDEX IF NOT EXISTS idx_filed_friction_filed_at ON filed_friction_trackers(filed_at);
```

The scan loop's filing path becomes:

1. Compute `Finding.DedupKey` (existing).
2. `SELECT issue_number FROM filed_friction_trackers WHERE dedup_key=?` — if row exists, skip (or comment-on-existing if W4 §6.1 step 5 ships).
3. Throttle gate (§2.5). If denied, log + skip.
4. File via `gh issue create` (label `state:auto-improve` per issue body c3 + standard `self-improvement` label per W4).
5. `INSERT INTO filed_friction_trackers(dedup_key, rule_name, issue_number, filed_at) VALUES(?,?,?,?)`.

The closed_at column is REQUIRED for v1 (not opt-in). Without it, a 7-day weekly cap consumes budget for already-closed issues, starving real signal in a session where the operator closes auto-filed bugs the same day they are filed. Scan loop: each iteration runs `gh issue view <number> --json state,closedAt` for any row where `closed_at IS NULL AND filed_at >= now-7d`, updates `closed_at` if state=CLOSED. The throttle gate counts ONLY rows with `closed_at IS NULL` against the cap.

**Dedup-key cardinality**: agent-exit `group_by` is `exit_reason` ALONE (no `last_text_hash`). The `last_text_hash` from spawner/claude.go:415-416 is sha256 of a 4KiB trailing-stdout window; every distinct stderr produces a unique key, which means three identical `provider_credit_exhausted` exits with different stderr text fragments dedup to three separate keys → zero dedup → throttle blown. The grouping signal MUST be the classifier output (`exit_reason`), not the raw text fingerprint. The text fingerprint stays in the substrate event payload for operator forensics; it does NOT participate in DedupKey computation.

### 2.4 Issue body — machine-authored, grep-friendly

Per issue #1077 design consideration 3: the body must explicitly state which detector + signal fingerprint produced it so the operator can grep + bulk-close if a detector is mis-firing.

Body template (one paragraph + one fenced block):

```
Filed by `regatta self-improve` detector_rules **R12-friction** /
sub-rule `friction_recurrence_agent_exit` at 2026-06-09T17:24:11Z.

Signal fingerprint (sha256 of rule + group_by + schema_v1):
`{{ DedupKey }}`

Window: last 24h. Threshold: 3. Observed count: {{ Count }}.

Source events (substrate_events.id):
{{ EventIDs joined as comma-list }}

Operator overrides:
- Add label `do-not-auto-file:R12-friction` to THIS issue to silence
  the rule until the issue is closed (or until the label is removed).
- Add label `do-not-auto-file:friction_recurrence_agent_exit` to
  silence only this sub-rule.
- Run `regatta self-improvement mute friction_recurrence_agent_exit`
  for a session-local mute (W4 §8.1).

Grep tag: `regatta-auto-friction-{{ shortHash(DedupKey, 8) }}`
```

No prose narration about what the bug "means" — that is `feedback_no_signatures` discipline. The operator reads the substrate events and decides. The grep tag mirrors `regatta-dedup-key:` used by the operator's manual filing flow and lets bulk-close find every auto-filed issue with one query.

**Machine-author-loop avoidance**: auto-filed issues are tagged with the label `state:auto-improve` ONLY. They MUST NOT carry the `autonomous` label that the `github_issues` adapter selector matches on (`internal/orchestrator/adapter/githubissues/selector.go::Match`). The orchestrator therefore never picks an auto-filed issue up as a dispatchable work-item; the operator must manually add the `autonomous` label after triaging it. Without this rule, R12-friction files an issue → adapter picks it up → agent fails → R12-friction re-files → infinite loop. Acceptance criterion c9 below tests this directly.

### 2.5 Throttle — hard caps

Two independent caps, both enforced before `gh issue create`:

- **5/day**: count rows in `filed_friction_trackers` with `filed_at >= now - 24h AND closed_at IS NULL`. If ≥5, skip and emit substrate event `friction_tracker_throttled`. Event payload schema (REQUIRED, not optional):
  ```
  {
    "dedup_key":  "<sha256-hex>",          // would-be Finding.DedupKey
    "rule_name":  "friction_recurrence",
    "sub_rule":   "friction_recurrence_agent_exit",
    "throttle_reason": "5_per_day" | "50_per_week"
  }
  ```
  The kind is itself a candidate for R12-friction in future revisions, so throttle exhaustion is observable.
- **50/week**: same query with 7d window.

Both caps share one in-process counter rebuilt at scan start; no separate persistence. **Race**: two scan iterations 5 min apart can each see count=4/day, both pass the gate, both file → cap overshoot by 1. The spec resolves this NOW (per `feedback_spec_pattern_authority`): **accept the one-fire slip** — the throttle is a soft signal not a hard contract, and a `BEGIN IMMEDIATE` transaction per scan would block legitimate concurrent writes from the dispatch loop on the same connection. The implementer MUST NOT invent a tighter primitive; the +1 overshoot is documented behavior. Conservative defaults per `feedback_default_simpler`; raise post-soak if the operator observes the throttle eating real signal. Lower if the auto-file rate is annoying.

### 2.6 Operator override — `do-not-auto-file:<rule-id>` label

Two override granularities, both label-driven on any OPEN issue:

- `do-not-auto-file:R12-friction` — silences the whole rule.
- `do-not-auto-file:<sub-rule-name>` — silences one sub-rule (e.g. `friction_recurrence_spawn_failed`).

The scan loop reads these via one `gh issue list --label "do-not-auto-file:R12-friction" --state open --json number -L 5` per scan iteration; presence of ANY open issue with the label short-circuits the rule. Removing the label OR closing the issue lifts the override. This is the "stop the detector while my fix is in flight" workflow per issue #1077 design consideration 5.

The label override is independent of the per-issue dedup table — a freshly-merged fix may legitimately want the detector active again, in which case the override label gets removed but the dedup row stays. The dedup row expires when its window passes (post-window scans re-fingerprint without finding a recent enough source-event match).

### 2.7 Serve loop wire-up

`cmd/regatta/serve.go` already runs a long-lived orchestrator with `internal/orchestrator/scheduler/scheduler.go::Tick` on a timer. Add one goroutine started alongside the scheduler:

```
go func() {
    t := time.NewTicker(cadence)   // default 5 min, --self-improve-cadence=N flag
    defer t.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-t.C:
            // existing dry-run path → apply with throttle+dedup
            runOneFrictionScan(ctx, db, ghClient, cadence)
        }
    }
}()
```

`runOneFrictionScan` reuses `selfimprove.Detector.Run` (existing surface at `internal/selfimprove/detector.go`), then walks `res.Findings` through the dedup table → throttle gate → `gh issue create` path described in §2.3.

Per issue #1077 c3: `--self-improve=false` opts out; default on. `--self-improve-cadence=N` (default 60 ticks ≈ 5 min at tick=5s; this spec pins WALLCLOCK duration not ticks — simpler under autotuned tick rates).

### 2.8 What this spec does NOT add

Per `feedback_default_simpler` and §4 below: no YAML config tier, no per-rule severity tuning surface, no metric-export of throttle state, no rule mute CLI extension (W4 §8.1 promised one and can absorb R12-friction without changes). No new package — everything lives under `internal/selfimprove/` and `cmd/regatta/serve.go`.

## §3 Acceptance

Per `feedback_tdd_discipline`, every test below lands as a FAILING commit BEFORE its impl in the implementation PR (this spec PR is docs-only).

1. **Substrate kinds wired** — `internal/orchestrator/spawner/spawner_obs_test.go` extended with `TestSpawner_AgentExited_RecordsSubstrateEvent` (asserts a `substrate_events` row with `kind=agent_non_completed_exit` lands when `exit_reason != completed`). Same shape for `TestSpawner_SpawnFailed_RecordsSubstrateRetry`. Scheduler-side: `internal/orchestrator/scheduler/scheduler_test.go` adds `TestScheduler_TickSlow_RecordsSubstrateEvent` (mirror of the existing `TestOrchestrator_Tick_SlowTickEmitsWarn` at `internal/orchestrator/orchestrator_test.go:452`).
2. **Detector rule registered** — `internal/selfimprove/rules_test.go` adds `TestR6Friction_FiresOnThreshold` per sub-rule (3 tests), each constructing the event fixture, calling `Match`, asserting one `Finding` with stable `DedupKey`.
3. **Dedup table + persistence** — new test file `internal/selfimprove/dedup_test.go`: `TestFiledFrictionTrackers_RoundTrip` (insert, lookup-hit, re-insert is a no-op via PK constraint).
4. **Throttle caps** — `TestThrottle_5PerDayHardCap` and `TestThrottle_50PerWeekHardCap` in same file. Each pre-populates the dedup table with synthetic rows + asserts `runOneFrictionScan` short-circuits.
5. **Operator override** — `TestOverride_DoNotAutoFileLabel_ShortCircuitsRule` in a new `cmd/regatta/serve_friction_integration_test.go`. Uses `internal/ghclient` test fakes; asserts that the presence of an open issue with label `do-not-auto-file:R12-friction` skips ALL three sub-rules. Companion `TestOverride_SubRuleLabel_ScopesToOneSubRule` asserts the sub-rule label only skips one sub-rule. Additional: `TestOverride_LabelRemoval_LiftsSilence` (label removed → next scan re-fires) and `TestOverride_IssueClosure_LiftsSilence` (issue closed with label intact → next scan re-fires) per reviewer feedback aa761794ae1ef0aa1.

9. **Machine-author-loop avoidance** — `TestAutoFiledIssue_NoAutonomousLabel` asserts every auto-filed issue carries `state:auto-improve` and `self-improvement` labels but NOT the `autonomous` label (the github_issues adapter selector match string). Without this gate, R12-friction infinite-loops on its own filed issues.
6. **Serve cadence** — `TestServe_FrictionScanRunsAtCadence` (mock clock; advance 5min, assert exactly one scan; advance 5min, assert two). Companion: `TestServe_DisableFlag_NoScan` (`--self-improve=false` → zero scans across 30min mock time).
7. **Issue body format** — `TestIssueBody_ContainsFingerprintAndOverrideHints` parses the rendered body, asserts the DedupKey, sub-rule name, and `do-not-auto-file:` hint strings are all present.
8. **Idempotent re-scan** — `TestIdempotentReScan_NoDupFile` (run scan twice over same event fixture, assert exactly one `gh issue create` call).
9. **No leaks across rules** — `TestThrottle_PerRuleNotShared` (R12-friction's throttle does NOT consume budget belonging to future detector rules; budget is per-rule-class if multiple ship).
10. **Wire-up gate** — `make ci-check` clean after impl; `scripts/check-spec-sections.sh`, `scripts/check-phase-x-leak.sh`, `scripts/check-doc-links.sh` clean on this PR (spec is in scope for the first; the others are noops here).

## §4 Out of scope

Per `feedback_default_simpler` + `feedback_recognize_session_end`:

- **YAML threshold tuning** — defaults pinned in Go constants. Calibration loop is "operator files a tracker against the default", not a tier system. Reopen on first observed mis-fire.
- **Rule R7–R11 from sibling spec** — `docs/engineer/specs/2026-06-08-w45-detector-rules-r6-r11.md` is skeleton-prefetch and baseline-gated. R12-friction does NOT block on that wedge.
- **Auto-close tracker on rule-mute** — if the operator mutes a rule, existing trackers stay open until they hit GH-state CLOSED. No auto-close-on-mute; defer until observed need.
- **Cross-rule correlation** — R12-friction fires three sub-rules independently. No correlation predicate ("agent exit AND spawn fail AND tick slow on same lane → super-finding"). Add when a real session shows it would have caught a defect the per-rule fires missed.
- **Severity → priority routing** — every R12-friction tracker lands with the `state:auto-improve` label and no priority. The operator triage flow handles routing; the detector does not pick priority.
- **Metrics export for throttle state** — `friction_tracker_throttled` substrate event is the surface. Prometheus exposition is not in v1.
- **Per-`agent_id` carve-outs in sub-rules** — every event of a kind counts. Per-agent throttle bands are a Phase-X reopen.
- **Closed-loop with autotuner (#926)** — R12-friction is operator-visibility only; it does NOT feed any autotuner knob. The sibling spec §8 ratifies which rules feed which knobs; R12-friction lands strictly outside that table.

## §5 Adversarial

This section is a SELF-AUDIT placeholder. Per `feedback_adversarial_review_every_step` + `feedback_no_self_tagged_approve` + the CLAUDE.md gate "Adversarial pass on specs mandatory", an independent reviewer subagent MUST review this spec before the PR carries any `Reviewer-recommendation:` token. The PR body for this spec deliberately omits the token — the spec ships in `[DOCS]` state and the reviewer gate is satisfied by an independent reviewer dispatch in a follow-up review pass.

Likely adversarial-review hunting grounds (the reviewer should NOT trust this list — they should hunt fresh):

- **Slog → substrate mirror drift.** If the spawner emits `agent.exited` via slog but the `RecordEvent` mirror is conditional on a write error path, the detector misses signal. Reviewer: trace every slog `EventAgentExited` call-site and confirm the mirror is unconditional (or the conditional is correctly scoped). Same audit for `EventSpawnFailed` and `EventTickSlow`.
- **Dedup-key cardinality blow-up.** `last_text_hash` in the agent-exit group_by may make EVERY exit unique → no dedup at all. Reviewer: confirm `last_text_hash` is sourced from the trimmed last-text ring (`internal/orchestrator/spawner/claude.go:368`) not the full stdout — high-entropy stdout busts dedup.
- **Throttle window race.** Two scan iterations 5min apart, each at 4/day count; both pass the gate, both file. Reviewer: confirm the count is computed inside a `BEGIN IMMEDIATE` transaction or the scan is single-threaded by design (only one timer goroutine).
- **Operator override label race.** Operator adds `do-not-auto-file:R12-friction` between the scan's label-query and the issue-create call; the override is missed for one fire. Reviewer: this race is small (one detect window) but real. Decide whether to re-check the label inside the create path or accept the one-fire slip.
- **Rule-numbering collision with W4.5.** §2.2 acknowledges this; reviewer should confirm the deferral (R6 split at impl) is acceptable to the W4.5 spec author OR that R12-friction renames to R12+ now.
- **`state:auto-improve` label cardinality.** Issue #1077 c3 names the label; this spec uses it. Reviewer: confirm GH org label namespace permits the colon syntax and the label exists or is created idempotently.
- **PR-watch interaction.** R12-friction creates issues; do those issues appear as work_items the orchestrator picks up and dispatches an agent against? If yes, infinite loop (agent fails → R12-friction files issue → agent picks up → fails). Reviewer: confirm the `state:auto-improve` label is in the orchestrator's adapter denylist OR explicitly NOT a triggering label.

Reviewer verdict goes here on the next pass: `Reviewer-recommendation:` is INTENTIONALLY ABSENT from the PR body in this submission.

## §6 Implementer brief

Per `feedback_dispatch_brief_only`:

```
Scope: Implement R12-friction per spec §2. Land in 3 PRs:
  PR-A: substrate event-kinds + emitters (spawner + scheduler).
  PR-B: detector rule + dedup table + throttle + override label.
  PR-C: serve.go cadence + flag wiring.
Files (PR-A): internal/selfimprove/rules.go (new EventKind* consts),
  internal/orchestrator/spawner/{claude.go,genai.go} (mirror RecordEvent
  alongside existing slog emits), internal/orchestrator/scheduler/scheduler.go
  (mirror tick.slow), corresponding *_test.go.
Files (PR-B): internal/selfimprove/rules.go (R12-friction registration),
  internal/selfimprove/dedup.go (new), internal/selfimprove/throttle.go (new),
  internal/selfimprove/dedup_test.go, internal/selfimprove/throttle_test.go,
  internal/selfimprove/rules_test.go (extend).
Files (PR-C): cmd/regatta/serve.go (+ --self-improve / --self-improve-cadence
  flags), cmd/regatta/serve_friction_integration_test.go.
TDD order: red commit per §3 acceptance test FIRST, then impl, then green.
make ci-check exit: 0 on each PR.
Reviewer dispatch: YES — load-bearing per check-reviewer-verdict.sh
  (paths under cmd/, internal/orchestrator/, internal/selfimprove/ all
  match the load-bearing allowlist).
```

Per spec discipline: implementer MUST NOT pick the rule numbering split (R6a/R6b vs R12) — re-spawn the design subagent if the W4.5 author's preference is unclear.

## §7 Reopen trigger

Per `feedback_recognize_session_end`:

- **Reopen when** any of:
  - The throttle caps eat ≥1 finding that the operator agrees was real signal (raise caps or add per-rule budgets).
  - A sub-rule's default threshold mis-fires ≥2 times in one session against the operator's judgment (calibrate threshold).
  - A new slog event-kind in the friction surface lands (`reaper_killed_repeat`, `prwatch_renamed_recurrent`, etc.) and the operator files ≥2 manual trackers for it within 7d (extend R12-friction to a 4th sub-rule).
  - W4.5 R6-R11 (skeleton-prefetch) unblocks the wedge and the rule-numbering needs reconciliation.
  - GH label `do-not-auto-file:*` accumulates ≥10 instances simultaneously (the override mechanism has become primary — that is a smell; redesign).
- **Stay closed** while: the operator's manual filing rate for friction-pattern trackers drops to ≤1/week AND the throttle caps are NOT being hit weekly.
