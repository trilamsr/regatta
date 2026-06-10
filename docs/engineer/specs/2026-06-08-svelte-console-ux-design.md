---
status: draft
phase: x-forward-fit
revision: v1
author: design pass (frontend-design + ui-ux-pro-max skills, operator review)
date: 2026-06-08
companion:
  - docs/engineer/specs/phase-x/2026-06-02-operator-console-design.md
scope_umbrella: "[#183](https://github.com/trilamsr/regatta/issues/183) operator web UI"
framing: |
  Design system + scaffold for the v5.1 operator console SvelteKit build.
  Wave 1 htmx-MVP at `internal/web/` ships in parallel and gets ripped in S1
  phase-3 per the in-flight v2 roadmap; this spec defines the visual + IA
  language for the SvelteKit replacement so subsequent slices land features,
  not boilerplate.
out_of_scope:
  - Backend changes (S0 substrate prereqs land in separate impl PRs).
  - Auth wiring (already shipped at `internal/web/auth.go`; the SvelteKit
    bundle inherits the same session cookie when embedded behind the Go
    handler).
  - htmx-MVP modifications (Wave 1 stays untouched until S1 phase-3 deletes
    it).
---

## 1. Design system choice

All three picks ran through the `ui-ux-pro-max` skill (CSV-backed) and were
cross-checked against the `frontend-design` skill's "no generic AI aesthetic"
gate. Each pick cites its source row.

### 1.1 Style — Data-Dense Dashboard

Source: `ui-ux-pro-max/data/styles.csv` row `Data-Dense Dashboard / BI-Analytics`.

- 12-col grid, gap `8px`, card padding `12px`, body type `12-14px`.
- Sticky table headers, sortable columns, row hover highlight at 150-300ms.
- Filter sidebar permanent at desktop widths (≥1024px).
- Light + dark mode full; ships dark-default to match terminal culture.
- Performance rating "Excellent" — no glassmorphism or backdrop-filter
  overhead.

Rejected alternative: Cyberpunk UI (neon-on-black). Disqualified by the skill's
own `Accessibility: ⚠ Limited (dark+neon)` flag and `Performance: Moderate`
rating. Operator console is read-many, read-fast — not a demo.

Rejected alternative: Exaggerated Minimalism. Operator screens carry 50+
data rows per surface; oversized type fights the IA.

### 1.2 Palette — Developer Tool / IDE

Source: `ui-ux-pro-max/data/colors.csv` row `Developer Tool / IDE`.

| Role          | Hex                        | Notes                                   |
|---------------|----------------------------|-----------------------------------------|
| Background    | `#0F172A`                  | Slate-900; matches CLI dark terminal.   |
| Card          | `#1B2336`                  | Slate-800 +1 tonal step for depth.      |
| Muted         | `#272F42`                  | Slate-700; table-row alt + disabled bg. |
| Border        | `#475569`                  | Slate-500; 3:1 against bg.              |
| Foreground    | `#F8FAFC`                  | Near-white; 16.8:1 against bg.          |
| Muted-fg      | `#94A3B8`                  | Secondary text; 7.2:1 against bg.       |
| Primary       | `#1E293B`                  | Slate-800 for primary surface buttons.  |
| Accent        | `#22C55E` (run-green)      | Status:running / success badge.         |
| Destructive   | `#EF4444`                  | Status:failed / cancel action.          |
| Ring (focus)  | `#22C55E`                  | High-contrast focus ring on accent.     |

Status semantic tokens layered on top of the base palette:
`--status-running: #22C55E`, `--status-pending: #F59E0B`,
`--status-blocked: #DC2626`, `--status-merged: #8B5CF6`,
`--status-cancelled: #64748B`. Every status pairs with a non-color affordance
(icon + uppercase label) per WCAG 1.4.1.

Light mode mirrors the palette via shadcn-svelte's CSS-variable swap. Light
default disabled — operators run terminals in dark; jarring whiteflash on tab
switch is the dominant complaint pattern in the Wave 1 htmx prototype.

### 1.3 Font pair — Developer Mono

Source: `ui-ux-pro-max/data/typography.csv` row `Developer Mono`.

- **Headings + tabular data + IDs + timestamps**: JetBrains Mono.
- **Body prose + form labels + helper text**: IBM Plex Sans.

Why: regatta is a CLI-first tool. Operators read run IDs, SHAs, durations,
PR numbers all day — these MUST be monospace so columns align without
`font-feature-settings: tabular-nums` hacks. IBM Plex Sans (humanist sans,
designed for terminals + UI by IBM) carries prose without clashing with the
mono. Pair scored `9/10` accessibility in the skill's CSV.

Loaded via local self-hosting (no Google Fonts CDN call from operator's
browser — single-tenant, offline-capable). Both families ship in `static/`
as WOFF2 subsetted to Latin + box-drawing.

Rejected alternative: Inter (sans only). The skill flagged Inter as
overused-AI-generic; operator pushed back on this in 2026-06-07 review of
the htmx Wave 1 prototype.

Rejected alternative: Pure Terminal CLI Monospace (all-mono). 14pt mono for
prose is tiring at long reading lengths — the operator-digest spec
(`docs/engineer/specs/2026-06-04-daily-weekly-operator-digest-design.md`)
ships paragraphs of narrative text that need a proper humanist face.

## 2. Information architecture — 5 primary surfaces

Top-level nav is a left sidebar (240px fixed) — not a top bar — because
operator screens benefit from vertical content space and the sidebar doubles
as a status-LED column. Bottom of sidebar holds command-palette hint + version
+ build SHA.

| Surface   | Path        | First-screen layout                                            | Key UX rules applied                                |
|-----------|-------------|----------------------------------------------------------------|------------------------------------------------------|
| Dashboard | `/`         | KPI row (5 cards) + live runs table (15 rows) + alert strip.   | `data-density`, `truncation-strategy`, `legend-visible` |
| Runs      | `/runs`     | Filterable table (full-bleed); detail drawer slides from right.| `sortable-table`, `state-preservation`, `virtualize-lists` |
| Agents    | `/agents`   | Agent grid (cards) + per-agent timeline pane.                  | `nav-state-active`, `empty-data-state`              |
| Reviews   | `/reviews`  | Two-pane: PR list (left) + diff/comment preview (right).       | `keyboard-nav`, `focus-management`, `escape-routes` |
| Settings  | `/settings` | Vertical tab list (sections) + form-per-section.               | `input-labels`, `inline-validation`, `error-clarity`|

Detail drawers + modals dismiss on `Esc`. Browser back is preserved via
SvelteKit's `goto(..., { keepFocus: true, noScroll: false })` + scroll
position cache in `+page.ts` `load`. Deep links work for every row
(`/runs/<id>`, `/agents/<id>`, `/reviews/<pr>`).

## 3. Component library — shadcn-svelte

Decision: **`shadcn-svelte`** (https://shadcn-svelte.com, pinned to
`0.14.x` at scaffold time).

Why:

1. **Not a dependency, source-on-copy** — components land in
   `web/console/src/lib/components/ui/` as plain `.svelte` files. We own +
   patch them; no semver-roulette on a wide UI surface. Matches CLAUDE.md
   "deletion default" pressure (we delete the install script after
   first run, keep only the components actually used).
2. **Tailwind-native** — fits the Data-Dense Dashboard style's atomic CSS
   need; no CSS-in-JS runtime cost.
3. **Mono-first variants exist** — `Badge`, `Table`, `Code` already render
   with mono. Custom theme override hits a single `app.css` file.
4. **CLI-installable per component** — `npx shadcn-svelte@latest add table`
   pulls only what we use; bundle stays small. The skeleton ships with
   Button, Input, Table, Badge, Dialog, Command (cmd+k palette), Sheet
   (drawer), Tooltip.

Rejected alternatives:

- **Skeleton.dev** — heavier opinionated theming; harder to escape from when
  we patch terminal aesthetics.
- **Flowbite-Svelte** — Bootstrap heritage; visual default conflicts with
  the dense-dashboard style.
- **Roll-our-own** — violates "adopt proven OSS over reimplementation"
  (CLAUDE.md cross-cutting design).

## 4. Interaction patterns

### 4.1 Keyboard-first navigation

Every primary action has a single-key or chorded keyboard binding. Bindings
ship in `src/lib/keymap.ts` as the single source of truth (rendered into the
`/help?keys` overlay).

| Action                | Binding   | Rule applied                       |
|-----------------------|-----------|------------------------------------|
| Command palette       | `cmd+k` / `ctrl+k` | `keyboard-shortcuts`      |
| Focus search          | `/`       | `focus-management`                 |
| Next row in table     | `j`       | vim-style; opt-in via Settings.    |
| Prev row in table     | `k`       | same.                              |
| Open selected row     | `enter`   | always-on.                         |
| Close drawer / modal  | `esc`     | `escape-routes`                    |
| Cycle surface (tab)   | `g d` / `g r` / `g a` / `g v` / `g s` | gmail-style. |

Tab order is verified against visual order on every surface via Svelte's
`use:focusTrap` action on modals + plain DOM order elsewhere.

### 4.2 Command palette (cmd+k)

Source: `shadcn-svelte` `Command` primitive (wraps `cmdk-sv`).

Three command groups:

1. **Navigation** — jump to surface / run-by-ID / agent-by-name.
2. **Action** — cancel run, retry run, open in CLI (`regatta runs open <id>`).
3. **Settings** — toggle theme, toggle vim mode, log out.

Fuzzy-search across all three groups; recent commands persist to
`localStorage`. Empty-state shows top 5 recent + 3 onboarding tips.

### 4.3 Dense table views

- Row height `36px` (per skill's `--table-row-height`).
- Sticky header + sticky first column at ≥1024px.
- Hover row tint: `bg-muted/40` (160ms ease-out).
- Column sort indicator inline (Lucide `arrow-up` / `arrow-down`, 14px).
- Selection: click-to-open detail drawer; `cmd+click` toggles multi-select.
- Action overflow: right-edge `...` button reveals dropdown
  (`rename`, `cancel`, `copy ID`).

### 4.4 Pagination vs infinite-scroll

**Decision: cursor-based pagination, NOT infinite scroll.**

Reasoning:

- Operator needs reproducible deep links (`?cursor=abc&limit=50`); infinite
  scroll breaks `state-preservation` on back-nav.
- Total counts matter ("show me the 47 failed runs this week"); infinite
  scroll hides totals.
- Virtualized rendering still applies within a page (`@tanstack/svelte-virtual`)
  so a 200-row page stays smooth on a 10-year-old laptop.

`load` functions accept `cursor` + `limit` URL params; default `limit=50`,
adjustable in Settings.

## 5. Accessibility — WCAG 2.1 AA baseline

Audit gates land in the implementation PR (not this design):

- **Contrast**: every color pair in §1.2 verified at 4.5:1+ (body) / 3:1+
  (large + UI components). Status colors paired with icon + label
  (`color-not-only`).
- **Keyboard**: `axe-core` run in CI via `@axe-core/playwright` against
  every surface; fails on serious/critical.
- **Screen reader**: `main`, `nav`, `aside`, `header` landmarks present on
  every layout. Skip-link to `#main` first focusable element.
- **Reduced motion**: all animations gated on
  `(prefers-reduced-motion: no-preference)`. Default duration `180ms`;
  reduced-motion fallback `0ms`.
- **Focus rings**: 2px solid `--ring` offset `2px`. NEVER removed
  (`focus-states`).
- **Dynamic type**: base `16px`; user-zoom permitted (no `user-scalable=no`).

## 6. Layout grid

- 12-col CSS Grid; `gap: 8px`; container `max-width: 1440px` centered.
- Min viewport width: `1024px` (operator on laptop). Below `1024px`,
  show a banner: "Open on a wider screen for full operator console."
  (Mobile triage via CLI / TUI — not a UI goal.)
- Breakpoints: `1024` / `1280` / `1440`. Sidebar collapses to icon-only at
  `<1280px`; full label at `≥1280px`.
- Vertical rhythm: `4 / 8 / 12 / 16 / 24 / 32 / 48` spacing scale; enforced
  via Tailwind `spacing` extension.
- Z-index scale: `base 0 / sticky 10 / drawer 30 / modal 50 / toast 70 /
  command-palette 90`.

## 7. Data fetching

- **SvelteKit `+page.ts` `load` functions** call the Go backend's JSON API
  (`/api/v1/runs`, etc) over `fetch`. Same-origin in production (Go binary
  serves both bundle + API); dev runs Vite on `:5173` with a proxy to
  `:8080`.
- **Streaming** via `Response.body` + `TransformStream` for the runs SSE
  endpoint (`/api/v1/events`). SvelteKit's `streamed` response wrapper
  yields partial UI before the full page resolves.
- **Optimistic updates**: cancel-run / retry-run mutations apply locally
  first, roll back on server error toast.
- **Cache**: each `load` returns `{ depends: ['runs:list'] }` so a mutation
  can `invalidate('runs:list')` to force refetch. No global state store —
  SvelteKit + URL params + `localStorage` for preferences only.

## 8. Build pipeline

- **Vite + SvelteKit** (`@sveltejs/kit` ^2.x, Svelte ^5.x with runes).
- **Static adapter** (`@sveltejs/adapter-static`) emits to
  `web/console/build/`. The Go backend embeds it via `//go:embed
  web/console/build/**` (post-S1 phase-3; htmx Wave 1 retains its own
  embed until then).
- **TypeScript strict mode** — `tsconfig.json` extends SvelteKit defaults
  with `"strict": true`, `"noUncheckedIndexedAccess": true`.
- **Tailwind** v3.4.x to match the existing `make build-tailwind` toolchain
  pin in `Makefile.d/build.mk`. Two Tailwind configs coexist
  (`internal/web/tailwind.config.js` for htmx, `web/console/tailwind.config.js`
  for SvelteKit) until htmx is deleted.
- **CI**: `make ui-build` runs `npm ci && npm run build` inside
  `web/console/`. Output checked into the embed path on release branches
  only (CI verifies the build is reproducible from `package-lock.json`).

## 9. Mock UI screens

### 9.1 Dashboard (`/`)

```
+--------------------------------------------------------------------+
| [R] regatta console            cmd+k             status: 142 runs  |
+-----+--------------------------------------------------------------+
| [D] | KPI ROW                                                      |
| [R] | +---------+---------+---------+---------+---------+          |
| [A] | | RUNNING | QUEUED  | BLOCKED | MERGED  | FAILED  |          |
| [V] | |   12    |   31    |   2     |  84     |   13    |          |
| [S] | +---------+---------+---------+---------+---------+          |
|     |                                                              |
|     | ALERTS  3 PRs blocked >2h                            [view]  |
|     |                                                              |
|     | LIVE RUNS                                          [/ search]|
|     | #ID    AGENT       STATUS     PR    DURATION    AGE          |
|     | 8472   builder-a   running    981   00:12:43    2m ago       |
|     | 8471   reviewer-b  blocked    984   01:02:11    1h ago       |
|     | 8470   designer-c  pending    -     -           3m ago       |
|     | ...                                                          |
|     |                                                              |
| ⌘ K |                                                              |
| v1  |                                                              |
+-----+--------------------------------------------------------------+
```

### 9.2 Run detail (`/runs/8472`)

```
+--------------------------------------------------------------------+
| < runs / 8472                          [cancel] [retry] [copy ID]  |
+--------------------------------------------------------------------+
| HEADER                                                             |
| #8472  builder-a  running  PR #981  started 2026-06-08 14:22:01   |
|                                                                    |
| TABS: [overview] [events] [logs] [diff] [llm-trace]               |
|                                                                    |
| OVERVIEW                                                           |
|   Goal:     "feat: add svelte console scaffold"                    |
|   Wave:     2 of 4                                                 |
|   Cost:     $0.42 (≈ 14% of budget)                                |
|   Tokens:   in 12,400 / out 3,180                                  |
|                                                                    |
| EVENTS (last 5, streaming)                                         |
|   14:22:01  spawned                                                |
|   14:22:14  tool:Edit  src/routes/+page.svelte                     |
|   14:23:02  tool:Bash  npm run build                               |
|   14:23:48  tool:Bash  make pre-push-check                         |
|   14:24:11  awaiting:review                                        |
|                                                                    |
+--------------------------------------------------------------------+
```

### 9.3 Review pane (`/reviews/981`)

```
+--------------------------------------------------------------------+
| reviews / PR #981                                  [approve] [block]|
+----------------------------+---------------------------------------+
| PR LIST           [filter] | DIFF                          (split) |
| #984 reviewer-b ! blocked  |  + web/console/src/routes/+page.svelte|
| #983 builder-a   pending   |  @@ -0,0 +1,42 @@                     |
| #981 builder-a   review    |  +<script lang="ts">                  |
| #980 designer-c  merged    |  +  import { onMount } ...            |
| #978 builder-d   merged    |                                       |
|                            |  COMMENTS                             |
|                            |  reviewer-b: nit, drop the comment    |
|                            |                                       |
|                            |  > [reply box]                        |
|                            |                                       |
+----------------------------+---------------------------------------+
```

## 10. Out of scope

- Backend API changes — S0 substrate (events SSE, runs list, agent list)
  lands in separate impl PRs per `docs/engineer/specs/phase-x/2026-06-02-operator-console-design.md`.
- Authentication — `internal/web/auth.go` shipped session-cookie + CSRF
  already; SvelteKit bundle reuses the cookie when embedded.
- Modifications to the Wave 1 htmx prototype at `internal/web/` — Wave 1
  remains the production UI until S1 phase-3 deletion.
- Mobile / tablet layouts — operator console is laptop-only by design;
  triage flows go through the CLI / future TUI.
- Production deployment changes — Go binary continues to serve `:8080`;
  embed swap happens in a future PR.

```release-notes
docs: spec operator console v5.1 SvelteKit design system + IA
```
