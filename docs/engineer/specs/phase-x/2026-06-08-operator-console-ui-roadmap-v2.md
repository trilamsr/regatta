---
status: phase-x-deferred
phase: x-forward-fit
revision: v2 (SvelteKit go-decision; supersedes 2026-06-04 html/template roadmap)
author: design-subagent
date: 2026-06-08
supersedes:
  - docs/engineer/specs/2026-06-04-operator-console-ui-roadmap.md
companion:
  - docs/engineer/specs/2026-06-02-operator-console-design.md
  - docs/engineer/specs/2026-06-02-operator-console-v2-backlog.md
  - docs/engineer/plans/2026-06-03-operator-console-s0-substrate-prereqs.md
scope_umbrella: "[#183](https://github.com/trilamsr/regatta/issues/183) operator web UI"
framing: |
  v5.1 design (`docs/engineer/specs/2026-06-02-operator-console-design.md`)
  is the aspirational target ledger. The 2026-06-04 v1 roadmap scoped
  delivery down to Go `html/template` + vendored CSS + no client
  framework. The 2026-06-08 operator decision flips that prohibition:
  build the SvelteKit console at full speed. This v2 roadmap re-sequences
  the slices for that target while preserving the substrate prereqs and
  the operator-experience principle. Single binary still embeds the
  client (`embed.FS` over the SvelteKit static build); no runtime npm.
  `phase: x-forward-fit` retained because the spec names Phase-X tokens
  it explicitly defers (RBAC, tenant_id, blackboard, etc).
deferred_on: 2026-06-10
---

# Operator Console — UI Roadmap v2 (SvelteKit go-decision)

## 0. Policy flip — what changed and why

The 2026-06-04 v1 roadmap (`docs/engineer/specs/2026-06-04-operator-console-ui-roadmap.md`)
forbade SvelteKit, `htmx`, and npm at the console layer under the
self-host filter (`docs/engineer/briefs/2026-06-01-self-host-first.md`
§4). v1 framed Phase-S delivery as Go `html/template` plus vendored CSS
only; the Wave-1 `htmx`-MVP at `internal/web/templates/` shipped under
that constraint.

**2026-06-08 operator decision: lift the prohibition.** Build the
SvelteKit console per v5.1 §3.1 at full speed. This v2 supersedes v1.

Rationale (load-bearing, not aspirational):

1. **GREEN-CLOCK matured.** The autonomous loop merged ~30 PRs/day this
   session under branch-protection automerge with `strict: false`. The
   prior worry — "operator can't run an npm supply-chain audit while
   shipping CLI work" — is empirically resolved: CI gates plus
   `make pre-push-check` plus reviewer-verdict gate hold the line
   without operator polling.
2. **Functionality footprint grew past the 80/20 cut.** v1 scoped to
   four read-only views plus four mutations. This session landed
   surfaces v1 cannot host without forcing the operator back to CLI:
   `regatta doctor` (#917), secrets management (#911), supervisor
   `--name` selection (#933), spec-sweep regenerator (#888, #886),
   chat-notifier (#974), digest (#976), live-validation (#955),
   autonomous-designer dispatch (#972), arbitrary-repo (#964), and
   external-platform adapters (#973). Each is a first-class surface
   the operator now invokes daily.
3. **`htmx`-MVP is load-bearing only as a stop-gap.** The Wave-1 UI
   at `internal/web/templates/` was authored under v1's html-template
   constraint. The v5.1 spec explicitly calls for SvelteKit replacement
   (§3.1 phase-2 drop, §3.1 phase-3 delete). v2 renews the 3-phase
   `htmx` rip and pins its deletion criterion.
4. **JSON contracts stay stable.** v1 anticipated this lift (§4.4 +
   §6 R8). The S2 mutation contracts (POST `/api/v1/operator/*`) are
   render-agnostic; only the render layer swaps.

Reopen-trigger for the prior prohibition: if a second-internal-operator
needs to dispatch concurrently AND we measurably regress on
single-binary deploy ergonomics, revisit. Neither has fired.

## 1. Decision priority (re-applied)

Per CLAUDE.md §Decision priority: UX > ease > performance > best-practices
> speed > velocity. Long-term > short-term.

| Axis | v1 decision | v2 decision (this doc) |
|---|---|---|
| UX | Server-render; zero-click landing. | SvelteKit SPA after first paint; zero-click landing; live streams without page reload. |
| Ease | No new runtime dep; `html/template` already in tree. | Build-time npm only; runtime stays single Go binary via `embed.FS` over SvelteKit static build. No runtime npm. |
| Maintainability | One CSS file; one JS file. | Co-located `app/` SvelteKit project; `make web-build` produces `internal/web/dist/` for `embed.FS`. `verify-vendored-assets` extended to cover the build output. |
| Performance | Sub-100 ms server-render budget. | First-paint sub-200 ms via SSG (SvelteKit `adapter-static`); subsequent client-side nav is local. |
| Long-term | Forward-fit seam to v5.1. | This IS the v5.1 render layer. No further lift planned. |

## 2. Current state audit (2026-06-08)

### 2.1 What ships today

- `internal/web/server.go` — sub-mux, CSP middleware, `embed.FS` asset
  serving, html/template render path at `internal/web/render.go`.
- `internal/web/csp.go` — CSP header set; verified by
  `internal/web/csp_test.go`.
- `internal/web/csrf.go` — double-submit token middleware.
- `internal/web/templates/` — `layout.tmpl` + `_flash.tmpl` only.
- `internal/web/static/` — `htmx.min.js` + `htmx-config.js` +
  `tailwind.min.css` vendored (Wave-1 stop-gap; scheduled for deletion
  in S1 phase-3 per v5.1 §3.1).
- S0 substrate prereqs (migrations 0018-0021 per
  `docs/engineer/plans/2026-06-03-operator-console-s0-substrate-prereqs.md`
  §1-8): `runs` registry, `work_items.run_id`, `approval_events.run_id`,
  `tool_call` substrate kind, scheduler dispatch boundary populates.

### 2.2 Functionality footprint that v2 must surface

Each row is a surface the operator currently drops to CLI for; v2's
target is to host every row in the SvelteKit console.

| Subcommand / surface | Landed | UI home in v5.1 console |
|---|---|---|
| `regatta doctor` health probe | #917 | `/console/doctor` (read-only matrix; re-run button → POST) |
| Secrets read/write/rotate | #911 | `/console/secrets` (list + rotate; never displays plaintext) |
| Supervisor `--name` selection | #933 | `/console/supervisor` (pick / start / stop named worker) |
| Spec sweep + regen | #888, #886 | `/console/specs` (status badges; trigger regen) |
| Chat-notifier dispatch | #974 | `/console/notifications` (channel routing; mute) |
| Digest emission | #976 | `/console/digest` (read recent digests; trigger ad-hoc) |
| Live-validation gate | #955 | `/console/validation` (per-PR validation tail; re-run) |
| Autonomous-designer dispatch | #972 | `/console/designer` (queue + dispatch state + last verdict) |
| Arbitrary-repo adapter | #964 | `/console/repos` (registered targets; add / remove) |
| External-platform adapters | #973 | `/console/platforms` (adapter health; per-platform throttle) |
| (v1 carry-over) Work-item queue | shipped pre-v2 | `/console/queue` |
| (v1 carry-over) Approvals | shipped pre-v2 | `/console/approvals` |
| (v1 carry-over) Recent merges | shipped pre-v2 | `/console/merges` |
| (v1 carry-over) Cost spend | shipped pre-v2 | `/console/cost` |
| (v1 carry-over) Substrate events tail | S3 of v1 | `/console/events` (SSE) |

Coverage rule: every row above maps to one v5.1 SvelteKit route by S4
acceptance. Any uncovered row at S4 close → backlog ticket against
`docs/engineer/specs/2026-06-02-operator-console-v2-backlog.md`.

### 2.3 What stays explicitly deferred (Phase-X)

`tenant_id`, `RBAC`, `Stripe`, `Sigstore`, `Rekor`, `blackboard`, and
`Temporal` remain Phase-X per `docs/engineer/briefs/2026-06-01-self-host-first.md`.
The SvelteKit lift does not relax these; only the render layer flips.
`phase: x-forward-fit` retained in frontmatter to satisfy
`scripts/check-phase-x-leak.sh` for the bare-mentions above.

## 3. Re-sequenced phased plan (S0 → S1 → S2 → S3 → S4)

Each slice is ≤ 6 child PRs. Implementer dispatch briefs MUST cite §5a
(operator-experience principle), inherited from v1.

### 3.0 S0 — Substrate prereqs (unchanged)

Already specced at `docs/engineer/plans/2026-06-03-operator-console-s0-substrate-prereqs.md`.
v2 does not re-author S0. S0 acceptance gates the start of S1.

### 3.1 S1 — SvelteKit scaffold + 3-phase htmx rip (5-6 child PRs)

**Goal:** stand up the SvelteKit project under `app/`, wire the Go
binary to embed the static build, then delete the html/template +
`htmx` stop-gap.

| # | PR | Surface | Files touched |
|---|---|---|---|
| S1-1 | SvelteKit project skeleton (`adapter-static`, Vite, TypeScript) | scaffold | `app/`, `app/package.json`, `app/svelte.config.js`, `app/vite.config.ts` |
| S1-2 | `make web-build` produces `internal/web/dist/`; `embed.FS` swaps over | build wiring | `Makefile`, `internal/web/server.go`, `internal/web/dist/.gitkeep` |
| S1-3 | Re-port the v1 four read-only routes (`/console/queue`, `/console/approvals`, `/console/merges`, `/console/cost`) to SvelteKit pages backed by `/api/v1/operator/*` GETs | UI + JSON contracts | `app/src/routes/console/**`, `internal/web/api_*.go` |
| S1-4 | Drop `htmx` + Tailwind vendored assets; CSP narrowed to `script-src 'self'` only | cleanup | delete `internal/web/static/htmx*.js`, `internal/web/static/tailwind.min.css`, update `internal/web/csp.go` |
| S1-5 | Delete `internal/web/templates/` + `internal/web/render.go` html/template path; html-template handlers replaced by SvelteKit-served `index.html` | deletion | delete `internal/web/templates/`, `internal/web/render*.go`, simplify `internal/web/server.go` |
| S1-6 (optional) | `verify-vendored-assets` extended to gate `internal/web/dist/` against the committed SvelteKit `package-lock.json` checksum | CI gate | `scripts/verify-vendored-assets.sh`, `Makefile` |

**Phase-3 deletion criterion (S1-5).** v1's Wave-1 `htmx`-MVP is
deletable when ALL of:

1. S1-3 ships and serves the four read-only routes from SvelteKit.
2. The S1-3 routes pass the v1 §5a clicks-budget walkthrough (each of
   the 5 golden-path flows resolves in ≤ 1 click from `/console`).
3. The `/api/v1/operator/*` JSON contracts under test in S1-3 are
   the same shape v1's html/template handlers consumed (no schema
   regression).

When (1) AND (2) AND (3) hold, S1-5 deletes the `htmx`+template stack
in one PR. If any criterion fails, S1-5 is blocked and the regression
files a tracker before S1 closes.

**Acceptance:** `regatta serve` binary embeds the SvelteKit static
build via `embed.FS`; `/console` renders the v1 four read-only routes
without any `internal/web/templates/` or `htmx` asset on disk.

**Sizing:** S1-1 ~150 LOC; S1-2 ~80 LOC + Makefile; S1-3 ~600-900 LOC
(SvelteKit pages + JSON handlers + tests); S1-4 ~-200 LOC (net
deletion); S1-5 ~-800 LOC (net deletion); S1-6 ~120 LOC.

### 3.2 S2 — Surface this-session functionality (5-6 child PRs)

Build out the seven this-session surfaces from §2.2 as v5.1 routes.
Each PR adds one SvelteKit page + one `/api/v1/operator/*` handler
pair. Forms remain POST with idempotency tuple per v1 §3.2 R6.

| # | PR | Surface | New route |
|---|---|---|---|
| S2-1 | `regatta doctor` matrix view + re-run trigger | `/console/doctor` |
| S2-2 | Secrets list + rotate (no plaintext display) | `/console/secrets` |
| S2-3 | Supervisor named-worker control | `/console/supervisor` |
| S2-4 | Spec sweep status + regen trigger | `/console/specs` |
| S2-5 | Notifications routing (chat-notifier + digest combined) | `/console/notifications` |
| S2-6 | Repos + platforms registry (arbitrary-repo + external-platform combined) | `/console/repos`, `/console/platforms` |

**Acceptance:** every surface in §2.2 is reachable from `/console`
landing in ≤ 1 click; CLI fallback retained but no longer the daily
driver.

### 3.3 S3 — Autonomous-designer + validation status (3-4 child PRs)

Surface the autonomous-loop state the operator needs to babysit
without `tmux`-into-CLI.

| # | PR | Surface | New route |
|---|---|---|---|
| S3-1 | Autonomous-designer dispatch queue + last verdict | `/console/designer` |
| S3-2 | Live-validation per-PR tail (SSE) | `/console/validation` |
| S3-3 | Substrate events tail (carry-over from v1 S3-1) | `/console/events` |
| S3-4 (optional) | Per-PR event sub-stream | `/console/events?pr=N` |

**Acceptance:** operator opens `/console/designer`, sees current
dispatch queue + last-N verdicts; opens `/console/validation`, sees
streaming gate output without manually polling `gh pr view`.

### 3.4 S4 — Polish + v5.1 §3.3 dual-principal hooks (optional)

Per v5.1 §3.3-§3.5 (dual-principal auth, audit-log Merkle chain, full
debug + steer panes). S4 is **optional** and gated on a second
operator joining the loop OR an external customer ask (per v1 §9
reopen-triggers, unchanged).

If S4 ships pre-trigger: it MUST be a pure-deletion or pure-rename PR
(no new surfaces beyond what S1-S3 ship). All net-new auth surfaces
defer to the reopen-trigger.

### 3.5 Phase totals

| Slice | Min PRs | Max PRs | Net LOC (incl. v1 deletions) |
|---|---|---|---|
| S0 | (already specced) | (already specced) | (already specced) |
| S1 | 5 | 6 | +500 to +1200 (after net -1000 deletion in S1-4 + S1-5) |
| S2 | 5 | 6 | +1500 to +3000 |
| S3 | 3 | 4 | +700 to +1500 |
| S4 | 0 (gated) | 0 (gated) | 0 |

## 4. Tech choices

### 4.1 SvelteKit + adapter-static + Vite

- **SvelteKit**: SSG via `@sveltejs/adapter-static`. No SSR server
  needed; the Go binary serves the static `dist/` over HTTP. Pinned
  version chosen at S1-1 (lockfile committed; no float).
- **Vite**: bundler for the SvelteKit build. Build-time only; no
  runtime Vite dependency.
- **TypeScript**: enabled in the SvelteKit scaffold.
- **No client-side router beyond SvelteKit's own.** No `htmx`, no
  React, no Vue. SvelteKit's file-router handles navigation.

### 4.2 Deployment story — single Go binary

The operator deploys exactly one binary. The SvelteKit build output
is embedded at build time:

1. `make web-build` runs `cd app && npm ci && npm run build`, producing
   `app/build/`.
2. Build copies `app/build/` to `internal/web/dist/`.
3. `internal/web/server.go` uses `//go:embed all:dist` to embed the
   directory into the binary.
4. `regatta serve` exposes `/console/*` from the embedded FS; the
   SvelteKit SPA handles client-side routing under that prefix.

No runtime `node`, `npm`, or external CDN. `verify-vendored-assets`
(S1-6) gates the committed `package-lock.json` checksum against the
build to prevent silent supply-chain drift.

### 4.3 SSR vs SPA tradeoff — SPA via SSG

- **SSR rejected**: would require a `node` runtime alongside the Go
  binary OR cross-compile of `node` server logic, breaking
  single-binary deploy.
- **SSG (adapter-static) chosen**: SvelteKit pre-renders shells at
  build time; client hydrates and fetches `/api/v1/operator/*` JSON
  for live data. First paint is the pre-rendered shell; subsequent
  data is fetched. Survives single-binary constraint.
- **First-paint budget**: 200 ms on localhost; 500 ms over LAN.

### 4.4 CSP with SvelteKit

SvelteKit emits inline scripts for hydration by default. v2 sets a
strict CSP via `internal/web/csp.go`:

- `script-src 'self'` — block inline. SvelteKit build configured with
  `csp.mode: 'hash'` so emitted script hashes are pre-computed and
  injected into the CSP header per route.
- `style-src 'self'` — block inline. Component-scoped styles are
  bundled to external `.css` files by Vite.
- `connect-src 'self'` — XHR only to same origin (the Go binary).
- `img-src 'self' data:` — allow inlined SVG icons.
- `frame-ancestors 'none'` — clickjacking defense.

Property test in `internal/web/csp_test.go` extended to assert no
inline script slips through the build.

### 4.5 Prompt-injection from operator UI inputs

R1 from v1 §6 (HTML-template auto-escape) is replaced by Svelte's
template auto-escape:

- All `{expression}` interpolations in `.svelte` files are auto-escaped
  to text nodes by the Svelte compiler. The `{@html ...}` directive
  is the only escape hatch and is forbidden by an ESLint rule pinned
  in `app/.eslintrc.json` (`svelte/no-at-html-tags: error`).
- Operator-controlled content (PR title, work-item label, doctor
  output) flows through `{expression}` only. Property test in
  `app/src/lib/__tests__/escape.test.ts` asserts a corpus of
  injection payloads renders as inert text.
- Backend `/api/v1/operator/*` handlers continue to validate inputs
  via `internal/validation/` (unchanged from v1 S2).

### 4.6 OSS prior art (SvelteKit served by Go binary)

- **gitea/gitea v1.22.3** (MIT, tag commit `bff5363d9d7596ee9099ad6a2cffa6d6bf661c34`,
  2024-09-28) — `html/template` precedent retained as a reference for
  the routes that stay server-rendered (e.g. `/healthz`, error pages
  rendered before SvelteKit hydration).
- **grafana/grafana v11.3.0** (AGPL-3.0, tag commit
  `cee9b9eef85b66f1a05c5b7b2e2db40d7c43b3eb`, 2024-11-12) — Go binary
  serves a `dist/` SPA via `embed.FS`; lockfile committed; build-time
  npm only. This v2 mirrors the deploy pattern at smaller scale.

(Grafana is AGPL; cited for pattern reference only — no code lifted.)

## 5. Operator UX flows (carry-over)

The five golden-path flows from v1 §5 carry over unchanged. Each must
resolve in ≤ 1 click from `/console` landing on the v5.1 SvelteKit
console:

1. "Just paged about failing merge" → `/console/merges` row + Resolve
   button.
2. "Cost-cap blew at 2 am" → `/console/cost` top-spender + Pause-spawner.
3. "5 PRs queued for HITL" → `/console/approvals` batch + Approve-all.
4. "Substrate chain break" → `/console/events` chain-break banner +
   Run-replay.
5. "Autonomous agent looks wedged" → `/console/queue` stale row + Kill.

## 5a. Operator-experience principle (binding, carry-over from v1 §5a)

Inherited verbatim from v1. Five clauses:

1. Default autonomous; operator input ONLY at irreversible decisions.
2. One-click resolve / approve / kill from `/console` landing.
3. Smart defaults > config knobs.
4. Push notifications (SSE) reach the operator.
5. Success metric: clicks-to-complete ≤ 1 for each golden-path flow.

Implementer dispatch briefs MUST cite §5a on every S1/S2/S3 child PR.

## 6. Risk register (delta vs v1)

| # | Risk | Severity | Mitigation |
|---|---|---|---|
| R1 | Svelte `{@html}` directive bypasses auto-escape | High | ESLint rule `svelte/no-at-html-tags: error` pinned in `app/.eslintrc.json`. Property test in `app/src/lib/__tests__/escape.test.ts`. |
| R2 | CSRF on `/api/v1/operator/*` POSTs | High | Double-submit token middleware (`internal/web/csrf.go`) reused; SvelteKit client reads token from `<meta>` tag emitted in the static shell. |
| R3 | npm supply-chain drift between local + CI | High | `package-lock.json` committed; `npm ci` only (never `npm install`) in `Makefile` web-build; `verify-vendored-assets` extended to gate lockfile checksum. |
| R4 | Vite build non-determinism breaks `embed.FS` checksum | Med | Pin Vite version in `app/package.json`; `app/.npmrc` sets `engine-strict=true`; CI runs the build twice and asserts byte-identical output. |
| R5 | First-paint regression on slow LAN | Low | SSG shell loads without JS; live data fills in. Budget: 200 ms localhost, 500 ms LAN. |
| R6 | Idempotency-key design (carry-over from v1 R6) | Med | Tuple `(actor, target_id, action, ts-bucket)` unchanged. |
| R7 | CSP hash drift between dev + prod build | Med | SvelteKit `csp.mode: 'hash'` emits hashes per route at build time; `internal/web/csp.go` reads `internal/web/dist/csp.json` at serve time. Test in `internal/web/csp_test.go`. |
| R8 | `htmx` stop-gap deletion (S1-5) regresses an in-use flow | High | Phase-3 deletion criterion (§3.1) requires the four read-only routes to ship AND pass §5a walkthrough first. Tracker filed if any criterion fails. |
| R9 | v1 functionality not surfaced in v5.1 | High | §2.2 coverage map: every row maps to a v5.1 route by S4 acceptance. Uncovered rows file backlog tickets. |
| R10 | npm vulnerability lands mid-S1 | Med | `npm audit` in `make pre-push-check` (S1-2 wires); fails-closed on `high`/`critical`. |

## 7. Out-of-scope (carry-over with one delta)

| Out | Reopen trigger |
|-----|-----|
| ~~SvelteKit~~ — **flipped to in-scope as of 2026-06-08** | n/a (lifted by this v2) |
| ~~`htmx` hot-swap~~ — **moot** (Wave-1 `htmx`-MVP scheduled for S1-5 deletion) | n/a |
| React / Vue / Angular | External-customer ask requiring framework parity. |
| WebSockets | SSE proves insufficient AND need bidirectional channel. |
| RBAC | Multi-operator collaboration need (currently 1 operator). |
| Multi-user / multi-tenant | Per `docs/engineer/briefs/2026-06-01-self-host-first.md` §4 (`tenant_id` is a Phase-X token). |
| Mobile-responsive | S5 of v5.1 covers this after v1 ships. |
| Real-time chat / collab | Out of v5.1 scope entirely; no trigger. |

## 8. References

- `docs/engineer/specs/2026-06-04-operator-console-ui-roadmap.md` (v1; superseded by this doc)
- `docs/engineer/specs/2026-06-02-operator-console-design.md` (v5.1 design ledger)
- `docs/engineer/specs/2026-06-02-operator-console-v2-backlog.md` (deferred items)
- `docs/engineer/plans/2026-06-03-operator-console-s0-substrate-prereqs.md` (S0 substrate)
- `docs/engineer/briefs/2026-06-01-self-host-first.md` (Phase-S filter; Phase-X gate)
- `internal/web/server.go` (asset embed; existing handler entry; target for S1-2 swap)
- `internal/web/render.go` (html/template path; target for S1-5 deletion)
- `internal/web/csp.go` + `internal/web/csp_test.go` (CSP directive set; extended in S1-4)
- `internal/web/csrf.go` (CSRF double-submit; reused unchanged)
- `internal/web/templates/` (target for S1-5 deletion)
- `internal/web/static/htmx*.js` + `tailwind.min.css` (target for S1-4 deletion)
- gitea/gitea tag `v1.22.3` sha bff5363d9d7596ee9099ad6a2cffa6d6bf661c34 license MIT
- grafana/grafana tag `v11.3.0` sha cee9b9eef85b66f1a05c5b7b2e2db40d7c43b3eb license AGPL-3.0 (pattern-cited only)

Issues cross-referenced: #183, #911, #917, #933, #888, #886, #955, #964, #972, #973, #974, #976.

Memory cites: `feedback_decision_priority`, `feedback_deletion_default`,
`feedback_operator_minimal_input`, `feedback_research_design_principles`,
`feedback_no_signatures`, `feedback_default_simpler`, `feedback_recognize_session_end`.

```release-notes
docs: operator console UI roadmap v2 (SvelteKit go-decision)
```
