---
title: "External-platform spec adapters (Linear / Jira / Confluence / Notion)"
status: active
summary: "Extend the schemas.SpecAdapter seam to ingest work items from Linear, Jira, Confluence, Notion so design lives where teams already author it. Five file-disjoint slices; github_issues remains the reference impl."
---

# External-Platform Spec Adapters — Design Spec

Date: 2026-06-08
Status: design (pre-implementation)
Source: operator question — "ingest design/spec/roadmap from non-GitHub platforms with minimal intervention."
Companion specs:

- `docs/engineer/specs/2026-06-01-adapter-contracts-design.md` — P3.8 adapter-contract pattern (`sql.Register`-style); the registration mechanism this spec composes onto.
- `docs/engineer/specs/phase-x/2026-06-02-mvr-1-t3-p38-scm-adapter-gitea-first.md` — sibling pattern (SCM seam) for second-source adapter.
- `docs/engineer/specs/2026-06-04-mvr-1-t4-github-issues-adapter-impl.md` — reference `SpecAdapter` impl (status filter, label routing, ETag mutation guard).
- `docs/engineer/specs/2026-06-07-scheduler-adaptersync-unification.md` — scheduler that already polls a single adapter; multi-adapter fan-in lands here.
- `contracts/schemas/spec_adapter.go` — interface authority (List / Get / UpdateStatus / Capabilities + sentinel errors).

```release-notes
none (internal — design spec)
```

---

## 1. Problem

regatta today reads only `github_issues`. Design, specs, and roadmaps frequently live in Linear, Jira, Confluence, or Notion — the operator either:

1. Mirrors every spec into a GitHub issue by hand (toil, drift, two sources of truth).
2. Forks every external doc into the repo as Markdown (same toil, plus lossy round-trip on edits).
3. Skips regatta for that work entirely.

None of the three is the right answer for a single-operator self-host loop. The `SpecAdapter` interface already factored the right seam (`List` + `Get` + `UpdateStatus` + `Capabilities`); what's missing is per-platform impls and a multi-source composition layer.

Operator UX target: drop a credentials block + a selector (Linear team, JQL, Confluence space + label, Notion database) into `regatta.yaml`, restart, and regatta dispatches against externally-authored work without further intervention.

---

## 2. Platform taxonomy

| Tier | Platform | API | Auth | In-scope |
|---|---|---|---|---|
| 1 | Linear | GraphQL (`api.linear.app/graphql`) | OAuth2 / personal API key | Slice 1 |
| 1 | Jira Cloud | REST v3 (`/rest/api/3/search/jql`) | OAuth 2.0 (3LO) / Atlassian API token | Slice 2 |
| 2 | Confluence Cloud | REST v2 (`/wiki/api/v2/pages`) | Atlassian API token (shared with Jira) | Slice 3 (read-only) |
| 2 | Notion | REST v1 (`api.notion.com/v1/databases`) | Internal integration token | Slice 4 (read-only) |
| 3 / Phase-X | Asana, Monday, GitLab Issues, Slack-thread specs | varies | varies | Out of scope (§10) |

**Tier rationale**:

- **Tier 1** ingests bidirectional work items (status writes back via `UpdateStatus`). Linear leads because GraphQL gives precise field selection (lower egress per poll), OAuth2 is documented, and AI-first teams already concentrate there. Jira follows because it is the enterprise default — every external-customer-ask in the reopen-trigger pool names Jira.
- **Tier 2** is read-only: pages and database rows are commonly the source of truth for *design*, but regatta does not own the page lifecycle (a Confluence page does not "transition to in-progress"). Status writeback would be lossy.
- **Tier 3** is deferred behind reopen-triggers: external customer ask OR 30-day-green on Tier 1 + 2.

---

## 3. Contract reuse (no interface churn)

The existing `schemas.SpecAdapter` interface is unchanged. Every new platform adapter implements the same four methods (`List`, `Get`, `UpdateStatus`, `Capabilities`). Sentinel errors (`ErrRateLimited`, `ErrSourceMutated`, `ErrNotFound`) carry the same semantics.

Reuse hits:

- `WorkItem.Source.SHA` opaque field absorbs platform-specific versioning (Linear `updatedAt`, Jira `versionAt`/ETag, Confluence `version.number`, Notion `last_edited_time`). The orchestrator's L0 mutation guard already treats `SHA` as opaque (`contracts/schemas/spec_adapter.go:108`).
- `Capabilities.MinPollInterval` already lets each platform pin its own floor (per `#885`); this is how Linear's 1500-req/h burst budget and Jira's 100-req/min are honored without orchestrator changes.
- `Capabilities.SupportedStatuses` already restricts which statuses an adapter accepts on `UpdateStatus`; Tier-2 adapters return an empty list and reject all writes with `ErrAdapterUnsupported` (added 2026-06-04 for exactly this case).

No interface bump. No major version. No migration.

---

## 4. Per-platform mapping

### 4.1 Linear (`internal/orchestrator/adapter/linear/`)

| Linear concept | `WorkItem` field | Notes |
|---|---|---|
| `Issue.id` (UUID) | `ID = "linear:" + uuid` | Composite key per §6. |
| `Issue.title` | `Title` | |
| `Issue.description` (Markdown) | `Body` | Acceptance-criteria parsing reuses `internal/orchestrator/adapter/markdown.go` checklist extractor. |
| `Issue.state.name` ∈ {Backlog, Todo, In Progress, Done, Canceled} | `Status` | Linear→regatta map: Todo/Backlog→`planned`; In Progress→`in_progress`; Done→`done`; Canceled→`closed-resolved`. |
| `Issue.labels` (filter `[autonomous]`) | included/excluded | Selector lives in `regatta.yaml`. |
| `Issue.updatedAt` (ISO 8601) | `Source.SHA` | Opaque version token. |
| `Issue.identifier` (e.g. "ENG-123") | `LinkedArtifact` | Human handle in operator-facing logs. |

**Selector**: `linear.team_keys: [ENG, OPS]` + `linear.label_filter: autonomous` + `linear.state_categories: [unstarted, started]`.

**Transport**: GraphQL POST with a single fragment that pulls the fields above. Pagination via `issues(first: 50, after: $cursor)`; pagination is internal per the interface contract.

**Auth**: API key in `LINEAR_API_KEY` env-var (resolved through the secrets router from `#911`); OAuth2 deferred to Phase-X (single-tenant self-host loop does not need 3LO yet).

**Rate-limit signal**: GraphQL response error code `RATELIMITED` OR HTTP 429 → wrap `ErrRateLimited` with `RateLimitHint{RetryAfter: response.headers["x-ratelimit-requests-reset"]}`.

### 4.2 Jira Cloud (`internal/orchestrator/adapter/jira/`)

| Jira concept | `WorkItem` field | Notes |
|---|---|---|
| `issue.key` (e.g. "PROJ-42") | `ID = "jira:" + key` | Composite key. |
| `issue.fields.summary` | `Title` | |
| `issue.fields.description` (ADF JSON) | `Body` | ADF→Markdown via `github.com/yuin/goldmark` extension OR Atlassian's documented `expand=renderedFields` to skip in-binary conversion (preferred — server-side render). |
| `issue.fields.status.statusCategory.key` ∈ {new, indeterminate, done} | `Status` | new→`planned`; indeterminate→`in_progress`; done→`done`. Cancellation maps to `closed-resolved` only when `resolution.name == "Won't Do"`. |
| `issue.fields.labels` + JQL `WHERE` | filter | Selector below. |
| `issue.fields.updated` | `Source.SHA` | ISO 8601 ms-precision. |

**Selector**: JQL string in `jira.jql` (e.g. `project = ENG AND labels = autonomous AND statusCategory != Done`). Raw JQL gives the operator full filter power without us inventing a DSL.

**Transport**: REST `/rest/api/3/search/jql` (the v3 endpoint, not the deprecated `/search`); cursor-based pagination via `nextPageToken`. Per-issue fetch uses `/rest/api/3/issue/{key}?fields=summary,description,status,labels,updated&expand=renderedFields`.

**Auth**: Atlassian email + API token (Basic auth) via `JIRA_EMAIL` + `JIRA_API_TOKEN` env-vars. OAuth 2.0 (3LO) deferred to Phase-X.

**Rate-limit signal**: HTTP 429 with `Retry-After` header → `ErrRateLimited` with `RateLimitHint`. Concurrent-request budget (Atlassian's "cost" model) handled by `Capabilities.MinPollInterval = 30s` default.

### 4.3 Confluence Cloud (`internal/orchestrator/adapter/confluence/`, read-only)

| Confluence concept | `WorkItem` field | Notes |
|---|---|---|
| `page.id` (numeric string) | `ID = "confluence:" + id` | Composite key. |
| `page.title` | `Title` | |
| `page.body.storage.value` (XHTML) | `Body` | XHTML→Markdown via `html-to-markdown` (proven OSS, MIT). Acceptance-criteria parsing same as §4.1. |
| Page status (derived) | `Status` | Confluence has no native status — derived from `regatta-status` page-property macro (operator inserts a property block). Default = `planned`. |
| `page.version.number` (int) | `Source.SHA` | Monotonic per-page. |
| Label = `autonomous` | filter | Confluence-side label selector. |

**Selector**: `confluence.space_keys: [DESIGN, ARCH]` + `confluence.label: autonomous`.

**Required page convention**: each ingestible page contains a property-macro block:

```html
<ac:structured-macro ac:name="details">
  <ac:parameter ac:name="id">regatta</ac:parameter>
  <ac:rich-text-body>
    <p>regatta-status: planned</p>
    <p>regatta-id: arch-001</p>
  </ac:rich-text-body>
</ac:structured-macro>
```

Pages without the block are skipped silently (logged once per poll). This keeps the operator in control — opt-in per page.

**Transport**: REST v2 `/wiki/api/v2/pages?space-id={id}&body-format=storage&limit=100` with `Link: <...>; rel="next"` pagination per Atlassian's documented v2 spec.

**Auth**: Atlassian API token (shared with Jira; same env-vars).

**Write path**: `UpdateStatus` returns `ErrAdapterUnsupported`. `Capabilities.SupportedStatuses = nil`. Status moves on the regatta side never echo back to Confluence — read-only is read-only.

### 4.4 Notion (`internal/orchestrator/adapter/notion/`, read-only)

| Notion concept | `WorkItem` field | Notes |
|---|---|---|
| `page.id` (UUID) | `ID = "notion:" + uuid` | Composite key. |
| `properties.Name.title[].plain_text` | `Title` | First title-property field. |
| Page body (blocks) | `Body` | Block-tree retrieved via `/v1/blocks/{id}/children` (paginated), flattened to Markdown via `notion-to-md`-style traversal. |
| `properties.Status.select.name` | `Status` | Notion-side select-property values map: `Backlog`/`Todo`→`planned`; `In Progress`→`in_progress`; `Done`→`done`; anything else→skipped. |
| `properties.Autonomous.checkbox == true` | filter | Property-name configurable in `regatta.yaml`. |
| `last_edited_time` | `Source.SHA` | ISO 8601. |

**Selector**: `notion.database_id: <uuid>` + `notion.filter: { property: "Autonomous", checkbox: { equals: true } }` (Notion's documented `database/query` filter shape — pass-through, not a DSL).

**Transport**: REST `/v1/databases/{id}/query` POST with `start_cursor` pagination.

**Auth**: Notion internal-integration token via `NOTION_API_KEY`. OAuth deferred to Phase-X. Integration must be granted access to the database (Notion-side workspace setting; documented in operator runbook).

**Write path**: same as Confluence — `ErrAdapterUnsupported`, empty `SupportedStatuses`.

---

## 5. Auth + secrets routing

All four adapters draw credentials through the secrets-router seam (`#911`). Per-platform env-var contract:

| Platform | Required env-vars | Optional |
|---|---|---|
| Linear | `LINEAR_API_KEY` | `LINEAR_OAUTH_TOKEN` (Phase-X) |
| Jira | `JIRA_EMAIL`, `JIRA_API_TOKEN` | `JIRA_BASE_URL` (Server install; defaults to `*.atlassian.net`) |
| Confluence | `CONFLUENCE_BASE_URL`, `JIRA_API_TOKEN` | (shares Jira token by default) |
| Notion | `NOTION_API_KEY` | — |

No secret material in `regatta.yaml`. The router enforces this via the existing redaction allowlist; an adapter that finds a non-empty literal in its `yaml.api_key` field (rather than `api_key_env`) MUST fail boot with `ErrPermanent("secret material in yaml")`.

---

## 6. Cross-platform dedup (composite key)

A single canonical work item often shows up twice: design page in Confluence and tracking issue in Jira; design doc in Notion and ticket in Linear. Naive ingestion enqueues both.

**Composite key**: every `WorkItem.ID` is namespaced by `<platform>:<platform-id>`. Examples: `github:42`, `linear:9d3e...`, `jira:PROJ-7`, `confluence:1310720`, `notion:c0ffee-...`. The orchestrator already keys on `WorkItem.ID` exact-match — composite keys land for free.

**Canonical-id seam**: each adapter MAY parse an optional `regatta-id: <slug>` token (Linear/Jira issue body; Confluence property macro; Notion property field). When present, the scheduler's fan-in layer (§7) uses it for cross-platform supersession: an item with `regatta-id: arch-001` ingested from both Confluence (planned) and Linear (in-progress) collapses to a single logical work item; the **highest-progress status wins**, and the fan-in stamps a `superseded_by` audit row for the loser.

Tie-break order when multiple adapters expose the same `regatta-id`:

1. Tier-1 writeback-capable platform (Linear, Jira, github_issues) over Tier-2 (Confluence, Notion).
2. Within a tier, the lexicographically smallest platform slug (deterministic; no race).

The composite key is opaque to the L0 mutation guard — `SourceRef.Locator` carries the full namespaced path (`"linear://team/ENG/issue/9d3e..."`); the L0 check still sees a stable per-source string.

---

## 7. Multi-adapter scheduler fan-in

Today `internal/orchestrator/scheduler` polls a single adapter (`#885` honored a single `MinPollInterval`). With N adapters configured:

- Boot: registry walks `regatta.yaml` `spec_adapters: [...]` list, calls `Open(name, cfg)` per entry, stores them in a slice `[]schemas.SpecAdapter`.
- Poll loop: per-adapter goroutine; each respects its own `Capabilities.MinPollInterval`. Fan-in via a single `chan WorkItem` consumed by the orchestrator's existing planner ingress.
- Dedup pass: collected `WorkItem`s buffered in a per-cycle map keyed by `regatta-id` (when present) OR `WorkItem.ID` (always); §6 tie-break resolves duplicates before handoff.

No central poller. No leader election. Adapters are independent.

Operator config sketch:

```yaml
spec_adapters:
  - name: github_issues
    repo: trilamsr/regatta
    label: autonomous
  - name: linear
    api_key_env: LINEAR_API_KEY
    team_keys: [ENG]
    label_filter: autonomous
    state_categories: [unstarted, started]
  - name: jira
    base_url: https://acme.atlassian.net
    email_env: JIRA_EMAIL
    token_env: JIRA_API_TOKEN
    jql: 'project = ENG AND labels = autonomous AND statusCategory != Done'
  - name: confluence
    base_url: https://acme.atlassian.net/wiki
    token_env: JIRA_API_TOKEN
    space_keys: [DESIGN]
    label: autonomous
  - name: notion
    token_env: NOTION_API_KEY
    database_id: c0ffee-...
    filter_property: Autonomous
```

---

## 8. Acceptance criteria

| # | Criterion | Evidence |
|---|---|---|
| A1 | regatta dispatches against a Linear team's `[autonomous]`-labeled issues without the operator filing any GitHub issues. | `TestLinearAdapter_DispatchesAgainstLabeledIssues` (integration test against a fixture Linear GraphQL server). |
| A2 | Each Tier-1 adapter (Linear, Jira) round-trips `UpdateStatus`: a regatta-side `in_progress`→`done` transition reflects in the source platform. | `TestLinearAdapter_UpdateStatus_RoundTrip`, `TestJiraAdapter_UpdateStatus_RoundTrip`. |
| A3 | Each Tier-2 adapter (Confluence, Notion) lists work items and rejects writes with `ErrAdapterUnsupported`. | `TestConfluenceAdapter_ReadOnly`, `TestNotionAdapter_ReadOnly`. |
| A4 | Cross-platform `regatta-id` dedup collapses a Linear+Confluence pair to one dispatchable work item; the higher-progress status wins. | `TestFanIn_RegattaIDDedup_HigherProgressWins`. |
| A5 | Rate-limit signals from any adapter back off without OOM or thrash; 429 + `Retry-After` honored. | `TestLinearAdapter_RateLimit_RespectsRetryAfter`, `TestJiraAdapter_RateLimit_RespectsRetryAfter`. |
| A6 | Source mutation between `List` and `Get` surfaces `ErrSourceMutated` (per the existing L0 guard contract). | `TestJiraAdapter_ErrSourceMutated_OnVersionBump`. |
| A7 | Secrets do not appear in `regatta.yaml` parsed config nor in any structured log line. | `TestSecretsRouter_NoLiteralInYAML`, `TestSecretsRouter_RedactsInLogs`. |
| A8 | Boot fails fast when an adapter's credentials are missing or invalid. | `TestAdapterBoot_FailFastOnMissingCreds`. |

---

## 9. Adversarial red-team

1. **Per-platform rate-limit collision** — three adapters polling concurrently on a shared customer subnet trip platform-level throttling (Linear 1500/h, Jira 100/min, Notion 3/sec, Confluence shares Jira budget). Mitigation: per-adapter `Capabilities.MinPollInterval` defaults pinned to half the documented budget (Linear 5s, Jira 30s, Notion 1s, Confluence 30s). Adapters share an HTTP client with a per-host token bucket (`golang.org/x/time/rate`).
2. **Sync latency vs operator mid-flight edit** — operator edits a Jira ticket between regatta's `List` and `Get`; the orchestrator dispatches against stale text. Mitigation: `ErrSourceMutated` on version mismatch (handled by the existing L0 guard per `#884`).
3. **Webhook vs poll** — long polling is the floor here, not the ceiling. Webhooks are a Phase-X extension because every webhook endpoint demands an internet-routable ingress (a single-operator self-host loop runs behind home NAT). Documented limitation. Reopen-trigger: regatta gains a hosted control-plane mode.
4. **Cross-platform comment cycle** — Tier-2 read-only writes nothing; Tier-1 writes back status only (never body). The interface forbids `Criterion.Text` mutation (`contracts/schemas/spec_adapter.go:21`), so even a misbehaving adapter cannot push regatta-side prose back to the source. Cycle is structurally impossible.
5. **ADF-to-Markdown lossy round-trip (Jira)** — Atlassian Document Format is a JSON tree; conversion to Markdown loses panel macros, info boxes, custom emoji. Mitigation: use Jira's documented `expand=renderedFields` to fetch server-rendered HTML, then convert HTML→Markdown via `html-to-markdown`. Server-side rendering is Atlassian's problem, not ours.
6. **Notion block-tree fan-out** — a Notion page with 200 children + 50 grand-children blows the per-poll budget. Mitigation: depth-limited traversal (default 3 levels) + `Capabilities.MinPollInterval = 60s` for Notion. Operator override documented.
7. **Confluence storage-format edge cases** — `<ac:link>`, `<ri:user>`, and other Atlassian-namespaced elements have no Markdown equivalent. Mitigation: round-trip via the `view` body-format (Atlassian-rendered HTML), not `storage`. Lossy but stable; operators reviewing acceptance criteria see what the page shows in-browser.
8. **OAuth token expiry** — Linear OAuth tokens expire after 10 years (effectively never), but Jira 3LO tokens expire hourly. Out of scope this slice (PAT only); Phase-X work picks up refresh-token machinery. Documented constraint.
9. **Dedup tie-break race** — two adapters return the same `regatta-id` in the same poll cycle; goroutine scheduling could pick different winners across runs. Mitigation: §6 tie-break is deterministic (tier first, then lexicographic slug). Property test (`TestFanIn_Dedup_Deterministic`).
10. **Adapter panic during `List`** — third-party transport bug panics mid-poll, takes the scheduler goroutine down. Mitigation: each per-adapter goroutine wraps its body in `defer recover()` + emits a span event + bumps `adapter.panic_total{adapter=...}` metric. The other adapters keep polling. (Borrows the §6.4 `adapters.instrument()` pattern from `2026-06-01-adapter-contracts-design.md`.)
11. **Body too large** — a Confluence page with a 5 MB embedded image-base64 OR a Notion database export. Mitigation: `Body` is truncated at 1 MiB with a documented marker `\n\n[regatta: body truncated at 1 MiB; see source]`; planner ingress already bounds prompt size downstream.
12. **JQL injection** — operator-supplied JQL is forwarded verbatim. Risk: a malicious operator config could escalate visibility (read other projects). Self-host context: the operator IS the threat actor for their own credentials. Documented; not mitigated.

---

## 10. Out of scope

- Closed-source platforms with SaaS-only APIs and restrictive ToS (Monday.com — no offline test fixture path).
- Two-way prose sync (write `WorkItem.Body` back to source). The interface forbids it; the spec does not change.
- OAuth 2.0 (3LO) for Jira / Linear / Notion. PAT/API-token only this slice.
- GitLab Issues (separate scope from the SCM adapter; reopen-trigger = GitLab-hosted repo lands).
- Asana, Monday, Slack-thread-driven specs — Phase-X with reopen-trigger = external customer ask OR 30-day-green on the four shipped here.
- Webhook ingress — Phase-X with reopen-trigger = regatta hosted control-plane.

---

## 11. Implementer brief (5 file-disjoint slices)

| # | Slice | Scope | Est. LOC | Order |
|---|---|---|---|---|
| S1 | Linear adapter (Tier 1) | `internal/orchestrator/adapter/linear/{adapter,parse,graphql}.go` + `_test.go` + fixture GraphQL server | ~700 | First (Tier-1 unlocks A1). |
| S2 | Jira adapter (Tier 1) | `internal/orchestrator/adapter/jira/{adapter,parse,jql}.go` + `_test.go` + fixture REST server | ~750 | Parallel with S1 (file-disjoint). |
| S3 | Confluence adapter (Tier 2, read-only) | `internal/orchestrator/adapter/confluence/{adapter,parse,storage}.go` + `_test.go` + fixture REST server | ~500 | Sequential after S2 (shares Atlassian auth helpers). |
| S4 | Notion adapter (Tier 2, read-only) | `internal/orchestrator/adapter/notion/{adapter,parse,blocks}.go` + `_test.go` + fixture REST server | ~550 | Parallel with S3 (file-disjoint). |
| S5 | Cross-platform fan-in + dedup | `internal/orchestrator/scheduler/fanin.go` + `regatta-id` parser + composite-key resolver + property tests | ~400 | Last (depends on S1-S4 for integration). |

**Shared primitive owner**: S1 owns the per-host HTTP token-bucket helper at `internal/orchestrator/adapter/transport/ratelimit.go` (§9 item 1). S2-S4 import. Per `feedback_shared_primitive_owner`.

**Per-slice adversarial reviewer subagent required** (the four adapters are load-bearing — secrets, third-party API, status-writeback) per `feedback_adversarial_review`. S5 is also load-bearing (cross-cutting scheduler change).

**Cap**: parallel implementers at 3-4 per `CLAUDE.md` § Dispatch. Wave 1 = S1 + S2 + S4 (file-disjoint). Wave 2 = S3 + S5.

---

## 12. Cite-trail

- `contracts/schemas/spec_adapter.go` — interface authority + sentinel errors (no churn this spec).
- `docs/engineer/specs/2026-06-01-adapter-contracts-design.md` — `sql.Register`-style registration + `adapters.instrument()` pattern.
- `docs/engineer/specs/2026-06-04-mvr-1-t4-github-issues-adapter-impl.md` — reference impl (label routing, ETag guard).
- `docs/engineer/specs/2026-06-07-scheduler-adaptersync-unification.md` — single-adapter scheduler this spec extends.
- `#885` — `Capabilities.MinPollInterval` honored in scheduler.
- `#884` — `ErrSourceMutated` on mid-flight edit (adversarial item 2).
- `#911` — secrets router (per-platform env-var contract).
- Linear GraphQL API — `https://developers.linear.app/docs/graphql/working-with-the-graphql-api`.
- Jira Cloud REST v3 — `https://developer.atlassian.com/cloud/jira/platform/rest/v3/`.
- Confluence Cloud REST v2 — `https://developer.atlassian.com/cloud/confluence/rest/v2/`.
- Notion REST v1 — `https://developers.notion.com/reference`.
