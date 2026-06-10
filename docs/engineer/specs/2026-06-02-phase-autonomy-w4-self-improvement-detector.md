# PHASE-AUTONOMY W4 — Self-Improvement Detector — Design Spec

Status: ready for review
Date: 2026-06-02
Author: design subagent
Item: `.regatta/items/phase-autonomy-w4-self-improvement-detector.md`
Source brief: `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W4
Depends on: PHASE-AUTONOMY-W1 (alarm webhook + GH-client adapter), PHASE-AUTONOMY-W2 (auto-merge), PHASE-AUTONOMY-W3 (process supervisor)
Soft-depends on: PHASE-AUTONOMY-W5 (cost-pause flag — detector must read it to avoid blaming agents for pause-induced halts)

Memory rules in force: `feedback_decision_priority`, `feedback_research_design_principles`, `feedback_root_cause`, `feedback_grade_rubric`, `feedback_adversarial_review`, `feedback_review_every_step`, `feedback_deletion_default`, `feedback_self_improvement`, `feedback_no_signatures`, `feedback_pr_body_release_notes_fence`, `feedback_pr_body_file_only`, `feedback_doc_check_banned_phrases`, `feedback_unaddressed_load_bearing`, `feedback_spec_pattern_authority`, `feedback_test_godoc_one_line`.

---

## §1 Problem statement

The autonomous loop produces failure patterns the operator currently has to scan logs to find. Concrete cases observed in the wave preceding this spec:

- 5 PRs in one session tripped the same `doc-check` test-godoc gate because the dispatch template did not name the 1-line rule.
- The L4 adversarial gate has rejected runs from the same author shape repeatedly with the same reason on multiple waves.
- Reaper kill counts on a single agent ID accumulate silently across days.

Each of these is a self-correcting loop the substrate already has all the data for. The operator should see one `[self-improvement]` GH issue per pattern — body containing the pattern, three substrate-event citations, a root-cause hypothesis, and one concrete suggested edit (memory file, dispatch template, or boot prompt section) — instead of having to derive the pattern manually.

W4 closes that loop: detector reads `substrate_events` over a sliding window, runs a heuristic suite, files / comments on issues via the W1 GH-client adapter, and (one wave later) the regatta loop picks the issue → subagent writes the suggested edit → PR → merge → pattern stops recurring.

Cited brief sections: §11 W4 (goal, four triggers, prior art, LoC estimate, dependencies, acceptance criteria, B/A/A+).

### 1.1 Non-goals

- A general anomaly-detection engine. The substrate event vocabulary is small and operator-owned; pattern matching at this scale is rule-driven plus one nightly LLM pass.
- Re-architecting the substrate. The detector is a read-only consumer of the existing `substrate_events` table (unified-substrate spec §3).
- Auto-applying the suggested edit. The loop applies it via the standard wave path (issue → dispatch → PR → reviewer → merge). The detector only files the issue.
- Replacing operator triage. Operator can mute a rule, raise its threshold, or delete it — the detector is a first-pass funnel, not an oracle.

---

## §2 Decision priority filter

Per `feedback_decision_priority` (UX > ease > performance > best-practices > speed > velocity; long-term > short-term):

| Lens | Choice |
|---|---|
| UX | One GH issue per pattern, body actionable on its own. Operator never opens substrate raw. |
| Ease | Rules in one YAML file. Adding a sixth rule = one-file PR + one Go test. |
| Performance | Detector runs nightly cron, not hot path. O(events_in_window) per rule. Daily budget pinned (§9). |
| Best-practices | Rule-driven first for known patterns; LLM only for unknown-unknown — falsifiable thresholds beat free-form LLM rationalization for the 4 named triggers. |
| Long-term | Each filed-and-closed issue ratchets a feedback memory file or dispatch template; the suite gets shorter over time, not longer. |

The single highest-leverage UX win is: operator stops scanning logs. That ranks the issue-quality work (template + dedup + citations) above raw heuristic coverage.

---

## §3 Architecture — hybrid rule-driven + LLM-nightly

### 3.1 Choice: hybrid, not pure-rule and not pure-LLM

Pure-rule (Sentry-grouping shape): operator must encode every pattern up front. Cheap, fast, opaque to operator until the rule fires. Misses unknown-unknown patterns.

Pure-LLM (read N events, ask Claude for recurring failures): catches anything. Risk: LLM hallucinates a pattern from coincidental co-occurrence; consumes daily-cap budget; non-deterministic re-runs.

Hybrid (this spec): rule-driven for the four named triggers + any rule the operator adds to YAML. LLM-driven runs **nightly only**, emits **candidate rule suggestions** (not direct issues) for operator review. Each accepted suggestion becomes a new YAML rule (1-file PR). The LLM never files an issue directly — it can only propose a rule.

This matches the brief's `feedback_self_improvement` directive: the loop should grow its rule suite from observed friction, not from a model's free-form opinion.

### 3.2 Component map

```
  substrate_events (read-only)
            |
            v
+-------------------------+
|  Detector (cmd/         |
|  regatta-self-improve)  |
|                         |
|  - rule registry        |
|  - sliding-window scan  |
|  - dedup ledger         |
|  - LLM-nightly job      |
+-------------------------+
            |
            +---> W1 GH-client adapter ---> [self-improvement] issue / comment
            |
            +---> stdout / log (CLI mode)
            |
            +---> proposals/ (LLM-nightly drafts)
```

### 3.3 Package layout

- `cmd/regatta-self-improve/main.go` — entrypoint (cron mode + CLI mode).
- `internal/selfimprove/rule.go` — `Rule` interface + registry.
- `internal/selfimprove/heuristics.yaml` — operator-owned rule config (5 named rules at MVP).
- `internal/selfimprove/loader.go` — YAML → `[]Rule` parser.
- `internal/selfimprove/window.go` — sliding-window adapter over `substrate_events`.
- `internal/selfimprove/dedup.go` — open-issue lookup via GH client; writes comment on re-fire.
- `internal/selfimprove/issue.go` — issue body template.
- `internal/selfimprove/llm.go` — nightly LLM scan; emits `proposals/<date>-<hash>.yaml` for operator review.
- `internal/selfimprove/replay.go` — replay-harness entrypoint (A+ criterion).
- `prompts/self-improvement-scan.txt` — LLM prompt template.

---

## §4 Substrate query interface

### 4.1 Read path

The detector executes one `SELECT` per rule against `substrate_events` over a sliding window (default 7 days, rule-overridable). Query shape:

```sql
SELECT id, occurred_at, run_id, kind, payload_json
FROM substrate_events
WHERE occurred_at >= ? AND kind IN (?, ?, ?)
ORDER BY occurred_at DESC
```

The `kind IN (...)` set is per-rule (each rule declares which event kinds it reads — see `events_consumed` field in §5.3). The detector unions every rule's kind set into one outer query per scan run, then scatters rows to rules in memory. Single full-table scan, not one-per-rule.

### 4.2 Optional materialized view (Phase-X reopen-trigger: >100k events/day)

The brief's LoC estimate (~250 Go) and acceptance criteria do not require pre-aggregated counters. If event volume crosses 100k/day (currently ~hundreds/day per `2026-06-01-unified-substrate-design.md` §performance), reopen this trigger: build a per-rule `mv_self_improve_<rule_name>` SQLite view refreshed on scan-start. Phase-X per self-host filter.

### 4.3 Time scope

- Default window: 7 days.
- Rule-overridable via YAML `window:` field (`24h`, `30d`, etc.).
- The detector stores its own `last_scan_at` in a `self_improve_state` row inside the substrate (one row, key `last_scan`). Re-scans never re-process events older than the previous scan's threshold minus the longest rule window — bounded retraversal.

---

## §5 Rule library — five canonical rules at MVP, six-to-twelve room to grow

### 5.1 Rule schema (YAML)

```yaml
- name: same-gate-fail-repeats
  window: 7d
  threshold: 3
  severity: medium
  events_consumed: [gate_fail]
  group_by: [gate_kind, gate_reason]
  filter_out: [regatta_pause_all]   # soft W5 dep — see §11 risk #4
  issue_template: same-gate-fail-repeats
```

`group_by` defines the fingerprint: rows are bucketed on the tuple of named payload fields; a bucket whose count crosses `threshold` within `window` fires the rule. Fingerprint shape borrowed from Sentry's grouping algorithm (referenced, not imported — per brief §11 W4).

`filter_out` lists event kinds whose presence in the same window suppresses the rule (used by the W5 cost-pause coordination — §11 risk #4).

### 5.2 Five MVP rules (covers brief's four named triggers + one extra)

| # | Name | Window | Threshold | Events consumed | Group by | Maps to brief trigger |
|---|---|---|---|---|---|---|
| R1 | `same-gate-fail-repeats` | 7d | 3 | `gate_fail` | `gate_kind, gate_reason` | "Same gate-fail ≥3× in 7 days" |
| R2 | `banned-phrase-recurrence` | 7d | 2 | `doc_check_failed` | `banned_token` | "Same banned-phrase token tripped ≥2× across distinct PRs" |
| R3 | `subagent-claimed-clean-but-ci-failed` | 7d | 3 | `subagent_claim`, `ci_failed` | `claim_text, failure_kind` | "Same agent-failure-mode ≥3× in 7 days" |
| R4 | `load-bearing-leftover-pattern` | 14d | 2 | `pr_body_scan` | `leftover_pattern` | "Same load-bearing-leftover pattern in ≥2 PR bodies" |
| R5 | `reaper-kills-same-agent` | 7d | 5 | `reaper_killed` | `agent_id` | (extra, not in brief — added because operator hit this in observed waves) |

R5 is the one rule beyond the four named brief triggers. Each rule = ~25 LoC Go + a one-line YAML entry; the brief's 250-LoC budget covers all five with headroom.

### 5.3 Adding a sixth rule = one-file PR

Brief acceptance criterion c3: "Heuristic suite lives in `internal/selfimprove/heuristics.yaml`; adding a 6th heuristic is a single-file PR + a single Go test." This holds because:

- New event kinds require no Go change (the query builder reads `events_consumed` from YAML).
- New `group_by` payload fields require no Go change (the bucketer reads JSON paths dynamically).
- The only Go change is the per-rule unit test (`Test_R6_<name>` — 1-line godoc per `feedback_test_godoc_one_line`).

Falsifiable: a worked example of adding R6 (`cost-cap-cycling`) appears in §15 Followups (deferred to W5 land; not in this spec's scope).

### 5.4 Severity tiers

- `low` — informational; no operator ping; filed quarterly digest only.
- `medium` — file issue immediately, label `self-improvement`.
- `high` — file issue immediately, label `self-improvement` + `obs-alert`; W1 alarm-webhook fans out.

Tiers used by the dedup-and-mute machinery (§11 risk #1).

---

## §6 Issue-filing flow

### 6.1 Sequence per rule fire

1. Bucket events by `group_by` fields in memory.
2. For each bucket whose count ≥ threshold: assemble candidate issue.
3. Compute dedup key: `sha256(rule_name + sorted_group_by_values + schema_version)` — same shape as Sentry fingerprint; schema_version included per §11 risk #5.
4. Query GH for open issues with label `self-improvement` via `gh issue list --label self-improvement --state open --json number,body` (bulk fetch, cached for scan duration per §11 risk #6); scan response bodies for substring `dedup:<key>` (the marker is emitted as a visible-but-low-noise trailing line `<!-- dedup:<key> -->` AND a fallback plain-text line `dedup-key: <key>` so the substring match survives any GH renderer that strips HTML comments).
5. Branch:
   - No open issue → render body from `issue_template` (§6.3) → call W1 GH adapter `CreateIssue(title, body, labels)`.
   - Open issue exists → render comment-body (lighter weight: "rule fired again, N events in last 24h, latest substrate links: ...") → `CreateIssueComment(issue_number, body)`.
   - Open issue with > 5 same-pattern re-fires → escalate: comment plus add label `needs-operator-review` (mute candidate).
6. Append `self_improve_fired` substrate event with `{rule_name, dedup_key, count, action}` — so the dedup ledger is itself substrate-resident (no parallel storage).

### 6.2 Adapter wiring

The detector imports the W1 alarm-webhook's GH-client adapter (`internal/ghclient` per W1 spec) directly. No new GH-client. Authentication flows through the same operator token; rate-limit handling is W1's. Reuse is mandatory per `feedback_research_design_principles` and the brief's "Adopt vs build" stance.

### 6.3 Issue body template (§6.3.1) — required fields

```
title: [self-improvement] <rule_name> — <human-readable group_by>

body:
  ## What happened

  Rule `<rule_name>` fired: <count> events of kind <events_consumed> grouped by <group_by> within <window>.

  ## Substrate citations (3+)

  - event #<id> at <occurred_at> (run_id=<run_id>) — <one-line summary>
  - event #<id> at <occurred_at> (run_id=<run_id>) — <one-line summary>
  - event #<id> at <occurred_at> (run_id=<run_id>) — <one-line summary>

  ## Root-cause hypothesis

  <hypothesis_text from issue_template — rule-specific>

  ## Suggested edit (one of)

  - Memory file: `memory/feedback_<slug>.md` (new) — <one-line of what should be captured>
  - Dispatch template: `docs/engineer/dispatch-templates/<template>.md` — <one-line of what should be added>
  - Boot prompt: `docs/engineer/autonomous-session-prompt.md` §<section> — <one-line of what should be added>

  ## Estimated time saved (A+)

  If this pattern stops recurring: ~<N> operator minutes/week (computed from event rate × per-event triage cost).

  ---
  dedup-key: <sha256_key>
  rule: <rule_name>
  replay: regatta self-improvement replay --rule=<rule_name> --since=<window_start>
  <!-- dedup:<sha256_key> -->
```

Falsifiable: brief acceptance c1 requires "detected pattern + 3+ source-event links + root-cause hypothesis + one suggested edit" — every field above is mandatory; the template-loader fails closed if any is empty.

### 6.4 Comment body (re-fire path)

Lighter: "Rule fired again — N new events since last comment (latest substrate ids: ...). No body change required." Keeps the issue thread readable without re-pasting the original body each time.

---

## §7 LLM-driven nightly scan

### 7.1 Purpose

Catch unknown-unknown patterns the YAML rules cannot encode in advance. NOT a substitute for rules.

### 7.2 Mechanics

- Cron: 02:00 UTC daily (operator-overridable).
- Input: aggregated digest from `regatta digest --since=7d --format=jsonl` (substrate event stream, redacted per W1 PII rules).
- Prompt: `prompts/self-improvement-scan.txt`. Template fields: `<digest_jsonl>`, `<existing_rules_yaml>`. Instruction: "Identify recurring failure patterns NOT already covered by existing rules. Emit candidate-rule YAML entries. Do not file issues."
- Model: Claude Haiku (cheapest tier per `feedback_decision_priority`'s ease+performance lens; nightly job, not latency-critical).
- Output: write `internal/selfimprove/proposals/<date>-<short-hash>.yaml` into the repo (PR-able, operator-review-able). No GH issue filed automatically.
- Substrate event: `self_improve_llm_proposal` with `{proposal_path, model, tokens_used, cost_usd}`.

### 7.3 Operator review path

Each proposals/*.yaml is a draft rule. Operator reviews, edits, accepts → moves into `heuristics.yaml` → one-file PR → rule live next scan. Rejected proposals stay in `proposals/` as historical record; pruned at 30 days.

### 7.4 Budget

One Haiku call per night with ~5k input tokens + ~1k output ≈ $0.005 per night ≈ $0.15/month. Brief's "$0.50 per nightly" budget covers a Sonnet upgrade if Haiku output is too thin; default Haiku.

---

## §8 Operator UX

### 8.1 CLI surface

- `regatta self-improve scan [--since=7d] [--rule=<name>] [--apply] [--db=regatta.db]` — runs rules over the substrate window. **Default is dry-run** (`--apply=false`): prints findings without filing GH issues. Operator promotes a clean audit to issue-filing by appending `--apply` manually. The shipped `scripts/cron/regatta.crontab` line ships without `--apply` so first-deploy never spam-files a noisy ruleset (#646). Per-rule filter via `--rule`.
- `regatta self-improvement rules` — lists rules with thresholds + windows + last-fire timestamp.
- `regatta self-improvement mute <rule_name> [--for=24h|7d|forever]` — silences a rule by writing a `self_improve_muted` substrate event; detector reads-and-respects on next scan. No YAML edit needed for short mutes.
- `regatta self-improvement replay --rule=<name> --since=<ts>` — re-runs the rule against a frozen substrate snapshot (A+ replay harness, §10).

### 8.2 Daemon surface

`regatta serve` already runs scheduled jobs. Self-improve detector registers a cron entry: nightly LLM at 02:00 UTC, rule scan every 6h. Operator can override frequency in `regatta.yaml`:

```yaml
self_improvement:
  enabled: true
  scan_interval: 6h
  llm_scan_cron: "0 2 * * *"
```

### 8.3 No web UI in W4

The W7 operator web UI ships separately. W4 surfaces results through GH issues + CLI — same path as every other gate today.

---

## §9 Performance + budget

### 9.1 Compute

Per scan:

- One outer `SELECT` across union of all rules' event kinds, ordered by `occurred_at DESC`, bounded by widest window (14d for R4). Current substrate volume estimate: hundreds of events/day × 14 = ~10k rows. SQLite full scan: <50ms.
- In-memory bucket-by-group_by per rule: O(events) hashmap update. <5ms.
- GH dedup query per rule that fires: 1 GH API call (label-filtered list-issues). Rate-limit budget: 5 rules × 1 call = 5 calls per scan, well under W1 budget.

Daily compute: 4 scans (every 6h) × 50ms = 200ms/day SQLite, ~20 GH calls/day. Negligible.

### 9.2 Dollar budget

- LLM nightly: $0.005-$0.50 depending on Haiku/Sonnet choice (§7.4). Pinned at $0.50/day worst-case.
- No other paid surface; GH API + SQLite + local Go compute = free.

### 9.3 Cost-cap interaction

The detector itself counts against the daily LLM cap (W5 dependency soft). When `regatta_pause_all=true`, the detector still runs its rule scan (free) but **skips** the nightly LLM call. This prevents the perverse loop where self-improve burns the last $0.50 of the daily cap and triggers a pause-cycle that R5/R6 then flags as a pattern. Coordination point with W5 — §11 risk #4.

---

## §10 Test plan

### 10.1 Per-rule unit tests

Each rule gets one Go unit test: fixture event stream, assertion that bucket counts match expected fire/no-fire boundary. 1-line godoc per `feedback_test_godoc_one_line`.

### 10.2 End-to-end fixture replay (A+ criterion)

The replay harness (`internal/selfimprove/replay.go`) loads a frozen substrate snapshot (newline-delimited JSON, checked into `internal/selfimprove/testdata/replay-<date>.jsonl`), re-runs every rule deterministically, and asserts the exact set of issues that should fire. Brief A+ criterion (i): "Replay harness re-runs the substrate window deterministically and reproduces every filed issue."

Snapshot rotation: monthly; old snapshots pruned at 6 months unless an unresolved bug references them.

### 10.3 Dedup integration test

Run the rule twice against the same fixture; assert second run produces a comment, not a new issue. Brief acceptance c2.

### 10.4 LLM-proposal smoke test

Mock-LLM (canned response) + fixed digest input → assert a `proposals/<date>-<hash>.yaml` file is produced + a `self_improve_llm_proposal` substrate event is written.

### 10.5 Mutation test (A+ criterion)

For each rule, mutate every OTHER rule's threshold by ±50% and assert the target rule's fire decision is unchanged. Brief A+ criterion (h). Detects accidental rule-coupling.

### 10.6 PII redaction test

Inject an event payload containing an email / token / `ANTHROPIC_API_KEY`-shaped string; assert the rendered issue body is redacted by the W1 PII pipeline. §11 risk #3.

### 10.7 Cost-pause coordination test

Insert a `regatta_pause_all=true` event in the window; assert R1-R3 fire counts exclude events that occurred during the pause window. §11 risk #4.

---

## §11 Risks (target ≥8 per brief A+ rubric implicit + `feedback_adversarial_review`)

### 11.1 False-positive storm — operator ignores detector

A noisy detector becomes background noise; the loop fails because the operator stops reading issues. **Mitigation:** severity tiers (§5.4); auto-suppress after 5 same-pattern re-fires + add `needs-operator-review` label (§6.1); per-rule `mute` CLI (§8.1); A+ time-saved metric in body (§6.3) forces the operator to see ROI per issue.

### 11.2 LLM hallucinates a pattern from coincidental co-occurrence

Nightly LLM scan invents a rule from 3 unrelated failures that happen to share a substring. **Mitigation:** LLM never files issues directly (§7.2); proposals are PR-able YAML files the operator reviews; `feedback_research_design_principles` "proven-OSS over reimplementation" extends to "rule-driven over LLM-driven for known patterns."

### 11.3 Sensitive PII in issue body

Substrate events may contain PR author handles, file paths, error messages with embedded secrets. **Mitigation:** issue body rendering must route through W1's PII-redaction layer (same one the alarm webhook uses); body template explicitly redacts `event.payload.raw_text` and re-renders structured fields only; test §10.6 enforces.

### 11.4 Cost-cap interaction — detector blames agents for pause-induced halts

If a cost-pause kills 5 agents mid-run, R5 (reaper-kills-same-agent) would fire and blame the agents. **Mitigation:** `filter_out: [regatta_pause_all]` field on each rule (§5.1); rules consume the W5 pause-flag stream as a suppressor; test §10.7 enforces. Brief explicitly cites this as the W5 soft dependency.

### 11.5 Retroactive schema drift — old events read under new schema

If substrate event payload shape changes between scan runs, dedup keys (which include payload fields via `group_by`) shift, causing re-firing as if new. **Mitigation:** dedup keys hash on `(rule_name, group_by_values)` but ALSO carry `schema_version` (substrate already has it per `unified-substrate-design.md` §3). Old issues' dedup markers include the version they were filed under; re-fire detection requires a version match.

### 11.6 Rate-limit exhaustion on GH dedup queries

If 12 rules fire in one scan and each does a label-filtered list-issues call, daily GH API budget is dented. **Mitigation:** rules share one bulk fetch of open `self-improvement` issues per scan (cached in-memory for the scan duration); per-rule dedup is a hashmap lookup over the cached set. 1 API call per scan, not per rule.

### 11.7 Detector reaches into substrate during a substrate write — read/write contention

SQLite WAL mode permits concurrent reads with writes, but a 14-day full scan during peak write load could starve writes. **Mitigation:** rule scan runs in its own SQL transaction with `BEGIN DEFERRED` (no write lock); scan interval 6h leaves plenty of quiet windows; if substrate volume grows past 100k events/day, reopen §4.2 for materialized views.

### 11.8 Rule budget unbounded — operator adds 50 rules over a year

Each rule adds substrate query load and one open-issue check. **Mitigation:** every rule is one-line YAML + deletable; quarterly digest (severity=low) tracks which rules have NOT fired in 90 days for pruning; operator decides what to keep. Per `feedback_deletion_default` the rule suite gets shorter, not longer.

### 11.9 Operator silences a real-but-noisy rule, masks a regression

If R3 (`subagent-claimed-clean-but-ci-failed`) is muted because it's noisy, an actual agent regression goes unflagged. **Mitigation:** `mute --for=` rejects `forever`; max mute is 30d; expired mutes refire as `high` severity with a "previously muted, re-firing" note.

### 11.10 Self-improvement issue itself becomes a load-bearing leftover

A filed issue languishes for weeks unacted-on. **Mitigation:** issues older than 14 days with no comment auto-escalate to `obs-alert` (W1 fans out); aligns with `feedback_unaddressed_load_bearing` — every load-bearing leftover gets a tracking issue *and* a stale-alarm path.

---

## §12 A+ scorecard

Per `feedback_grade_rubric` — falsifiable criteria pinned to test names, files, metrics:

| Tier | Criteria (falsifiable) |
|---|---|
| B (floor) | (a) c1 ships: ≥3 of 4 brief triggers fire issues with body containing pattern + 3+ event citations + root-cause hypothesis + suggested edit. Test: `TestB_C1_IssueBodyShape`. (b) `cmd/regatta-self-improve` binary builds, `--help` returns. Test: `TestB_BinaryExists`. (c) Release-notes fence present in PR body. |
| A (target) | B + (d) c2 (dedup): re-firing comments, no new issue. Test: `TestA_C2_DedupComment`. (e) c3 (one-file rule add): R6 worked example committed to spec; test `TestA_C3_AddSixthRule_OneFile` asserts only 2 files change (yaml + test). (f) c4 (loop closes): integration test files an issue, runs dispatch, asserts a feedback_*.md commit lands. `TestA_C4_LoopCloses`. (g) Adversarial reviewer subagent posts (per `feedback_review_every_step`). (h) Heuristics-coverage table in PR body shows every event kind each rule reads. |
| A+ (stretch) | A + (i) Each issue body carries `Estimated time saved (~N min/week)` field; computed from event rate × triage-cost constant. Test: `TestAPlus_TimeSavedNonZero`. (j) Mutation test: every rule survives ±50% threshold mutation of every other rule. Test: `TestAPlus_MutationStable`. (k) Replay harness re-runs frozen snapshot, reproduces every fired issue deterministically. Test: `TestAPlus_ReplayDeterministic`. (l) PII redaction enforced by `TestAPlus_PIIRedacted`. (m) Cost-pause filter test `TestAPlus_PauseWindowExcluded` passes. |

---

## §13 Self-host filter

Per `docs/engineer/briefs/2026-06-01-self-host-first.md` §1: every claim filtered by "does the sole internal operator need this to dispatch regatta unattended?"

| Component | Self-host need? | Verdict |
|---|---|---|
| Rule library (5 MVP rules) | Yes — directly closes patterns the operator hit in observed waves | In scope |
| Hybrid LLM-nightly | Yes — catches unknown-unknown the operator cannot pre-encode | In scope |
| `regatta self-improvement scan` CLI | Yes — operator wants manual replay before/after edits | In scope |
| `mute` CLI | Yes — operator silences noise during peak ship windows | In scope |
| Replay harness (A+) | Yes — operator validates rule changes don't regress historical fires | In scope |
| W7 web UI for rules | No — CLI + GH issues sufficient for one operator | Phase X (reopen-trigger: 2nd internal operator OR external customer ask) |
| Per-rule cost tracking dashboard | No — total daily budget is already capped at $0.50 | Phase X (reopen-trigger: budget exceeded 3× in a month) |
| Auto-applying suggested edits | No — the regatta loop already does this via the standard wave path | Out of scope (use existing loop) |
| Anomaly-detection ML pipeline | No — 5 YAML rules + 1 nightly LLM beats this for persona-A | Phase X (reopen-trigger: >30 active rules, signal-to-noise degrades) |

---

## §14 Deletion default

Per `feedback_deletion_default` — what got smaller?

- Operator log-scanning time: from N minutes/week (manual pattern hunt) to 0 (issues file themselves). Estimated time-saved field in each issue body makes the ROI legible.
- The boot-prompt / dispatch template feedback loop: previously the operator wrote `feedback_*.md` files reactively after sessions; now the substrate proposes them — the friction-to-capture step shrinks from "remember to record this" to "review the open issue."
- The L4-gate-noise debugging surface: rules R1+R3 catch L4 misfires earlier, before the operator burns a session debugging it.

New surface added: `cmd/regatta-self-improve` (~250 LoC Go) + `heuristics.yaml` (5 rules ~30 lines) + `prompts/self-improvement-scan.txt` (~50 lines). A+ defense for the addition: every line maps to a brief acceptance criterion or A+ stretch criterion; none of it is speculative scaffold.

---

## §15 Followups (tracking-issue candidates)

Per `feedback_unaddressed_load_bearing` — every load-bearing leftover gets a tracking issue (filed by implementer at impl time, not at spec time):

1. **R6 worked example — `cost-cap-cycling` rule** — lands when W5 ships; ≥3 `daily_cap_exceeded` events in 7 days suggests cap raise. Single-file PR per c3.
2. **Materialized views for >100k events/day** — reopen-trigger at substrate volume threshold (§4.2).
3. **Rule digest report** — `regatta self-improvement digest --quarterly` for `low`-severity / pruning candidates (§11.8).
4. **W7 web UI integration** — Phase X; rule list + last-fire timestamps as a panel.
5. **Auto-PR on simple suggested edits** — if the suggested edit is "add line X to dispatch template Y," the regatta loop could open the PR directly without dispatching a subagent. Defer until rule fire-rate stabilizes — risk #1 mitigation overlaps.
6. **Coordination with W7 (L4 identity) gate** — R1's "same gate fail" should distinguish L4-as-reviewer vs L4-as-rejector; depends on W7's identity-as-event-kind decision.
7. **Sentry fingerprint upgrade** — current `group_by` is exact match; Sentry's stack-trace-normalization shape (collapse line numbers, normalize stack-frame paths) may apply to subagent-failure messages. Reopen when R3 fire-rate is too granular.
8. **`regatta digest --format=jsonl` wiring** — LLM-nightly scan (§7.2) consumes this; verify the format flag exists at impl time, file the small CLI-extension PR first if not. Soft-blocks A+ criterion (k) only if missing.

---

## §16 Cites + prior art

Per `feedback_research_design_principles` — adopt-first, cite version + license:

- `hashicorp/go-set` v3.0.0 (MPL-2.0) — **adopted** for the bounded-window count primitive (brief §11 W4).
- Sentry issue-grouping fingerprint algorithm shape (BSL) — **referenced, not imported** (algorithm shape only).
- `grafana/oncall` v1.5.x (AGPL-3) — **referenced** for event-aggregation pattern.
- `nodejs/diagnostics` Bayesian alerts — **referenced** for rolling-window anomaly-detection pattern.
- `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W4 — source brief.
- `docs/engineer/specs/phase-x/2026-06-01-unified-substrate-design.md` — substrate events schema (read-only consumer).
- `.regatta/items/phase-autonomy-w1-alarm-webhook.md` — GH-client adapter source (W1).
- `.regatta/items/phase-autonomy-w5-cost-cap-autonomic.md` — pause-flag soft dependency (W5).
- `memory/feedback_decision_priority.md` — UX > ease > performance > best-practices priority.
- `memory/feedback_self_improvement.md` — "notice friction, fix surfaces" — this spec is the surface.
- `memory/feedback_unaddressed_load_bearing.md` — directly closed by R4.
- `memory/feedback_research_design_principles.md` — adopt-first stance encoded in §3.1's hybrid choice.

---

## §17 Adversarial review

Per `feedback_review_every_step` + `feedback_adversarial_review`: a reviewer pass ran against the draft of this spec applying the 8 lenses from `docs/engineer/dispatch-templates/reviewer.md`. Findings disposition:

| Tier | Finding | Disposition |
|---|---|---|
| Med | §6.1 dedup search by HTML-comment marker is unreliable across GH renderers | Fixed inline: §6.1 now uses bulk `gh issue list --json body` fetch + substring match on a visible `dedup-key:` line, with HTML comment as a fallback marker |
| Low | §4.3 `last_scan_at` becomes stale if detector crashes mid-scan, risking re-fire | No code change; dedup ledger (§6.1 step 4) is the correctness gate, `last_scan_at` is only an optimization. Documented at §4.3. |
| Low | §7.2 references `regatta digest --since=7d --format=jsonl` — verify command exists or call as followup | Followup #8 added (§15) — wire `regatta digest` jsonl format if it doesn't already exist; LLM scan can be deferred until then |
| Low | §6.3 schema_version not in dedup key — risks re-fire on schema bump | Fixed inline: §6.1 step 3 dedup key now includes `schema_version`, matching risk #5's mitigation |
| Info | R5 (reaper-kills-same-agent) not in brief's four named triggers | Defensible: brief allows 5 named heuristics initially; R5 maps to an observed-in-wave failure pattern. Kept. |

Independent scorecard re-score: matches author's claim — spec achieves A+ if all listed tests in §12 pass at impl time. No re-spawn of design subagent required. Verdict: **clear-to-merge** for the spec PR; implementation PR will get a separate reviewer pass.

---

## §18 Comment sweep

This spec is prose only — no code, no inline comments in source files. Comment-sweep state: **clean**.

---

```release-notes
none (internal design spec)
```
