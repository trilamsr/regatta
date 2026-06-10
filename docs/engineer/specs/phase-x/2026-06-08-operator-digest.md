---
status: phase-x-deferred
summary: "Extend `regatta digest` (today: writes Markdown to `docs/digests/<date>.md` per `2026-06-02-observability-roadmap.md` §6.2 + A-T4) with HTML and email delivery so the sole internal operator gets daily/weekly awareness without depending on the `regatta status` TUI being open. Adds `--email <addr>`, `--html <path>`, and `--weekly` flags to the existing `cmd/regatta/digest.go` seam, an SMTP client behind `internal/obs/digest/mail`, and a cron entry alongside `scripts/cron/daily-digest.sh`. Reads the same OTel meters and substrate events the day-0-to-30 spec cites — `regatta.green_clock.day_count`, `regatta.trigger.days_remaining`, `regatta.cost.usd`, `regatta_cost_cap_*`, `regatta.l4.cache.{hits,misses}`, `regatta.adversarial.findings`, `regatta.pr.failure`, `regatta.alarm_webhook.alerts.total`, `regatta.substrate.chain.break`. Self-host filter: single recipient, single operator; multi-recipient distribution is Phase-X."
deferred_on: 2026-06-10
---

# Operator digest — daily/weekly email + HTML delivery

_Author: design session, 2026-06-08. Companion to `docs/engineer/specs/2026-06-08-operator-day-0-to-30.md` (uses identical metric set). Builds on `docs/engineer/specs/2026-06-02-observability-roadmap.md` §6.2 (Markdown digest) + A-T4 (`cmd/regatta/digest.go`, `scripts/cron/daily-digest.sh`, `internal/obs/digest/`). Cites self-host filter in `docs/engineer/briefs/2026-06-01-self-host-first.md` §1. Memory rules: `feedback_decision_priority`, `feedback_default_simpler`, `feedback_no_signatures`, `feedback_single_user_priority`, `feedback_operator_minimal_input`._

## 0. TL;DR

`regatta status` works when the operator is at a terminal. Daily/weekly reach the operator at low frequency without requiring TUI presence — phone mail-app on a walk, a single tab in the morning, a weekly retrospective with a partner. The Markdown digest at `docs/digests/<date>.md` is already the canonical aggregator; this spec adds email + HTML rendering on top of the same renderer, plus a `--weekly` rollup, behind the existing `regatta digest` subcommand. No new metric instruments, no new aggregation logic — only an output-side fan-out.

Three deliverables: (1) `regatta digest --html <path>` writes a styled standalone HTML to disk; (2) `regatta digest --email <addr>` sends the HTML via SMTP using credentials from the existing `internal/secrets` resolver; (3) `regatta digest --weekly` aggregates seven days. Cron extends `scripts/cron/regatta.crontab` with a 09:00-local weekday trigger and an 09:05 Monday weekly trigger.

## 1. Scope

In scope:

- New flags on existing `regatta digest`: `--html <path>`, `--email <addr>`, `--weekly`.
- New package `internal/obs/digest/render` (HTML renderer) and `internal/obs/digest/mail` (SMTP sender).
- Cron extension: weekly entry in `scripts/cron/regatta.crontab`; updated wrapper in `scripts/cron/daily-digest.sh` OR a new sibling `scripts/cron/weekly-digest.sh` (decision in §3.4).
- SMTP credential surface routed through `internal/secrets` (keychain on darwin, pass on linux, env elsewhere).
- Operator unsubscribe: a single config knob in `regatta.yaml` plus a one-shot `regatta digest --disable-email` subcommand verb.

Out of scope (Phase X — reopen-trigger per `CLAUDE.md` self-host filter):

- Multi-recipient distribution lists. One operator, one address.
- Per-recipient template customization. One template, one renderer.
- Transactional-email providers (SendGrid / Resend / Postmark). SMTP is the lowest-common-denominator surface a self-hosted operator can point at any provider via the SMTP relay endpoint — see §5. A pluggable provider seam is forward-fit only, not built.
- Subscription management UI / opt-in flows. Self-host has no other recipients to opt in.
- Inbound bounce processing beyond the rate-limit circuit breaker in §7.

## 2. Why this spec exists

The Markdown digest at `docs/digests/<date>.md` is committed to the repo and visible only when the operator opens the repo. Three real-world reads where the operator does not have the repo open:

1. Morning catch-up on the phone before opening a laptop. Mail-app render of an HTML digest beats `git pull && less docs/digests/$(date -u +%F).md`.
2. Weekly retrospective. The operator wants a 7-day rollup that does NOT exist today — `regatta digest` is single-day only.
3. Multi-day absence. When the operator is offline for 48-72h, scrolling through three days of committed Markdown is friction. One HTML email per day in the inbox is the lowest-friction surface.

The TUI surface (`regatta status`) covers the "I am at the terminal right now" path. The committed Markdown covers the "I am reading the repo right now" path. Email closes the "I am neither at the terminal nor in the repo" path. The three surfaces share the same renderer-input contract (the `digest.Snapshot` struct in `internal/obs/digest/digest.go`).

## 3. Design

### 3.1 Subcommand surface

```sh
# Already works (no change):
regatta digest --date YYYY-MM-DD [--root .]

# New flags:
regatta digest --date YYYY-MM-DD --html ./out.html
regatta digest --date YYYY-MM-DD --email operator@example.com
regatta digest --weekly --week YYYY-Www [--email <addr>] [--html <path>]
regatta digest --disable-email   # one-shot; writes regatta.yaml stanza
```

Flag interactions:

- `--html` and `--email` are compatible; `--email` always renders HTML internally and POSTs to SMTP.
- `--weekly` requires `--week` (ISO week, e.g. `2026-W23`) OR resolves "the week ending yesterday" when omitted.
- `--date` and `--weekly` are mutually exclusive.
- Without `--html` or `--email`, behavior is unchanged — Markdown still writes to `docs/digests/<date>.md` (or `docs/digests/<week>.md` for weekly).

### 3.2 Renderer layering

Existing layout:

```
internal/obs/digest/
  digest.go        — Snapshot struct, Markdown renderer
  digest_test.go
  prom.go          — PromSource (reads /metrics)
  prom_test.go
```

New additions:

```
internal/obs/digest/
  render/
    html.go        — Snapshot → HTML string (uses html/template)
    html_test.go
    weekly.go      — 7×Snapshot → Snapshot (aggregation: sums, deltas, max-by-severity)
    weekly_test.go
  mail/
    smtp.go        — SMTP client (net/smtp with STARTTLS)
    smtp_test.go   — table-driven; uses testserver
    config.go      — Resolves SMTP creds via internal/secrets
    config_test.go
```

The Markdown renderer in `digest.go` does not move. HTML renderer reads the same `Snapshot` struct so a future weekly Markdown rollup also works.

### 3.3 Content per digest (single source — same as day-0-to-30 §5.1)

Both daily and weekly cover the seven items the task brief enumerates. Each maps to an existing meter or substrate query — NO new instruments.

| Section | OTel source | Aggregation |
|---|---|---|
| PRs merged (count + categories) | substrate: `substrate_events` where `kind='agent_pr_merged'`; categories from `release-notes` prefix `[FEAT]/[FIX]/[CHORE]/...` | Daily: count today. Weekly: count 7d, by-category breakdown. |
| Cost spent (vs cap) | `regatta.cost.usd` (Float64Counter), `regatta_cost_cap_24h_spend_usd`, `safety.spend_cap_usd_per_day` from `regatta.yaml` | Daily: today's delta + cap. Weekly: sum + max-day + cap-breach count. |
| Green-clock day count + delta | `regatta.green_clock.day_count` (gauge), `regatta.trigger.days_remaining{trigger="green_clock"}` | Daily: today's value + Δ vs yesterday. Weekly: start + end + reset count. |
| L4 cache hit-rate | `regatta.l4.cache.hits` / `(hits + misses)` | Daily: point-in-time rate. Weekly: rolling 7d. |
| Top reviewer findings (by severity) | `regatta.adversarial.findings` (tag `severity × scope × pattern`) | Daily: top-3 buckets. Weekly: top-5 + rate-vs-merged-PR. |
| Alarms fired | `regatta.alarm_webhook.alerts.total` | Daily: count + GH-issue links. Weekly: count by severity. |
| Wedges unblocked / failed | substrate: `substrate_events` where `kind IN ('wedge_unblocked','wedge_failed')` | Daily: list. Weekly: counts + ratio. |

### 3.4 Cron schedule

`scripts/cron/regatta.crontab` today carries the daily 09:00 entry:

```cron
0 9 * * * /usr/local/bin/regatta-cron-wrapper daily-digest
```

Additions:

```cron
# Daily digest — Markdown + email; runs after Markdown commit.
5 9 * * * /usr/local/bin/regatta-cron-wrapper daily-digest-email

# Weekly digest — Monday 09:05 local; covers Mon-Sun of prior ISO week.
5 9 * * 1 /usr/local/bin/regatta-cron-wrapper weekly-digest
```

Decision (§1 enumeration): extend `scripts/cron/daily-digest.sh` with an `--email` mode and add a sibling `scripts/cron/weekly-digest.sh`. The two scripts are <30 lines each; collapsing into one with a verb-dispatcher fails the "default simpler" gate. The cron wrapper (`scripts/cron/regatta-cron-wrapper`) is the seam that picks per-verb.

Manual run: `regatta digest --date $(date -u +%F) --email $(yq '.digest.email_to' regatta.yaml)`. Cron simply calls the same.

### 3.5 Config surface in `regatta.yaml`

```yaml
digest:
  email_to: operator@example.com          # required for --email
  smtp:
    host: smtp.example.com
    port: 587
    starttls: true
    username_secret: digest_smtp_user      # resolves via internal/secrets
    password_secret: digest_smtp_pass
    from: regatta@example.com
  weekly:
    enabled: true                          # if false, weekly cron no-ops
    timezone: local                        # ISO week boundary
  unsubscribed: false                      # set true by --disable-email
```

The `digest:` stanza is optional. Missing → `--email` flag errors with usage; Markdown and `--html` still work.

## 4. Schedule — cron via the existing supervisor unit

The supervisor unit (`docs/engineer/specs/2026-06-02-phase-autonomy-w3-service-supervisor.md`) already manages `scripts/cron/regatta.crontab` via launchd (darwin) / systemd-timer (linux). Adding two cron rows is a config-only change; no new supervisor work.

Failure modes the supervisor must already handle (verified in spec §6 of the W3 supervisor doc):

- Cron run overlap — the wrapper writes a per-verb flock; second run no-ops.
- Stale env — wrapper reloads `.env` per run.
- Failure isolation — failed daily-digest-email run does NOT block the next day's run.

Operator can fire on demand: `regatta digest --date YYYY-MM-DD --email <addr>`. Manual fire respects the same unsubscribe flag (§6).

## 5. Email config — SMTP via secrets routing

SMTP credentials route through `internal/secrets` (`composite.go` is the resolver). Resolution order: keychain (darwin) → pass (linux) → env → fail-closed.

`regatta.yaml`'s `username_secret` / `password_secret` fields name secret keys, not literals — same convention as the Anthropic-key wiring at `cmd/regatta/keys.go`.

SMTP envelope:

- `net/smtp` from stdlib. No third-party dep.
- STARTTLS required when `starttls: true` (default). Plain-SMTP only via explicit `starttls: false` — call-site comment cites "self-host operator running their own MTA" as the lone use case.
- Auth: `PLAIN` (over STARTTLS) is the only mechanism. No DIGEST-MD5, no XOAUTH2 — operator picks an SMTP provider that supports `PLAIN/STARTTLS` (Fastmail, Mailgun SMTP relay, AWS SES SMTP, self-hosted Postfix).
- Timeout: 30s per send. One retry on transient (network) error; no retry on permanent (550 mailbox).
- TLS cert verification on by default; `--insecure-skip-verify` is NOT exposed (would defeat the whole point of SMTP-over-STARTTLS).

Provider alternative consideration — SendGrid/Resend/Postmark APIs would buy deliverability tooling but cost a transactional-email vendor dep, billing surface, and per-operator account setup. SMTP is what every operator can already point at any provider's SMTP relay endpoint. Decision: SMTP only for self-host phase. Provider-API seam is forward-fit only; not in this spec.

## 6. Operator unsubscribe + spam control

Two knobs:

1. `digest.unsubscribed: true` in `regatta.yaml`. The mail sender checks this before `Mail()`; true → log "unsubscribed; skip" and exit 0.
2. `regatta digest --disable-email` writes that line into `regatta.yaml` via the same writer that `regatta install-service` uses (`internal/configwrite/yaml.go` if it exists; else the simple in-place merger at `cmd/regatta/init.go`). Re-enable by editing the field or running `regatta digest --enable-email`.

Rate-limit / bounce loop protection — see §7.

## 7. Adversarial — threats + mitigations

| Threat | Mitigation |
|---|---|
| SMTP credential leak via logs | `internal/obs/digest/mail/smtp.go` MUST NOT log `username`, `password`, or the full URL. Test `TestSMTP_NoSecretsInLogs` asserts every log line passes a redaction filter. |
| SMTP credential leak via PR-body digest content | The Markdown digest in `docs/digests/<date>.md` is committed; the operator MUST NOT paste SMTP creds into `regatta.yaml` literals. Routing via `internal/secrets` is the structural fix; the spec example uses `username_secret` not inline literal. Lint: `scripts/check-secrets-in-yaml.sh` (new, scoped to `regatta.yaml`) fails closed on plausible SMTP literals in the file (a `:` after `password` or `password_inline`). |
| PII in digest body — work_item content leaks via email | The digest already aggregates counts and metric values, NOT work_item bodies. The "wedges unblocked / failed" section lists wedge titles only (not bodies). HTML renderer asserts no `work_item.body` field is referenced (test `TestHTMLRender_NoWorkItemBody`). |
| Digest spam — cron misfires N times an hour, operator inbox floods | Wrapper flock + per-verb daily lockfile under `/var/run/regatta/digest-<verb>-<YYYY-MM-DD>.lock`. Existence = "already ran today, skip". Tested in `scripts/cron/crontab_test.go`. |
| Email-bounce loop — operator's mailbox bounces; SMTP retries forever | One retry per send (§5). After two consecutive permanent-bounce days, the sender sets `digest.bounce_streak: 2` in `regatta.yaml` and skips for the next 7 days; operator sees the field on next config read and clears it manually. Test `TestSMTP_BounceCircuitBreaker`. |
| Email body contains banned-phrase token from runbook copy | The HTML renderer pulls only metric values + section headers. Banned-phrase gate (`scripts/doc-check.sh`) already runs against `docs/digests/*.md`; HTML is regenerated from the same Snapshot so the same content review applies. No separate banned-phrase check on HTML output. |
| HTML injection via metric label values | `html/template` (stdlib) auto-escapes. No `template.HTML` casts in the renderer. Test `TestHTMLRender_EscapesHTMLInLabels` injects `<script>` into a synthetic finding bucket and asserts the output contains `&lt;script&gt;`. |
| SMTP TLS cert MITM | STARTTLS + cert verification on by default (§5). No `InsecureSkipVerify`. |
| Operator unsubscribes but cron still sends | `digest.unsubscribed` check is the FIRST thing `--email` mode does after flag parse. Test `TestDigest_UnsubscribedNoSend`. |
| Cron-wrapper privilege escalation | Wrapper runs under the existing `regatta` service user; no `sudo` invocations. Same as the daily-digest entry today. |
| Weekly digest fires on partial week | `--weekly` aggregator skips a week if `<7` days of data; logs "partial week, skip" and exits 0. Test `TestWeeklyDigest_SkipsPartialWeek`. |

## 8. Acceptance

- [ ] `cmd/regatta/digest.go` accepts `--html <path>`, `--email <addr>`, `--weekly`, `--week YYYY-Www`, `--disable-email`, `--enable-email`. Existing `--date` / `--root` unchanged.
- [ ] `internal/obs/digest/render/html.go` renders a single-file HTML with inline CSS (no external assets, no `<img>` to remote URLs).
- [ ] `internal/obs/digest/mail/smtp.go` sends via `net/smtp` with STARTTLS + `PLAIN` auth; reads creds via `internal/secrets`.
- [ ] `scripts/cron/regatta.crontab` carries the two new entries.
- [ ] `regatta.yaml` schema documents the `digest:` stanza in the same place as `safety.spend_cap_usd_per_day`.
- [ ] `make pre-push-check` green on a host with the digest stanza absent (the default in the repo's own `regatta.yaml`).
- [ ] All §7 adversarial mitigations carry a named test.
- [ ] Operator acceptance: with `digest.email_to` set + SMTP creds in keychain, the next 09:05 cron fires and the operator receives the previous day's HTML digest within 60s.
- [ ] No new OTel instruments introduced. All sections in §3.3 cite existing meters or substrate event kinds.
- [ ] Self-host filter: no `tenant_id`, no per-recipient routing, no multi-recipient distribution. Single operator, single inbox.

## 9. 3-slice implementer brief

**Slice 1 — digest subcommand + Markdown renderer extension**

Scope: `cmd/regatta/digest.go` adds `--weekly` + `--week` flags; `internal/obs/digest/render/weekly.go` aggregates 7×Snapshot → 1 Snapshot. Markdown renderer reused as-is. Cron: `scripts/cron/weekly-digest.sh` calls `regatta digest --weekly`. Tests: `TestDigest_WeeklyAggregatesSevenDays`, `TestDigest_WeeklySkipsPartialWeek`, `TestWeeklyRenderer_MatchesSchema`. Out: HTML, email, secrets.

**Slice 2 — HTML/email renderer + SMTP**

Scope: `internal/obs/digest/render/html.go` + `html_test.go`. `internal/obs/digest/mail/smtp.go` + `config.go` + tests. `--html <path>` and `--email <addr>` flags on the subcommand. Secret-name routing through `internal/secrets`. Adversarial test set §7 first three rows + HTML-escape row + STARTTLS row + bounce-circuit row. RED commit lands first per TDD discipline.

**Slice 3 — cron schedule + unsubscribe**

Scope: extend `scripts/cron/regatta.crontab` with two rows; `regatta digest --disable-email` / `--enable-email` verbs writing the `regatta.yaml` stanza; `digest.unsubscribed` check on the send path. Tests: `TestCrontab_HasDigestEntries`, `TestDigest_DisableEnableEmail_Roundtrip`, `TestDigest_UnsubscribedNoSend`. Cron wrapper changes only if §4 lock behavior misses today.

## 10. Out of scope (Phase X)

- Per-user customization (template themes, section ordering, opt-out per-section).
- Multi-recipient distribution lists.
- Provider-API senders (SendGrid / Resend / Postmark) — SMTP only.
- Inbound-bounce webhook handlers (parse the bounce envelope, auto-disable). The §7 bounce-streak counter is the manual circuit breaker; webhook parsing is forward-fit.
- Mobile push (APNs / FCM). Mail-app on a phone is the path.

## 11. Followups (file as separate issues if approved)

- F1: `regatta digest --weekly --md-only` for operators who want the weekly Markdown without HTML/email side effects.
- F2: HTML renderer dark-mode CSS. Single-template default is light-mode; system-preference media query can land as a one-line `@media (prefers-color-scheme: dark)` block once the operator asks.
- F3: Digest delivery latency SLO — track time from cron-fire to SMTP-accepted. Defer until the loop has 30 days of green-clock data showing the cron actually fires reliably.

```release-notes
[DOCS] docs: spec daily/weekly operator digest (email + HTML delivery)
```
