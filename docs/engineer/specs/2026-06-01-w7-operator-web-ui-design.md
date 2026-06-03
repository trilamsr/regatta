---
title: "MVP-3 W7 operator web UI v2"
status: active
summary: "W7 operator web UI v2: server-rendered approval flow + read-only DAG + cost panel; Go embed.FS + htmx + Tailwind CDN. 14 tasks across 4 waves (W7.0 listener prereq + 3 build waves). Authorizer interface seam designed pre-W8."
---

# MVP-3 W7 — Operator Web UI (design spec, v2)

_Author: design subagent, 2026-06-01. Scope umbrella: [#183](https://github.com/trilamsr/regatta/issues/183). Source-of-truth: `docs/superpowers/briefs/2026-05-31-mvp-3-next-level.md` §4 W7 + §6 red-team #2 (web UI tar-pit risk). **v2 rev** addresses all 7 Risk + 6 Important findings from `docs/superpowers/reviews/2026-06-01-w7-operator-ui-review.md`._

## 0. Changes from v1 (revision notes)

Every change tagged with the Risk/Important finding it closes. Reviewer should walk this list first.

| Tag | Section | Change |
|---|---|---|
| R1 | §3.1, §3.9, §8 #5 | Cost-panel data source switched from invented `span_events` table to substrate `events WHERE kind='token_spend'` (substrate spec `0006_substrate.sql`). v1 cost-panel is a **single SUM aggregate** ("current burn this run"); time-series chart deferred to W7.4 followup. |
| R2 | §1.3, §3.1, §3.2, §7 | Added new **Wave W7.0**: introduce HTTP listener to `cmd/regatta/serve.go` + wire concrete `InteractiveNotifier.CallbackRoute()` impl. Task count: 14 tasks across 4 waves (was 12 across 3). The listener is a **prerequisite**, not a free assumption. |
| R3 | §3.5, §3.7, §6 | Self-host Tailwind (build-time `npx tailwindcss -o`); drop CDN entirely. CSP becomes `default-src 'self'; script-src 'self'; style-src 'self'`. No third-party origins; no `unsafe-inline`. htmx 2 CSP-strict via `data-*` patterns + `htmx.config.allowEval=false`. Adds `Referrer-Policy: no-referrer`. |
| R4 | §3.6.4 | Dropped `Authorizer` interface. v1 has ONE auth caller (approval HMAC token verify); abstraction deferred until W8 has a second caller. No premature interface. |
| R5 | §3.10 (new) | Added "Data sources (post-substrate)" cross-reference: cost-panel + DAG list **read substrate events fold**; approval-decide writes via existing `approvals`/`approval_events` (already shipped, low-churn). Reconciliation paragraph cites substrate spec by path + names migration `0006_substrate.sql`. |
| R6 | §3.3, §8 #5 | Cost-panel `Cache-Control: no-store` (was `max-age=2`). The stale-cache lie on the approval surface is unacceptable; load is trivial per S3. |
| R7 | §4, §6 | Added N=500 + N=2000 benchmark rows; cite `internal/orchestrator/state/dbtest/query_counter.go` as W7.0 task; added Makefile lint targets for N+1 + `template.HTML` + `hx-trigger` without `hx-sync`. |
| I1 | §3.6.1, §8 #1 | Token redirected from URL path to `Authorization`-equivalent cookie + opaque approval-id in path. Slack-unfurl: notifier emits `unfurl_links: false`. |
| I2 | §3.4 | 8 KiB hard cap on diff render lifted from §9 open-q to §3 body. Overflow → `/approve/{approval_id}/diff` streaming route w/ 1 MiB cap. Now A-rubric, not A+. |
| I3 | §3.4, §6 B-rubric | `lint-web-template-html` Makefile target + wired into `make check`; same for `hx-trigger`-without-`hx-sync` AST lint. |
| I4 | §3.6.4 | `Principal{ID, Tenant, Roles}` type locked in `internal/web/auth.go` even though v1 only populates `ID` (the reviewer). W8 expands, doesn't re-shape. |
| I5 | §5.3 | Playwright runs with `--ui-vendor-css=true` test flag + container egress disabled; B-rubric item. |
| I6 | §3.4 | `approval_decided.tmpl` shows: "Your vote: ALLOW. Recorded at HH:MM UTC. Awaiting N more votes from: …" — principal echoed back, peer list explicit. Property-tested. |
| S1 | §1.1, §7 | DAG view ships as **plain HTML list (no SVG graph)** at v1. SVG render deferred to W7.4 followup. Reduces W7.2 wave size. |
| S2 | §3.6.2 | Origin-header check kept as defense-in-depth alongside CSRF cookie. Both lock. |
| S3 | §3.9 | USD-per-1k table moved out of W7. Substrate `kind='token_spend'` payload carries pre-computed `usd_micros` written by the LLM-call site. W7 reads `SUM(json_extract(payload_json,'$.usd_micros'))`. No model→price table in `internal/web/`. |
| S4 | §3.4 | `runs.tmpl` v1 has **no sort UI**. Operators ⌘-F. Sort deferred to W7.4 followup. |
| S5 | §6 A+ | OTel-span-per-route demoted from A+ to W7.4 followup. Decouples W7 from W6 Wave 3 churn. |
| 9.1–9.8 | §3, §6 | All open-question rulings from review applied: Tailwind vendored day 1, no dev-session bypass, fail-boot if `REGATTA_HMAC_KEY` unset, 5 s polling stays, etc. §9 deleted. |

**Verification done before writing this rev** (per `feedback_verify_before_asking`):
- `grep -rn span_events /Users/treedesk/Desktop/Projects/regatta/` → zero hits in `internal/` or any migration. Only the v1 spec + review reference it. **Confirmed: table does not exist.**
- `grep -n "http\." cmd/regatta/serve.go` → zero hits across 418 LOC. **Confirmed: no HTTP listener.**
- `grep -rn CallbackRoute internal/gates/approval/` → one hit: `notify.go:38` (interface declaration only, no concrete impl). **Confirmed: interface declared, not wired.**
- `gh issue view 183` → IN scope = approval flow + read-only DAG view + cost panel; OUT = workflow authoring, multi-tenant, SSO, real-time push. **Re-confirmed scope-lock unchanged.**
- Substrate spec `docs/superpowers/specs/2026-06-01-unified-substrate-design.md` §2.1 confirms `kind='token_spend'` events ship on `substrate_events` (DDL at lines 74-113). Sister subagent re-spec renumbers to `0006_substrate.sql`.

---

## 1. Scope lock

Locked to umbrella issue #183 (re-verified). Brief §6 red-team #2 ("Web UI is a tar-pit") is the load-bearing risk; the entire spec exists to fence it.

### 1.1 Hard scope IN — three surfaces, nothing else

1. **Approval flow** — operator clicks a per-approver URL embedded in their Slack/email notification, lands on a server-rendered HTML page that shows work_item context, upstream-output diff (capped at 8 KiB — see §3.4 / I2), cost-so-far panel (single SUM — addresses R1 + S3), and two buttons: **Approve** / **Reject** (plus optional reason textarea). Submit → server-side `approval.DecideTx` (same path the CLI takes) → success page or typed-sentinel error page.
2. **Read-only work-items list view** — per run_id: **plain HTML table** of work_items (id, lane, state, started_at, ended_at, trace_id link) plus a separate **edges** list (`from_id → to_id` rows). Live filtering by state + lane via server-rendered query params. **No SVG graph render.** (S1 — graph deferred to W7.4 followup.) No edit affordances. No write paths. Polls every 5 s via `hx-trigger="every 5s"`.
3. **Cost panel** — per run_id: **single SUM** of `usd_micros` + token counts from substrate `events WHERE kind='token_spend' AND run_id=?` (R1 + S3). Renders alongside budget cap when `policies WHERE kind='budget'` row exists; otherwise shows "budget unset" badge. Read-only. Same 5-s poll cadence. Time-series chart deferred to W7.4 (addresses R1 simplification).

### 1.2 Hard scope OUT — explicit non-goals

- **NO workflow authoring UI** — operators edit `regatta.yaml` in their repo. UI never writes to plan files.
- **NO write paths beyond approve/reject** — no force-rerun, no kill, no requeue, no plan-mutation. CLI-only.
- **NO multi-tenant / no SSO / no OIDC** — single org. Identity = HMAC token. W8 OPA RBAC wedge handles multi-tenant + SSO.
- **NO real-time push** (no WebSocket, no SSE) — htmx polling every 5 s is sufficient (open-q 9.4 ruling).
- **NO mobile-optimized layout** — responsive enough that buttons aren't broken on a phone; not designed mobile-first.
- **NO trace viewer embed** — `trace_id` link → operator's configured OTel backend (Jaeger / Honeycomb / Tempo).
- **NO JS framework** — plain HTML + htmx 2.x. No React/Vue/Svelte/Alpine.
- **NO CDN dependency** (R3) — Tailwind compiled at build time, served from `embed.FS`. No `npm` at runtime. No `npm` on CI machines (build step runs on a developer machine + dist is committed under `internal/web/static/`).
- **NO SVG / graph render** (S1) — v1 is HTML list only.
- **NO sort UI on DAG list** (S4) — v1 ships fixed `ORDER BY started_at`.
- **NO dev-session bypass** (open-q 9.3 ruling) — no `--ui-localhost-trusted` flag. Token-only, strict.
- **NO USD-per-1k table in `internal/web/`** (S3) — substrate writer (LLM call site) is the price-table owner; W7 sums precomputed `usd_micros`.

### 1.3 Compatibility envelope

- Single binary: `regatta serve` keeps its CLI surface; UI ships under `/ui/*` prefix on a **newly stood-up HTTP listener** (R2 — see §3.2 Wave W7.0).
- Opt-out flag: `regatta serve --ui=false` skips listener boot entirely. Default: `--ui=true`. When false, no port is bound; useful for headless CI.
- `regatta serve --addr=:8080` (default `:8080`) — port flag is new in W7.0.
- Fail-boot if `--ui=true` AND `REGATTA_HMAC_KEY` unset → exit non-zero with clear error (open-q 9.8 ruling). Loud-at-boot beats lying-at-render.
- Zero new processes: no separate `regatta-ui` daemon. The listener runs in-process on `regatta serve`.

## 2. Prior art adopted

Per `feedback_research_design_principles` — adopt proven OSS before bespoke.

| Component | Adopted from | Why |
|---|---|---|
| Single-binary HTML+API server | [Temporal Web UI](https://github.com/temporalio/ui) pattern — single binary, embedded assets via Go `embed.FS`. | Operator already runs `regatta serve`; one binary stays one binary. |
| HTML-template-driven, server-rendered | Go `html/template` + `embed.FS` (stdlib, BSD-3). | Auto-escape closes XSS class at template layer. Zero external deps. |
| Progressive enhancement | [htmx 2.x](https://htmx.org) (MIT, vendored to `internal/web/static/htmx.min.js`). htmx 2 supports CSP-strict via `data-*` selectors + `htmx.config.allowEval = false` ([CSP guide](https://htmx.org/docs/#csp)). | Zero JS framework lock-in. Operator can disable JS and approve/reject still works as plain HTML forms. |
| CSS | [Tailwind CSS](https://tailwindcss.com) v3 compiled to a static dist at build time via `npx tailwindcss -i ./internal/web/css/input.css -o ./internal/web/static/tailwind.min.css --minify` (run on a developer machine; output committed). | No CDN. No bundler at runtime. No `npm install` on CI. R3-clean. |
| HMAC URL-bound auth | Existing `internal/canon` package + approval token from `internal/gates/approval/gate.go`. | Re-use, do **not** invent. Brief §6 red-team #4 sticks. |
| E2E browser test | [Playwright CLI](https://playwright.dev) (Apache-2.0). Out-of-process via Go `os/exec`. | Matches `docs/design.md` P3.1 prior-art. Smaller blast radius than chromedp. |
| Handler unit test | stdlib `net/http/httptest`. | Idiomatic, zero deps. |
| Query-counter middleware | New: `internal/orchestrator/state/dbtest/query_counter.go` (W7.0 T2). | Detects N+1 (addresses R7 gap 2). |

**Rejected**:
- React/Vue/Svelte — SPA bundle ≥40 KB minified gz; lock-in over a 5-year horizon outweighs zero short-term gain.
- Alpine.js — same shape as htmx but smaller community.
- chromedp/rod — embeds Chrome into Go test process; flaky on macOS.
- SSE/WebSocket — out of #183 scope per open-q 9.4 ruling.
- Tailwind Play CDN — see R3. Bearer-token leak via Referer to third party = control-plane-credibility kill.

## 3. Architecture

### 3.1 Wire diagram

```
                ┌────────────────────────────────────────────────────────┐
                │  regatta serve (single Go binary)                       │
                │                                                         │
                │  net/http.Server :ADDR  (NEW in W7.0 — R2)              │
   browser ───▶ │   ├── /approve/{approval_id}        (UI: approval page)│
                │   ├── /approve/{approval_id}/decide (POST: form submit)│
                │   ├── /approve/{approval_id}/diff   (GET: full diff   )│
                │   ├── /runs/{run_id}                (UI: list view    )│
                │   ├── /runs/{run_id}/cost           (UI: cost panel   )│
                │   ├── /ui/static/*                  (embed.FS assets  )│
                │   ├── /healthz                      (liveness         )│
                │   └── /api/approval/callback        (CallbackRoute()  )│
                │                                                         │
                │   handlers ──▶ internal/web (NEW)                       │
                │                  ├── render(tmpl, data)  ◄── embed.FS   │
                │                  ├── verifyToken         ◄── canon     │
                │                  └── approval.DecideTx   ◄── lifted    │
                │                                            from CLI    │
                └────────────────────────────────────────────────────────┘
                          ▲                              ▲
                          │                              │
                          │ reads approvals,             │ reads substrate
                          │ approval_events,             │ events WHERE
                          │ work_items, work_item_edges  │ kind='token_spend'
                          │ (legacy, low-churn — R5)     │ AND run_id=? (R1)
                          │                              │ (migration 0006)
```

**Data-source pinning (R5)**: see §3.10 "Data sources (post-substrate)".

### 3.2 Package layout

```
internal/web/                       # NEW PACKAGE
├── server.go                       # http.Handler factory; route table
├── server_test.go
├── render.go                       # template loader; embed.FS plumb
├── render_test.go
├── approval.go                     # GET /approve/{approval_id} + POST decide + GET /diff
├── approval_test.go                # XSS/CSRF/replay
├── runs.go                         # GET /runs/{run_id}  + GET /runs/{run_id}/cost
├── runs_test.go
├── health.go                       # /healthz
├── auth.go                         # HMAC token verify + Principal type (I4)
├── auth_test.go
├── cost.go                         # substrate kind='token_spend' fold (R1 + S3)
├── cost_test.go
├── csp.go                          # CSP middleware (Referrer-Policy too — R3)
├── csp_test.go
├── const.go                        # all magic numbers named here
├── css/
│   └── input.css                   # tailwind directives (@tailwind base/components/utilities)
├── templates/                      # embed.FS root
│   ├── layout.tmpl                 # outer skeleton; CSP-clean head
│   ├── approval.tmpl
│   ├── approval_decided.tmpl       # principal-echo + peer list (I6)
│   ├── approval_error.tmpl         # typed-sentinel
│   ├── approval_diff.tmpl          # full diff page (8 KiB+ overflow — I2)
│   ├── runs.tmpl                   # HTML list — work_items + edges (S1)
│   ├── runs_cost.tmpl              # cost panel partial (single SUM)
│   ├── _diff.tmpl                  # capped diff sub-partial (8 KiB)
│   └── _flash.tmpl                 # error/info flash bar
└── static/                         # embed.FS root
    ├── htmx.min.js                 # vendored htmx 2.x (config.allowEval=false at boot)
    └── tailwind.min.css            # build-time output (committed)

cmd/regatta/serve.go                # MODIFIED — boot http.Server, --addr/--ui flags
internal/orchestrator/state/dbtest/
└── query_counter.go                # NEW in W7.0 T2 — N+1 lint via test middleware (R7)

internal/gates/approval/decide.go   # NEW — lifted from cmd (W7.0 T3)
internal/gates/approval/decide_test.go

tests/e2e/playwright/               # NEW — opt-in via `make e2e-ui`
├── approval_happy.spec.ts
└── approval_nojs.spec.ts

Makefile                            # ADD: lint-web-template-html, lint-hx-sync,
                                    #      bench-ui, e2e-ui, build-tailwind
```

**Zero modifications to approval state machine** (`internal/gates/approval/gate.go`, `fold.go`, `notify.go`). `decideTx` is lifted from `cmd/regatta/approval_decide.go` into `internal/gates/approval/decide.go` so both CLI and web call `approval.DecideTx` — a pure refactor with zero behavior change.

### 3.3 HTTP route table

| Method | Path | Purpose | Auth | Caching |
|---|---|---|---|---|
| `GET` | `/approve/redeem?t=<hmac-token>` | One-time redeem endpoint. Validates the HMAC token in the query string, sets `regatta_approval_token` + `regatta_csrf` cookies (both per-ULID scope; see §3.6.1–§3.6.2), then 303-redirects to `/approve/{approval_id}`. Token never reappears in path or referer after this hop (I1). | HMAC token in `t` query param (validated via `canon.VerifyToken`) | `Cache-Control: no-store, no-cache` + `Referrer-Policy: no-referrer` |
| `GET` | `/approve/{approval_id}` | Render approval page. The path carries **only the ULID** of the approval (public, audit-trail-safe). The HMAC token is in a short-lived cookie set on first redirect from the Slack/email click (I1). | HMAC token in cookie (validated via `canon.VerifyToken`) + Origin-header check (S2) | `Cache-Control: no-store, no-cache` + `Referrer-Policy: no-referrer` |
| `POST` | `/approve/{approval_id}/decide` | Submit decision. Body: form-urlencoded `decision={allow,deny}` + `reason=<text>` + `csrf=<form-token>`. Server calls `approval.DecideTx`. | HMAC cookie + CSRF (double-submit) + Origin check (S2) | `Cache-Control: no-store` |
| `GET` | `/approve/{approval_id}/diff` | Full diff stream (Transfer-Encoding: chunked) with 1 MiB hard cap + redaction notice (I2). Only used when `_diff.tmpl` overflows the 8 KiB cap. | same as approval page | `Cache-Control: no-store` |
| `GET` | `/runs/{run_id}` | Read-only work-items + edges HTML list (S1). Filter via `?state=` + `?lane=`. Polls itself via htmx every 5 s. | HMAC cookie (token-gated, strict — open-q 9.7) | `Cache-Control: no-store` |
| `GET` | `/runs/{run_id}/cost` | Cost panel partial (single SUM of `kind='token_spend'`). Htmx swap target. | inherits parent | `Cache-Control: no-store` (R6 — the approval-page consumer cannot tolerate a 2 s lie) |
| `GET` | `/healthz` | Liveness: `200 OK\nok\n`. No DB query. | none | `Cache-Control: no-store` |
| `GET` | `/ui/static/*` | embed.FS assets (htmx.min.js, tailwind.min.css). | none | `Cache-Control: public, max-age=86400, immutable` |
| `POST` | `/api/approval/callback` | Wave-1 `InteractiveNotifier.CallbackRoute()` — **wired in W7.0** to the same listener. Concrete impl returns a route+handler that calls into `approval.DecideTx`. | HMAC token in POST body | `Cache-Control: no-store` |

Route registration is the only HTTP surface on `regatta serve`. **The listener itself is new** (R2).

### 3.4 Template structure (embed.FS, `templates/*.tmpl`)

All templates under `internal/web/templates/` embedded via `//go:embed templates`. Loaded once at boot via `template.New("layout").Funcs(...).ParseFS(fsys, "*.tmpl")` — malformed template fails at boot, not at first request.

| File | Type | Renders | Server-side includes | htmx swap targets |
|---|---|---|---|---|
| `layout.tmpl` | base | HTML doc; CSP-clean head; `<link rel=stylesheet href=/ui/static/tailwind.min.css>` + `<script src=/ui/static/htmx.min.js>`; sets `htmx.config.allowEval=false` via a CSP-allowed `<script>` block from `embed.FS` (no inline). | _none_ | _none_ |
| `approval.tmpl` | page | work_item summary, upstream-output diff (via `_diff.tmpl`; cap = 8 KiB — I2), cost panel (htmx swap), Approve+Reject buttons, reason textarea | `_diff.tmpl`, `_flash.tmpl` | `runs_cost.tmpl` via `hx-get="/runs/{run_id}/cost"` |
| `approval_decided.tmpl` | page | "Your vote: ALLOW. Recorded at HH:MM UTC. Awaiting N more votes from: bob@co, carol@co." with green check next to submitter's principal in the decided_by list. (I6) | `_flash.tmpl` | _none_ |
| `approval_error.tmpl` | page | Typed-sentinel display: token_invalid, token_expired, token_replay, unknown_key, not_reviewer, self_review | `_flash.tmpl` | _none_ |
| `approval_diff.tmpl` | page | Full-diff overflow renderer; chunked stream; 1 MiB hard cap (I2) | `_flash.tmpl` | _none_ |
| `runs.tmpl` | page | HTML table: work_items (id, lane, state, started_at, ended_at, trace_id link) ORDER BY started_at; HTML list: edges (`from_id → to_id`); filter form (state + lane). NO sort UI (S4). NO SVG (S1). | `_flash.tmpl` | `runs_cost.tmpl` via `hx-get="/runs/{run_id}/cost"` |
| `runs_cost.tmpl` | partial | Cost panel: input_tokens + output_tokens + USD = `SUM(json_extract(payload_json,'$.usd_micros'))/1e6` (S3); budget bar if `policies WHERE kind='budget' AND scope_id=run_id` row exists. Single SUM aggregate. Time-series deferred to W7.4. | _none_ | _none_ |
| `_diff.tmpl` | partial | Server-rendered unified diff; HARD CAP **8192 bytes (raw byte count, not runes)** output (I2 — lifted from §9 v1 open-q); hard-truncate at byte boundary, then `utf8.ValidString` guard trims the trailing partial code-point if the cut landed mid-rune; overflow renders truncation indicator `... (8 KiB cap reached)` plus "Show full diff →" link to `/approve/{approval_id}/diff`. Operates on `[]DiffLine{Op, Text, OldLineNum, NewLineNum}`. | _none_ | _none_ |
| `_flash.tmpl` | partial | Green/red banner. | _none_ | _none_ |

**Template funcs whitelisted at load time**: `formatTime`, `formatBytes`, `formatTokens`, `formatUSDMicros`, `truncate`, `humanizeDuration`, `safeURL` (whitelisted scheme prefixes only), `csrfToken`, `clampDiff` (enforces 8 KiB cap server-side — I2).

**`template.HTML` is forbidden** in `internal/web/` (I3). `Makefile` target `lint-web-template-html` runs `! grep -rE 'template\.HTML' internal/web/` and is wired into `make check`.

**`hx-trigger` without `hx-sync`** is forbidden (I3 + addresses §8 #7). `Makefile` target `lint-hx-sync` walks `internal/web/templates/*.tmpl` via a tiny Go AST visitor (`tools/lint-hx-sync/main.go`); failure on any `hx-trigger` containing `every` that lacks a sibling `hx-sync`.

### 3.5 CSS strategy — vendored Tailwind, compiled once

```html
<!-- inside layout.tmpl <head> -->
<link rel="stylesheet" href="/ui/static/tailwind.min.css">
<script src="/ui/static/htmx.min.js" defer></script>
<script src="/ui/static/htmx-config.js" defer></script>  <!-- sets allowEval=false, configures data-* selectors -->
```

**Build step (developer machine only)**:
```bash
make build-tailwind   # runs: npx tailwindcss@3.4.1 -i ./internal/web/css/input.css \
                      #                              -o ./internal/web/static/tailwind.min.css \
                      #                              --minify --content './internal/web/templates/*.tmpl'
```

**Pinned versions** (as of 2026-06-01, latest stable on each major):
- Tailwind CSS `3.4.1`
- htmx `2.0.4`

`tailwind.min.css` is **committed** under `internal/web/static/`. CI does NOT run `npx`. The artifact ships as part of the source tree and goes into `embed.FS` at `go build` time. Net binary delta after Tailwind minify+purge against our template content: ≤ 15 KiB gzipped.

**SHA-256 hashes for vendored assets**: tracked in `internal/web/static/vendored-assets.lock` (one line per artifact: `<sha256>  <relpath>`). Pre-merge CI re-derives hashes from the in-tree bytes and fails if `vendored-assets.lock` drifts. Lock file shipped in T5; standing tracking issue opens if hash discipline ever regresses.

**Why this beats CDN** (closes R3):
- No third-party origin in CSP.
- No supply-chain risk; no SRI brittleness.
- No Referer leak of bearer to third party (combined with `Referrer-Policy: no-referrer` — defense-in-depth).
- Works airgapped on day 1.
- Build cost = one `make build-tailwind` per Tailwind version bump.

### 3.6 Auth model — HMAC token (cookie-bound) + CSRF cookie + Origin check

#### 3.6.1 Approval page (`/approve/{approval_id}`)

**Token never traverses URL path** (I1). The flow:

1. Reviewer clicks Slack/email link: `https://regatta.host/approve/redeem?t=<hmac-token>` (one-time redeem endpoint).
2. Server validates token; sets short-lived cookie `regatta_approval_token=<hmac-token>; SameSite=Strict; Secure; HttpOnly=true; Path=/approve/<approval_id>; Max-Age=int(cfg.DecisionWindow.Seconds())`; redirects 303 to `/approve/<approval_id>` (opaque ULID). **Cookie scope is per-ULID**, deliberately tighter than a `/approve/*` glob — one stolen cookie cannot replay against a sibling approval.
3. All subsequent GET/POST to `/approve/<approval_id>*` read the token from the cookie. Path now logs as `/approve/01H8...` in reverse-proxy + browser-history — opaque ULID, audit-safe.
4. Slack notifier emits message with `unfurl_links: false` so Slackbot does not preflight-GET the redeem URL.

TTL = `cfg.DecisionWindow` (existing token's `Window` field), wired to cookie as `Max-Age=int(cfg.DecisionWindow.Seconds())`. Expired → `approval_error.tmpl` with `token_expired`.

Single-use: token's `jti` consumed on POST via existing `token_consumed` event in `approval_events`. Second POST → `token_replay` page.

**Sentinel folding (HTTP layer only)**: `classifyDecideErr` in `internal/gates/approval/notify_http.go` maps both `state.ErrTokenReplay` (UNIQUE-`token_jti` constraint trip) AND `approval.ErrDoubleVote` (in-memory `DecidedBy` guard in `DecideTx`) to HTTP 409 + `token_replay` sentinel. Reason: `DecideTx`'s in-memory guard fires before the constraint when the same reviewer re-clicks a Slack button on the same approval id with two different tokens — operator-facing semantic ("your vote already counted") is identical, Slack-button retries are the dominant trigger, and a distinct `double_vote` HTTP sentinel would surface a Go-side implementation ordering detail to operators with no actionable difference. The CLI (`cmd/regatta/approval_decide.go::exitCodeFor`) keeps them distinct — `ErrTokenReplay` → exit 4, `ErrDoubleVote` → exit 1 (generic) — because terminal exit codes are stable contract surfaces grepped by runbooks. Audit trail in `approval_events` is unchanged: the underlying kind (`token_consumed` UNIQUE-trip vs no row written by the in-mem guard) reveals which path tripped.

Self-review prevention: server-side, before render. If `payload.Reviewer == approval.RequestedBy && cfg.PreventSelfReview`, error page.

View (GET) does NOT consume the token. Refresh is safe; only POST mutates state.

#### 3.6.2 CSRF defense + Origin check (S2)

- Cookie: `regatta_csrf={random-32-hex}; SameSite=Strict; Secure; HttpOnly=true; Path=/approve/<approval_id>` (per-ULID, matches token cookie scope). Value = 16 bytes from `crypto/rand` (NOT `math/rand`) hex-encoded → 32 chars. Issued on first GET after redeem.
- Form: hidden `<input type="hidden" name="csrf" value="{{.CSRFToken}}">` populated **server-side** from the cookie (template func runs in handler scope, reads cookie at render time — cookie itself is HttpOnly, JS cannot read it).
- Server validates `r.PostFormValue("csrf") == cookie.Value` via `subtle.ConstantTimeCompare`. Mismatch → 403, no audit row.
- **Plus Origin check**: server rejects POST if `Origin` header missing or `Origin != "https://" + r.Host`. `r.Host` carries the port (e.g. `localhost:8080`), so the comparison is exact-match including port — a request to `:8080` cannot be forged from `:8081`. htmx 2 sets `Origin` automatically. Curl-based attacks blocked.
- Both layers locked (open-q S2 ruling).

#### 3.6.3 DAG view + cost panel (`/runs/{run_id}*`)

- Phase-1 (W7.2/W7.3): same cookie-bound HMAC model. Operator first clicks an approval link for any gate in this run; the cookie scope widens implicitly via `Path=/` of an auxiliary `regatta_run_token` cookie set during redeem if the token's `WI → run_id` join is non-null. Open-q 9.7 ruling: strict, token-only, no localhost bypass.
- Phase-2 (W8): when RBAC ships, cookie validation swaps to tenant-scoped session. Handler signatures stay stable via the `Principal` type (§3.6.4).

#### 3.6.4 Forward-compat with W8 RBAC — `Principal` type only, no premature interface (R4)

v1 has exactly ONE auth caller: `approval.DecideTx`'s token-verify. There is no second authorizer to abstract over. Per `feedback_unaddressed_load_bearing`, abstraction without a second use-case is over-design.

**What W7 locks now** (I4) — only the type contract:

```go
// internal/web/auth.go
package web

type Principal struct {
    ID     string   // W7: payload.Reviewer; W8: session.UserID
    Tenant string   // W7: "default"; W8: session.Tenant
    Roles  []string // W7: nil; W8: session.Roles
}

func PrincipalFromRequest(r *http.Request) (Principal, error) {
    // W7 impl: read cookie, verifyToken, return Principal{ID: payload.Reviewer}.
    // W8 will swap the body of this function. The signature stays.
}
```

Handlers take `Principal` as a parameter. When W8 lands, **only** `PrincipalFromRequest`'s body changes. Roles slice expansion is semver-compat. The Authorizer interface from v1 is deleted (R4).

### 3.7 CSP / sandbox posture (R3)

```
Content-Security-Policy:
  default-src 'self';
  script-src 'self';
  style-src 'self';
  img-src 'self';
  connect-src 'self';
  frame-ancestors 'none';
  base-uri 'self';
  form-action 'self';

Referrer-Policy: no-referrer
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
```

`data:` URIs dropped from `img-src` — no v1 template uses them. Re-add (and document the use) if SVG icons ever need inline embedding. Tightening reduces attack surface (inline-image vector for content-sniff escapes).

Wire-format (single line for reverse-proxy override):
`Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'` (N2)

**No third-party origins. No `unsafe-inline`. No `unsafe-eval`.** htmx 2 runs with `htmx.config.allowEval = false` and templates use `data-hx-*` (not `hx-on::*`) for any handler attachment.

CSP middleware lives in `internal/web/csp.go`; integration test asserts header byte-equal on every route (B-rubric).

### 3.8 Embedded asset strategy

```go
// internal/web/server.go
//go:embed templates static
var assetsFS embed.FS
```

- `templates/*.tmpl` parsed at boot via `template.ParseFS`. Parse failure → server fails to start (loud).
- `static/*` served via `http.FileServer(http.FS(staticSubFS))`. Contains vendored `htmx.min.js` (~50 KiB) + compiled `tailwind.min.css` (~15 KiB gzipped after purge) + tiny `htmx-config.js`.
- Total binary size delta: ~80 KiB. Well below the noise floor.

### 3.9 Cost-panel data source (R1 + S3)

The cost panel reads **substrate `events` table** (migration `0006_substrate.sql`, per the in-flight substrate spec re-rev), filtered by `kind='token_spend' AND run_id=?`.

**v1 query** (single SUM, no time-series):

```sql
SELECT
  COALESCE(SUM(json_extract(payload_json,'$.usd_micros')), 0)        AS total_usd_micros,
  COALESCE(SUM(json_extract(payload_json,'$.input_tokens')), 0)      AS total_input_tokens,
  COALESCE(SUM(json_extract(payload_json,'$.output_tokens')), 0)     AS total_output_tokens,
  MAX(written_at)                                                     AS as_of
FROM substrate_events
WHERE kind = 'token_spend'
  AND run_id = ?;
```

USD is computed at the LLM-call site (substrate writer per `wedge_cost_governor.md`) and stored as `usd_micros` on the event payload (S3). W7 holds no model→price table. Unknown model → writer-side problem, not reader-side.

**Budget cap** (when present): join with `policies WHERE kind='budget' AND scope_kind IN ('dag','run','tenant') AND scope_id=? AND revoked_at IS NULL` ordered by `precedence_rank ASC LIMIT 1` (matches substrate-spec policy resolution order, §2.2).

**Time-series chart deferred** (R1 simplification + tracking issue cited in §9).

**Fail-soft when substrate not yet shipping**: handler checks `pragma table_info('substrate_events')`; if table missing (substrate Phase 1 not applied), render cost panel with "cost data unavailable — substrate migration `0006_substrate.sql` not applied" banner instead of 500. B-rubric item.

### 3.10 Data sources (post-substrate) — reconciliation with `2026-06-01-unified-substrate-design.md` (R5)

W7 ships during the substrate migration window. Reading legacy tables that get ripped out two waves later is the exact churn `feedback_session_2026_05_31_lessons` warns against. Explicit per-surface declaration:

| Surface | v1 source | Post-substrate-Phase-4 source | Rationale |
|---|---|---|---|
| **Approval page render** (work_item context, upstream_output, decided_by list) | `work_items` + `work_item_outputs` + `approvals` (legacy) | `substrate_events` fold (`kind='node_output'` + `kind='approval_event'`) | Approval rows are low-churn; substrate dual-write Phase 2 will shadow them. W7.4 followup migrates the read side after Phase 3 cutover. Tracking: see §9. |
| **Approval decide write** | Existing `approval.DecideTx` → `approvals` + `approval_events` (legacy tables, already shipped MVP-2 W1) | UNCHANGED — substrate Phase 2 dual-writes invisibly; W7 calls `DecideTx`, doesn't pick a table | W7 must not invent a new write path. The substrate dual-write is upstream of W7. |
| **DAG list view** (work_items + edges) | `work_items` + `work_item_edges` (legacy) | `substrate_events` fold (`kind='node_output'` for work_items; edges stay in DDL — substrate spec §2.1 keeps edges as structural metadata, not event log) | List view is a read-only render; W7.4 followup swaps the SELECT to `fold(kind='node_output')` once Phase 3 lands. |
| **Cost panel** | `substrate_events WHERE kind='token_spend'` (R1 — substrate is the **only** source; v1 has no legacy table to read from) | UNCHANGED | Brand-new surface; no legacy precedent. Blocking on substrate Phase 1 (table ships) — see §7 wave ordering. |

**Substrate-spec citation**: per `docs/superpowers/specs/2026-06-01-unified-substrate-design.md` §2.1 (event log table definition + payload validators section) the `substrate_events` table ships in migration `0006_substrate.sql` (renumbered from spec's `0005` per dispatch note) with `kind='token_spend'` payload carrying `usd_micros`, `input_tokens`, `output_tokens`, `model`, `llm_call_id`. The fold semantics in §4 of the substrate spec specify `set-union over (work_item_id, llm_call_id)` for `token_spend` — duplicates dedupe at fold time, not at SUM time. W7's single-SUM query above is correct under set-union because the substrate writer enforces idempotency at insert via the unique `(run_id, written_by, nonce)` index declared in the §2.1 event log DDL.

**W7.0 task T1 explicitly declares** "W7 depends on substrate Phase 1 (migration `0006_substrate.sql` applied)." If substrate slips, W7.3 (cost panel) blocks but W7.0/W7.1/W7.2 proceed.

## 4. Performance budget

Per #183 A+ rubric target: first-byte ≤ 50 ms on warm DB. **N=500 and N=2000 benchmark rows added per R7.**

| Page | N | Budget | Hard fail (2×) | Measurement |
|---|---|---|---|---|
| `GET /approve/{approval_id}` first byte | — | ≤ 50 ms | 100 ms | httptest TTFB benchmark, N=100 cached approvals |
| `GET /approve/{approval_id}` HTML size before assets | — | ≤ 5 KiB | 10 KiB | `wc -c` on rendered template |
| `POST /approve/{approval_id}/decide` first byte | — | ≤ 80 ms | 160 ms | benchmark, sqlite tx in budget |
| `GET /runs/{run_id}` first byte | N=50 work_items | ≤ 100 ms | 200 ms | benchmark |
| `GET /runs/{run_id}` first byte | N=500 work_items (R7) | ≤ 250 ms | 500 ms | benchmark (warn-only at first; track) |
| `GET /runs/{run_id}` first byte | N=2000 work_items (R7) | ≤ 1500 ms | 3000 ms | benchmark (warn-only; tracks degradation curve) |
| `GET /runs/{run_id}/cost` first byte | N=200 spend events | ≤ 50 ms | 100 ms | benchmark |
| `GET /runs/{run_id}/cost` first byte | N=5000 spend events (R7) | ≤ 200 ms | 400 ms | benchmark; substrate `idx_substrate_events_kind` covers it |
| `GET /healthz` first byte | — | ≤ 5 ms | 10 ms | benchmark |
| Memory per concurrent connection | — | ≤ 64 KiB | 128 KiB | `runtime.MemStats` |
| Polling burden | 10 ops × 5 s | ≤ 1% CPU | 2% | `pprof` profile under load script |
| Polling burden | 20 ops × 5 s × N=500 (R7) | ≤ 3% CPU | 6% | same |
| **Query count per `/runs/{run_id}` render** | (R7) | ≤ 2 SQL queries | 4 (hard fail) | `internal/orchestrator/state/dbtest/query_counter.go` (new W7.0 T2) wraps `state.DB`, increments counter on every `Exec`/`Query`, test fails if `>2` per render. |

**N+1 lint** (R7): static-grep rule in `Makefile` target `lint-web-nplus1`: `! grep -rE 'for[[:space:]]+_,[[:space:]]+\w+[[:space:]]+:=[[:space:]]+range[[:space:]]+\w+\.WorkItems' internal/web/`. Catches "loop over work_items doing per-row state.Get*". Pattern-narrow; false-positive accepted as a guardrail.

**Hard fail**: any benchmark exceeding 2× budget fails CI. 1.0–2.0× = warning. 2.0×+ = blocks merge.

**JS-disabled fallback**: approve/reject MUST work with JavaScript fully off. htmx degrades to `<form action method="POST">`. Verified by Playwright with `javaScriptEnabled: false`.

## 5. Test strategy

Per `feedback_tdd_discipline` — every implementer captures failing-test output before impl. Per `feedback_adversarial_review` — reviewer subagent clears every PR.

### 5.1 Unit (httptest)

One `_test.go` per handler. Each handler test asserts:
- happy-path 200 + expected fragment substrings
- auth: missing cookie → 401 (no body leak), expired → typed error page, replay → typed error page
- CSRF: missing csrf form field on POST → 403, no audit row written
- Origin check: missing/foreign `Origin` on POST → 403, no audit row (S2)
- input validation: malformed `decision` → 400
- output: response body's `<title>` + key CSS classes present
- header: `Content-Security-Policy` present + matches §3.7 verbatim (regex match); `Referrer-Policy: no-referrer` present (R3)

### 5.2 Property (rapid or gopter)

- `verifyToken` round-trip: random reviewer mint → decode → assert `payload.Reviewer` byte-equal. Re-uses `internal/canon` property tests.
- CSRF cookie/form match: random 32-hex → form input → handler accepts; flip one byte → handler rejects.
- `_diff.tmpl` escape: random `[]DiffLine` (≥1000 cases) → render → assert no `<script>` substring not present in input verbatim (§8 edge case 2).
- `_diff.tmpl` 8 KiB cap (I2): input >> 8 KiB → output ≤ 8192 bytes + contains "Show full diff →" overflow link.

### 5.3 E2E (Playwright CLI out-of-process)

Two specs:
- `approval_happy.spec.ts` — server up; mint token via test helper; navigate to redeem URL; assert redirect to `/approve/<ULID>`; click Approve; assert `approval_decided.tmpl` with principal echo + peer list (I6); assert `approval_events` row count via test-only `/test/state` endpoint (gated behind `--test-endpoints` flag).
- `approval_nojs.spec.ts` — `javaScriptEnabled: false` context; assert approve flow still works as plain HTML form.

**Network-egress disabled** (I5): Playwright container runs with `--no-net` (or equivalent); test must work entirely offline because Tailwind is vendored (R3 alignment). `--ui-vendor-css=true` flag forces the test server to refuse any non-`/ui/static/` asset path. B-rubric: "Playwright E2E passes with zero network egress."

Run via `make e2e-ui`; opt-in. CI nightly on Linux + macOS. Local dev: one-time `npm i -g playwright && playwright install chromium`. If missing, `t.Skip()` with clear "install playwright" message.

### 5.4 Visual regression (deferred)

A+ target only. Snapshot rendered HTML via `golden.txt`. Tracking issue if not in initial scope.

## 6. Grade rubric (B / A / A+)

Per `feedback_grade_rubric`. Tool-checkable, distinct per tier.

### B — floor (ships)

- [ ] All 8 routes in §3.3 implemented + httptest pass
- [ ] Approval happy-path Playwright E2E green on Linux + macOS **with zero network egress** (I5)
- [ ] Approval no-JS-fallback Playwright E2E green
- [ ] `regatta serve` boots with `--ui=true` default; `--ui=false` skips listener entirely (integration test asserts no `LISTEN` bind)
- [ ] `regatta serve --ui=true` without `REGATTA_HMAC_KEY` set fails boot non-zero with clear error (open-q 9.8 ruling)
- [ ] CSP header present + matches §3.7 verbatim (regex test); `Referrer-Policy: no-referrer` present (R3)
- [ ] No third-party origins in CSP (`! grep -E 'https://' internal/web/csp.go` plus byte-equal header test) (R3)
- [ ] HMAC token verify reuses `internal/canon` (lint asserts no new auth primitive)
- [ ] No JS framework in `go.mod` deps; htmx + Tailwind vendored under `internal/web/static/` (lint)
- [ ] `Makefile` `lint-web-template-html` target wired into `make check` and passes (I3)
- [ ] `Makefile` `lint-hx-sync` target wired into `make check` and passes (I3)
- [ ] `Makefile` `lint-web-nplus1` target wired into `make check` and passes (R7)
- [ ] Cost-panel handler fails-soft if `substrate_events` table absent (banner, no 500) (§3.9)
- [ ] `make check` clean + `make bench-ui` shows all routes within 2× budget for N=50; N=500 measured (warn-only OK)
- [ ] Query-counter middleware (`internal/orchestrator/state/dbtest/query_counter.go`) ships + `/runs/{run_id}` render asserted ≤ 2 SQL queries (R7)

### A — target (expected)

All B, plus:
- [ ] Approval error pages for every typed sentinel (token_invalid, expired, replay, unknown_key, not_reviewer, self_review) — one test per sentinel
- [ ] CSRF double-submit-cookie + Origin-header check both wired (S2); ≥100 randomized form/cookie property cases
- [ ] DAG-list filter form (state + lane) round-trips through htmx swap (E2E)
- [ ] Cost panel partial renders without JS (curl + grep test, no browser)
- [ ] `_diff.tmpl` 8 KiB cap + overflow `/approve/{approval_id}/diff` route both shipped, property-tested (I2)
- [ ] `approval_decided.tmpl` echoes principal + names pending peers (I6); property test on every quorum state
- [ ] N=500 work_items benchmark within 2× budget (warn-only if 1–2×, hard-fail if >2×)
- [ ] Adversarial reviewer subagent cleared the PR with zero unaddressed Risk-tier findings (per `feedback_agent_pr_review`)
- [ ] Tracking issues filed for all §9 followup items + cited by number in PR body (per `feedback_unaddressed_load_bearing`)

### A+ — stretch (aspirational)

All A, plus:
- [ ] First-byte ≤ 50 ms verified by `make bench-ui` (not just 2×=100 ms; the spec target)
- [ ] HTML payload ≤ 5 KiB verified by size assertion
- [ ] Property test on `_diff.tmpl` ≥1000 cases (covered in A — promote here only if mutation-coverage ≥95%)
- [ ] Visual regression golden-file test (3 templates × 1 theme = 3 goldens; multi-theme deferred)
- [ ] Operator MTTF screencast (60 s, never-seen-regatta SRE completes one approval) → `docs/ux/2026-06-w7-mttf-screencast.md`
- [ ] Lighthouse / axe-core accessibility score ≥95 (run in Playwright)
- [ ] Zero magic numbers in `internal/web/*.go` — all timeouts/sizes/budgets named in `const.go`
- [ ] N=2000 work_items benchmark within 2× budget

## 7. Implementation plan — 4 waves, 14 file-disjoint tasks (R2)

Per `feedback_parallel_dispatch` — every task is file-disjoint where possible.

### Wave 7.0 — HTTP listener + shared primitives (PREREQ, R2) — 3 tasks

| # | Task | Files touched | Spawn-disjoint? |
|---|---|---|---|
| **T1** | Boot `net/http.Server` in `cmd/regatta/serve.go`; `--addr` + `--ui` flags; graceful shutdown wired to existing `serve` lifecycle (signal handling); fail-boot if `--ui=true && REGATTA_HMAC_KEY=""`; wire concrete `InteractiveNotifier.CallbackRoute()` impl at `/api/approval/callback` calling into `approval.DecideTx` | `cmd/regatta/serve.go`, `cmd/regatta/serve_test.go` (new integration test asserts listener binds + `--ui=false` skips bind), `internal/gates/approval/notify_http.go` (new — `CallbackRoute()` concrete impl) | OWNER per `feedback_shared_primitive_owner` |
| **T2** | Query-counter test middleware (R7) | `internal/orchestrator/state/dbtest/query_counter.go`, `query_counter_test.go` | ✓ |
| **T3** | Lift `decideTx` from `cmd/regatta/approval_decide.go` → `internal/gates/approval/decide.go`. Pure refactor: CLI now calls `approval.DecideTx`. Establishes shared seam. | `cmd/regatta/approval_decide.go`, `internal/gates/approval/decide.go`, `decide_test.go` | OWNER; T6/T8 importers wait |

### Wave 7.1 — HTTP scaffold + approval flow — 4 tasks

| # | Task | Files touched | Spawn-disjoint? |
|---|---|---|---|
| T4 | `internal/web/server.go` + `render.go` + `health.go` + `const.go` + `csp.go` — http.Handler factory, route mux, embed.FS, template loader, `/healthz`, CSP middleware (R3 — Referrer-Policy too); static asset serving | `internal/web/server.go`, `render.go`, `health.go`, `const.go`, `csp.go`, `csp_test.go`, `templates/layout.tmpl`, `templates/_flash.tmpl`, `static/htmx-config.js` | ✓ |
| T5 | Tailwind vendoring + build step (R3) — `internal/web/css/input.css`, `internal/web/static/tailwind.min.css` (committed output), `Makefile` `build-tailwind` target | `internal/web/css/input.css`, `internal/web/static/tailwind.min.css`, `internal/web/static/htmx.min.js` (vendored), `Makefile` (build-tailwind target) | ✓ |
| T6 | `internal/web/approval.go` — GET `/approve/redeem`, GET `/approve/{approval_id}`, POST `/approve/{approval_id}/decide`, GET `/approve/{approval_id}/diff` handlers + `approval.tmpl` + `approval_decided.tmpl` (I6) + `approval_error.tmpl` + `approval_diff.tmpl` + `_diff.tmpl` with 8 KiB cap (I2) | `internal/web/approval.go`, `approval_test.go`, `templates/approval*.tmpl`, `templates/_diff.tmpl` | depends on T3 |
| T7 | `internal/web/auth.go` — token cookie-bound flow (I1), `Principal` type (I4), CSRF + Origin middleware (S2), HttpOnly cookies | `internal/web/auth.go`, `auth_test.go` | depends on T4 (csp.go must exist) |

### Wave 7.2 — DAG list view — 3 tasks

| # | Task | Files touched | Spawn-disjoint? |
|---|---|---|---|
| T8 | `internal/web/runs.go` — `GET /runs/{run_id}` handler + `templates/runs.tmpl` + DB query for work_items + edges (≤2 queries enforced via T2 middleware) — HTML list only (S1) | `internal/web/runs.go`, `runs_test.go`, `templates/runs.tmpl` | depends on T4+T2 |
| T9 | Filter form (state + lane) + htmx polling + query params + `hx-sync` (I3 — addresses §8 #7) | same files as T8; sequential | depends on T8 |
| T10 | E2E Playwright spec for DAG list (load run, filter, assert poll cycle, no-egress) + golden HTML | `tests/e2e/playwright/dag_view.spec.ts`, `internal/web/runs_golden_test.go`, `testdata/runs_golden.html` | depends on T8 |

### Wave 7.3 — Cost panel + Playwright E2E + perf — 4 tasks

| # | Task | Files touched | Spawn-disjoint? |
|---|---|---|---|
| T11 | `internal/web/cost.go` — `GET /runs/{run_id}/cost` partial + `templates/runs_cost.tmpl` — substrate `events WHERE kind='token_spend'` SUM aggregator (R1 + S3); fail-soft if substrate table absent | `internal/web/cost.go`, `cost_test.go`, `templates/runs_cost.tmpl` | depends on T4; blocked on substrate Phase 1 if cost-panel render needed |
| T12 | Playwright happy-path approval E2E + JS-disabled fallback (I5: zero network egress) | `tests/e2e/playwright/approval_happy.spec.ts`, `tests/e2e/playwright/approval_nojs.spec.ts`, `Makefile` `e2e-ui` target | depends on T6 |
| T13 | Perf benchmarks (`make bench-ui`) — N=50, N=500, N=2000 (R7); A+ checks (first-byte, payload size, zero-magic-numbers lint) | `internal/web/server_bench_test.go`, `internal/web/const.go` extensions, `Makefile` `bench-ui` target | depends on T4+T6+T8+T11 |
| T14 | Lint targets: `lint-web-template-html` (I3), `lint-hx-sync` (I3), `lint-web-nplus1` (R7); wire into `make check`; new `tools/lint-hx-sync/main.go` AST walker | `Makefile`, `tools/lint-hx-sync/main.go`, `tools/lint-hx-sync/main_test.go` | ✓ |

**Total: 14 file-disjoint tasks across 4 waves.** Each wave is independently mergeable. W7.0 + W7.1 alone is a B-grade deliverable (approval flow only); W7.2 + W7.3 layer on the DAG list + cost panel.

**Owner declarations** (per `feedback_shared_primitive_owner`): T1 owns the listener; T2 owns query-counter; T3 owns the lifted DecideTx. T4 (web server scaffold) waits for T1+T3.

## 8. Adversarial red-team (8 edge cases — must be addressed before merge)

Per `feedback_adversarial_review`. Each item below must either land a regression test or a tracking issue cited in the PR body before merge.

1. **Token replay across UI + CLI**. Reviewer clicks the Slack link (web POST), then races to paste `regatta approval decide` in a terminal. The `UNIQUE(approval_id, kind, token_jti)` index on `approval_events` (per existing `insertApprovalEvent`) is the single source of truth: whichever transaction commits second sees `ErrTokenReplay`. Web → `token_replay` error page, CLI → exit code 4. Regression test: spawn web POST + CLI in parallel goroutines, assert one succeeds + one returns `state.ErrTokenReplay`.

2. **XSS via diff render**. Operator-controlled upstream output flows into `_diff.tmpl`. Go's `html/template` auto-escapes; future `template.HTML` slip = bypass. **Mitigation**: property test in `_diff_test.go` — random `[]DiffLine` inputs ≥1000 cases, assert no `<script>` substring not in input verbatim. **CI lint** (I3): `Makefile` `lint-web-template-html` (`! grep -rE 'template\.HTML' internal/web/`) wired into `make check`.

3. **CDN compromise** — **N/A in v2** (R3). Tailwind + htmx are vendored at build time, served from `embed.FS`. Zero third-party origins in CSP. `Referrer-Policy: no-referrer` adds defense-in-depth. Tracking issue: SRI on future external assets only if scope ever re-opens that door.

4. **DAG-list N+1 query blowup** (R7). Naive `runs.go` could fetch work_items then per-item edge queries. **Mitigation**: single query for work_items + single query for edges (`SELECT * FROM work_item_edges WHERE from_id IN (SELECT id FROM work_items WHERE run_id=?)`). Test asserts ≤2 SQL queries via T2 query-counter middleware. **CI lint**: `lint-web-nplus1` Makefile target.

5. **Cost-panel stale-cache lies to operator** (R6). v1 cache: **`Cache-Control: no-store` on `/runs/{run_id}/cost`**. The 2 s cache from v1 is gone. Load is trivial — single indexed SUM under `idx_substrate_events_kind`. Approval-page decision input cannot tolerate even a 2 s lie. Tracking issue: SSE push when substrate ships an event-bus.

6. **Slow browser rendering** — **N/A in v2**. Tailwind is pre-compiled + purged; no JIT in browser. First-paint cost = network + parse only, well under 30 ms.

7. **htmx swap-target collision**. Two `hx-trigger` poll targets with same destination race. **Mitigation**: `hx-sync="this:replace"` on every polling target. **CI lint** (I3): `Makefile` `lint-hx-sync` Go AST walker (`tools/lint-hx-sync/main.go`) fails on any `hx-trigger` containing `every` without sibling `hx-sync`.

8. **Approval race: web Approve + CLI Approve same reviewer**. Both call `approval.DecideTx`; first commits, second sees `ErrTokenReplay`. Covered by edge case 1 — no new test needed.

**Plus (closed by I1 + I3 + R3 in §3, no separate red-team row needed)**:
- Bearer-token leak via URL path → cookie-bound token (I1).
- Bearer-token leak via Referer to CDN → CDN dropped + `Referrer-Policy: no-referrer` (R3).
- Slack unfurl-bot preflight → notifier emits `unfurl_links: false` (I1).

**Deferred-OK**:
- 9. **Reverse-proxy header injection**. If operator runs regatta behind nginx without `X-Forwarded-Host`, the same-origin form-action could fail. Doc-only in v1: `docs/operator/reverse-proxy.md` + tracking issue for `--public-url` flag.

## 9. Tracking issues to file at implementation time

Per `feedback_unaddressed_load_bearing` — file before merge, cite by number in PR body.

- [ ] **W7.4 followup — SVG/graph DAG render** (S1, R5 partial)
- [ ] **W7.4 followup — DAG list sort UI** (S4)
- [ ] **W7.4 followup — cost-panel time-series chart** (R1 simplification)
- [ ] **W7.4 followup — read approvals + work_items + edges from substrate fold after Phase 4** (R5)
- [ ] **W7.4 followup — OTel-span-per-route + W6 tracer integration** (S5; was A+ in v1)
- [ ] **`--public-url` flag for reverse-proxy deployments** (red-team #9)
- [ ] **SSE upgrade path for real-time DAG / cost updates** (R6 mitigation)
- [ ] **Plan-authoring UI — explicit "no, never in this wedge" decision**; redirect to W7-vNext only if a real user asks
- [ ] **Multi-tenant UI scope** (depends on W8 OPA RBAC wedge)
- [ ] **Visual regression goldens for multi-theme** (A+ deferred)

Each annotated `→ file as gh issue, cite as "Tracking: #NNN" in PR body before merge` (N3).

## 10. References

- Brief: `docs/superpowers/briefs/2026-05-31-mvp-3-next-level.md` §4 W7 + §6 #2
- Umbrella issue: #183
- v1 spec + adversarial review: `docs/superpowers/specs/2026-06-01-w7-operator-web-ui-design.md` (this file, prior version), `docs/superpowers/reviews/2026-06-01-w7-operator-ui-review.md`
- **Unified substrate spec: `docs/superpowers/specs/2026-06-01-unified-substrate-design.md` §2.1 + §4** (N4 — cited inline at §3.9 + §3.10)
- W6 OTel backbone spec: `docs/superpowers/specs/2026-05-31-mvp-3-w6-otel-backbone.md`
- Approval-gates wedge: `docs/wedges/approval-gates.md`
- Approval state machine: `internal/gates/approval/*` (gate.go, config.go, notify.go, fold.go, reaper.go)
- CLI approve flow: `cmd/regatta/approval_decide.go`
- Daemon entry: `cmd/regatta/serve.go` (HTTP listener added in W7.0)
- htmx CSP guide: https://htmx.org/docs/#csp
- Memory: `feedback_decision_priority`, `feedback_grade_rubric`, `feedback_research_design_principles`, `feedback_adversarial_review`, `feedback_simplify_reviewer`, `feedback_spec_pattern_authority`, `feedback_shared_primitive_owner`, `feedback_unaddressed_load_bearing`, `feedback_verify_before_asking`, `feedback_session_2026_05_31_lessons`
