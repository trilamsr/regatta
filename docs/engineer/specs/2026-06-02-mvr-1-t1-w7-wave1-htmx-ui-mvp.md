---
title: "MVR-1 T1 W7 Wave 1 — htmx operator dashboard MVP"
status: active
summary: "First external-facing UI surface. Read-only 5-panel dashboard (active agents, in-flight PRs, recent merges, today's cost, green-clock progress) served by `regatta serve --ui-addr`. htmx 2.0.4 + Pico CSS 2.0.6 + Go html/template, all vendored under `internal/web/ui/`. 5s polling, no SSE, no write paths, no auth (localhost-bind default). One dedicated read-only sqlite WAL connection. Mutations stay CLI-only."
---

# MVR-1 T1 — W7 Wave 1 htmx UI MVP (design spec)

_Author: design subagent, 2026-06-02. Item: `.regatta/items/mvr-1-t1-w7-wave1-htmx-ui-mvp.md`. Source-of-truth: `docs/engineer/specs/2026-06-02-next-horizon-roadmap.md` #433 §4 MVR-1 T1. Background context (NOT source-of-truth): prior W7 design at `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` — that spec covers the larger approval-flow + cost-reset surface and remains valid for W7 Wave 2/3. This spec carves off a strictly smaller read-only dashboard so the first external-customer-facing UI ships in days, not weeks._

## 1. Problem

The MVR-1 T1 item exists because there is currently no surface — CLI or otherwise — from which a non-engineer operator can answer the four questions a customer-0 ops shift will ask in the first 5 minutes of using regatta:

1. What agents are running right now?
2. Which PRs are in flight from those agents?
3. What merged recently?
4. How much did today cost, and how close are we to the 30-day green-clock target?

Today operators answer (1)–(4) by stringing together `gh pr list`, `regatta status`, `sqlite3 substrate.db` queries, and W5 cost-reader CLI invocations. That is acceptable for a single internal operator with the binary in `$PATH`. It is not acceptable for the first external persona-A maintainer, who is the gate this item closes (per item `gate: mvr-1-entry`).

The W7 Wave 1 MVP cut is the smallest UI that turns those four questions into a single browser tab. Approvals and cost-cap-reset (the write paths in the parent item) ship in Wave 2 against the existing larger W7 design — they are explicitly out of scope here so the first surface can land behind a single read-only handler set and a single dedicated sqlite connection. Per the next-horizon roadmap §4 MVR-1 T1 dispatch list, this wedge is rank 1 of the four MVR-1 wedges and effort-bounded to days (Wave 1 alone), not the parent item's 3–5 wk band.

Citation chain:
- Roadmap: `docs/engineer/specs/2026-06-02-next-horizon-roadmap.md` §4 MVR-1 T1 (PR #433) + §11 dispatch list rank 1.
- Item: `.regatta/items/mvr-1-t1-w7-wave1-htmx-ui-mvp.md` acceptance criteria c3 (5s polling home page) and the home-page surface specifically; this spec implements the read-only home-page slice of c3 and DEFERS c1 + c2 (approval queue + reset-cap action) to Wave 2.
- Prior W7 design (background only, not authority): `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` §1.1 surface #2 (read-only list view) and #3 (cost panel) are the lineage this dashboard descends from. Approvals (§1.1 #1) stay out per Wave 1 scope.

## 2. Scope — MVP

### 2.1 In scope

Single page `GET /ui` with 5 server-rendered panels, htmx polling each panel every 5 s, served from one in-process listener on `regatta serve --ui-addr <addr>`. All five panels render even if their upstream subsystem is missing or returning errors — the panel shows an `EMPTY` or `MISSING` state and the rest of the page keeps refreshing.

The 5 panels:

| # | Panel | Upstream data | Update cadence | Empty-state |
|---|---|---|---|---|
| P1 | Active agents | `internal/orchestrator/state` agent table via dedicated read-only WAL conn | `hx-trigger="every 5s"` | `no agents` |
| P2 | In-flight PRs | `gh pr list --json` cached 5 s in-process | `hx-trigger="every 5s"` | `no open PRs` or `gh not on PATH` |
| P3 | Recent merges | Substrate `events WHERE kind='pr_merged' ORDER BY ts DESC LIMIT 20` | `hx-trigger="every 5s"` | `no merges in window` |
| P4 | Today's cost | W5 spend reader sum over current UTC day | `hx-trigger="every 5s"` | `cost subsystem unavailable` |
| P5 | Green-clock progress | W3 trigger-clock counter — last 30 days of green/red day flags | `hx-trigger="every 30s"` | `clock not yet seeded` |

Each panel is its own handler returning a HTML fragment (not a full document). The base page (`GET /ui`) returns the 5 slotted `<section>` containers; htmx swaps each section independently on its own timer (out-of-band swap not required at this size — independent `hx-get` per section suffices).

### 2.2 Listener

- Flag: `regatta serve --ui-addr <addr>`. Default value: `localhost:8079`. Absent flag → UI listener does not bind at all (zero-port behavior, identical to today). The flag is the ONLY enable path — no env var, no config-file key in Wave 1.
- Bind validation: if `<addr>` resolves to a non-loopback host AND `--ui-allow-public` is not also set, log WARN at boot but proceed (operator escape hatch). When `--ui-allow-public` is set, log INFO with the bound address. The two-flag pattern means a typo cannot silently expose the dashboard.
- Auth: NONE in Wave 1. Same posture as `/healthz` today (network-boundary protected). Mutations are CLI-only (see §2.3) so the dashboard is read-only by construction; the only data exposure is the operator's own loop state on their own machine.

### 2.3 Out of scope (Wave 1)

Everything below is explicitly deferred, with the wave or wedge that owns it named so reopen-trigger is unambiguous:

- **Approval queue / HITL decide** → Wave 2 against existing `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` §1.1 surface #1. (Item c1.)
- **Cost-cap reset button** → Wave 2 against the same prior W7 design. (Item c2.)
- **SSE / WebSocket streaming** → Phase X. htmx polling at 5 s is sufficient for a dashboard refresh; SSE adds a long-lived connection class without a load-bearing UX win at this surface.
- **DAG visualization** → W7 Wave 3 (graph render was already deferred from Wave 1 of the prior W7 design — S1 in that spec's v2 revision notes).
- **Reviewer-rich PR UI** → W7 Wave 3 (the parent W7 design's reviewer comment surface).
- **User auth / multi-tenant** → W8 (OPA RBAC wedge).
- **Mobile-optimized layout** → Wave 2. Wave 1 ships responsive enough that the table panels collapse under 480 px (Pico CSS default behavior) but is not designed mobile-first.
- **Telemetry / OTel spans per HTTP handler** → W7 Wave 3 (matches prior W7 design's S5 deferral).
- **Mutations of any kind** (kill agent, rerun, manual merge) — Wave 1 is strictly read-only. The operator keeps the CLI as the write path. This is a deliberate constraint, not an oversight: it cuts the auth question entirely.

## 3. Architecture

### 3.1 Package layout

```
internal/web/ui/
  handler.go            // mux + per-panel handlers
  templates.go          // embed.FS + parsed *template.Template at boot
  cache.go              // per-panel 5s in-process cache (sync.Map + ts)
  conn.go               // dedicated read-only sqlite WAL connection helper
  static.go             // embed.FS for htmx.min.js + pico.min.css + favicon
  templates/
    index.html          // base layout, 5 partial slots
    partials/
      agents.html
      prs.html
      merges.html
      cost.html
      greenclock.html
  static/
    htmx.min.js         // vendored 2.0.4 (Apache 2)
    pico.min.css        // vendored 2.0.6 (MIT)
    favicon.svg         // 1 KB inline SVG
```

### 3.2 Listener wiring

`cmd/regatta/serve.go` gains:

- New flag block: `--ui-addr string` (default `localhost:8079`) and `--ui-allow-public bool` (default `false`).
- New goroutine: `if uiAddr != "" { go web/ui.Serve(ctx, cfg) }` — Serve returns on ctx.Done; `regatta serve` shutdown sequencing closes the listener before the orchestrator stops (so the dashboard does not show stale "running" state during shutdown).

The UI listener is its own `*http.Server` instance — NOT mounted on the existing `/healthz` listener. Separation lets the UI bind on a different port (default `:8079`) than the operational health endpoint (today `:8081`) and lets ops disable the UI without losing health probes. (The `serve.go` flag landed in W7.0 of the prior W7 design; this spec extends the same flag set rather than introducing a parallel one.)

### 3.3 Read-only sqlite connection

Per W3 supervisor pattern (`internal/orchestrator/state/`), the dashboard opens **one** dedicated `*sql.DB` with `?mode=ro&_journal_mode=WAL&_query_only=1&_busy_timeout=2000` against the same `state.db`. The connection pool size is pinned to `MaxOpen=4, MaxIdle=2` so a stuck panel handler cannot starve other panels.

Rationale for a dedicated conn rather than borrowing the main pool: panel queries are bursty (5 per 5 s = 1 qps steady, 5 qps during a refresh storm if every panel coincides). Sharing the main pool risks contention with the orchestrator's transaction-heavy write path. Per `_query_only=1`, sqlite rejects any accidental write at the engine level — defense in depth on top of the read-only handler design.

### 3.4 Static assets

Vendored, not CDN:

- `htmx.min.js` — htmx 2.0.4, Apache 2.0 license, SRI sha256 pinned in `internal/web/ui/static.go` constant. Source: `https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js`. Vendor task: `make vendor-htmx` runs `curl + sha256sum` and fails if the hash drifts.
- `pico.min.css` — Pico CSS 2.0.6, MIT license, SRI sha256 pinned. Source: `https://github.com/picocss/pico/releases/tag/v2.0.6`. Vendor task: `make vendor-pico`.
- Both served with `Cache-Control: public, max-age=86400, immutable` and an ETag derived from the embedded content hash (computed once at `init()`).

CSP header on every UI response:

```
default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'
```

`frame-ancestors 'none'` blocks clickjacking on the eventual write surfaces (Wave 2) — set day 1 so the policy never has to soften.

### 3.5 Template precompilation

All templates parsed once at process boot via `template.Must(template.ParseFS(...))`. Parse errors fail boot (loud-at-boot, same posture as `REGATTA_HMAC_KEY` missing in the prior W7 design). Hot-reload is NOT a feature; operators restart for template changes.

### 3.6 Per-panel cache

Each panel handler wraps its upstream call in a 5 s TTL cache keyed by panel name. The cache is process-local (`sync.Map[string]cachedFragment`), zero-deps, and serves stale content while a refresh is in flight if the upstream is slow (single-flight guard via `golang.org/x/sync/singleflight`, already in `go.sum`). This caps load at one upstream call per panel per 5 s regardless of how many browser tabs are open against the dashboard.

## 4. Templates per panel

### 4.1 `index.html` (base layout)

- `<head>`: title, CSP via meta (defense in depth — handler also sets the header), Pico CSS link, htmx script tag with `defer`.
- `<body>`: single `<main class="container">` with 5 `<section>` slots, each carrying `hx-get="/ui/p/<name>" hx-trigger="every 5s" hx-swap="innerHTML"`.
- Initial render: server inlines each panel's first fragment so the page is fully rendered before htmx boots — no FOUC, no "loading" flash. Subsequent updates are htmx swaps.

### 4.2 `partials/agents.html`

Table with columns: name, lane, started, last-event, state-badge. Sorted by `started DESC`. Empty: `<p>no agents</p>`.

### 4.3 `partials/prs.html`

Table with columns: number, title (truncated 60 chars), author, branch, age (humanized), checks-state. Source: `gh pr list --json number,title,author,headRefName,createdAt,statusCheckRollup --limit 50` invoked via `os/exec`, cached 5 s. If `gh` is not on PATH → empty-state shows `gh not on PATH; install GitHub CLI to populate this panel`.

### 4.4 `partials/merges.html`

Substrate query: `SELECT ts, json_extract(payload_json,'$.pr_number'), json_extract(payload_json,'$.title') FROM events WHERE kind='pr_merged' ORDER BY ts DESC LIMIT 20`. Table with columns: when (humanized), PR#, title. Empty: `<p>no merges in window</p>`.

### 4.5 `partials/cost.html`

Calls the W5 cost reader (per `docs/engineer/specs/2026-06-02-phase-autonomy-w5-cost-cap-autonomic-enforcement.md` reader interface) for `sum(usd_micros) WHERE ts >= start_of_today_in_operator_tz`. Renders: today's spend in USD (2 decimals), cap if set, and a `<progress>` bar. Empty / W5 missing: `<p>cost subsystem unavailable</p>`. No reset button (Wave 2).

### 4.6 `partials/greenclock.html`

30-day grid (6 rows × 5 cols, oldest top-left). Each cell is `<span class="day green|red|none" title="2026-05-13">▪</span>`. Source: W3 trigger-clock state (`docs/engineer/specs/2026-06-02-phase-autonomy-w3-service-supervisor.md` exposes a `Last30() []DayFlag` accessor — if that accessor is not yet wired, the panel renders all-none and shows `<p>clock not yet seeded</p>`). 30 s refresh cadence (not 5 s) because the underlying counter changes at most once per day.

## 5. Operator UX

The intended cold-path experience:

1. `regatta serve --ui-addr :8079` (or just `regatta serve` if the default is acceptable).
2. Operator opens `http://localhost:8079/ui` in a browser.
3. Page paints in under 100 ms TTFB (server-render of all 5 inline panels + one Pico CSS file + htmx script, total wire payload under 110 KB gzipped).
4. Each panel updates independently on its own 5 s (or 30 s for greenclock) timer.
5. If a subsystem is missing on first boot (W3 not yet seeded, W5 reader not wired, `gh` not installed), the page still paints — only the affected panels show their `MISSING` / `EMPTY` text.

The page has no navigation, no settings, no login. It is one screen. The operator's mental model: "this is the regatta dashboard, period."

## 6. Performance budget

Per-panel handler p95 budgets (measured at N=500 events in substrate, N=50 PRs returned by `gh`):

| Panel | p95 budget | Rationale |
|---|---|---|
| Agents | 20 ms | Indexed sqlite lookup, <100 rows expected. |
| PRs | 50 ms | `gh pr list` is the upstream; cached 5 s server-side. |
| Merges | 20 ms | Indexed sqlite, LIMIT 20. |
| Cost | 30 ms | Substrate scan over today's events; `kind` index assumed. |
| Greenclock | 10 ms | 30-row read against the clock counter table. |

Whole-page TTFB on cold load: < 100 ms p95. After cache warm-up (post first 5 s): < 30 ms p95 per panel.

Static asset total wire weight gzipped: htmx (~14 KB raw → ~6 KB gzip) + Pico (~80 KB raw → ~15 KB gzip) + page HTML (~10 KB raw → ~3 KB gzip) = ~24 KB on the wire for first load. Subsequent loads hit the ETag.

## 7. Security

- **Default-localhost bind** (`localhost:8079`). Operator must pass BOTH a non-loopback `--ui-addr` AND `--ui-allow-public` to expose the dashboard externally — two-flag pattern means accidental binding requires two affirmative steps, not one.
- **CSP strict** as specified in §3.4. Locks the page against third-party script/style/image origins from day 1.
- **No JavaScript except vendored htmx**. SRI-pinned hash in `internal/web/ui/static.go`. `htmx.config.allowEval = false` set in an inline `<meta name="htmx-config">` tag.
- **html/template auto-escape only**. The codebase already forbids `template.HTML` via the `lint-web-template-html` Makefile target from the prior W7 design (I3 in v2 revision notes). Re-uses that lint — does not introduce a new gate.
- **Read-only sqlite connection** (§3.3) — `_query_only=1` enforced at the engine layer.
- **No write paths in Wave 1.** The handler set has zero `POST`/`PUT`/`DELETE` routes. The router rejects non-GET methods on `/ui/*` with a 405. This means no CSRF surface, no token surface, no auth surface — those questions defer to Wave 2.
- **Referrer-Policy: no-referrer** + **X-Content-Type-Options: nosniff** + **X-Frame-Options: DENY** set on every UI response.

## 8. Risks (Risk-tier addressed below; full adversarial review in §13)

| # | Risk | Mitigation | Tier |
|---|---|---|---|
| R1 | htmx "swap-bombs": server returns malformed HTML → page corrupts mid-swap. | Templates parsed at boot (fails boot on parse error); handler runs `html/template` execution, which itself rejects invalid output; integration test fuzzes substrate event payloads to confirm no template execution path produces unparseable fragments. | Risk |
| R2 | Polling load: 5 panels × 5 s × N tabs × M operators → unbounded multiplication. | Server-side cache (§3.6) caps upstream calls at 5 panels / 5 s regardless of tab count. Single-flight guard suppresses thundering herd if cache expires mid-storm. | Risk |
| R3 | Sqlite read-lock contention with main orchestrator write pool. | Dedicated read-only WAL conn (§3.3) with pinned MaxOpen=4. WAL mode allows readers + writer concurrency without locking. | Important |
| R4 | Cold start: subsystems missing (W3 not seeded, W5 not wired, `gh` not installed) → page fails to render. | Per-panel MISSING/EMPTY state, panel errors isolated to that panel via per-handler recover() + degraded fragment. Page always renders. | Risk |
| R5 | htmx CVEs land between Wave 1 ship and customer install. | Pinned version 2.0.4 with SRI sha256 in `static.go`. `make vendor-htmx` re-runs hash check on every rebuild; mismatch fails build. Quarterly bump cadence in followup tracking issue. | Risk |
| R6 | `0.0.0.0` bind without auth = open dashboard. | Two-flag pattern (`--ui-addr` + `--ui-allow-public`) plus WARN log on bind. Single-flag misuse is blocked at the listener-validation layer. | Risk |
| R7 | User-agent fingerprint leakage if operator's browser hits a non-self origin. | CSP `connect-src 'self'` blocks all outbound XHR/fetch. No telemetry / no analytics / no third-party fonts. Documented in §7. | Important |
| R8 | Stylesheet 80 KB on slow networks (sat link operators). | Pico CSS gzips to ~15 KB; ETag'd; cached 86400 s. If a customer complains, swap for a custom 5 KB hand-rolled subset in a Wave 2 followup. Tracking issue filed at merge. | Important |
| R9 | Locale / timezone for cost panel — UTC vs operator TZ shows wrong "today." | Cost handler reads `cfg.OperatorTZ` (already a regatta.yaml key) and computes start-of-today in that zone. Default `UTC`. Test fixture: spend at 2026-06-02 23:59 UTC must show under "today" for `OperatorTZ=America/Los_Angeles`. | Risk |
| R10 | Multi-instance same host → port collision on `:8079`. | Listener.Listen returns the `EADDRINUSE` error; `regatta serve` exits non-zero with the bind error verbatim (no swallow). Doc note in `regatta serve --help`: "to run multiple instances, pass different `--ui-addr` per instance." | Important |
| R11 | `gh` binary not on PATH for the PR panel. | Panel renders EMPTY with `gh not on PATH; install GitHub CLI to populate this panel`. No retry storm — failure is sticky for the 5 s cache window. | Important |
| R12 | Long PR title or substrate event payload → row blows out layout. | Server-side truncate at 60 chars for PR titles, 80 for merge titles. `text-overflow: ellipsis` via Pico defaults as visual fallback. | Important |

## 9. Test plan + test names

Tests live alongside the package at `internal/web/ui/*_test.go`. All test functions get 1-line godocs per `feedback_test_godoc_one_line`.

```
// TestServe_BindsAndServesIndex confirms --ui-addr brings up the listener and GET /ui returns 200 with all 5 panels inlined.
// TestServe_AbsentFlagDoesNotBind confirms absence of --ui-addr leaves no listener bound on any port.
// TestServe_PublicBindRequiresAllowPublicFlag confirms non-loopback addr without --ui-allow-public WARNs but proceeds.
// TestPanelAgents_EmptyStateRendersWhenZeroRows confirms agents panel renders "no agents" fragment cleanly.
// TestPanelPRs_MissingGhBinaryFallsBackToEmpty confirms PR panel returns EMPTY fragment when `gh` is not on PATH.
// TestPanelMerges_ReturnsLast20FromSubstrate confirms substrate query returns the expected 20 rows in DESC order at N=500.
// TestPanelCost_AppliesOperatorTimezone confirms cost handler computes start-of-today against cfg.OperatorTZ, not UTC.
// TestPanelGreenclock_MissingCounterRendersAllNoneCells confirms 30-cell grid renders even when W3 clock accessor returns nil.
// TestPanelCache_SuppressesUpstreamCallsWithin5sWindow confirms second hit within 5 s reads from cache, not upstream.
// TestPanelCache_SingleflightUnderRefreshStorm confirms 100 concurrent requests during cache miss collapse to 1 upstream call.
// TestHandler_RejectsNonGETMethods confirms POST/PUT/DELETE on any /ui route returns 405.
// TestHandler_SetsCSPAndSecurityHeaders confirms every UI response carries CSP, Referrer-Policy, X-Frame-Options, X-Content-Type-Options.
// TestStaticAssets_ETagAndImmutableCache confirms /ui/static/htmx.min.js and pico.min.css carry ETag + Cache-Control immutable.
// TestStaticAssets_SRIHashMatchesEmbedded confirms the SRI sha256 constants in static.go match the embedded byte contents.
// TestTemplatesParseAtBoot confirms templates.go fails to initialize if any template file fails ParseFS.
// FuzzPanelMerges_NoTemplatePanic fuzzes substrate payloads to confirm no template-execution path panics.
```

Total: 15 named tests + 1 fuzz. Property-tested where the input space is bounded (cache TTL behavior, timezone arithmetic, 30-day grid math).

## 10. B/A/A+ rubric

Implementer scorecard MUST measure against this rubric verbatim. Each tier names falsifiable criteria.

### B (floor — Wave 1 ships at this tier or it does not ship)

- (a) `regatta serve --ui-addr :8079` brings up an HTTP listener bound to `localhost:8079`. `GET /ui` returns 200 with a non-empty HTML body.
- (b) All 5 panels render on first paint (no JS required for initial visibility).
- (c) Zero new runtime dependencies in `go.mod` beyond what is already vendored. No `npm`, no `node_modules`, no build chain.
- (d) Static assets vendored under `internal/web/ui/static/` with SRI sha256 constants and `make vendor-{htmx,pico}` targets present.
- (e) Read-only sqlite connection (§3.3); no write path on any `/ui/*` route; non-GET returns 405.
- (f) CSP header strict per §3.4 on every response; CSP-violation report counts zero against headless-chrome smoke test.
- (g) Release-notes fence present in PR body. No banned phrases. No AI signatures.
- (h) All 15 unit tests + fuzz pass.

### A (target — what this spec is written to land)

B plus:

- (i) Per-panel cache (§3.6) collapses 100-concurrent-requests refresh storm to 1 upstream call (tested via `TestPanelCache_SingleflightUnderRefreshStorm`).
- (j) Cold-load TTFB under 100 ms p95 measured on the regatta dev machine against an N=500 substrate fixture.
- (k) Per-panel handler p95 under 50 ms against the same fixture.
- (l) Adversarial reviewer subagent re-scored against this rubric and posted on the PR per `feedback_agent_pr_review`.
- (m) Greenclock 30-cell grid renders against W3 trigger-clock accessor (live, not stub) — or renders all-none if accessor not yet wired, with a tracking issue filed for the wire-up.
- (n) `--ui-allow-public` two-flag pattern verified by integration test (non-loopback bind without the flag emits WARN; with the flag, INFO).

### A+ (stretch — only ship if cheap)

A plus:

- (o) Axe-core accessibility audit reports zero violations on the rendered `/ui` page (run via `playwright` headless against a seeded fixture).
- (p) Lighthouse performance score ≥ 90 on the same headless run.
- (q) Effort lands in days, not weeks: PR opens and merges within 5 working days of spec merge. Drift past 5 days surfaces a slim-down followup (drop A+ items, ship at A).
- (r) Operator opening `localhost:8079/ui` cold for the first time goes from "what is regatta doing" to "I understand the loop state" in under 30 s (measured against a single internal operator video).
- (s) Smoke test runs the dashboard against a seeded substrate + W3 + W5 stack via the existing S1-T5 smoke harness — zero stub, zero mocks.

## 11. Cross-references (load-bearing memory rules)

- `feedback_decision_priority` — UX (one-screen operator dashboard, 30 s to understanding) > ease > performance (<100 ms TTFB) > best-practices > velocity. This spec defends each priority in §1, §5, §6.
- `feedback_research_design_principles` — proven OSS adopted: htmx 2.0.4 (Apache 2) + Pico CSS 2.0.6 (MIT) + Go html/template (stdlib). Each comes with version + license pin in §3.4. No reimplementation.
- `feedback_deletion_default` — answers "what got smaller?": the parent item's three-surface, write-path-included spec collapses to ONE read-only surface in Wave 1. Approvals + cost-reset stay deleted from this spec's surface area and ship in Wave 2 against the EXISTING prior W7 design — not a fresh design. Net change: zero new specs after this one for those surfaces.
- `feedback_grade_rubric` — §10 above is the B/A/A+ rubric with falsifiable criteria. Implementer scorecards measure against it verbatim.
- `feedback_adversarial_review` — §13 below holds the post-draft reviewer findings.
- `feedback_root_cause` — the missing-UI gap closes by adding ONE surface (the dashboard), not by patching every CLI invocation with a UI mirror. Single root, single fix.
- `feedback_comments_discipline` — prose spec only (no Go code lands in this PR); the implementer PR sweep applies.
- `feedback_spec_pattern_authority` — if the implementer needs to deviate from §3.1 package layout or §3.4 vendored-asset pattern, the spec subagent gets re-spawned, not the implementer guessing.

## 12. Dependency order

Wave 1 ships against:

- **W3 service supervisor** (`docs/engineer/specs/2026-06-02-phase-autonomy-w3-service-supervisor.md`) — provides the read-only WAL connection pattern + the trigger-clock accessor for P5 (greenclock). If W3 has not landed when this implementer dispatches, P5 ships against an empty accessor and a tracking issue is filed.
- **W5 cost-cap reader** (`docs/engineer/specs/2026-06-02-phase-autonomy-w5-cost-cap-autonomic-enforcement.md`) — provides the spend-reader interface for P4 (cost panel). Same fallback: missing W5 → P4 shows MISSING.
- **Substrate v1** (`docs/engineer/specs/2026-06-01-unified-substrate-design.md`) — `events` table with `kind='pr_merged'` and `kind='token_spend'` rows. Already shipped per substrate cutover.
- **`gh` CLI on PATH** — runtime dependency on the operator's machine for P2 (PRs). Documented as a soft dep; absent → EMPTY fragment.

Order: W3 + W5 ideally land first; substrate is already in place. If either W3 or W5 slips, this wave still ships at A-tier (per §10 rubric m and the cost-panel fallback).

## 13. Adversarial review (Risk-tier)

Reviewer subagent dispatched against draft. Findings:

| # | Finding | Severity | Resolution |
|---|---|---|---|
| AR1 | Default port `:8079` collides with no documented service, but operators with their own dashboards on 8080 may have habituated muscle memory; doc the choice. | Important | Added §2.2 default-value rationale + `regatta serve --help` text MUST name the default. Inline. |
| AR2 | The "no auth in Wave 1" claim is correct only if the dashboard is strictly read-only. Are we sure no panel exposes a side-effecting URL even indirectly (e.g. a "view trace" link that triggers a heavy substrate scan)? | Risk | Audited each panel: P2 has a `gh pr view` link rendered as plain `<a href>` to github.com (no regatta side-effect). P3 row links to `gh pr view` on github.com same posture. No panel renders a regatta-internal link that triggers a write or a heavy query beyond the panel's own cached fetch. Resolution: confirmed inline §2.3. |
| AR3 | `gh pr list --json` is a fork+exec per cache miss = 1 fork/5s baseline. On a constrained box that's measurable. | Important | Single-flight + 5 s cache caps it at exactly 1 fork per 5 s regardless of tab count. p95 cost of `gh pr list` measured at ~200 ms on the regatta dev machine; well under the panel budget when amortized over 5 s. Acceptable. Tracking followup: replace `gh` with direct GitHub API call if forking proves load-bearing on customer-0's box. |
| AR4 | The 5 panels are functionally independent but visually share one `<main>` — what happens if one panel's swap is slow and pushes layout? | Important | Each `<section>` is fixed-height (`min-height: 12rem` via Pico defaults plus inline style). Swap replaces only the section's inner content; geometry is stable. Verified by `playwright` layout snapshot. |
| AR5 | `_query_only=1` is an sqlite pragma — confirm it survives connection-pool reuse. | Risk | Pragma is per-connection in sqlite; the connection pool sets it via the DSN `?_query_only=1`, which sqlite re-applies on each new connection. Confirmed against `internal/orchestrator/state/conn.go` pattern already in use. |
| AR6 | CSP `frame-ancestors 'none'` blocks the dashboard from being embedded in any iframe — operator may want to embed in their own ops portal. | Important | Wave 2 followup: add `--ui-frame-ancestors '<origin>'` flag for ops-portal embedding. Wave 1 default stays strict per §3.4. |
| AR7 | The W3 trigger-clock accessor `Last30() []DayFlag` is named here but the W3 spec calls it `Trigger.Last30()` (path differs). Reconcile. | Important | This spec yields to the W3 spec. Implementer reads §6 of `2026-06-02-phase-autonomy-w3-service-supervisor.md` for the exact accessor signature. If it differs, this spec's §4.6 is updated in the implementer PR — not re-specced. |
| AR8 | `Operator TZ` is read from `cfg.OperatorTZ` — is that key actually in `regatta.yaml` today, or does this spec invent it? | Risk | Verified by `grep -rn 'OperatorTZ' .` — not currently present. Resolution: this spec REQUIRES the implementer to add `operator_tz` to `regatta.yaml` schema (default UTC) as a Wave 1 prerequisite. Inline note added to §10 rubric and dep order §12. |
| AR9 | The "page renders even if subsystems missing" claim needs a panic-recover, not just nil-checks. A panic in template execution kills the goroutine and returns 500. | Important | Each panel handler wraps body in `defer recover()` and on panic returns the panel's MISSING fragment with a `X-Panel-Error: <kind>` header for diagnostics. Inline added to §3.6. |
| AR10 | Pico CSS 2.0.6 ships dark-mode auto-switch via `prefers-color-scheme`. Operators on light-mode machines won't see it, but the implementer should know this is the default. | Important | Documented. Wave 2 followup: explicit theme toggle (`?theme=light\|dark` query param). |
| AR11 | The spec does not specify how the dashboard is reached from `regatta serve --help` — operator on a fresh install needs to know the URL exists. | Important | Implementer task: `regatta serve --help` MUST print the bound UI URL on startup (`web UI: http://localhost:8079/ui`) once the listener confirms. Inline added to §2.2. |
| AR12 | What is the licensing posture of the vendored Pico + htmx files? Are NOTICE / LICENSE files required? | Important | htmx Apache 2.0 — include a NOTICE entry citing htmx + Apache 2.0 + 2.0.4. Pico MIT — include LICENSE entry citing Pico CSS + MIT + 2.0.6. Both go in `internal/web/ui/static/LICENSES.md`. Inline added to §3.4. |
| AR13 | Simplification candidate: do we need ALL FIVE panels in Wave 1, or could we ship 3 and add 2 in a followup PR? | Risk | All five close a distinct operator question (§1). Cutting any leaves a CLI invocation in the cold path. The five panels sum to ~600 LOC including templates; the marginal cost of two more panels over three is negligible. Decision: ship all five at A-tier. |
| AR14 | Deletion candidate: is greenclock (P5) really a Wave 1 panel, or is it a "nice-to-have" the operator can live without? | Risk | Greenclock is the load-bearing self-host signal — it's the panel that answers "is the loop healthy over time, not just right now." Cutting it pushes the answer back to `regatta status --green-clock`. Decision: keep. |

Reviewer overall: spec is A-tier with the 13 findings folded in. No Risk-tier finding blocks merge. Followup items (AR3 `gh` → API, AR6 frame-ancestors flag, AR10 theme toggle) are filed at PR merge time per `feedback_unaddressed_load_bearing`.

## 14. Followups (filed at merge)

- **W7 Wave 2** — approval queue + cost-cap reset. Builds on this dashboard, adds the first POST routes, introduces the auth question. Owner: existing `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md`.
- **W7 Wave 3** — DAG visualization + reviewer-rich PR UI. Pulls in SVG render (S1 deferral from prior W7 design v2). Owner: a new spec dispatched against the prior W7 design's wave map.
- **SSE / WebSocket streaming** — Phase X reopen-trigger: a customer reports the 5 s poll cadence is too slow for their flow.
- **Direct GitHub API call** (replace `gh pr list` fork) — reopen-trigger: panel p95 budget violated on a customer machine.
- **`--ui-frame-ancestors` flag** (AR6) — reopen-trigger: customer asks to embed the dashboard in their ops portal.
- **Theme toggle** (AR10) — reopen-trigger: operator preference signal.
- **htmx quarterly version bump** — calendar trigger, not feature trigger.
- **`operator_tz` regatta.yaml schema addition** — prerequisite to this wave's merge; tracked as a sub-task of the implementer PR (AR8).

## 15. Comment sweep

State: clean. Prose spec; no Go code lands in this PR. The implementer PR that follows applies the standard `feedback_comments_discipline` sweep on its own diff.

```release-notes
none (internal)
```
