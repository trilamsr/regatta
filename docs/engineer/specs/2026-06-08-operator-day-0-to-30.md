---
status: active
phase: x-forward-fit
summary: "Phase DEPLOY → Phase GREEN-CLOCK transition wedge for the sole internal operator. What to watch, when to escalate, how to know day-30 fired. Maps day-0 (post `regatta install-service`) → day-7 (early-signal calibration) → day-30 (steady-state automerge). Concrete OTel queries against the existing meter surface — `regatta.green_clock.day_count`, `regatta.trigger.days_remaining`, `regatta.cost.usd`, `regatta.l4.cache.{hits,misses}`, `regatta.adversarial.findings`, `regatta.pr.failure`, `regatta.review.posts.attempted`. NOT a customer onboarding doc — that audience is served by `docs/operator/day{1,7,30}.md`. This spec is the single-internal-operator gate sheet for the unattended dispatch journey."
---

# Operator day-0-to-30 — DEPLOY → GREEN-CLOCK transition

_Author: design session, 2026-06-08. Brief: `docs/engineer/briefs/2026-06-01-self-host-first.md` §7 (trigger to exit self-host phase). Boot prompt §PHASE DEPLOY + §PHASE GREEN-CLOCK in `docs/engineer/autonomous-session-prompt.md`. Memory rule cites: `feedback_default_simpler`, `feedback_decision_priority`, `feedback_single_user_priority`, `feedback_recognize_session_end`, `feedback_no_signatures`._

## 0. TL;DR

Phase DEPLOY is READY but the gate between "operator ran `regatta install-service`" and "Phase X unlocks on a 30-day-green trigger" has no operator-facing playbook. The runbook at `docs/operator/native-deploy.md` ends at Step 14 (`24h unattended green criteria`). This spec extends that to day-30: per-day-window watch list, common failure ladder, escalation rules, and the concrete OTel query the operator runs to know day-30 fired. Counter-resets are explicit — a manual merge or any `operator_intervention` event in a day's window resets `regatta.green_clock.day_count` to zero, by design (`internal/triggers/greenclock.go:6-7` — "Reset is intentional and explicit so CI flake cannot silently roll back 25+ days of progress"). The operator's job in this window is to (a) keep the counter advancing, (b) diagnose and fix anything that resets it, (c) wait. No new code; this is a runbook spec.

The existing customer-facing `day1.md` / `day7.md` / `day30.md` under `docs/operator/` target a different reader — an external persona-A onboarding with `regatta init` and `regatta pilot`. They are NOT the internal-operator GREEN-CLOCK playbook. This spec writes the internal version; the two doc-trees stay separate per the self-host filter (one operator, one repo, one binary).

## 1. Scope

In scope:

- Day-0 action checklist post `regatta install-service` (Phase DEPLOY exit → Phase GREEN-CLOCK day-0).
- Day-1 to day-7 early-signal calibration: green-merge counter reset rules; common failures + diagnostic ladder.
- Day-8 to day-30 steady-state metrics: automerge cadence target, cost calibration, L4 cache hit-rate target, reviewer-finding rate target.
- Failure escalation: what trips a re-spec / re-impl / wedge re-prioritization.
- Day-30 transition signal: concrete OTel query the operator runs to confirm GREEN-CLOCK window closed; what unlocks next.

Out of scope (Phase X — reopen-trigger per CLAUDE.md self-host filter):

- Multi-tenant operator personas. Day-0-to-30 here is single-operator. Per-tenant green-clock windows are W8 territory.
- Team-of-operators. Day-0-to-30 here assumes one human. Shared-state ownership belongs to W11 blackboard.
- External customer onboarding. Lives in `docs/operator/day{1,7,30}.md`; that doc-tree is read by persona-A/B/D installers using `regatta init`, not by the internal operator running `install-service` on the regatta repo itself.
- Multi-target-repo. Per `docs/engineer/specs/2026-06-07-multi-target-repo.md` Option A graduation criteria; not a day-30-window concern.

## 2. Why this spec exists

Three operator-facing surfaces touch this window but none cover end-to-end:

1. `docs/operator/native-deploy.md` Step 14 — 24h stop criteria. Stops at 24h, mentions 7d + 30d adds but does not enumerate.
2. `docs/engineer/autonomous-session-prompt.md` §PHASE GREEN-CLOCK — names the trigger (`≥10 PRs/day green-merge ≥30 consecutive days unattended`) but defines no day-0 action sequence and no escalation ladder.
3. `internal/triggers/greenclock.go` — implementation source-of-truth for the counter rules. Operator-readable indirectly via `regatta status` (Green-clock panel) + the `regatta.green_clock.day_count` OTel gauge, but the rules themselves are buried in Go comments.

Operator picks up `install-service` then has no canonical "what do I do for 30 days" answer. Result on first install: drift between what `regatta status` shows and what the operator believes the counter is doing; manual merges fire as a reflex and silently reset the counter; spend-cap calibration happens by surprise on day-5 when caps trip mid-week.

## 3. Day-0 — install exit → GREEN-CLOCK day-0 (one-shot checklist)

Day-0 starts the moment `regatta install-service` returns and `/healthz` is `ok`. The `native-deploy.md` runbook Step 14 already captures the 24h stop criteria; the additions below are GREEN-CLOCK-specific.

**Pre-conditions (carried from `native-deploy.md` Step 1-13)**:

- [ ] `.env` populated + `chmod 600`.
- [ ] `regatta.yaml` has `safety.spend_cap_usd_per_day` set (self-host recommended default `20`).
- [ ] `regatta validate-config && echo ok` → ok.
- [ ] Anthropic console monthly cap configured (the only hard ceiling).
- [ ] `regatta verify-repo-config` → OK on every required check.
- [ ] `make ci-check` exit 0 on the deploy SHA.
- [ ] `regatta install-service` returned 0 and `/healthz` is `ok`.

**Day-0 GREEN-CLOCK additions (run once, in order)**:

1. **Run `regatta doctor`** (#917 shipped). Exit 0 = all 7 preflight checks PASS (secrets, binaries, gh auth, git state, regatta.yaml, branch protection, supervisor unit). Any FAIL → fix before continuing; the green-clock will not advance through a doctor-FAIL host.

   ```sh
   regatta doctor --json | jq '.summary'
   # expect: {"pass": 7, "fail": 0, "skip": 0}
   ```

2. **Snapshot the starting day-count**. The counter is zero on a fresh install; this is the baseline.

   ```sh
   regatta status --once
   # Green-clock panel: "0 / 30 days, last reset: <never>"
   ```

   The gauge `regatta.green_clock.day_count` reads `0.0`; `regatta.trigger.days_remaining{trigger="green_clock"}` reads `30.0` (per `internal/triggers/gauge.go:55-66`).

3. **Drop the smoke item** (`docs/operator/native-deploy.md` Step 11 — `.regatta/items/SMOKE-1.md` OR a `[autonomous]`-labeled GH issue per #864). End-to-end SLO target: <10 min from drop to merge for a trivial refactor.

4. **Watch the first PR walk end-to-end** (`docs/operator/native-deploy.md` Step 12 — five transitions `brief.signed → spawn → L4 verdict → automerge gate → human-merge`). On day-0 the operator clicks the merge button manually; this single human-merge counts as an `operator_intervention` event under the green-clock rules — see §4 — and that is fine on day-0 because the counter has not started yet.

5. **Confirm alarm-webhook trips** (`docs/operator/native-deploy.md` Step 13 — curl `127.0.0.1:9101` → GH issue files in 30s). The alarm-webhook is load-bearing for Phase GREEN-CLOCK because a cost-cap trip on day-12 is the canonical scenario that resets the counter and needs operator visibility.

6. **File first-day baseline issue**. Title `Day-0 baseline — GREEN-CLOCK window opens YYYY-MM-DD`. Body: snapshot of `regatta status --once`, cost-today panel value, today's spend-cap setting, the operator's wall-clock TZ. This is the receipt the operator queries against on day-30.

**Exit criterion for day-0**: smoke item merged + first real `[autonomous]`-labeled work-item picked up + alarm-webhook fires once + baseline issue filed. The 30-day window opens at the start of the operator's next local day (per `internal/triggers/greenclock.go:75-113` — `Compute` walks backward from the day BEFORE today; today is always "in-flight pending").

## 4. Day-1 to day-7 — early-signal calibration

### 4.1 Counter rules (operator-facing, paraphrased from `internal/triggers/greenclock.go:75-130`)

A day is **green** when both hold:

- `agent_pr_merged` event count in the day ≥ `ThresholdPRsPerDay` (default 10; spec §5 of original counter).
- Zero `manual_merge` events AND zero `operator_intervention` events in the day.

A day is **non-green** otherwise. The counter is the trailing consecutive-green-day streak counted from yesterday backward; today is always "pending" (does not flip until day rolls over in the operator's configured TZ).

**Reset semantics (load-bearing)**:

- A non-green day breaks the streak and the counter resets to zero on the next compute.
- Reset is intentional and explicit — `internal/triggers/greenclock.go:6-7`. No partial credit, no rolling exponential decay, no "skip one bad day".
- The first non-green day's date is captured as `LastReset` and surfaces on the `regatta status` green-clock panel + the `regatta.trigger.days_remaining` gauge tooltip.

### 4.2 What counts as an `operator_intervention` (per `internal/triggers/greenclock.go:18-22`)

The two reset-triggering event kinds the substrate writes:

- `manual_merge` — operator clicked the GitHub merge button (not L4-as-review bot per W7). Includes "I rebased the branch myself" flows.
- `operator_intervention` — operator ran `regatta resume`, `regatta cost backfill`, manually closed a stuck PR, manually re-labeled a `[autonomous]` issue, or any other write the supervisor categorizes as human-in-the-loop.

The two event-name constants are the single source of truth — if the substrate emits a third reset-class event in the future, it lands in `internal/triggers/greenclock.go` first and propagates here.

**Operator-merge corner cases**:

- Clicking "merge" on a PR the L4-as-review bot already approved still counts as `manual_merge` if the operator's account performs the click. The bot identity merging via `--auto` is the green path.
- A `git push` to `main` that bypasses GitHub PR flow registers as `operator_intervention` because the substrate has no `agent_pr_merged` event to credit.
- A PR closed without merging is neither a merge nor a reset event by itself — but if the loop opens 12 such close-without-merges in a day, the day will fail the `≥10 agent_pr_merged` threshold and reset anyway.

### 4.3 Diagnostic ladder — common day-1-to-day-7 failures

| Symptom | First check | Root cause class |
|---|---|---|
| `regatta status` green-clock panel reads `MISSING` | `regatta.green_clock.day_count` gauge not exposed; tail `journalctl -u regatta` for triggers-package startup | Supervisor wedge — restart the unit. |
| Day-count stuck at 0 for ≥3 days, no resets visible | Threshold (10 PRs/day) not met. Query `regatta.cost.usd` last 24h — is the loop spawning at all? | Adapter not picking up items, or spend-cap throttling — see §4.4. |
| Day-count jumped from N → 0 unexpectedly | `LastReset` panel field shows the date; cross-reference substrate events for `manual_merge` / `operator_intervention` on that day | Manual intervention, possibly by a tool not the operator (CI bot, dependabot merge). |
| L4 cache hit-rate < 20% — `regatta.l4.cache.hits / (hits + misses)` | L4 invocations spike on every PR head change | Reviewer prompt drifting per-PR; #852 cache-control work outstanding. |
| Cost-cap trips repeatedly | `regatta.cost.usd` over `safety.spend_cap_usd_per_day` for ≥2 days/wk | Cap calibration too low — raise after Day-7 once steady-state spend known. |
| `regatta.alarm_webhook.alerts.total` increments with no GH issue filed | `alarm_webhook.gh_repo` misconfigured or token scope missing | Step 13 wiring drift; re-run `regatta doctor` (#917). |
| `regatta.pr.failure` counter spikes | One failure-taxonomy bucket dominates | Subagent prompt drift, dispatch template lag, or model regression. |

### 4.4 Day-7 checkpoint

By end of day-7 the operator should have:

- [ ] At least one green day on the counter (day-count ≥ 1) OR a documented reset cause from §4.3.
- [ ] Spend-cap calibrated to actual daily throughput. Steady-state expectation: `regatta.cost.usd` daily aggregate under cap with ≥30% headroom. If headroom <30%, raise the cap by 25% in `regatta.yaml`, `regatta validate-config`, restart.
- [ ] L4 cache hit-rate ≥ 40% (`regatta.l4.cache.hits` / `regatta.l4.cache.hits + regatta.l4.cache.misses` over rolling 7d). Below 40% → file followup against #852 cache-control work.
- [ ] Adversarial-reviewer finding rate baseline captured: `regatta.adversarial.findings` total / merged-PR count over the week. No target on day-7 — capture for day-30 trend comparison.

## 5. Day-8 to day-30 — steady-state metrics

The operator is mostly watching, occasionally fixing. Day-8 onward the loop should be self-driving; operator intervention is failure, not feature.

### 5.1 Per-day targets (steady-state)

| Metric | OTel name | Target | Reset behavior |
|---|---|---|---|
| Green-merge cadence | `regatta.green_clock.day_count` (gauge) | Advancing by 1 per local-TZ day | Resets to 0 on any non-green day |
| PR-merge throughput | derived: `agent_pr_merged` event count per day from substrate | ≥10 per day | Day fails threshold below 10 |
| Cost vs cap | `regatta.cost.usd` (Float64Counter) daily delta | Under `safety.spend_cap_usd_per_day` with ≥30% headroom | Cap trip emits `regatta_cost_cap_throttled_total` |
| L4 cache hit-rate | `regatta.l4.cache.hits` / `(hits + misses)` | ≥ 60% after day-14 (warming) | n/a |
| Reviewer-finding rate | `regatta.adversarial.findings` / `agent_pr_merged` | ≤ 1.0 mean over rolling 7d | Trend up = prompt drift |
| PR-failure taxonomy | `regatta.pr.failure` by bucket | No single bucket > 30% share | Trend a bucket up = systemic regression |
| Substrate health | `regatta.substrate.chain.break` | Zero in window | Non-zero = chain-detector trip; runbook in OBS Wave-B |

### 5.2 Concrete OTel queries — copy-paste recipes

The operator does not need a Prom stack on day-8; the gauges are exposed at `http://127.0.0.1:8080/metrics` (the supervisor `--ui-addr` listener). Direct curl + `awk` is sufficient for a single-operator window:

```sh
# Current green-clock day-count.
curl -fsS http://127.0.0.1:8080/metrics | awk -F'[ {}]' '
  /^regatta_green_clock_day_count[ {]/ { print "day_count:", $NF }
  /^regatta_trigger_days_remaining{trigger="green_clock"} / { print "days_remaining:", $NF }
'

# Today's spend vs cap.
curl -fsS http://127.0.0.1:8080/metrics | awk '
  /^regatta_cost_cap_24h_spend_usd/ { print "spend_24h:", $2 }
  /^regatta_cost_cap_state/         { print "cap_state:", $2 }   # 0=ok, 1=throttled
'

# L4 cache hit-rate (point-in-time).
curl -fsS http://127.0.0.1:8080/metrics | awk '
  /^regatta_l4_cache_hits_total /   { h=$2 }
  /^regatta_l4_cache_misses_total / { m=$2 }
  END { if (h+m > 0) printf "l4_hit_rate: %.2f\n", h/(h+m) }
'

# Reviewer-finding rate (denominator from substrate; numerator from meter).
# substrate path:
sqlite3 .regatta/state.db \
  "SELECT COUNT(*) FROM substrate_events WHERE kind='agent_pr_merged' AND at > strftime('%s','now','-7 days')"
curl -fsS http://127.0.0.1:8080/metrics | awk '
  /^regatta_adversarial_findings_total / { sum += $2 }
  END { print "findings_7d_total:", sum }
'
```

Prom-name shape: Prometheus exposition mangles `.` → `_` and `_total` is the Prom-side suffix per its naming convention (per `internal/obs/adversarial/counter.go:53-55` comment). Operator queries against `/metrics` use the underscored form; OTel-internal references in this doc use the original dotted form.

### 5.3 Cost-cap calibration loop (day-8 onward)

Day-7 set the cap with 30% headroom. Day-8 onward calibration is hands-off unless one of:

- `regatta_cost_cap_throttled_total` increments — loop hit the cap; investigate today's spawn count + L4 invocation count.
- `regatta.cost.usd` 7d-trailing average breaches 70% of cap two weeks running — raise cap by 25%.
- 7d-trailing average drops below 30% of cap two weeks running — lower cap by 25% (defensive; reduces blast radius of a runaway loop).

The cap is a soft throttle — the Anthropic console monthly cap is the hard ceiling. Raise the soft cap freely within the hard cap's headroom; tightening below 30% of throughput risks spurious resets if the loop is in a high-spawn day.

### 5.4 Reviewer-finding rate trend

`regatta.adversarial.findings` is bucketed by `severity × scope × pattern` (per `internal/obs/adversarial/counter.go:53-58`). The operator-relevant view is the per-PR rate, not absolute count:

- Day-8 baseline (from day-7 §4.4): record the value.
- Day-14: compare. Trending up → reviewer prompt drift OR codebase entropy growing. Either way, file a followup.
- Day-21: re-baseline if a dispatch template changed in the interim.
- Day-30: snapshot. Day-30 number is part of the green-clock-window receipt — see §7.

## 6. Failure escalation

The operator does not pause every reset; one reset is normal noise on day-3 and trivial on day-21. Escalation rules are about pattern, not single events.

### 6.1 Tier 1 — diagnose and continue

Single reset in the window. Counter restarts at 0. Operator:

1. Tag the reset cause from §4.3 ladder.
2. File no-op follow-up issue iff the cause is novel (not already in `[followup]` backlog).
3. Continue watching. The reset is the system working as designed.

### 6.2 Tier 2 — file tracker, lower spend cap defensively

Two resets in the window OR one reset paired with a tier-1 metric breach (cache hit-rate, finding-rate, taxonomy bucket > 30%):

1. File `[FOLLOWUP]` issue with title `Day-N reset — <root cause>` and `regatta.adversarial.findings` snapshot.
2. Lower `safety.spend_cap_usd_per_day` by 25% as a circuit breaker until the trend stabilizes.
3. Wait one full local-TZ day before resuming normal calibration.

### 6.3 Tier 3 — re-spec / re-impl trigger

Any one of:

- ≥3 resets in a rolling 7-day window.
- Counter has reset to 0 ≥4 times since day-0 without ever crossing day-15.
- A single reset cause has fired ≥2 times across separate resets.
- `regatta.substrate.chain.break` non-zero — substrate corruption stops the loop's audit chain and is a hard halt regardless of green-clock state.

Operator stops the loop (`launchctl bootout` / `systemctl stop`), files a `[wedge]` design issue, and re-specs the failing surface. This is the failure mode that pulls Phase-X work back into focus per `docs/engineer/briefs/2026-06-01-self-host-first.md` §7.

### 6.4 Tier 4 — wedge re-prioritization

If §6.3 trips and the cause is a deferred Phase-X seam (e.g. operator is forced to manual-merge because L4-as-review identity #589 has a regression; or substrate cutover S3-T2 is incomplete), re-prioritization is in scope. Reopen the Phase-X spec, file a back-port issue, and dispatch.

### 6.5 What does NOT trigger escalation

To prevent operator hyperactivity (anti-pattern per `feedback_recognize_session_end`):

- A reset on day-1 or day-2 is not noise to investigate; the loop is still warming.
- A cache hit-rate dip during a 48h reviewer-prompt-template change is expected, not a trigger.
- A single `regatta.alarm_webhook.alerts.total` increment that the operator handled is closed; do not raise tier on already-handled alarms.
- An L4 verdict of `block` on a PR is the gate working — file a tracker only if blocks have a pattern (e.g. same path stripped repeatedly).

## 7. Day-30 transition signal — how the operator KNOWS

Day-30 fires when `regatta.green_clock.day_count == 30` for at least one scrape AND no `manual_merge` or `operator_intervention` events have fired in the trailing 30 days. The operator does not eyeball this — they run one query:

```sh
# Day-30 transition probe — exit 0 means trigger fired.
day_count=$(curl -fsS http://127.0.0.1:8080/metrics | awk '
  /^regatta_green_clock_day_count / { print $NF; exit }
')
if [ "${day_count%.*}" -ge 30 ]; then
  echo "GREEN-CLOCK trigger fired — day_count=${day_count}"
  exit 0
fi
echo "not yet — day_count=${day_count}"
exit 1
```

For belt-and-suspenders, cross-check the substrate directly:

```sh
# Trailing 30-day audit: zero resets allowed.
sqlite3 .regatta/state.db <<'SQL'
SELECT
  COUNT(*) FILTER (WHERE kind='manual_merge')          AS manual_merges,
  COUNT(*) FILTER (WHERE kind='operator_intervention') AS interventions,
  COUNT(*) FILTER (WHERE kind='agent_pr_merged')       AS green_merges
FROM substrate_events
WHERE at > strftime('%s','now','-30 days');
SQL
# Expected: manual_merges = 0, interventions = 0, green_merges ≥ 300 (10/day × 30).
```

**What firing unlocks** (per `docs/engineer/briefs/2026-06-01-self-host-first.md` §7 and §4):

- Phase-X external-buyer wedges become eligible for impl dispatch: W7 htmx UI Waves 2/3 elaboration (already MVR-2 specs), W8 multi-tenant routing, W10 Sigstore, W11 blackboard, W12 billing, P3.8 swap-out adapters (LLM-gateway, signer, billing, OPA, OTel exporter), W9 Temporal-backed `DurableHistory`.
- `docs/engineer/autonomous-session-prompt.md` PRIORITY block re-orders. The operator runs `make gen-boot-status` to refresh the auto-priority block; the next session boot reads the new order.
- The boot prompt's "DO NOT dispatch implementers" rule on Phase X §75 lifts.

**What does NOT unlock**:

- Multi-tenant scope. That trigger is "first external paying customer signs a paid pilot" — distinct from 30-day-green. Phase X has two reopen-triggers; this spec only handles the second.
- A free pass on resets. Day-31 reset still drops the operator back to day-0; the 30-day-green is a one-shot trigger that fires once and immediately the counter resumes its job. The operator may now choose to ship Phase-X work, but the loop's audit posture is unchanged.

## 8. Out-of-band followups (file as separate issues if approved)

- F1: `regatta status` add a one-line "next milestone" hint when `day_count ≥ 25` so operator gets a 5-day heads-up. (default-simpler: skip unless operator asks twice; pure UX.)
- F2: `regatta.green_clock.transition_fired` Int64Counter that increments once when the trigger fires. Today the dashboard reads only the gauge; a counter would let alarm-webhook fire on the transition. Defer to MVR-1 era.
- F3: Substrate query in §5.2 / §7 — move to a `regatta status --window 30d` flag once Phase X opens. Today the curl-+-sqlite recipes are fine for a single operator.
- F4: Cross-link to `docs/operator/native-deploy.md` Step 14 — add a one-line pointer from Step 14 to this spec. (filed as F5 below — `native-deploy.md` is operator-doc-tree, this spec is engineer-doc-tree; one-way link respects the boundary.)
- F5: `docs/operator/native-deploy.md` Step 14 amendment — add a tail-paragraph linking to this spec for the day-7/day-30 watch criteria. Spec keeps the load-bearing rules; operator doc points to it. Two-PR sequence if operator wants ceremony, but a single one-liner edit suffices.

## 9. Acceptance

- [ ] `docs/engineer/specs/2026-06-08-operator-day-0-to-30.md` ships, frontmatter `status: active`, `make pre-push-check` green.
- [ ] `docs/engineer/specs/README.md` re-generated; row for this spec lands.
- [ ] OTel metric names referenced match the actual instruments registered in tree (`regatta.green_clock.day_count`, `regatta.trigger.days_remaining`, `regatta.cost.usd`, `regatta_cost_cap_*`, `regatta.l4.cache.{hits,misses}`, `regatta.adversarial.findings`, `regatta.pr.failure`, `regatta.alarm_webhook.alerts.total`, `regatta.substrate.chain.break`).
- [ ] No invented metric names. F2 above is the one new instrument proposal and is filed as a followup, not assumed shipped.
- [ ] Self-host filter respected — no tenant fanout, no multi-operator scope, no team-of-operators.

```release-notes
docs: spec operator day-0-to-30 wedge (Phase DEPLOY → GREEN-CLOCK transition)
```
