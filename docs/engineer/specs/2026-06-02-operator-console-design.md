---
status: draft
revision: v5.1 (verification-pass fixes folded: drop Sigstore v1; policy-class effect; plan-level nits)
author: brainstorming-session (operator + 14 reviewer subagents across 4 rounds)
date: 2026-06-02
last_revised: 2026-06-03
supersedes:
  - docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md
  - docs/engineer/specs/2026-06-01-w7-wave2-admin-pages-design.md
  - docs/engineer/specs/2026-06-02-obs-wave-d-operator-surface.md (T2 bubbletea TUI portion only)
companion:
  - docs/engineer/specs/2026-06-02-operator-console-v2-backlog.md
scope_umbrella: "[#183](https://github.com/trilamsr/regatta/issues/183) operator web UI"
framing: |
  Sunk-cost = free. Existing code may be ripped + rebuilt whenever
  measurable design quality improves. Delivery quality > velocity >
  preserving in-flight work.
---

# Regatta Operator Console — v1 design (v5.1)

## 0. TL;DR

**Customer 0 = regatta itself.** Dual-principal console: regatta-as-service-
identity drives the autonomous loop + writes audit + self-actions; human
operator intervenes on exception.

v5.1 patches two v5 verification-pass findings:
- **Sigstore Rekor dropped from v1** → S3-bucket-object-lock alone. Sigstore
  moves to v2 backlog D6.
- **`tool_call.expected_effect` replaced by `declared_effect_class`** —
  policy-class declared at run-dispatch from agent-config, NOT per-call ML
  prediction. Feasible without Claude-Code interception.

Plus plan-level nits folded: migration numbers pinned 0017-0027;
`/api/v1/operator/self-attest` endpoint added; CHECK constraint on
`audit_log` regatta-self confidence; idempotency_dedupe PK includes
`run_id`; override-rate decay = rolling 24h; shadow-mode "consecutive" =
per action-kind grouping.

**Honest timeline: ~26 weeks for v1.**

### Slice shape

- **S0 — Substrate prereqs** (5 wk): runs registry + producer; `run_id`
  backfill on `work_items` + `approval_events`; `tool_call` substrate
  kind w/ `declared_effect_class` + `observed_effect`; Claude-Code shim
  observed-effect capture; CI-checks poller; mergeStateStatus poller.
- **S1 — Foundation** (6 wk): **3-phase htmx rip** (re-port → drop →
  CSP-rewrite); SvelteKit static scaffold; Go-served `/` redirect + no-JS
  fallback; dual-principal auth (`__Host-` cookie + bearer);
  service-identity 2nd KID w/ rotation table + version-pinned + overlap-
  grace; `audit_log` Merkle chain + S3-bucket append-only anchor; full
  causal-hash; dual-principal Idempotency + rate-limit; self-API skeleton
  (heartbeat, blocked, surprise, actions, retract, shadow-proposals,
  operator-reactions, issues, comments).
- **S2 — Witness + diagnostic** (4 wk): `/witness/autonomy` landing w/
  hero loop status + 14-d calibration sparkline + 5-state stuck enum +
  confidence color bands + cost / latency / blast-radius anomaly chips +
  `[file-gap-issue]` everywhere + Cmd-K + keyboard + `/operator/self-attest`.
- **S3 — Debug surface** (5 wk): runs registry + event-list + postmortem
  cross-table aggregator + `causal_hash` rerun + sibling-run diff +
  same-shape past-failures sidebar (sqlite FTS5) + surprise log +
  three-click-why breadcrumb.
- **S4 — Steer** (4 wk): operator mutations + regatta self-mutations w/
  execution_mode (shadow|live) + retract trail + cost-budget +
  blast-radius assertions + queue drag-reorder (a11y) + self-actions
  log + shadow-proposals verdict UX.
- **Integration buffer** (2 wk).
- **S5 — Human-operator polish** (2 wk; ships after v1).

**Total v1: ~26 wk.** S5 = +2 wk after.

Companion: `docs/engineer/specs/2026-06-02-operator-console-v2-backlog.md`.

---

## 1. Framing

### 1.1 Two principals, one substrate

| Principal | Auth | Tree | actor_kind |
|---|---|---|---|
| `regatta-self` | HMAC bearer (2nd KID, version-pinned, overlap-grace rotation) | `/api/v1/self/*` write + read-own + reactions-read; `/api/v1/operator/*` read | `regatta-self` |
| `human-operator` | `__Host-regatta` cookie SameSite=Lax + CSRF double-submit | `/api/v1/operator/*` read + write | `human` |

Both audit through `audit_log` w/ `actor_kind` tag + Merkle hash chain
+ S3-bucket append-only anchor. Both share Idempotency + rate-limit
infra (separate buckets).

### 1.2 v1 acceptance — policy-coverage ratio

> **Policy-coverage ratio ≥ 0.85** over rolling 14-day window:
> `policy_coverage = self_rows_with_confidence_ge_0.5 / (self_rows + human_rows_overriding_self)`
>
> Measures: regatta self-acts with high confidence + humans rarely
> override. Closing the gap > suppressing rows.

Plus:
- Operator self-attests no `gh pr` / `ssh` invocation in window via
  `/api/v1/operator/self-attest` (audited under `actor_kind='human'
  action='self-attest'`).
- Loop heartbeat green for full window (gap ≥ 5 min = red).

### 1.3 Slice ordering — Debug before Steer

Human opens UI only on exception. When exception happens, human needs
diagnostic before intervention. Order **S0 → S1 → S2 → S3 → S4**.
Landing page `/witness/autonomy` shows fleet + loop health + recent
self-actions; inbox + debug + steer one click each.

### 1.4 v1 non-goals (reordered, not deleted)

| Item | Reordered to |
|---|---|
| Mobile WCAG AA + swipe + bottom-sheet | S5 |
| Slack-deep-link approval | S5 (Partitioned cookie alongside `__Host-`) |
| PWA / push | v2 backlog C5 |
| Dashboards (time-series + virtualized table) | v2 backlog B1, B2 |
| Loop detector (auto) | v2 backlog C6 (5-state stuck enum lives v1) |
| Multi-tenant + tenant context-threading | v2 backlog C1, C2 |
| Vendor-bearer embed mode | v2 backlog C4 (regatta-self bearer ships v1) |
| SLSA L2 + sigstore-signing + SBOM | v2 backlog D1-D5 |
| Sigstore Rekor anchor | v2 backlog **D6 (new)** |
| OpenAPI codegen | v2 backlog E1 |
| Cost rollup tables | v2 backlog E6 |
| Persistent fleet-pulse strip | v2 backlog C8 (autonomy landing covers v1) |
| "Since last visit" delta card | v2 backlog C7 |

---

## 2. Stack — final

| Layer | Choice | Reasoning |
|---|---|---|
| Frontend | SvelteKit 2 `@sveltejs/adapter-static` | Build-time HTML+JS into `embed.FS`; Go owns ALL runtime. |
| Language | TypeScript strict + `noUncheckedIndexedAccess` | Compile-time XSS + contract enforcement. |
| Components | shadcn-svelte vendored under `web/src/lib/components/ui/` | Source-owned; pinned-version manifest. |
| Charts | Layerchart SVG renderer v1 | Canvas-mode = v2 backlog B1. |
| Style | Tailwind v4, content globs scoped to `web/src/**/*.{svelte,ts}` | No template double-scan. |
| Design tokens | shadcn defaults + slate + one accent (locked S2) | Chip palette depends on it. |
| Client state | SvelteKit client-side `load()` + per-channel SSE stores | adapter-static = no SSR; per-channel cursors. |
| Backend | Go `cmd/regatta serve`; rip + replace `internal/web/` in 3 phases | Sunk-cost = free; rebuild to lift bundle size, a11y, and audit-chain depth. |
| API | REST `/api/v1/operator/<resource>` + `/api/v1/self/<resource>` + per-channel SSE `/api/v1/stream/<channel>` | Two-principal tree. |
| Auth — operator | `__Host-regatta` cookie SameSite=Lax httpOnly Secure + CSRF double-submit + Origin / Sec-Fetch-Site | S5 Slack-deep-link uses Partitioned cookie alongside. |
| Auth — regatta-self | `Authorization: Bearer <self-KID-token>` HMAC over `(method, path, body-hash, ts, nonce, kid_version)` + version-pinned + overlap-grace rotation | Closes silent self-API outage on rotation. |
| Replay protection | Mandatory `Idempotency-Key`; scoped `(actor_kind, user_id, run_id, key)` | No cross-run collision. |
| Rate limit | Per-principal token bucket (separate human / self) + global cap | Stolen-cookie blast bounded. |
| Audit | `audit_log` Merkle hash chain + actor_kind + causal_hash + execution_mode + cost_budget + blast_radius + retracted_by_row_id + rollback_policy + regatta_confidence + predicted_outcome columns + **single S3-bucket external anchor v1** (object-lock + versioning, hourly). Sigstore Rekor → v2 backlog D6. | S3+object-lock is S3 object-lock + versioning tamper-evidence. |
| Build | Dev-machine SvelteKit dist v1; CI + SLSA = v2 backlog D1-D5 | No ceremony v1. |
| Assets | `embed.FS` into Go binary; hashed filenames + `Cache-Control: immutable` + `.br`/`.gz` siblings | Sub-100ms TTI on localhost. |
| CSP | `default-src 'self'; connect-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'` + standard hardening + Referrer-Policy: no-referrer + Permissions-Policy camera/mic/geo deny + X-Content-Type-Options nosniff + COR same-origin. **External-only assets; zero inline.** | adapter-static + Vite emit external-only; `inlineStyleThreshold: 0`. |
| Accessibility | WCAG 2.2 AA desktop day-1; mobile S5; mobile-blocker < 640 px in S2 | No retrofit cost. |
| Deploy | Localhost-bind default; Tailscale Serve runbook tested S5 | Phone access waits S5. |
| Index.html | **Go-served** — reads SvelteKit `web/build/index.html` from embed.FS; injects CSP + cookie + `/` → `/witness/autonomy` redirect | Resolves adapter-static-no-SSR. |
| No-JS fallback | Go-templated minimal HTML for `/witness/inbox` only (form POST approve/reject/kill) | Progressive enhancement. |

---

## 3. Backend

### 3.1 htmx rip — 3 phases (S1)

Existing `internal/web/` ships ~1370 LOC htmx-based UI on main. Single
external importer: `cmd/regatta/serve.go` (`web.LoadTemplates`,
`web.NewHandler`, `web.AssetsFS`, `web.Dependencies`, `RouteRegistrar`).
The `/approve/{id}` UI rides on `Dependencies.RouteRegistrar`.

**Phase 1 — S1.0 (~5 d):** Re-port `/approve/{id}` behind new SvelteKit
route `/steer/approvals/[id]/` w/ thin adapter; keep htmx live in
parallel under `--ui-v2=true` feature flag.

**Phase 2 — S1.1 (~5 d):** Drop htmx assets + templates + `render.go`;
remove `RouteRegistrar` seam; mount SvelteKit static handler +
Go-served index.html.

**Phase 3 — S1.2 (~3 d):** Rewrite `csp.go` for external-only assets;
delete htmx-CSP allowances; ship full directive set per §2.

Optional Phase 0 — freeze new commits to `internal/web/` for S1
duration.

### 3.2 Substrate prereqs (S0)

Net-new infrastructure. Migration numbers pinned for collision-safety:

| # | Migration | Purpose |
|---|---|---|
| 0017 | `create_runs.sql` | runs registry + producer hook |
| 0018 | `work_items_run_id.sql` | + run_id column + index + backfill |
| 0019 | `approval_events_run_id.sql` | + run_id column + index + backfill |
| 0020 | `substrate_kind_tool_call.sql` | full-rewrite per 0012 precedent |
| 0021 | `audit_log.sql` | Merkle chain table |
| 0022 | `idempotency_dedupe.sql` | replay-protection dedupe table |
| 0023 | `audit_anchor.sql` | external anchor table (S3 only v1) |
| 0024 | `regatta_self_key_rotation.sql` | version-pinned KID rotation |
| 0025 | `regatta_surprise.sql` | calibration log |
| 0026 | `shadow_proposals.sql` | shadow-mode proposal queue |
| 0027 | `last_seen_cursors.sql` | per-channel cursors |

**`runs` table:**

```sql
-- 0017
CREATE TABLE runs (
  id TEXT PRIMARY KEY,
  started_at INTEGER NOT NULL,
  finished_at INTEGER,
  status TEXT NOT NULL DEFAULT '',
  spec_hash TEXT NOT NULL DEFAULT '',
  model_hash TEXT NOT NULL DEFAULT '',
  prompt_template_hash TEXT NOT NULL DEFAULT '',
  tool_impl_hash TEXT NOT NULL DEFAULT '',
  seed TEXT NOT NULL DEFAULT '',
  versions_json TEXT NOT NULL DEFAULT '{}',
  causal_hash TEXT NOT NULL DEFAULT '',     -- sha256(canon(all above))
  rerun_of TEXT,                             -- FK to runs.id when this is a rerun
  trace_id TEXT NOT NULL DEFAULT '',
  declared_effect_class TEXT NOT NULL DEFAULT '' -- policy envelope from agent-config at dispatch
);
CREATE INDEX idx_runs_started ON runs(started_at DESC);
CREATE INDEX idx_runs_causal_hash ON runs(causal_hash);
CREATE INDEX idx_runs_rerun_of ON runs(rerun_of) WHERE rerun_of IS NOT NULL;
```

**`run_id` on work_items + approval_events:** 4 INSERT sites
(work_items) + 2 sites (approval_events) need passthrough — thread per
trace_id precedent (migration 0005). `approval_events` requires
extending `decide.Deps`.

**`tool_call` substrate kind** (0020, full table-rewrite per 0012):

```json
{
  "agent_id": "...",
  "signature": "<sha256 of normalized tool-name + args-shape>",
  "args_hash": "<sha256 of canonical args>",
  "declared_effect_class": "<from runs.declared_effect_class at dispatch>",
  "observed_effect": "<actual after-action observation>",
  "started_at": 1717000000,
  "finished_at": 1717000003
}
```

`declared_effect_class` = **policy declared at run-dispatch**, NOT
per-call ML prediction. Agent-config envelope (e.g.
`"filesystem-write+gh-mutation"` or `"read-only+gh-comment"`) copied
into `runs.declared_effect_class` at dispatch + into every `tool_call`
event under that run.

`observed_effect` compared against envelope. Exceeds envelope → emit
`surprise{metric='side-effect-unmodeled'}`. Coarse but feasible — no
Claude-Code interception required.

**`runs` producer:** S0 adds INSERT in
`internal/orchestrator/scheduler/dispatch.go` (or equivalent dispatch
site) emitting one row per run w/ full causal-hash computed from
canonical-JSON of inputs.

**Claude-Code shim observed-effect capture:** S0 wraps tool-call exit
in `internal/orchestrator/spawner/claude.go` (235 LOC today, no
instrumentation) to emit `tool_call` substrate event w/
`observed_effect` derived from post-call substrate scan (filesystem
diff, gh API mutations, cost delta).

**Pollers:**
- `internal/orchestrator/checks/` (net-new pkg) — `gh pr checks` poll →
  `agent_ci_changed` event payload `{pr, conclusion, status}` (~3-4 d).
- `internal/orchestrator/prwatch/ghcli.go` extended to decode
  `mergeStateStatus`; emit `agent_pr_dirty` event on DIRTY. Coordinate
  with `internal/orchestrator/merge/merge.go` decoder. (~1-2 d).

### 3.3 New JSON API — two-principal tree

#### S1 — Foundation
| Route | Verb | Principal |
|---|---|---|
| `/api/v1/healthz` | GET | both |
| `/api/v1/stream/<channel>` | SSE | both (channels: `inbox`, `heartbeat`, `replay`, `self-actions`) |

#### S2 — Witness
| Route | Verb | Principal |
|---|---|---|
| `/api/v1/operator/autonomy` | GET | operator |
| `/api/v1/operator/inbox` | GET | operator |
| `/api/v1/operator/inbox/seen` | POST | operator |
| `/api/v1/operator/self-attest` | POST | operator (declares no SSH/CLI; audited `action='self-attest'`) |
| `/api/v1/operator/agents/<id>/logs?tail=200` | GET | operator |
| `/api/v1/self/heartbeat` | POST | self |
| `/api/v1/self/blocked` | POST | self (5-state stuck enum + reason + ask) |

#### S3 — Debug
| Route | Verb | Principal |
|---|---|---|
| `/api/v1/operator/runs` | GET | operator |
| `/api/v1/operator/runs/<id>` | GET | operator |
| `/api/v1/operator/runs/<id>/replay` | GET | operator |
| `/api/v1/operator/runs/<id>/postmortem` | GET | operator |
| `/api/v1/operator/runs/<id>/rerun?causal_hash=<h>` | POST | operator (lookup via runs.id; new row carries rerun_of=parent.id) |
| `/api/v1/operator/runs/<id>/diff/<sibling_id>` | GET | operator |
| `/api/v1/operator/runs/similar?signature=<sig>` | GET | operator (FTS5 trace+tool_call signatures) |
| `/api/v1/operator/traces/<trace_id>` | GET | operator |
| `/api/v1/operator/surprise` | GET | operator |
| `/api/v1/self/surprise` | POST | self (9 metrics: cost/duration/verdict/novel-error/retry-exhausted/side-effect-unmodeled/self-contradiction/latency/blast-radius) |

#### S4 — Steer (operator + self mutations)
| Route | Verb | Principal |
|---|---|---|
| `/api/v1/operator/approvals/<id>` | GET | operator |
| `/api/v1/operator/approvals/<id>/decide` | POST | operator |
| `/api/v1/operator/agents/<id>/kill` | POST | operator |
| `/api/v1/operator/runs/<id>/kill` | POST | operator |
| `/api/v1/operator/cost/cap/resume` | POST | operator |
| `/api/v1/operator/gates/<gate>/override` | POST | operator |
| `/api/v1/operator/queue` | GET / PATCH | operator |
| `/api/v1/operator/issues/ui-gap` | POST | operator |
| `/api/v1/self/actions` | POST | self (auto-kill/resume/override w/ execution_mode + cost_budget + blast_radius declared) |
| `/api/v1/self/actions` | GET | both (self writes; both read) |
| `/api/v1/self/retract` | POST | self (links via `audit_log.retracted_by_row_id`) |
| `/api/v1/self/shadow-proposals` | POST | self |
| `/api/v1/self/shadow-proposals/<id>/verdict` | POST | operator |
| `/api/v1/self/operator-reactions?since=<cursor>` | GET | self (closed-loop read of human overrides) |
| `/api/v1/self/issues` | POST | self (regatta files own friction issues) |
| `/api/v1/self/comments` | POST | self (regatta comments own PRs) |

#### S5 — Human-operator polish
| Route | Verb | Principal |
|---|---|---|
| `/api/v1/operator/sessions/redeem` | POST | operator (Slack-deep-link short-lived signed token in POST body; Partitioned cookie set alongside `__Host-`) |

### 3.4 Tables (S0 + S1)

S0 tables in §3.2.

**S1 tables (migrations 0021-0027):**

```sql
-- 0021
CREATE TABLE audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  actor_kind TEXT NOT NULL,                -- 'human' | 'regatta-self'
  user_id TEXT NOT NULL,                    -- active HMAC KID
  action TEXT NOT NULL,
  target_id TEXT NOT NULL,                  -- for human overrides: target_id = <self-row-id>
  reason TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL DEFAULT '{}',
  causal_hash TEXT NOT NULL DEFAULT '',     -- links to runs.causal_hash
  regatta_confidence REAL,                  -- non-null when actor_kind=regatta-self
  predicted_outcome TEXT,                   -- non-null when actor_kind=regatta-self
  execution_mode TEXT NOT NULL DEFAULT 'live',  -- 'shadow' | 'live'
  cost_budget_micro INTEGER,
  blast_radius TEXT NOT NULL DEFAULT '',    -- 'agent' | 'run' | 'pr' | 'tenant'
  rollback_policy TEXT NOT NULL DEFAULT 'manual',
  retracted_by_row_id INTEGER,
  idempotency_key TEXT NOT NULL,
  run_id TEXT NOT NULL DEFAULT '',
  prev_hash TEXT NOT NULL,
  row_hash TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE (actor_kind, user_id, run_id, idempotency_key),
  FOREIGN KEY (retracted_by_row_id) REFERENCES audit_log(id),
  CHECK (
    (actor_kind = 'human') OR
    (actor_kind = 'regatta-self' AND regatta_confidence IS NOT NULL AND predicted_outcome IS NOT NULL)
  )
);
CREATE INDEX idx_audit_log_actor ON audit_log(actor_kind, created_at DESC);
CREATE INDEX idx_audit_log_target ON audit_log(target_id, created_at DESC);
CREATE INDEX idx_audit_log_causal ON audit_log(causal_hash) WHERE causal_hash != '';
CREATE INDEX idx_audit_log_shadow ON audit_log(execution_mode, created_at DESC) WHERE execution_mode = 'shadow';

-- 0022
CREATE TABLE idempotency_dedupe (
  actor_kind TEXT NOT NULL,
  user_id TEXT NOT NULL,
  run_id TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL,
  response_json TEXT NOT NULL,
  status_code INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  PRIMARY KEY (actor_kind, user_id, run_id, idempotency_key)
);
CREATE INDEX idx_idempotency_expires ON idempotency_dedupe(expires_at);

-- 0023 (single S3 anchor v1; Sigstore Rekor = v2 backlog D6)
CREATE TABLE audit_anchor (
  anchor_at INTEGER PRIMARY KEY,
  latest_row_id INTEGER NOT NULL,
  latest_row_hash TEXT NOT NULL,
  s3_uri TEXT NOT NULL DEFAULT '',
  s3_anchored INTEGER NOT NULL DEFAULT 0,
  retried_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_audit_anchor_at ON audit_anchor(anchor_at DESC);

-- 0024
CREATE TABLE regatta_self_key_rotation (
  kid_version INTEGER PRIMARY KEY,
  kid TEXT NOT NULL,
  active_from INTEGER NOT NULL,
  overlap_until INTEGER NOT NULL,
  retired_at INTEGER,
  code_version TEXT NOT NULL
);
CREATE INDEX idx_self_key_active ON regatta_self_key_rotation(active_from DESC);

-- 0025
CREATE TABLE regatta_surprise (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL,
  metric TEXT NOT NULL,  -- 9 metrics, see §3.7
  predicted REAL,
  actual REAL,
  z_score REAL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
);
CREATE INDEX idx_regatta_surprise_run ON regatta_surprise(run_id);
CREATE INDEX idx_regatta_surprise_z ON regatta_surprise(z_score DESC) WHERE z_score >= 2.0;

-- 0026
CREATE TABLE shadow_proposals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  action TEXT NOT NULL,
  target_id TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  regatta_confidence REAL NOT NULL,
  proposed_at INTEGER NOT NULL,
  resolved_at INTEGER,
  resolution TEXT,  -- 'ramp-to-live' | 'reject' | 'expired'
  resolved_by_user_id TEXT,
  resolved_reason TEXT
);
CREATE INDEX idx_shadow_proposals_pending ON shadow_proposals(proposed_at DESC) WHERE resolved_at IS NULL;
CREATE INDEX idx_shadow_proposals_action ON shadow_proposals(action, proposed_at DESC);

-- 0027
CREATE TABLE last_seen_cursors (
  user_id TEXT NOT NULL,
  channel TEXT NOT NULL,
  cursor TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, channel)
);
```

### 3.5 Stuck-reason taxonomy — 5-state machine enum + 3 human chips

| Enum | Meaning |
|---|---|
| `POLICY-GAP` | No policy covers this case; needs human policy authoring. |
| `NOVEL-STATE` | Unprecedented state-machine transition; needs human classification. |
| `VERDICT-AMBIGUOUS` | L4 verdict signal weak; needs human tiebreak. |
| `COST-AMBIGUOUS` | Cost vs benefit unclear; needs human approval. |
| `SAFETY-UNCERTAIN` | Blast radius / side-effect risk unclear; needs human sign-off. |

Each renders distinct chip (icon + reason text). Operator triages by
enum type, not opaque "stuck."

**Human-eye chips (rule-based):**

| Chip | Color | Icon | Rule |
|---|---|---|---|
| `AWAIT-APPROVAL` | blue | hand | `approval_event:requested` w/o `decided` > 10 min |
| `CI-RED` | red | x-circle | Latest `agent_ci_changed` conclusion=`failure` |
| `DIRTY` | amber | git-merge | `agent_pr_dirty` event in window |

### 3.6 Anomaly chips — cost / latency / blast-radius

Background sweeper computes trailing-7-d mean + sigma per metric per
PR over `substrate_events`. Emits:
- `cost_anomaly` (z > 2.0 on running token_spend rate)
- `latency_anomaly` (z > 2.0 on agent step duration)
- `blast_radius_anomaly` (observed effect span exceeds declared
  `blast_radius`)

Surfaces inline chip on inbox row + writes audit row.

### 3.7 Surprise log — 9 metrics

`/api/v1/self/surprise` accepts:
`cost` | `duration` | `verdict` | `novel-error` | `retry-exhausted` |
`side-effect-unmodeled` | `self-contradiction` | `latency` | `blast-radius`.

Console `/debug/surprise` renders rolling top-N + miscalibration trend.
`/witness/autonomy` shows 14-day calibration sparkline.

### 3.8 Full causal hash

`runs.causal_hash = sha256(canon({spec, seed, versions, model_hash,
prompt_template_hash, tool_impl_hash}))` via `internal/canon`. All
causal inputs included; hash drift = causal drift.

`/api/v1/operator/runs/<id>/rerun?causal_hash=<h>` — endpoint LOOKS UP
parent run via `runs.id` (PK), validates the supplied
`causal_hash` matches parent's, triggers a fresh run with a new
`runs.id` + `rerun_of = parent.id`. New run carries same `causal_hash`.
Sibling-run delta via `/api/v1/operator/runs/<id>/diff/<sibling_id>`.

### 3.9 Closed-loop calibration

**Linking convention (no new schema):** human override audit rows set
`target_id = <self-row-id>`. This is the closed-loop link.

`/api/v1/self/operator-reactions?since=<cursor>` returns recent human
overrides: `SELECT id, action, reason, created_at FROM audit_log WHERE
actor_kind='human' AND target_id IN (SELECT id FROM audit_log WHERE
actor_kind='regatta-self') AND id > ?`. Regatta polls hourly +
aggregates per-action-kind override rate.

**Decay function:** rolling 24h window. Per action-kind:
`override_rate = override_count_24h / self_action_count_24h`.

When `override_rate > 0.3` for action-kind, regatta auto-drops new
actions of that kind to `execution_mode='shadow'` until rate falls
back below 0.3.

### 3.10 Shadow-mode + retract

**Shadow:** novel action kinds (or after override-rate spike) POST to
`/self/shadow-proposals`. Human verdicts via
`/shadow-proposals/<id>/verdict`. **Ramp policy:** 3 consecutive
`ramp-to-live` verdicts grouped per `shadow_proposals.action` kind
(rejected or expired breaks streak). Auto-expire 7 d.

**Retract:** when surprise > 2σ or operator-reaction signals bad self-
action, regatta POSTs `/self/retract` linking via
`retracted_by_row_id`. Audit chain preserves both; UI renders both
with strikethrough on retracted.

### 3.11 Substrate integration

Read-only from existing tables + new `runs`. Writes through existing
transactional surfaces + new self-mutation surfaces
(`agent.SelfKill`, `gate.SelfOverride`, `cost.SelfResume`,
`self.Retract`, `self.ShadowPropose`) — all audited under correct
`actor_kind`.

### 3.12 Cost field

`usd_micro` (singular int64).

---

## 4. Frontend

### 4.1 Directory layout

```
web/
  package.json
  package-lock.json
  vite.config.ts
  svelte.config.js
  tailwind.config.ts
  src/
    app.html
    routes/
      +layout.svelte
      +layout.ts
      witness/
        autonomy/+page.svelte    ← S2 landing
        inbox/+page.svelte       ← S2
      debug/                     ← S3
        runs/+page.svelte
        runs/[run_id]/+page.svelte
        runs/[run_id]/postmortem/+page.svelte
        runs/[run_id]/diff/[sibling_id]/+page.svelte
        traces/[trace_id]/+page.svelte
        surprise/+page.svelte
      steer/                     ← S4
        approvals/[id]/+page.svelte
        overrides/+page.svelte
        queue/+page.svelte
        self-actions/+page.svelte
        shadow-proposals/+page.svelte
    lib/
      api/
        client.ts
        types.ts                 ← hand-written v1; codegen v2 backlog E1
        sse.ts                   ← per-channel SSE store
      components/
        ui/                      ← shadcn-svelte vendored
        cmdk/
        autonomy/
        inbox/
        debug/
        steer/
      stores/
        autonomy.ts
        inbox.ts
        replay.ts
        auth.ts
        cmdk.ts
  static/favicon.svg
  build/                         ← gitignored
```

### 4.2 Routing

- Go HTTP handler serves `/` w/ 302 → `/witness/autonomy`; sets CSRF
  cookie if missing.
- Routes: `/witness/{autonomy,inbox}`, `/debug/*`, `/steer/*`.
- Cmd-K global.
- No-JS: Go renders minimal `/witness/inbox`.

### 4.3 `/witness/autonomy` (S2) — hero loop status

```
┌─────────────────────────────────────────────────────┐
│ regatta autonomy                                    │
│                                                     │
│ 🟢 LOOP ALIVE — last self-action 7s ago             │
│   ─── HERO CELL (largest, color-band coded) ───     │
│                                                     │
│ 14-day green clock: day 9 / 14 — policy coverage    │
│   0.92 ▰▰▰▰▰▰▰▰▰░ — calibration sparkline →       │
│                                                     │
│ stuck (3)  •  self-actions today (142)              │
│ surprises 24h (2)  •  cost today $4.20 (z=0.4)      │
│ shadow proposals (1 pending)                        │
├─────────────────────────────────────────────────────┤
│ INBOX                                               │
│ [POLICY-GAP] #588 "ambiguous gate verdict" — esc?   │
│   regatta says: I cannot resolve · conf 0.41 🔴     │
│ [AWAIT-APPROVAL] #585 "merge-execute impl" · 12m   │
│ [CI-RED] #584 "tests timing out on darwin" · 4h    │
├─────────────────────────────────────────────────────┤
│ RECENT SELF-ACTIONS                                 │
│ 12:04 auto-kill agent on PR#586 (LOOP; conf 0.92)   │
│ 11:51 auto-resume cost-cap (estim $0.40 backfill)   │
│ 11:43 shadow-proposal: skip-tests-on-doc-only-PR    │
└─────────────────────────────────────────────────────┘
```

Hero cell = LOOP STATUS at largest visual weight. < 1 s scan for "is
autonomy healthy?"

### 4.4 Inbox row (S2)

```
[chip] [PR#] title                        [last-event-age]
       signed-by · confidence ▮▮▮░░ 0.41  [Primary] [⋯] [📝]
```

- Confidence rendered as color band (red < 0.3, amber 0.3-0.7, green >
  0.7) + numeric. Threshold-action: < 0.5 triggers auto-shadow-mode
  badge.
- Single context-aware primary action.
- Overflow `⋯` for rest.
- `📝` = one-click file-UI-gap-issue.
- Click row → right-side peek pane: diff + last-200 logs + last-error
  + 3-click-why breadcrumb + causal hash + rerun button.

### 4.5 Cmd-K palette + keyboard

S2 ships scaffold w/ inbox actions only. S3/S4 lift in their own PRs.
No fake "ships in S3" toasts.

S2 keyboard: `j`/`k` nav, `Enter` peek, `a` approve, `r` reject, `x`
kill (Undo-snackbar 5 s), `/` search, `?` cheat-sheet, `g a` autonomy,
`g i` inbox. Cheat-sheet auto-records unrecognized keystrokes → backlog.

### 4.6 Debug surface (S3)

- `/debug/runs/<id>` event list (joins work because S0 added run_id).
- `/debug/runs/<id>/postmortem` aggregator + tool_call
  `declared_effect_class` vs `observed_effect` view + CLI one-liner
  `regatta replay <causal_hash>` at top. Right sidebar: same-shape
  past failures (sqlite FTS5 on trace + tool_call signatures).
- `/debug/runs/<id>/diff/<sibling_id>` sibling-run delta.
- `/debug/traces/<trace_id>` bare text + copy (v2 backlog A2 adds URL
  template).
- `/debug/surprise` calibration log + 9-metric breakdown.

Three-click-why guaranteed on every event row.

### 4.7 Steer surface (S4)

- `/steer/approvals/[id]` desktop-first.
- `/steer/overrides`.
- `/steer/queue` drag-reorder w/ a11y (arrow-keys + ARIA live-region).
- `/steer/self-actions` filterable + CSV/JSON export.
- `/steer/shadow-proposals` verdict UX.

Modal-fatigue: Undo-snackbar 5 s for Kill/Reject; modal w/ reason for
Override/Resume/Retract.

### 4.8 Mobile + Slack-deep-link (S5)

After v1. WCAG AA at ≤ 640 px + swipe + bottom-sheet + 44×44 tap-
targets. Slack-deep-link redemption: token in POST body; Partitioned
cookie (CHIPS) alongside `__Host-`. Safari < 17 fallback documented.
Tailscale Serve runbook iOS + Android tested.

### 4.9 Empty + offline (S2)

- Empty inbox: "All clear. Loop alive — last self-action 7s ago.
  Policy coverage 0.92."
- SSE per-channel stale banner w/ cursor-age + refresh.
- No JS: Go renders minimal HTML for `/witness/inbox`.

### 4.10 File-UI-gap-issue one-click (S2)

`📝` everywhere inbox row + Debug surface + error state + empty state
→ pre-filled `[regatta-console-ui-gap]` GH issue: title + URL +
actor_kind + last 5 actions context. Regatta-self parity via
`/api/v1/self/issues`.

---

## 5. Slice roadmap (~26 wk v1)

### S0 — Substrate prereqs (~5 wk)

- `runs` table (0017) + dispatch-site producer + full causal-hash
  population + `declared_effect_class` from agent-config envelope.
- `work_items.run_id` (0018) + 4-site backfill.
- `approval_events.run_id` (0019) + 2-site backfill via `decide.Deps`.
- `tool_call` substrate kind (0020; full table-rewrite per 0012) +
  payload incl. `declared_effect_class` + `observed_effect`.
- Claude-Code shim `observed_effect` capture in
  `internal/orchestrator/spawner/claude.go`.
- `internal/orchestrator/checks/` new pkg — `gh pr checks` poller.
- prwatch + merge decoder unification for `mergeStateStatus`.
- CI: migration property tests + poller liveness tests.
- **Acceptance:** S2-S4 surfaces have all data dependencies.

### S1 — Foundation (~6 wk)

- Phase 1 htmx rip (5 d): re-port `/approve/{id}` behind SvelteKit
  route w/ feature flag.
- Phase 2 (5 d): drop htmx assets + templates + render.go.
- Phase 3 (3 d): rewrite csp.go external-only.
- SvelteKit static scaffold + Tailwind v4 + shadcn-svelte vendored +
  Layerchart SVG.
- Go-served `/` redirect + no-JS `/witness/inbox` fallback.
- Dual-principal auth: `__Host-` cookie SameSite=Lax + bearer.
- `regatta_self_key_rotation` (0024) + version-pinned KID + overlap-
  grace.
- `audit_log` (0021) Merkle chain + actor_kind + execution_mode +
  cost_budget + blast_radius + rollback_policy + retracted_by_row_id +
  causal_hash + regatta_confidence + predicted_outcome columns + CHECK
  constraint.
- `idempotency_dedupe` (0022).
- `audit_anchor` (0023) + S3-bucket append-only anchor (object-lock +
  versioning) hourly. Single anchor v1.
- Per-actor-kind Idempotency middleware (5-min dedupe).
- Per-principal rate-limit middleware (separate buckets).
- Per-channel SSE handler + reconnect-with-cursor (Last-Event-ID
  header; ~150 LOC).
- Self-API skeleton: `/heartbeat`, `/blocked`, `/surprise`, `/actions`,
  `/retract`, `/shadow-proposals`, `/operator-reactions`, `/issues`,
  `/comments`.
- CI: golangci-lint + Go vet + Merkle property tests + S3 anchor
  reconcile + dual-principal Idempotency + bearer-auth + `npm ci` +
  `svelte-check` + `vitest` + `axe-core` + `pa11y-ci` + Playwright.
- **Acceptance:** dual-principal mutations idempotent + rate-limited +
  hash-chained + S3-anchored. Service-identity key rotates w/
  overlap-grace.

### S2 — Witness + diagnostic (~4 wk)

- Design tokens locked (slate + accent + chip palette).
- `/witness/autonomy` w/ hero loop status + 14-day calibration
  sparkline + 5-state stuck enum + 3 human chips + cost / latency /
  blast anomaly chips.
- `/witness/inbox` w/ peek pane + confidence color band + threshold-
  action rule + `📝` everywhere.
- `/api/v1/operator/self-attest` endpoint.
- Cmd-K scaffold + keyboard.
- Empty + offline + mobile-blocker (< 640 px) states.
- Operator-reactions ingestion sweeper.
- **Acceptance:** operator answers "is loop healthy?", "stuck + why?",
  "regatta's confidence?", "24h surprises?", "calibration trend?" from
  desktop, no SSH.

### S3 — Debug (~5 wk)

- `/debug/runs/<id>` event list.
- `/debug/runs/<id>/postmortem` aggregator + tool_call effect-class
  view.
- `/debug/runs/<id>/diff/<sibling_id>` sibling delta.
- `/debug/traces/<trace_id>` bare text.
- `/debug/surprise` calibration log.
- Rerun-from-causal-hash button + endpoint.
- Same-shape past-failures sidebar (FTS5 trace+tool_call signatures).
- Cmd-K palette lifts.
- Three-click-why breadcrumb everywhere.
- **Acceptance:** diagnose any past failure + reproduce via
  rerun-from-causal-hash from UI alone.

### S4 — Steer (~4 wk)

- `/steer/approvals/[id]` desktop-first.
- `/steer/overrides`.
- `/steer/queue` drag-reorder w/ a11y.
- `/steer/self-actions`.
- `/steer/shadow-proposals` verdict UX.
- New transactional surfaces:
  - `agent.Kill`, `agent.SelfKill`
  - `gate.Override`, `gate.SelfOverride`
  - `cost.Resume`, `cost.SelfResume`
  - `self.Retract`, `self.ShadowPropose`
- All write `audit_log` w/ correct actor_kind + execution_mode +
  cost_budget + blast_radius.
- Closed-loop: shadow-mode triggered automatically when
  operator-reactions override-rate > 0.3 per action-kind (rolling 24h).
- Cmd-K palette lifts.
- **Acceptance:** §1.2 policy-coverage ratio ≥ 0.85 over 14-day window.

### Integration buffer (~2 wk)

### S5 — Human-operator polish (~2 wk; ships after v1)

- WCAG AA mobile + swipe + bottom-sheet.
- Slack-deep-link redemption + Partitioned cookie.
- Tailscale Serve runbook iOS + Android tested.

**Total v1: ~26 wk.** S5 = +2 wk after.

---

## 6. Auth + audit

### 6.1 v1 — dual principals

| Principal | Mechanism |
|---|---|
| `human-operator` | `__Host-regatta` SameSite=Lax httpOnly Secure + CSRF double-submit + Origin/Sec-Fetch-Site + Idempotency-Key + rate-bucket |
| `regatta-self` | `Authorization: Bearer <self-KID-token>` HMAC over `(method, path, body-hash, ts, nonce, kid_version)` + rate-bucket + scoped Idempotency `(actor_kind, user_id, run_id, key)` |

**Key rotation (`regatta_self_key_rotation`):**
- Monotonic `kid_version`.
- New KID on every regatta binary release; `code_version` = git-SHA.
- `overlap_until` = active_from + 24h grace; both KIDs valid in grace.
- Rotation cron promotes + retires; CI verifies overlap-grace coverage.

Both principals audit through `audit_log` w/ `actor_kind`, single
Merkle chain. Tamper-evidence via per-row hash + hourly S3-bucket
append-only anchor (object-lock + versioning). Verify via
`regatta audit verify` CLI (S1 ships).

### 6.2 Multi-user / multi-tenant / vendor-embed

All v2 backlog (C1, C2, C3, C4).

---

## 7. CI gates v1

- `golangci-lint` + `go test ./...` + `go vet`
- Audit Merkle chain property tests
- S3 external anchor reconcile test
- Key rotation overlap-grace coverage test
- Dual-principal Idempotency-Key tests
- Dual-principal rate-limit tests
- Self-API bearer-auth tests
- `npm ci` (lockfile-strict)
- `svelte-check --threshold warning`
- `vitest`
- `playwright` for autonomy + inbox + replay + steer + shadow-proposal
- `axe-core` page-level + `pa11y-ci` route-level — WCAG 2.2 AA desktop
- Bundle size < 200 KiB initial gz

v2 backlog: SLSA L2/L3 + sigstore-signing + SBOM dual-emit + bundle-
visualizer + heap-snapshot + npm supply-chain hardening (D1-D5) +
Sigstore Rekor transparency-log anchor (D6).

---

## 8. Risks + open questions

| # | Item | Severity | Decision |
|---|---|---|---|
| R1 | 26-wk timeline vs autonomy-loop concurrent dispatches | High | S0 dispatches in parallel; serialize on `internal/web/` rip. |
| R2 | `tool_call` substrate kind = full table-rewrite per 0012 precedent | Med | S0 budget 3-4 d; pattern proven. |
| R3 | Claude-Code shim observed-effect capture requires in-tree wrap | Med | S0 wraps inside `internal/orchestrator/spawner/claude.go`. |
| R4 | S3 bucket dependency for external audit anchor | Low | S3 object-lock + versioning tamper-evidence; Sigstore = v2 backlog D6. |
| R5 | S3 anchor sole external store v1; operator-with-S3-write could rewrite | Low | Object-lock + versioning prevents same-key overwrite; old versions retained 7 y per Q4. Sigstore lifts from D6 if defense-in-depth needed. |
| R6 | shadcn-svelte runes-port lag | Med | Pin versions; fix upstream bugs in-tree. |
| R7 | Closed-loop calibration needs operator-reactions volume | Med | First weeks low volume; thresholds tunable. |
| R8 | Shadow-mode ramp policy too cautious / too aggressive | Med | 3-consecutive-verdict default; per-kind tunable. |
| R9 | Rerun-from-hash requires deterministic spec serialization | Med | `internal/canon` provides canonical JSON. |
| R10 | drag-reorder a11y complexity | Med | S4 includes arrow-key + ARIA live-region; pa11y-ci gate. |
| Q1 | Accent color | Locked S2 design pass; default single blue. |
| Q2 | Heartbeat thresholds | green < 60 s, yellow 60 s – 5 m, red > 5 m. |
| Q3 | Default inbox group + sort | Group by stuck-reason enum, sort oldest-first, URL-persistable. |
| Q4 | S3 bucket lifecycle | Hourly anchors retained 90 d; monthly snapshot 7 y; encryption SSE-S3. |
| Q5 | causal_hash backfill for pre-S0 runs | Best-effort lossy; rerun works only post-S0. |
| Q6 | Override-rate threshold for auto-shadow | 0.3 per action-kind default; tunable. |
| Q7 | Shadow proposal expiry | 7 days w/o human verdict. |

---

## 9. Acceptance criteria

- [ ] All 5 v1 slices ship.
- [ ] §1.2 measurable: policy-coverage ratio ≥ 0.85 over rolling 14-d
  window. Operator self-attests via `/api/v1/operator/self-attest`
  (audited under `action='self-attest'`). Loop heartbeat green full
  window.
- [ ] Operator diagnoses any past failure + reproduces via rerun-from-
  causal-hash from UI alone.
- [ ] Cmd-K covers 100% of v1 operator + self actions by S4.
- [ ] Audit hash chain verifiable + S3 anchor reconcilable via
  `regatta audit verify`.
- [ ] Service-identity key rotates with overlap-grace; CI proves
  zero-outage rotation.
- [ ] Dual-principal Idempotency-Key dedupe works.
- [ ] Dual-principal rate-limit triggers on synthetic burst.
- [ ] Desktop WCAG 2.2 AA clean.
- [ ] 5-state stuck enum surfaced when regatta posts `/self/blocked`.
- [ ] Surprise log ≥ 1 entry per 100 self-actions (calibration cover).
- [ ] Closed-loop: regatta auto-drops action-kind to shadow when
  override-rate > 0.3 — synthetic override-burst test.
- [ ] Retract: regatta self-retracts ≥ 1 action over 14-d window.
- [ ] Shadow proposals: ≥ 1 ramp-to-live + ≥ 1 reject in 14-d window.
- [ ] `[file-gap-issue]` works from every surface (parity human +
  regatta-self).
- [ ] S5 ships in ≤ 2 wk after v1: Slack-deep-link approve < 30 s cold
  mobile.

---

## 10. Honest self-grade (v5.1 final)

| Lens | Tier |
|---|---|
| User-friendliness | A (Cmd-K + keyboard + autonomy hero) |
| Speed / perf | A- (per-channel SSE + SVG + FTS5 + on-read cost) |
| Clarity / structure | A (two-principal tree; phased rip; pinned migrations; named risks) |
| Best-practice / security | A (Merkle + S3 anchor + dual-principal Idempotency + rate-limit + CSP external-only + key rotation overlap-grace + retract + shadow + closed-loop self-distrust) |
| Operator-workflow | A+ (Debug-promoted + autonomy hero + rerun-from-causal-hash + sibling-diff + same-shape sidebar + 5-state stuck enum + confidence color band + 9-metric surprise log) |
| V2-evolution | A (backlog explicit + triggers + no v1 pre-pay) |
| Feasibility | A- (26 wk honest; sunk-cost framing accepts rebuild) |
| Coherence | A (Go owns `/`; per-channel SSE; external-only assets; Partitioned cookie S5; no contradictions) |
| Customer-0 fit | A+ (closed-loop read + retract + shadow + key-rotation + full causal hash + 5-state enum + dogfood parity) |

**Overall: A** with two A+ lenses (operator-workflow + customer-0 fit).
Best-practice/security dropped from A+ in v5 to A in v5.1 due to
single anchor (vs dual). Defensible tradeoff; Sigstore Rekor lifts via
v2 backlog D6 when warranted.

---

## 11. References

- Companion: `docs/engineer/specs/2026-06-02-operator-console-v2-backlog.md`
- `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md`
- `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` (superseded)
- `docs/engineer/specs/2026-06-01-w7-wave2-admin-pages-design.md` (superseded)
- `docs/engineer/specs/2026-06-02-obs-wave-d-operator-surface.md` (T2 only)
- `docs/engineer/specs/2026-06-02-phase-autonomy-w2-c2-merge-execute.md`
- `docs/engineer/specs/2026-06-02-phase-autonomy-w7-l4-as-review-identity.md`
- `internal/orchestrator/state/migrations/0006_substrate.sql`
- `internal/orchestrator/state/migrations/0012_substrate_brief_rejected_kind.sql` (kind-expand precedent)
- `internal/cost/spend/{writer,payload,reader}.go`
- `internal/gates/approval/{decide,notify_http,audit}.go`
- `internal/orchestrator/prwatch/prwatch.go`
- `internal/orchestrator/merge/merge.go`
- `internal/orchestrator/spawner/claude.go` (shim instrumentation target)
- `internal/canon/canon.go` (causal-hash canonicalization)
- `cmd/regatta/serve.go:680-712`
- `internal/web/*` (ripped in S1 phases 1-3)
