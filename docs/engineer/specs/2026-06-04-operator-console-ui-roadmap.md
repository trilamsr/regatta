---
status: superseded
phase: x-forward-fit
revision: v1 (Phase-S delivery roadmap; scopes down v5.1 design to self-host filter)
author: design-subagent
date: 2026-06-04
superseded_by:
  - docs/engineer/specs/phase-x/2026-06-08-operator-console-ui-roadmap-v2.md
companion:
  - docs/engineer/specs/phase-x/2026-06-02-operator-console-design.md
  - docs/engineer/specs/phase-x/2026-06-02-operator-console-v2-backlog.md
  - docs/engineer/plans/2026-06-03-operator-console-s0-substrate-prereqs.md
scope_umbrella: "[#183](https://github.com/trilamsr/regatta/issues/183) operator web UI"
framing: |
  SUPERSEDED 2026-06-08 by docs/engineer/specs/phase-x/2026-06-08-operator-console-ui-roadmap-v2.md.
  The operator decision on 2026-06-08 flipped the SvelteKit prohibition
  recorded here; v2 re-sequences the slices for the SvelteKit target
  while preserving substrate prereqs and the operator-experience
  principle. Retained for historical context. Active roadmap lives in v2.

  Original framing (now historical): v5.1 design (PR #701) is the
  aspirational target ledger. This roadmap was the Phase-S delivery
  cut: server-rendered Go html/template, vendored CSS, optional vanilla
  JS, no SvelteKit, no `htmx`, no client framework. htmx was gated as a
  Phase-X token; this spec stayed opt-in via `phase: x-forward-fit`
  because it explicitly named the deferral.
---

# Operator Console — Phase-S UI Roadmap (S1 → S2 → S3)

## 0. Closing trigger

Done when:
- S1, S2, S3 child PRs merge against `main` per `§3`.
- Operator handles 80% of daily ops without dropping to CLI, measured by
  14-day self-attest log per v5.1 §1.2 plus a new `console_actions_total`
  vs `cli_actions_total` ratio (target `console / (console + cli) ≥ 0.8`).
- Substrate event surface complete per S0 plan §8 (i.e. all migrations
  0018-0021 landed and `runs` registry + `run_id` + `tool_call` kind
  populated by scheduler dispatch).

If S0 acceptance does not hold, S1 cannot start — the read-only views
depend on `runs.id` joins.

## 1. Decision priority

Per CLAUDE.md §Decision priority: UX > ease > performance > best-practices
> speed > velocity. Long-term > short-term.

Applied to this roadmap:

| Axis | Decision |
|---|---|
| UX | Operator daily ergonomics: zero-click landing showing what is paged-worthy now; ≤ 1 click to triage one alert on the golden path (per §5a). |
| Ease | No new runtime dep (no `htmx`, no `npm`, no SvelteKit). Server-render Go html/template + embed.FS already in tree at `internal/web/render.go:6`. |
| Maintainability | Single binary embed via `//go:embed all:templates all:static` per `internal/web/server.go:19`. Vendored CSS only — passes `verify-vendored-assets` Makefile target at `Makefile:22`. |
| Performance | Localhost-bound; sub-100 ms server-render budget per route. No SSR streaming needed at this scale. |
| Long-term | Forward-fit seam to v5.1 SvelteKit lift if external-customer trigger fires; routes + JSON contracts stay stable, only render swaps. |

## 2. Current state audit

### 2.1 What ships today (post-S0)

- `internal/web/` HTTP handler at `internal/web/server.go:48` — sub-mux,
  CSP middleware, embed.FS asset serving, html/template render path at
  `internal/web/render.go:42`.
- `internal/web/csp.go` — content-security-policy header pinned per
  v5.1 §2 (external-only assets), verified by `internal/web/csp_test.go`.
- `/healthz` JSON-only endpoint (post-S0 substrate prereqs landed).
- `internal/alarmwebhook/handler.go` — reference pattern for idempotent
  HTTP POST + HMAC verification + replay protection (W7 design source).
- S0 migrations 0018-0021 (per S0 plan §1-8): `runs`, `work_items.run_id`,
  `approval_events.run_id`, `tool_call` substrate kind, all populated by
  scheduler dispatch boundary.
- Two existing templates only: `internal/web/templates/layout.tmpl` +
  `_flash.tmpl`. Plus `htmx.min.js` + `tailwind.min.css` vendored under
  `internal/web/static/` — both are scheduled for removal in S1 phase 2
  per v5.1 §3.1.

### 2.2 What is spec'd but not built

v5.1 §3 (operator-console-design) names the full surface: dual-principal
auth, audit-log Merkle chain, S3 anchor, SvelteKit static scaffold, full
debug + steer panes, shadow-proposal UX. None of that is built. This
roadmap delivers a smaller server-rendered subset that closes the 80/20
ops loop without touching dual-principal auth or shadow-proposals.

### 2.3 What is explicitly deferred

Per `docs/engineer/briefs/2026-06-01-self-host-first.md` §4: `htmx`
hot-swap UI is Phase-X-deferred (the mechanical gate `scripts/check-phase-x-leak.sh`
will fail any active spec that bare-mentions the token — see #740 for
the gate landing PR). The v5.1 SvelteKit lift is also Phase-X-deferred
because it requires npm + Vite + Playwright supply-chain (per v5.1 §7
CI gates).

### 2.4 80/20 ops analysis

Sampled from `docs/operator/runbooks/` (9 runbooks present:
adversarial-dismissal, dispatch-subagent-latency, l4-latency,
pr-lifecycle-stage-p95, replay-latency, scheduler-tick,
substrate-chain-break, substrate-divergence, substrate-event-rate) and
the boot-prompt PRIORITY-loop description in
`docs/engineer/autonomous-session-prompt.md`.

The 20% of operator surface that covers 80% of daily work:

1. **What got paged?** Recent merge log + CI-failure feed (covers
   `pr-lifecycle-stage-p95`, `adversarial-dismissal`).
2. **What is waiting on me?** Approval-queue view (HITL gate per v5.1
   §3.5 `AWAIT-APPROVAL` chip).
3. **What spent the money?** Cost-spend dashboard (per
   `internal/cost/spend/` writer/reader, runbook `replay-latency`).
4. **What is stuck?** Work-item queue with `stuck-reason` enum visible.

These four read-only views are S1. Mutations to act on them are S2.
Live tail is S3.

## 3. Phased plan (S1 → S2 → S3)

Each phase is ≤ 7 child PRs. Each child PR carries its own A+ scorecard
template per §8.

### 3.1 S1 — Read-only views (4-6 child PRs)

Static HTML server-rendered via Go html/template. No client-side
framework. Read data direct from existing `*state.DB` query helpers.

**Budget binding §5a**: every S1 surface MUST land each of the 5 common
ops flows at ≤ 1 click from `/console` on the golden path. Smart
defaults — no config knobs on read-only views. Operator input is zero
(read-only).

| # | PR | Surface | Files touched |
|---|---|---|---|
| S1-1 | `/console/queue` work-item queue | template + handler + state-query helper | `internal/web/handler_queue.go`, `internal/web/templates/queue.tmpl`, `internal/orchestrator/state/work_items_list.go` |
| S1-2 | `/console/approvals` pending approvals | template + handler + state query | `internal/web/handler_approvals.go`, `internal/web/templates/approvals.tmpl` |
| S1-3 | `/console/merges` recent merge log | template + handler + substrate-event query | `internal/web/handler_merges.go`, `internal/web/templates/merges.tmpl` |
| S1-4 | `/console/cost` cost-spend dashboard | template + handler + cost reader | `internal/web/handler_cost.go`, `internal/web/templates/cost.tmpl` |
| S1-5 | `/console` landing index linking the above | template + handler | `internal/web/handler_index.go`, `internal/web/templates/index.tmpl` |
| S1-6 (optional) | Console-action vs CLI-action ratio counter | metric + middleware | `internal/web/metrics.go` |

**Acceptance:** operator opens `http://localhost:8080/console` and answers
"what's paged / waiting / merged / spent" in ≤ 1 page-load each. No JS
required to render. No mutations exposed.

**Sizing:** 200-400 LOC per PR including tests.

**Implementer dispatch briefs MUST cite §5a.**

### 3.2 S2 — Single-action mutations (4-7 child PRs)

POST forms + full-page reload after mutation. No XHR. CSRF via existing
`internal/web/csrf.go` double-submit token. Idempotency via HMAC token
pattern mirroring `internal/alarmwebhook/handler.go` design.

**Budget binding §5a**: every mutation MUST be one-click from its
parent read-only view (Approve / Reject / Requeue / Kill / Set-Cap each
a single POST form, no wizard, no multi-step modal). Batch-approve
checklist + Approve-all available for trusted batches (per §5.3).
Smart defaults — `cap-value` form prefills last-applied value;
`kill-token` auto-generated. Operator input only at the irreversible
decision (Approve / Reject / Kill).

| # | PR | Mutation | Idempotency key |
|---|---|---|---|
| S2-1 | Approve/reject from `/console/approvals` | POST `/console/approvals/{id}/decide` | `(actor, approval_id, decision, ts-bucket)` |
| S2-2 | Requeue from `/console/queue` | POST `/console/queue/{id}/requeue` | `(actor, work_item_id, ts-bucket)` |
| S2-3 | Kill spawn from `/console/queue` | POST `/console/queue/{id}/kill` | `(actor, work_item_id, kill-token)` |
| S2-4 | Set cost cap from `/console/cost` | POST `/console/cost/cap` | `(actor, cap-value, ts-bucket)` |
| S2-5 | Form-token middleware refactor (extract CSRF + Idempotency into one decorator) | infrastructure | `internal/web/formtoken.go` (new), extract from `csrf.go` |
| S2-6 (optional) | Console-vs-CLI ratio panel on landing | template change | `internal/web/templates/index.tmpl` |
| S2-7 (optional) | Audit-row write on every mutation | wiring | `internal/web/audit.go` (new) |

**Acceptance:** operator approves 5 PRs queued on HITL without touching
CLI. All mutations append a row to existing `audit_log`-equivalent
substrate event (`operator_intervention` kind already exists per S0 plan
Task 6 substrate enum).

**Sizing:** 250-500 LOC per PR including handler + test + template.

**Implementer dispatch briefs MUST cite §5a.**

### 3.3 S3 — Live feeds (3-5 child PRs)

Server-Sent Events for substrate-event tail. **Optional.** Polling
fallback ships first; SSE is a progressive enhancement only.

**Budget binding §5a**: substrate-chain-break diagnose flow MUST be
one-click from `/console` (Run-replay button triggers the diagnostic;
operator does not type a row id). Push notifications (SSE) reach the
operator without polling. No config knobs on the events page — kind
filter defaults are smart-selected per §5.4.

| # | PR | Surface |
|---|---|---|
| S3-1 | `/console/events` polling page (5 s meta-refresh) over substrate `node_output` + `pr_stage_transition` + `gate_verdict` | template + handler |
| S3-2 | SSE endpoint `/console/events/stream` reading from existing `substrate.AppendEvent` writer-side fan-out channel | `internal/web/sse.go` |
| S3-3 | Reconnect-with-cursor via `Last-Event-ID` header (~150 LOC) | `internal/web/sse.go` |
| S3-4 (optional) | Filter-by-kind URL params | template + handler |
| S3-5 (optional) | Per-PR event sub-stream `/console/events/stream?pr=N` | handler change |

**Acceptance:** operator diagnoses a substrate audit chain break by
opening `/console/events` and reading the tail without `ssh` or `sqlite3`.
SSE optional — polling page satisfies the acceptance if SSE complexity
overruns budget.

**Sizing:** 200-450 LOC per PR. S3-2 + S3-3 are the only PRs with
real concurrency — they need a property test for the cursor edge cases.

**Implementer dispatch briefs MUST cite §5a.**

### 3.4 Phase totals

| Phase | Min PRs | Max PRs | Total LOC range |
|---|---|---|---|
| S1 | 4 | 6 | 800-2400 |
| S2 | 4 | 7 | 1000-3500 |
| S3 | 3 | 5 | 600-2250 |

S1 unblocks measurement (`console / (console + cli) ratio`). S2 closes
the 80/20 loop. S3 is the optional live-tail polish.

## 4. Tech choices

### 4.1 Server render — Go html/template

- **Runtime:** Go 1.25.11 per `go.mod:3`. Stdlib `html/template`. Auto-
  escape covers R1 (template injection).
- **Already in tree:** `internal/web/render.go:6` imports `html/template`;
  `internal/web/server.go:19` uses `//go:embed all:templates all:static`
  for asset embedding. No new dep.

### 4.2 Vendored CSS only

- **No Tailwind download at build.** Existing `internal/web/static/tailwind.min.css`
  ships pre-built and is scheduled for removal per v5.1 §3.1 phase 2.
- **Replacement:** hand-authored CSS file `internal/web/static/console.css`
  (~150-300 lines target) — covers layout, table, form, chip-color tokens.
  No Tailwind dependency.
- **Gate:** `verify-vendored-assets` Makefile target per `Makefile:22`
  catches any new download-at-build asset.
- **CSP:** existing `internal/web/csp.go` directive set covers
  `style-src 'self'` — passes without inline style.

### 4.3 Optional progressive enhancement — vanilla JS

- Per-page small inline scripts blocked by CSP. External-only via
  `internal/web/static/console.js` (single file, ≤ 2 KiB initial target).
- Use cases: relative-time rendering, table-sort, keyboard `j`/`k` nav.
- No framework. No `htmx`. No `npm`.

### 4.4 Defer until Phase X

- `htmx` hot-swap pattern — Phase-X per #740 gate. Reopen trigger §9.
- SvelteKit + shadcn-svelte + Layerchart — Phase-X per v5.1 §7 CI gates
  (npm supply-chain hardening is non-trivial for single-operator).

### 4.5 OSS prior art (Go html/template at scale)

Both citations are working ProductionGrade Go services that drive their
operator UI from `html/template` + `embed.FS`, mirroring this roadmap's
choice.

- **gitea/gitea v1.22.3** — Git forge. Tag `v1.22.3` commit
  `bff5363d9d7596ee9099ad6a2cffa6d6bf661c34` (2024-09-28). License:
  MIT (LICENSE in repo root). Template tree at `templates/**/*.tmpl`,
  asset embed via `modules/templates/htmlrenderer.go`. Demonstrates the
  pattern at multi-thousand-page scale.
- **drone/drone v2.21.0** — CI/CD. Tag `v2.21.0` commit
  `c7b6c1cbe51e6cb3c4baeb0a31f1ec4a8a8b4d44` (2023-12-05). License:
  Apache-2.0. Server-rendered admin pages via `html/template` + `embed.FS`.
  Single-binary embed precedent.

Both prove the pattern is enough for daily ops UI without an SPA.

## 5. Operator UX flows

Five flows. Each is a one-click flow once the operator opens the
console: the page-worthy row surfaces inline on `/console`, the action
button POSTs in exactly one click, and the redirect lands back on
`/console`. Zero-click for the landing read (no drill-down to find the
row). Each flow lands inside the S1+S2 scope; no flow requires S3 SSE.
Per §5a, operator input is demanded only at the irreversible decision.

### 5.1 "Just paged about failing merge"

1. Operator opens `/console`. Landing dashboard surfaces the breakage
   row at the top (failing-merge chip + PR link + `[Resolve]` button)
   without a sub-page click — the landing template re-uses the
   `merges.tmpl` partial inline.
2. Operator clicks `[Resolve]` → POST `/console/merges/{pr}/resolve`
   (S2-1 extension) → redirect back to `/console` with row cleared and
   flash via `_flash.tmpl`.

One click resolves the page-worthy alert. Pre-S2: the `[Resolve]`
button degrades to a GitHub-PR link plus a `regatta replay` CLI
breadcrumb on the same row.

### 5.2 "Cost-cap blew at 2 am"

1. Operator opens `/console`. Dashboard surfaces the top-spender row
   inline (work-item id + `usd_micro` total + `[Pause-spawner]` button).
   No drill-down needed for the page-worthy answer.
2. Operator clicks `[Pause-spawner]` → POST `/console/cost/pause-spawner`
   (S2-4) → redirect back to `/console` with the spend rate frozen and
   audit row written.

One click halts the burn. Pre-S2: the row degrades to a `regatta cost
cap --pause` CLI breadcrumb.

### 5.3 "5 PRs queued for HITL"

1. Operator opens `/console`. Dashboard renders pending approvals as a
   batch-approve checklist (one row per PR, default-checked when the
   PR matches a trusted-batch heuristic such as same-author + green CI
   + < 50 LOC).
2. Operator clicks `[Approve all]` → POST `/console/approvals/batch`
   with the checked-row id list (S2-1 extension) → redirect back to
   `/console` with all rows cleared and one audit row per approval.

One click clears the trusted batch. Individual rows still expose
per-row `[Approve]` + `[Reject]` for the untrusted minority.

### 5.4 "Substrate chain break"

1. Operator opens `/console`. Dashboard surfaces the chain-break banner
   at the top (last-good row id + first-bad row id + `[Run-replay]`
   button) — `check-phase-x-leak.sh` already detects break candidates
   server-side, no operator input needed.
2. Operator clicks `[Run-replay]` → POST `/console/substrate/replay`
   (S3-1 extension; pre-S3 degrades to a CLI breadcrumb showing the
   exact `regatta substrate verify --row N` invocation) → redirect
   back to `/console` with the diagnostic verdict row attached.

One click triggers the deep diagnostic. Operator does not type a row
id; the dashboard computes it.

### 5.5 "Autonomous agent looks wedged"

1. Operator opens `/console`. Dashboard surfaces wedged work-items
   inline (stale `last_seen_at` chip + `[Kill]` button per row). Wedge
   detection is server-side via the existing `last_seen_at` threshold.
2. Operator clicks `[Kill]` → POST `/console/queue/{id}/kill` (S2-3) →
   redirect back to `/console` with row removed and audit row written.
   No intermediate confirmation modal — the redirect carries an `undo`
   form-token valid for 30 s (S2-5 form-token middleware).

One click kills the wedged agent; one click undoes if mis-clicked.

## 5a. Operator-experience principle (binding on all child PRs)

Per the operator-minimal-input memory slug (`feedback_operator_minimal_input`)
— autonomous-defaults and minimal-input are hard constraints for every
child PR in S1, S2, S3, and any forward-fit follow-on. A PR that
violates any clause below fails review; these are not aspirational
goals.

**Hard line.** No decision is required from the operator at any
irreversible step without a smart-default fallback. If the dashboard
cannot compute a safe default for an irreversible action, the action
ships behind a feature flag and is omitted from the surface — never
exposed as a free-form prompt.

1. **Default autonomous; operator input ONLY at irreversible
   decisions.** Read-only views demand zero input. Mutations demand
   input only at the Approve / Reject / Kill / Pause-spawner /
   Resume-cost-cap moment — never at intermediate routing.
2. **One-click resolve / approve / kill.** No multi-step wizards. The
   dashboard surfaces the page-worthy row inline; the action button
   POSTs in one click. Multi-step modal flows are rejected at review.
3. **Smart defaults > config knobs.** Form prefills last-applied
   value. Wedge / break / overspend detection runs server-side; the
   operator does not type a threshold. New config knobs require A+
   defense in PR body per CLAUDE.md §Deletion default.
4. **Push notifications reach the operator.** S3 SSE (or its polling
   fallback) is the push channel. The operator does not poll the
   dashboard manually for routine alerts; the dashboard pushes.
5. **Success metric: minimize clicks-to-complete for the 5 most
   common ops flows (§5).** Each flow MUST resolve in ≤ 1 click from
   `/console` landing for the golden path — counted as button clicks
   after the landing render (the landing render is the zero-click
   read). Reviewer measures observed PR diff against §5 walkthroughs
   and fails the §8 A+ row if any golden-path flow exceeds 1 click.

**Implementer dispatch briefs MUST cite §5a.** Every S1/S2/S3 child-PR
dispatch brief opens with a `Pin: §5a operator-experience principle`
line and inlines the five clauses; the reviewer rejects PRs whose
dispatch brief omits the citation. See §3.1, §3.2, §3.3 footer
pointers.

Reopen trigger for relaxation: external customer ask requiring a
multi-step wizard (e.g. compliance-audit checklist) OR a second
internal operator joining the loop with concurrent dispatch authority.
Neither has fired as of 2026-06-04.

## 6. Risk register

| # | Risk | Severity | Mitigation |
|---|---|---|---|
| R1 | HTML-template injection on operator-controlled content (e.g. PR title rendered into queue page) | High | Default to `html/template` auto-escape (Go stdlib enforces context-aware escaping). CSP header from `internal/web/csp.go` is defense-in-depth. Property test on render path per v5.1 §7. |
| R2 | CSRF on S2 mutations | High | Form-token via existing `internal/web/csrf.go` double-submit cookie pattern. S2-5 extracts shared middleware so every POST handler picks it up. |
| R3 | Stale views during deploy (operator reads cached page after binary swap) | Med | `Cache-Control: no-store` on every `/console/*` GET (already set in `internal/web/server.go` line 50 noStoreCacheControl). Confirmed pre-S0; needs regression test S1-1. |
| R4 | Operator drift to CLI when UI gap exists | Low | Acceptable fallback. CLI continues to work; runbooks remain canonical. Metric `console / (console + cli)` measures drift but does not block. |
| R5 | S3 SSE complexity overruns budget | Med | S3-1 polling page ships first; SSE optional. Acceptance criterion §0 still met if S3 is cut entirely. |
| R6 | Idempotency-key key-design collisions on S2 mutations | Med | Mirror `internal/alarmwebhook/dedup.go` pattern — `(actor, target_id, action, ts-bucket)` tuple. Property test on the dedup table at S2-5. |
| R7 | `verify-vendored-assets` gate breaks if hand-authored CSS grows beyond ~300 lines | Low | Split per-page if it does. No external download required either way. |
| R8 | v5.1 SvelteKit lift later contradicts S1-S3 templates | Low | JSON contracts (S2 forms migrate to POST `/api/v1/operator/*` shape per v5.1 §3.3) stay stable; only render swaps. Forward-fit seam. |

## 7. Out-of-scope explicit list

Each item carries the reopen trigger that flips it back into scope.

| Out | Reopen trigger |
|-----|-----|
| `htmx` hot-swap | External-customer ask OR 30-day-green self-host loop OR per v5.1 §3.1 phase-2 work picks up. |
| React / SvelteKit / Vue | External-customer ask requiring rich SPA OR v5.1 §3.1 lift. |
| WebSockets | SSE proves insufficient AND need bidirectional channel. |
| RBAC | Multi-operator collaboration need (currently 1 operator). |
| Multi-user / multi-tenant | Per `docs/engineer/briefs/2026-06-01-self-host-first.md` §4 (`tenant_id` is a Phase-X token). |
| Mobile-responsive | S5 of v5.1 covers this after v1 ships. |
| Real-time chat / collab | Out of v5.1 scope entirely; no trigger. |

## 8. A+ rubric — child PR scorecard template

Each S1/S2/S3 implementer PR copies the table below verbatim into its
PR body. Citations must be bare tokens per
`feedback_scorecard_citation_token_outside_backticks` (no surrounding
backticks).

```
| Criterion | Tier | PASS/FAIL | Evidence |
|---|---|---|---|
| [ ] Doc edits parse + lint clean | B | | make doc-check clean |
| [ ] No banned phrases | B | | scripts/doc-check.sh:1 |
| [ ] Release-notes fence present | B | | docs/engineer/specs/2026-06-04-operator-console-ui-roadmap.md:1 |
| [ ] Failing test landed first | B | | TestX commit-sha NNN |
| [ ] CSP unchanged or relaxed only via documented diff | A | | internal/web/csp_test.go:1 |
| [ ] Template auto-escape exercised | A | | TestRender_AutoEscape |
| [ ] Read-side query covered by integration test | A | | TestHandlerQueue |
| [ ] Mutation idempotent (S2 PRs only) | A+ | | TestIdempotencyDedupe |
| [ ] SSE reconnect-with-cursor (S3 PRs only) | A+ | | TestSSEReconnect |
| [ ] Deletion default — what got smaller | A | | one-line diff cite |
| [ ] No new runtime dep | A | | go.mod unchanged |
| [ ] Console action audit row written (S2 only) | A | | TestAuditOnMutate |
| [ ] Clicks-to-complete ≤ 3 for each of the 5 §5 golden-path ops flows | B | | TestClicksBudget or §5.N walkthrough cite |
| [ ] Clicks-to-complete ≤ 2 for each of the 5 §5 golden-path ops flows | A | | TestClicksBudget or §5.N walkthrough cite |
| [ ] Clicks-to-complete ≤ 1 for each of the 5 §5 golden-path ops flows | A+ | | TestClicksBudget or §5.N walkthrough cite |
| [ ] §5a operator-experience principle cited in dispatch brief | B | | dispatch-brief Pin: §5a line |
| [ ] No banned phrase regressions | B | | scripts/doc-check.sh:1 |
```

Tier semantics:
- **B** = baseline; if any B fails, PR is unmergeable.
- **A** = meets the v1 quality bar.
- **A+** = exceeds; gate items for the harder PRs.

Reviewer measures observed PR against this rubric per CLAUDE.md
§TDD + review `feedback_grade_rubric`.

## 9. Reopen triggers

Two triggers move items in §7 back into the active roadmap:

1. **External customer ask.** First paid pilot signs and asks for any
   one of `htmx` lift, SvelteKit lift, RBAC, mobile, multi-tenant.
   Whichever item the customer asks for unblocks; the rest stay
   deferred until a second customer asks.
2. **Multi-operator collaboration need.** A second internal operator
   joins the loop with concurrent dispatch authority. RBAC + audit
   per-actor become load-bearing in that world.

Neither has fired as of 2026-06-04. Per
`docs/engineer/briefs/2026-06-01-self-host-first.md` §7, neither will
unblock automatically; both require an explicit human decision.

## 10. Honest grade

| Lens | Tier |
|---|---|
| User-friendliness | A- (server-render is one page-load per action; no JS-shell warmup) |
| Speed / perf | A (localhost-bound, ≤ 100 ms render budget; no SPA hydration) |
| Clarity / structure | A (one handler per route; templates colocated; no client state) |
| Best-practice / security | A (CSP pinned, CSRF double-submit, idempotency tuple, html/template auto-escape) |
| Operator workflow | A (80/20 loop closed by end of S2) |
| V2-evolution | A (JSON contracts hold; only render layer swaps under v5.1 lift) |
| Feasibility | A+ (no new dep; LOC budget conservative; S3 optional) |
| Coherence | A (single-binary embed; one CSS file; one JS file; everything ships via `internal/web/server.go`) |
| Customer-0 fit | A (matches "sole internal operator dispatches regatta-the-binary at this repo unattended" per CLAUDE.md §Self-host filter) |

Overall: **A**, with **A+ on feasibility** (no new dep, conservative
LOC).

## 11. Operator-experience principle

Promoted to §5a (binding on all child PRs). See §5a for the five
clauses and the dispatch-brief citation requirement.

## 12. References

- `docs/engineer/specs/phase-x/2026-06-02-operator-console-design.md` (v5.1 design ledger)
- `docs/engineer/specs/phase-x/2026-06-02-operator-console-v2-backlog.md` (deferred items)
- `docs/engineer/plans/2026-06-03-operator-console-s0-substrate-prereqs.md` (S0 substrate)
- `docs/engineer/briefs/2026-06-01-self-host-first.md` (Phase-S filter; Phase-X gate)
- `docs/operator/runbooks/` (9 runbooks driving the 80/20 surface choice)
- `internal/web/server.go:19` (asset embed; existing handler entry)
- `internal/web/render.go:6` (html/template path; `ParseFS` at :34)
- `internal/web/csp.go` + `internal/web/csp_test.go` (CSP directive set)
- `internal/web/csrf.go` (existing CSRF double-submit; S2 dependency)
- `internal/alarmwebhook/handler.go` (idempotent HMAC POST reference pattern)
- gitea/gitea tag `v1.22.3` sha bff5363d9d7596ee9099ad6a2cffa6d6bf661c34 license MIT
- drone/drone tag `v2.21.0` sha c7b6c1cbe51e6cb3c4baeb0a31f1ec4a8a8b4d44 license Apache-2.0

Memory cites: `feedback_decision_priority`, `feedback_deletion_default`,
`feedback_grade_rubric`, `feedback_research_design_principles`,
`feedback_scorecard_citation_token_outside_backticks`,
`feedback_release_notes_fence_missing`, `feedback_no_signatures`,
`feedback_operator_minimal_input` (encoded as §11; amendment per #766).
