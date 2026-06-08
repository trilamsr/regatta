---
title: "Chat notifier integration — Slack/Discord/Mattermost webhook push for high-severity events"
status: active
summary: "Push-notify the operator on high-severity events (SLO breach, green-clock-reset, cost-cap-hit, automerge-stuck) via a pluggable Notifier interface backed by Slack/Discord/Mattermost incoming webhooks. Per-severity routing (Critical=immediate, Med=hourly batch, Low=daily digest). Webhook URL routed through the existing `secrets:` block (#911). Three implementer slices: (1) Notifier interface + Slack adapter, (2) severity routing + batching, (3) digest schedule."
---

# Chat notifier integration — Design Spec

Status: ready for review
Date: 2026-06-08
Author: design subagent
Source: operator request — alarm-webhook today only files GitHub issues; operator must poll `regatta status` / `/healthz` / `/metrics` to learn of high-severity events.
Depends on: `internal/alarmwebhook/handler.go` (event surface), `internal/secrets/secrets.go` (`#911` secrets routing), `internal/orchestrator/scheduler` (digest tick), `cmd/regatta/serve.go` (composition root).

Memory rules in force: `feedback_decision_priority`, `feedback_default_simpler`, `feedback_root_cause`, `feedback_deletion_default`, `feedback_research_design_principles`, `feedback_adversarial_review`, `feedback_spec_pattern_authority`, `feedback_unaddressed_load_bearing`, `feedback_no_signatures`, `feedback_dispatch_brief_only`, `feedback_grade_rubric`.

---

## §1 Problem

The operator is the sole human in the loop. Today, the alarm-webhook receiver at `internal/alarmwebhook/handler.go:312` resolves every AlertManager firing to either `CreateIssue` or `CommentOnIssue` via `ghclient.Client`. That is the only sink. To learn that anything fired, the operator must:

- open GitHub on the repo and look at the issue tracker, or
- curl `/healthz` or `/metrics` and read `regatta.alarm_webhook.alerts.total`, or
- run `regatta status`.

All three are pull-mode. The operator has no push-mode channel. Consequences observed in the autonomous loop:

- SLO breach fires at 02:00 local. Operator sees it at 09:00 — 7-hour latency on a Critical event.
- Green-clock-day-reset (#867 self-improve cadence) fires at midnight UTC. Operator never sees the event because no issue is filed (it is a counter reset, not an alert).
- Cost-cap-hit (W5 global daily cap) trips and the orchestrator pauses dispatch. Operator notices only when they observe an idle dispatch queue.
- Automerge-stuck-N-hours (PR approved + CI green + `mergeStateStatus=BLOCKED` for >2h) silently strands PRs.

The shared cause: regatta has rich event taxonomy and rich telemetry but no operator-facing push channel. The two existing push channels — GitHub issue creation and `slog` to stderr — are both pull-mode in practice (operator must look).

## §2 Goal

ONE pluggable `Notifier` interface lives in `internal/notifier/`. Adapters wrap a chat webhook (Slack-incoming, Discord-incoming, Mattermost-incoming — all the same JSON-over-HTTP shape). The alarm-webhook handler and a handful of other event sites call `Notifier.Notify(ctx, Event)`; the notifier routes by severity (Critical = immediate POST, Med = hourly batch, Low = daily digest). The webhook URL is one entry in the existing `secrets:` block (#911).

Acceptance benchmark: SLO breach landing in the alarm-webhook receiver produces a chat message in the operator's channel within 30s (P95) of the AlertManager POST.

Self-host operator pain solved: SLO breach at 02:00 wakes the operator's phone via Slack push, not a polled tab at 09:00.

## §3 Scope (in)

3.1 New package `internal/notifier/` with one interface and one default implementation:

```go
package notifier

type Severity int
const (
    SeverityLow Severity = iota + 1
    SeverityMed
    SeverityCritical
)

type Event struct {
    Kind      string            // "alarm-fired" | "green-clock-day-reset" | "cost-cap-hit" | "automerge-stuck"
    Severity  Severity
    Title     string            // 1-line summary, ≤120 chars
    Body      string            // detail block, ≤2 KiB, plain text
    Labels    map[string]string // small set: alertname, repo, severity-token, etc.
    OccurredAt time.Time
}

type Notifier interface {
    Notify(ctx context.Context, e Event) error
}
```

3.2 Three adapters (file-disjoint, same JSON shape — Slack/Discord/Mattermost all accept `{ "text": "..." }` POST):
- `internal/notifier/slack/adapter.go` — `https://hooks.slack.com/services/...`
- `internal/notifier/discord/adapter.go` — `https://discord.com/api/webhooks/...`
- `internal/notifier/mattermost/adapter.go` — `https://mattermost.example.com/hooks/...`

Slice 1 ships Slack only; Discord + Mattermost land in followup PRs by literal copy + URL constant swap. Three similar adapters beat a premature `WebhookFlavour` enum (`feedback_default_simpler`).

3.3 Severity-routing `router` (decorator over Notifier):
- Critical → immediate POST (no batching).
- Med → buffer per-Kind for `notifiers.batch_interval` (default 1h); flush as one digest message; drop the buffer on flush.
- Low → buffer per-Kind for `notifiers.digest_interval` (default 24h); flush as one daily digest.

Buffer is in-process map keyed on `(Kind, severity)`. Bounded: 256 events per bucket; oldest evicted with a `[truncated N events]` footer added at flush. No persistence — process restart loses buffered Low/Med (acceptable; SLO is on Critical).

3.4 Event taxonomy — events that emit `Notify`:

| Kind | Severity | Source | Wire point |
|---|---|---|---|
| `alarm-fired` | label-derived (`labels.severity` → Critical/Med/Low) | AlertManager | `internal/alarmwebhook/handler.go:route` after `bump(..., "issue_created")` |
| `alarm-fired` | same | AlertManager | same `route` after `bump(..., "comment_added")` (only when severity=Critical; avoid storm) |
| `green-clock-day-reset` | Low | self-improve cadence | `internal/selfimprove/cadence.go::onDayReset` (new hook) |
| `cost-cap-hit` | Critical | cost governor | `internal/gates/cost/governor.go::tripGlobalCap` |
| `automerge-stuck` | Med | PR watcher | `internal/orchestrator/prwatch/stuck.go` (new file in followup) — fires when `mergeStateStatus=BLOCKED` AND `approvalsCount≥1` AND age>2h |

3.5 Config block in `regatta.yaml` (CUE-modeled):

```yaml
notifiers:
  enabled: true
  slack:
    webhook_url:
      source: env             # routed through #911 secrets:
      name: REGATTA_SLACK_WEBHOOK_URL
    channel: "#regatta-ops"   # optional override; default = webhook's bound channel
  batch_interval: 1h
  digest_interval: 24h
  min_severity: low           # gate: low|med|critical; default = low
```

3.6 CUE delta at `contracts/schemas/regatta.v1.cue`:

```cue
#Notifiers: {
    enabled?: *true | bool
    slack?:    #SlackNotifier
    discord?:  #DiscordNotifier        // slice-2 followup
    mattermost?: #MattermostNotifier   // slice-2 followup
    batch_interval?:  *"1h" | string   // time.ParseDuration shape
    digest_interval?: *"24h" | string
    min_severity?:    *"low" | "low" | "med" | "critical"
}

#SlackNotifier: {
    webhook_url: #Secret               // reuses #Secret from #911
    channel?:    string
}
#DiscordNotifier: webhook_url: #Secret
#MattermostNotifier: webhook_url: #Secret
```

`#Config.notifiers?: #Notifiers` added alongside `secrets?:`.

3.7 Wiring at `cmd/regatta/serve.go`:
- Parse `config.Notifiers`. If `enabled=false` OR block omitted → install `notifier.NoOp{}` everywhere (zero-config back-compat).
- If enabled → resolve `webhook_url` via existing `secrets.Fetcher.Get(ctx, "regatta.slack_webhook_url")` (new canonical key registered in `internal/secrets/keys.go`).
- Construct `notifier.Router{Inner: slack.New(url), Batch: ...}` and pass to alarm-webhook Handler via new `Handler.Notifier` field (nil-safe default = NoOp).

3.8 Tests:
- `internal/notifier/router_test.go` — Critical bypasses buffer (failing test first per `feedback_tdd_discipline`).
- `internal/notifier/router_test.go::TestMedBatchesAtInterval` — Med events flush at `batch_interval` boundary.
- `internal/notifier/router_test.go::TestLowBufferEvictsOldest` — 257th event evicts oldest with footer.
- `internal/notifier/slack/adapter_test.go` — adapter POSTs canonical JSON shape; 429 surfaces as error; 5xx retries once with backoff; 200 = success.
- `internal/alarmwebhook/handler_test.go::TestRouteCallsNotifierOnCreate` — wire-up test.
- Integration: `internal/notifier/router_integration_test.go` uses `httptest.Server` standing in for Slack; assert end-to-end JSON shape.

## §4 Scope (out)

4.1 **Two-way ack** (operator clicks "silence" in Slack → regatta mutes alertname for N hours). Out — Slack interactive endpoints need a public HTTPS callback URL, signed request verification, and a webhook receiver. Phase-X with trigger: ≥1 weekly false-positive alarm-fired event sustained 4 weeks.

4.2 **Per-channel routing** (Critical→#regatta-prod, Med→#regatta-ops). Out for slice-1; single `channel` field today. Reopen-trigger: operator requests segmentation OR ≥3 distinct channels surfaced.

4.3 **Rich message formatting** (Slack blocks, Discord embeds, Markdown tables). Out — `{ "text": "..." }` plain text is the lowest-common-denominator shape all three sinks accept. Followup spec when an operator names the missing UX.

4.4 **Email/SMS/PagerDuty**. Out — chat is the operator's working surface; adding paging-grade sinks needs an on-call rotation. Phase-X.

4.5 **Notifier-side dedup**. Out — alarm-webhook already dedups upstream via `dedupCache` (`internal/alarmwebhook/dedup.go`). Cost-cap and green-clock fire at most once per day; automerge-stuck is intrinsically rate-limited by PR cadence. If a separate event source ever spams Notifier, file a followup at that point (`feedback_default_simpler`).

4.6 **Outbound HTTP through a configurable proxy**. Out — `http.DefaultTransport` honours `HTTPS_PROXY`; Notifier inherits.

4.7 **Cryptographically-signed webhook payloads** (provenance attestation on outbound chat messages). Out (Phase-X — external customer trust boundary, single-operator self-host does not need provenance on outbound chatter).

4.8 **Webhook URL rotation UX** beyond editing `regatta.yaml` + restart. Out. Hot-reload deferred to W8 work.

## §5 Schema delta (CUE)

See §3.6. One additive block; no existing field changes. CUE `*"low"` default keeps `notifiers:` valid when written empty.

## §6 Acceptance criteria

6.1 **SLO**: Critical event lands in operator chat within 30s P95 of source event timestamp. Measured by integration test using `httptest.Server` + injected clock. Negative test: backend 503 surfaces as error; operator sees no message but `slog.Error` is logged AND `regatta.notifier.send.failures` counter increments.

6.2 **Back-compat**: regatta.yaml without `notifiers:` block parses + runs unchanged. No new required env vars. No change to existing alarm-webhook behavior (issue still filed).

6.3 **Zero added env vars**: `REGATTA_SLACK_WEBHOOK_URL` resolved through `secrets.Fetcher`, not raw `os.Getenv`. Reuses `#911` canonical-key registry.

6.4 **CI green**: `make pre-push-check` clean, including `check-comment-density` ≤5% on new prod files. Per-criterion citation token rules apply to the implementer scorecard.

6.5 **Property test**: severity router preserves event count modulo eviction (`internal/notifier/router_property_test.go`); no event is double-delivered to backend.

## §7 Adversarial review (operator-self pass)

7.1 **Webhook URL leak in logs/issues**. RISK. Slack webhook URLs are bearer tokens. Mitigation: `secrets.Value`-typed at config-load time so the `<redacted>` formatter masks all `slog`/`fmt.Sprintf` paths. Lint via existing `secrets.Value` test pattern. Followup gate: `golangci-lint`-side check that `webhook_url` never crosses an `fmt.Stringer`-implementing log call un-redacted.

7.2 **Replay attacks**. Slack/Discord/Mattermost incoming webhooks are POST-only, no challenge token, no payload signing. An attacker with the URL can post arbitrary messages. Mitigation: this is the bearer-token model the upstream services chose; nothing regatta-side can change it. Rotate URL on suspected leak; doctor surface in followup.

7.3 **Message spam → operator alert fatigue**. RISK — the very problem we are trying to solve, inverted. Mitigation: per-Kind buffering at the router prevents a storm of 100 alarm-fired in 5min from producing 100 chat messages (buffered into 1 Med digest). Critical still bypasses — by definition operator wants every one. Bound: 256-event-per-bucket eviction caps memory on a runaway producer; eviction surfaces in the footer so operator sees `[truncated 412 events]` rather than a silent drop.

7.4 **Secret rotation**. Operator edits `regatta.yaml::notifiers.slack.webhook_url.name` to point at a new env var → restart picks up new value via `secrets.Fetcher`. Hot-reload deferred. Followup: file tracking issue if a rotation event happens before W8 hot-reload lands.

7.5 **Downstream rate-limit**. Slack throttles incoming-webhook at ~1 message/sec/channel; bursts return 429 with `Retry-After`. Mitigation: adapter respects `Retry-After` header (single retry with backoff capped at 30s); persistent 429 surfaces as `Notify` error + counter increment. Critical events still buffered to in-process queue during throttle so they are not lost.

7.6 **Notifier failure blocks alarm-webhook**. RISK — if `Notify` blocks on a slow Slack POST, the alarm-webhook ServeHTTP path stalls, AlertManager retries, dedup cache mis-handles. Mitigation: `Notifier.Notify` MUST be non-blocking from caller's POV — adapter dispatches via internal buffered channel + worker goroutine. Test: `TestNotifyReturnsImmediately` asserts `Notify` returns in <10ms with a backend that sleeps 5s.

7.7 **Webhook URL in CUE config file checked into git**. RISK. Mitigation: schema requires `webhook_url: #Secret` — operator can NOT inline the URL as a string (CUE validation fails). Only `source: env|keychain|pass|file` paths allowed. Hard-failure at `cue vet` time prevents the foot-gun.

7.8 **Adversarial review of this spec itself** (`feedback_adversarial_review_every_step`): one independent reviewer subagent reviews this spec before slice-1 dispatch; findings filed as tracking issues or addressed inline.

## §8 Implementer brief — three slices

### Slice 1 — Notifier interface + Slack adapter + wiring (single PR)

Branch: `feat/notifier-slack-adapter`. Tracks: new issue (filed pre-merge per `feedback_unaddressed_load_bearing`).

Files (file-disjoint):
- `internal/notifier/notifier.go` — interface + Event + Severity + NoOp.
- `internal/notifier/notifier_test.go` — interface compile-check, NoOp behavior.
- `internal/notifier/slack/adapter.go` — Slack adapter (Discord/Mattermost follow in slice-1 followup PRs).
- `internal/notifier/slack/adapter_test.go` — httptest backend; canonical JSON shape; 429/503 paths.
- `internal/secrets/keys.go` — add `KeySlackWebhookURL = "regatta.slack_webhook_url"`.
- `contracts/schemas/regatta.v1.cue` — `#Notifiers` block + `#Config.notifiers?:` field.
- `cmd/regatta/serve.go` — wire Notifier into `alarmwebhook.Handler{Notifier: …}`.
- `internal/alarmwebhook/handler.go` — add `Notifier notifier.Notifier` field (nil-safe = NoOp).
- `internal/alarmwebhook/handler_test.go` — `TestRouteCallsNotifierOnCreate`.

TDD order: write `TestNotifyReturnsImmediately` + `TestSlackAdapterPostsCanonicalJSON` first, capture RED in PR body, then implement.

Reviewer dispatch: load-bearing PR (touches schemas/, cmd/, secrets/) — independent reviewer required per CLAUDE.md `Reviewer-verdict gate`.

Out of slice 1: routing/batching (slice 2), digest scheduler (slice 3).

### Slice 2 — Severity routing + batching (single PR)

Branch: `feat/notifier-severity-router`. Depends on slice-1 merge.

Files:
- `internal/notifier/router.go` — `Router{Inner, BatchInterval, DigestInterval, MinSeverity}` with internal per-Kind buffer.
- `internal/notifier/router_test.go` — Critical bypass, Med batches at interval, Low bucket-bounded eviction, MinSeverity filter.
- `internal/notifier/router_property_test.go` — event-count invariant (modulo eviction).
- `cmd/regatta/serve.go` — wrap slice-1 Notifier with Router.
- `contracts/schemas/regatta.v1.cue` — wire `batch_interval`, `digest_interval`, `min_severity` (already in #Notifiers from slice-1, default-values activated here).

TDD: write `TestCriticalBypassesBuffer` + `TestMedFlushesAtInterval` first.

### Slice 3 — Digest schedule + remaining event sites (single PR)

Branch: `feat/notifier-event-sites`. Depends on slice-2 merge.

Files:
- `internal/notifier/digest.go` — periodic tick that flushes Low bucket via scheduler hook (NOT a new goroutine in main; piggyback on existing `internal/orchestrator/scheduler` tick to avoid lifecycle drift).
- `internal/selfimprove/cadence.go` — `onDayReset` hook calls `Notifier.Notify(ctx, Event{Kind: "green-clock-day-reset", ...})`.
- `internal/gates/cost/governor.go` — `tripGlobalCap` calls `Notifier.Notify(ctx, Event{Kind: "cost-cap-hit", Severity: Critical, ...})`.
- `internal/orchestrator/prwatch/stuck.go` — new watcher; fires `Event{Kind: "automerge-stuck", Severity: Med, ...}` when PR is BLOCKED+approved+age>2h.
- Tests in each touched package per TDD-RED-first.

TDD: write `TestCostCapNotifies` + `TestAutomergeStuckEmits` first.

## §9 Out-of-band followups

- **Slack two-way ack**: filed as Phase-X with trigger ≥1 weekly false-positive alarm sustained 4 weeks.
- **Discord + Mattermost adapters**: file under `area:notifier` after slice-1 merges; copy-paste of `slack/adapter.go` with URL constant swap + JSON shape unchanged. Three similar files beat premature abstraction.
- **`regatta doctor` Slack preflight**: filed alongside the broader doctor spec (out of this scope) — POST a synthetic event at startup to verify webhook URL resolves + returns 200.
- **Webhook URL hot-reload**: blocked on W8 hot-reload; file followup with reference once W8 lands.

## §10 Operator-grade rubric (self-grade, no CI gate per CLAUDE.md)

Author drafts; reviewer rescore expected to pull this up by one tier.

B-tier (this draft):
- Problem framing crisp.
- Three-slice dispatch plan, file-disjoint.
- Acceptance benchmark concrete (30s P95).
- Adversarial review hits 7 risk surfaces.

A-tier (reviewer pull-up targets):
- Property-test invariant proves no double-delivery + bounded buffer.
- Slack/Discord/Mattermost JSON-shape parity asserted in one shared adapter contract test.

A+ (aspirational, file followup if not met):
- E2E test wired with a real Slack incoming webhook against a test workspace gated by `REGATTA_E2E_SLACK_*` env vars (skip when unset), proving the 30s SLO empirically — not just under `httptest.Server`.

## §11 Pointers

- Event source: `internal/alarmwebhook/handler.go:312` (`route`).
- Secrets routing: `internal/secrets/secrets.go:69` (`Fetcher` interface), spec `docs/engineer/specs/2026-06-07-secrets-config-unification.md` (#911).
- CUE config root: `contracts/schemas/regatta.v1.cue` (`#Config`).
- Composition root: `cmd/regatta/serve.go`.
- Self-host filter: `docs/engineer/briefs/2026-06-01-self-host-first.md` §1 — operator-awareness is on the keep side (single operator NEEDS push-mode awareness to dispatch unattended).

```release-notes
[DOCS] Spec for Slack/Discord/Mattermost chat-notifier integration. Pluggable Notifier interface + per-severity routing (Critical immediate, Med hourly batch, Low daily digest). Webhook URL routed through #911 secrets block. Three implementer slices: interface+Slack adapter, severity router, digest+event sites.
```
