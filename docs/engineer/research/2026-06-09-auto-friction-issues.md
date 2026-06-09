---
status: research
issue: 1077
date: 2026-06-09
---

# Auto-file friction tracker issues from orchestrator-observed patterns

Goal: orchestrator detects recurring friction in its own substrate + opens GH issues automatically. Operator goes from "100% issue author" to "review + label". Dogfood 2026-06-08: operator manually filed 21 issues; orchestrator observed every triggering event but filed zero.

## 1. Existing self-improve infra (survey)

`cmd/regatta/selfimprove.go` (115 LoC) + `internal/selfimprove/` (~1.1k LoC) ship a working detector wired as a CLI subcommand:

- `regatta self-improve scan [--since=7d] [--apply] [--db=regatta.db]` — dry-run by default (#646). With `--apply`, opens substrate read-only WAL, runs `DefaultRules()`, dedups against open GH issues, files new ones.
- `regatta self-improve rules` — lists registered rules.
- `Detector.Run` (detector.go): fetch via `EventFetcher` (production = `SQLEventSource.Fetch` against `substrate_events`), scatter to rules, dedup by SHA fingerprint, file or comment.
- `streakRule` (rules.go) is the shared primitive: bucket events by `groupBy` closure inside `Window`, fire `Finding` when `threshold` crossed.
- Dedup: `ComputeDedupKey(ruleName, groupByMap)` SHA → `regatta-dedup-key:` HTML comment in body. `Detector` queries open issues w/ `label=self-improvement` and matches.
- `PauseAllTag` filter prevents cost-pause storms from blaming agents (spec §11 risk #4).
- `LLMScanner` (llm.go) is the nightly Haiku-fed proposal path — writes YAML to disk, NEVER auto-files (guarded by test). Out of scope for #1077.

Wiring gap: detector exists, but `runSelfImproveScan` is invoked only via operator CLI. Nothing inside `regatta serve` calls it on a cadence. Spec §8.1 explicitly chose dry-run default; the leap to "auto-file from `serve`" is unspec'd.

## 2. Gap analysis — what's caught vs missed

Currently detected (5 rules): `same-gate-fail-repeats`, `banned-phrase-recurrence`, `subagent-claimed-clean-but-ci-failed`, `load-bearing-leftover-pattern`, `reaper-kills-same-agent`. All require event-kinds already projected into `substrate_events` (gate_fail, doc_check_failed, subagent_claim, ci_failed, pr_body_scan, reaper_killed).

NOT detected (this session's friction):
- **WARN ticker storms** — `orchestrator.item_body_missing` fires every 5s tick; no event-kind written, only slog. (#1066-class)
- **exit_reason clusters** — `provider_credit_exhausted` cluster of 3+ within an hour. `exit_reason` is recorded on agent rows but not projected as a `substrate_events` kind. (#1096)
- **spawn.failed retry storms** — `obs.EventSpawnFailed` emitted as slog + `state.RecordEvent` kind=`spawn_failed` (orchestrator_schedule.go:99). Already in DB, just no rule consumes it. (#1093)
- **Stale-binary log noise** — repeated `stale-binary` WARN. No substrate event. (#1079)

## 3. Five proposed new rules

Pattern: extend `EventKind*` constants + `substrateRow` projection + append to `DefaultRules()`. Tests in `rules_test.go` clone existing streak-rule tests w/ new fixtures.

1. **`rule_warn_ticker_repeats`** (window=15m, threshold=10, severity=low) — group by `warn_event` (e.g. `item_body_missing`). Requires new substrate-event kind `warn_emitted` written from the orchestrator slog warn path w/ payload `{"warn_event": "..."}`. Issue: `BUG-AUTO-WARN: <event> firing every tick`.
2. **`rule_exit_reason_cluster`** (window=1h, threshold=3, severity=high) — group by `exit_reason`. Already-emitted kind `agent_exited` (or add it w/ `exit_reason` payload). Catches `provider_credit_exhausted` clusters. Issue: `BUG-AUTO-EXIT: <reason> cluster (N agents)`.
3. **`rule_spawn_failed_retry_storm`** (window=30m, threshold=5, severity=high) — group by `error_class` from existing `spawn_failed` kind. Issue: `BUG-AUTO-SPAWN: <error_class> retry storm`.
4. **`rule_stale_binary_repeats`** (window=1h, threshold=6, severity=low) — group by `binary_sha` from a new `stale_binary_detected` kind. Issue: `BUG-AUTO-STALE: binary <sha> stale Nx`.
5. **`rule_dup_warn_per_agent`** (window=10m, threshold=20, severity=med) — group by `(agent_id, warn_event)`. Catches per-agent tight loops distinct from system-wide noise.

## 4. Anti-spam strategy

Already mature: `ComputeDedupKey` SHA fingerprint + `regatta-dedup-key` HTML comment + open-issue label filter. Reuse verbatim. Additions:

- **Auto-close on resolution**: when rule stops firing for N consecutive scans, comment "no recurrence in 24h" (do NOT auto-close — operator decides).
- **Severity floor for `serve` mode**: ticker storm + stale binary at `low` severity comment-only on existing issue rather than file new; `high` files new. CLI `--apply` keeps current behavior.
- **Per-scan file cap**: max 3 new issues per scan; remainder queued to slog WARN until next scan. Prevents first-run flood after long offline window.
- **`state:auto-improve` label**: distinct from operator-filed `self-improvement` so triage lane is separable.

## 5. Operator workflow

Before (today): tail logs → notice pattern → write title/body/dedup key → `gh issue create` → label → assign. ~3min per issue × 21 = ~60min/session of pure scribe work.

After: orchestrator emits issue on Nth occurrence → operator reviews `state:auto-improve` queue → bulk-relabels for triage → closes false positives. ~10s per issue × 21 = ~3min review. Operator authoring drops to edge cases the rule engine misses.

CLI escape hatches: `regatta serve --self-improve=false` (default on); `regatta self-improve scan --apply=false` dry-run preserved verbatim; per-rule mute via `--mute=rule_warn_ticker_repeats` (new flag, persisted in config).

## 6. Estimated effort

- Substrate event-kind additions (`warn_emitted`, `agent_exited` w/ exit_reason, `stale_binary_detected`): ~1d. Touches orchestrator slog → substrate seam at 3-4 callsites.
- Five rules + tests: ~0.5d. Pure copy-of-streakRule.
- `serve`-loop wiring (`--self-improve-cadence=60` ticks): ~0.5d. New goroutine in `cmd/regatta/serve.go` invoking `Detector.Run` w/ existing GH client.
- Severity-aware comment-vs-file branch + per-scan file cap: ~0.5d in `detector.go`.
- Integration test (`selfimprove_serve_integration_test.go`): ~0.5d.

Total: ~3 engineer-days. No new deps. Single-PR or 2-PR split (substrate first, then rules+wiring) per shared-primitive owner discipline.
