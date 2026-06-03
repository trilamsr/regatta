---
title: "MVR-2 T5 — W7 Wave 3 htmx polish + docs"
status: active
summary: "Pre-fetch skeleton for MVR-2 T5. Final W7 wave: mobile-optimized layout, per-handler OTel spans, a11y pass on the DAG tree, operator docs (one-page how-to + screenshots), and the polish items deferred from Wave 1/2. S (1 wk) effort. Closes W7 work-line. SKELETON."
---

# MVR-2 T5 — W7 Wave 3 polish + docs — skeleton spec

_Pre-fetch skeleton, 2026-06-03. Material elaboration deferred to MVR-2 dispatch. Source-of-truth: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 MVR-2-T5. Closes the W7 work-line begun at `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` and extended by MVR-1-T1 (Wave 1) + MVR-2-T1 (Wave 2)._

## 1. Scope

### 1.1 In scope

Polish items deferred from Wave 1 + Wave 2, plus the user-facing documentation that closes the W7 work-line:

| # | Item | Origin | Effort |
|---|---|---|---|
| P1 | Mobile-optimized layout (responsive collapse at <768 px) | Wave-1 §2.3 defer | ~1d |
| P2 | Per-handler OTel spans (`http.server.duration`, `http.server.active_requests`) | Wave-1 §2.3 + Wave-2 §1.2 | ~1d |
| P3 | A11y pass on DAG `<details>` tree (screen-reader nav, keyboard focus) | Wave-2 §6 deferred | ~1d |
| P4 | UI per-tenant filter dropdown | T2 §1.2 deferred | ~0.5d |
| P5 | Operator docs — `docs/user/regatta-web-ui.md` (one-page) + 4 screenshots | New | ~1d |
| P6 | Wave-1 cost panel sparkline (cost trend over 24h) | Wave-1 followup #2 | ~1d |
| P7 | Wave-2 PR detail "reviewer reaction summary" (emoji counts) | Wave-2 nicety | ~0.5d |

Total: ~6 person-days = 1 wk solo-implementer.

### 1.2 Out of scope

- Write paths — still CLI-only. UI write surface is its own multi-week wedge (post-MVR-2).
- SSE / WebSocket streaming — defer (Phase X).
- Internationalization / i18n — followup.
- Theme customization — followup.
- Dark mode toggle — opt-in via Pico CSS `@media (prefers-color-scheme: dark)` ships automatically; no toggle in v1.

## 2. Architecture (high-level)

### 2.1 Mobile layout (P1)

Pico CSS already responsive by default. Wave-3 work: explicit `<meta viewport>` audit, table panels collapse to card-style under 768 px (Pico's `<table role="grid">` does this), DAG `<details>` indents reduce. CSS additions: ~30 lines in `internal/web/ui/static/pico.min.css` overlay (no library swap).

### 2.2 OTel spans (P2)

`http.Handler` middleware wraps every UI handler:

```go
import "go.opentelemetry.io/otel/instrumentation/net/http/otelhttp"
mux := http.NewServeMux()
// register routes
handler := otelhttp.NewHandler(mux, "regatta.ui")
```

Spans tagged with `http.route`, `http.method`, `http.status_code`. Counter `regatta_ui_requests_total{route, status}` for the dashboard's own self-observation.

### 2.3 A11y (P3)

- `<details>` tree gets `aria-expanded` synced to open state via inline JS (~20 LoC, CSP-safe via `<script>` tag in template)
- Keyboard: Tab into `<summary>`, Enter toggles, arrow keys navigate siblings (native HTML spec — verify Pico CSS doesn't break it)
- Color contrast: WCAG AA on the cost panel's red/green status colors (Pico defaults pass; verify with `axe-core` CI step)

### 2.4 Per-tenant filter (P4)

Dropdown on `/ui` base page, populated from `SELECT DISTINCT tenant_id FROM events`. Cookie persists choice. Operator with single tenant (`'default'`) sees no dropdown (degenerate case).

### 2.5 Docs (P5)

`docs/user/regatta-web-ui.md` — one-page operator how-to:
- Section 1: enable the UI (`--ui-addr localhost:8079`)
- Section 2: the 5+ panels and what each answers
- Section 3: the DAG view (when + how)
- Section 4: the PR detail surface
- Section 5: troubleshooting (port conflict, empty panels, gh not on PATH)

Four PNG screenshots checked into `docs/user/img/` (gitignore exception). Screenshots regenerated via `make ui-screenshots` (chromedp script — followup automation, not Wave-3 scope).

## 3. Key risks (named, ≥6)

| # | Risk | Mitigation seed |
|---|---|---|
| R1 | Mobile layout breaks the DAG tree at deep depth (5+ indents overflow viewport) | Horizontal scroll on `<details>` overflow; tested at 320 px (oldest target) |
| R2 | OTel span cardinality explosion if `http.route` includes path params (`/ui/dag/{run_id}`) | Use route templates not concrete paths in span name; verified via cardinality test |
| R3 | A11y inline JS violates CSP (`script-src 'self'`) | Nonce-based CSP OR move JS to vendored static file; pick second (simpler) |
| R4 | Tenant dropdown queries `SELECT DISTINCT` on a 1M-event table every page load | Cache 60 s; tenants change rarely. Cache invalidates on `policy_revision` event (W8) |
| R5 | Docs go stale immediately on Wave-3 launch | Screenshots dated in caption + regenerated via `make ui-screenshots`; doc-check.sh asserts screenshot dates within 30 d of doc-mtime |
| R6 | Wave-3 polish slips into Wave-4 (no Wave-4 planned → load-bearing W7 leftover) | Aggressive scope-cut: any P-item that goes over budget gets deferred to followup issue + tracked per `feedback_unaddressed_load_bearing` |
| R7 | OTel adds latency at handler boundary (otelhttp.NewHandler overhead) | Bench: target <100 µs added per request; baseline measured pre-Wave-3 |
| R8 | Screenshots leak operator data (PR titles, cost numbers, tenant IDs) | Screenshot generator runs against a synthetic fixture tenant; no real data in `docs/user/img/` |

## 4. Test plan (≥8)

- `TestUI_MobileLayout_320pxNoHorizontalScroll` — viewport regression, fixture HTML rendered + width-asserted
- `TestUI_OTelMiddleware_SpansEmittedOnEveryHandler` — fakeexporter checks span count
- `TestUI_OTelCardinality_RouteTemplatesNotConcretePaths` — assert span names match a fixed set
- `TestUI_A11y_DetailsTreeAriaExpandedSyncs` — html parse + attribute check
- `TestUI_A11y_KeyboardNavigation` — chromedp e2e (followup), unit test verifies tabindex
- `TestUI_TenantDropdown_HiddenForSingleTenant` — default-only operator sees no dropdown
- `TestUI_TenantDropdown_CacheInvalidatesOnPolicyRevision` — substrate event triggers refresh
- `TestUI_CostSparkline_RendersZeroState` — empty cost ledger shows flat sparkline, not 500
- `TestUI_ReviewerReactionSummary_GroupsByEmoji` — fixture PR with 3 reactions, grouped correctly
- `TestDocs_RegattaWebUIMd_ScreenshotsExistAndDated` — doc-check.sh assertion

## 5. Dependency order

`MVR-1-T1 W7 Wave 1` (shipped or in-flight at MVR-2 dispatch) → `MVR-2-T1 W7 Wave 2` (lands first MVR-2 PR) → `MVR-2-T2 multi-tenant` (tenant dropdown depends on tenant_id read path) → this spec lands as final W7 polish. Parallelizable with T6+T7 (substrate bridge + /workflows UI) since touch-sets are disjoint.

## 6. Deferred to dispatch-time elaboration

- Exact OTel attribute naming (semconv 1.27+ vs 1.30 at dispatch)
- Pico CSS overlay file name + bundling
- Screenshot generator script (chromedp vs playwright-go vs manual) — pick at dispatch
- A11y testing harness (axe-core via chromedp vs go-axe) — pick at dispatch

```release-notes
none (internal — design spec skeleton, pre-fetched for MVR-2)
```
