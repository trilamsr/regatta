# WorkItemSource platform constraints

**Status:** research draft, v1.
**Scope:** what L0 (the deterministic spec-immutability gate) can actually
verify for each built-in adapter. Inputs:
[`schemas/work_item_source.go`](../schemas/work_item_source.go),
[`schemas/work_item.schema.json`](../schemas/work_item.schema.json),
[`docs/design.md` §Spec contract](../docs/design.md).

## Why this document exists

The contract says `acceptance_criteria[].text` is "immutable
post-publication" and that L0 enforces this. That guarantee is only
real if the upstream spec source gives the adapter one of:

1. **An immutable identifier we can pin against** — a commit SHA, an
   ETag, a monotonic version number — that proves we are comparing the
   same bytes the agent read.
2. **An audited change-log we can replay** — so that even if the live
   resource has drifted, we can fetch "the body as it was at SHA X" or
   diff against the version the agent observed.

Without one of these, the adapter cannot offer better than
*best-effort* detection, and L0 degrades from "fails hard on mutation"
to "warns on mutation, hopes the human noticed."

Each of the five built-in platforms gives a different combination of
(1) and (2). This document maps that out and proposes how the
`Capabilities` struct should expose it so the orchestrator can pick
the right safety mode per repo.

---

## 1. GitHub Issues

**Immutability model:** ETag-pinned via REST `If-None-Match` plus the
GraphQL `userContentEdits` audit log. Both are real and load-bearing.

**API endpoints:**

- `GET /repos/{owner}/{repo}/issues/{number}` — returns an `ETag`
  response header. A subsequent request with `If-None-Match:
  <etag>` returns `304 Not Modified` and does **not** count against
  the primary rate limit ([REST best-practices][gh-rest-best],
  [Octokit conditional-request thread][gh-octokit]).
- `GET /repos/{owner}/{repo}/issues/{number}/timeline` — returns
  `labeled`, `renamed`, `assigned`, etc. **No `body_edited` event is
  emitted in the timeline**; the REST timeline cannot see body
  changes ([Timeline API][gh-timeline]).
- GraphQL `issue.userContentEdits(first: N)` — returns the audit log
  of body edits with `editedAt`, `editor`, `diff`, `deletedAt`. This is
  the only API surface that gives you the prior body text. The UI's
  "edited" pencil-menu reads from the same source.

**What the adapter can offer:**

- `SourceRef{Kind:"issue", Locator:"owner/repo#123", SHA:<ETag>}` is
  legitimately immutable per-fetch. On any subsequent read, the
  adapter sends `If-None-Match: <SHA>`; a 200 means the body changed,
  and the adapter MUST emit `ErrSourceMutated`.
- L0 verifies criterion text by re-fetching at the recorded ETag.
  If the live ETag differs, L0 calls the GraphQL `userContentEdits`
  endpoint to retrieve the body as it was at edit-time-T (where T is
  the timestamp embedded in the SHA-tracking record), compares
  byte-equal under NFC, and fails hard if the criterion span
  changed.

**Gotchas:**

- ETags on GitHub are weak validators (`W/"..."`); they change on any
  body, title, label, or comment-count change. Useful for "did
  *anything* change" but not for narrowing to the body. Pair with
  `userContentEdits` for narrowing.
- `userContentEdits` is only on the GraphQL API; not available via
  REST. GraphQL does **not** support conditional requests, so reads
  cost full rate.
- The `userContentEdits` log is *write-able* via the UI in the sense
  that GitHub allows admins to delete edits (the `deletedAt` field).
  Treat deletion as equivalent to mutation.
- Issue-comment edits are tracked on the comment, not on the issue
  body. If criteria live in comments, the adapter must walk
  `issue.comments[].userContentEdits` too.

**Rate limits:** 5,000 req/hr per personal-access token; 5,000 req/hr
per GitHub App installation (15,000 on Enterprise Cloud); 60 req/hr
unauthenticated ([GitHub rate-limits][gh-ratelimits]). Conditional
304s don't count. Secondary limits cap concurrent requests and
content-creation; orchestrator polling should respect `Retry-After`.

**Webhooks:** at-least-once. Every delivery carries
`X-GitHub-Delivery: <UUID>` — use as idempotency key. Redelivery is
available for the last 30 days via REST
`/repos/{o}/{r}/hooks/{h}/deliveries/{id}/attempts`. Issues fire
`action=edited` with a `changes.body.from` field carrying the *prior*
body — i.e. the orchestrator can reconstruct the diff from the
webhook payload alone without calling back ([GitHub webhook
delivery][gh-webhook]).

**Verdict:** **first-class.** Full immutability guarantee.

---

## 2. GitLab Issues

**Immutability model:** weaker than GitHub. `updated_at` timestamp is
the only built-in versioning signal; there is **no public REST endpoint
for description edit history** ([gitlab-org/gitlab#10103, #10104,
#15597][gl-history-issue]). Feature has been requested since 2018 and
is still open.

**API endpoints:**

- `GET /projects/:id/issues/:iid` — returns `updated_at` but no ETag.
- `GET /projects/:id/issues/:iid/resource_state_events` — only
  tracks open/close transitions ([state events][gl-state-events]).
- `GET /projects/:id/issues/:iid/notes` — system notes record some
  field changes (labels, milestones) but **do not record description
  edits**. The system note for description changes only says "changed
  the description" with no diff.

**What the adapter can offer:**

- `SourceRef{Kind:"issue", Locator:"group/proj#42", SHA:<updated_at
  RFC3339>}`. This is a *timestamp pin*, not an immutability pin: two
  edits within the same second are indistinguishable, and the
  adapter cannot retrieve the prior body if `updated_at` advances.
- L0 can detect "the description changed since we read it" by
  comparing `updated_at`, but cannot prove *which bytes* changed.
  Best it can do: fetch the live body, normalize, byte-compare the
  criterion span, fail if different. Cannot recover the prior text
  if the human has already overwritten it.

**Gotchas:**

- `updated_at` advances on label changes, milestone changes,
  assignment, etc. — not just description edits. So `updated_at`
  drift is necessary-but-not-sufficient for description mutation.
- GitLab's web UI shows no edit history to humans either; once
  overwritten, the prior text is gone unless GitLab itself has a
  database-level audit log (Premium/Ultimate "Audit Events" tier may
  capture this, but it's not surfaced in the standard Issues API).
- No conditional-request / `If-None-Match` support documented for
  issues endpoints.

**Rate limits:** 2,000 authenticated requests/min per user
(default; admin-configurable); `RateLimit-*` response headers
present ([User and IP rate limits][gl-ratelimits]). Generous compared
to GitHub.

**Webhooks:** retry up to 4 consecutive failures (temporary disable)
or 40 (permanent disable); each delivery carries `webhook-id` header
that is "consistent across retries" — usable as idempotency key.
Manual replay via the project-webhooks API. The `Issue Hook` event
fires on description change and includes the new description but
**not** the old description in the payload — losing the diff
([GitLab webhook docs][gl-webhook]).

**Verdict:** **degraded-mode.** Adapter can flag mutation, cannot
reconstruct it. Document the gap in the per-repo onboarding doc.

---

## 3. Markdown catalog (file on `main`)

**Immutability model:** strongest of the five. Git commit SHA pins
the entire file deterministically. L0 already runs against git, so
this is the native case.

**Convention question:** how does the adapter identify a stable
work-item ID inside a file?

Three candidates considered:

1. **Heading slug** (`## Add OIDC support` → `add-oidc-support`).
   *Rejected:* renaming a heading silently re-IDs the item. No
   stability across renames.
2. **Anchor comment** (`<!-- regatta-id: wi-oidc-001 -->`).
   *Workable but ugly* and fights markdown editors.
3. **YAML front-matter block per item.** Stable, machine-readable,
   survives heading renames. **Recommended.**

The adapter detects "this is the same item, edited" iff the `id` in
front-matter matches a prior `id`. New file commit + same id = the
agent or a human edited the item. L0 then performs the same byte-
equality check against the previous commit's version of the file.

### Recommended canonical schema for `MILESTONES.md`

```markdown
---
regatta_schema: v1
generated_by: hand
---

# Milestones

<!-- Each work item is one `## Item <id>` block. The id is stable. -->
<!-- Acceptance criteria use the `- [ ] <id>: <text>` form. L0       -->
<!-- byte-compares text under NFC; do not rename ids after publish.   -->

## Item wi-oidc-001

```yaml
title: Add OIDC support to login flow
status: planned
lane: server
dependencies: []
linked_artifact: docs/rfcs/oidc.md
```

Acceptance criteria:

- [ ] ac-1: A user can sign in via Google OIDC and a session cookie
      is set on the apex domain.
- [ ] ac-2: An OIDC `id_token` with an invalid signature is rejected
      with HTTP 401 and a log line at `level=warn`.
- [ ] ac-3: The new flow is documented in `docs/auth.md` and the
      example config is updated in `config/auth.example.yaml`.

## Item wi-oidc-002

```yaml
title: Add OIDC group-claim → role mapping
status: planned
lane: server
dependencies: [wi-oidc-001]
linked_artifact: docs/rfcs/oidc.md
```

Acceptance criteria:

- [ ] ac-1: A user in the `admins` OIDC group receives the `admin`
      role at session-creation time.
- [ ] ac-2: Group-to-role mapping is configurable via
      `config/auth.yaml: oidc.group_role_map`.
```

The adapter parses every `## Item <id>` block, extracts the YAML
front-matter, and treats each `- [ ] <ac-id>: <text>` line as a
`Criterion`. `SourceRef.SHA` is the commit SHA the file was read at;
`SourceRef.Locator` is `MILESTONES.md:<line-start>-<line-end>` so L0
can scope the diff to that range and ignore unrelated edits to the
same file.

**Gotchas:**

- Renaming an `ac-id` (e.g. `ac-1` → `ac-criteria-1`) is a mutation;
  L0 must fail hard. Document this in the parser's error messages.
- Reordering criteria within an item is *not* a text mutation; L0
  should be order-insensitive within the criteria list.
- Wrapping long lines: L0 normalizes to a single logical line per
  criterion (collapses internal whitespace runs) before byte-compare.
  Document this with fixtures in `gates/l0/testdata/`.

**Verdict:** **first-class.** Strongest immutability guarantee.

---

## 4. Jira

**Immutability model:** versioned via the changelog, but the
description field's content model (Atlassian Document Format, ADF) is
hostile to byte-equality.

**API endpoints:**

- `GET /rest/api/3/issue/{key}?expand=changelog` — returns
  `changelog.histories[].items[]` with `field`, `fromString`,
  `toString`. For the description field, `field='description'` and
  the strings are *plain-text projections* of the ADF — not the
  underlying ADF JSON ([changelog format][jira-changelog]).
- `GET /rest/api/3/issue/{key}` — returns the live description as
  ADF (JSON tree of paragraph/text/marks/etc.) per ADF migration
  ([ADF requirement][jira-adf]).
- Issue version: each issue has a numeric `version` field
  incremented on every update.

**What the adapter can offer:**

- `SourceRef{Kind:"ticket", Locator:"PROJ-123",
  SHA:<issue.version>:<changelog.histories[last].id>}` — a
  monotonic version pin plus the last changelog entry id so the
  adapter can detect "the version is the same but a sub-resource
  changed" (e.g. someone added a comment) and distinguish that from
  description mutation.
- L0 fetches description-at-version-N by walking
  `changelog.histories[]` backwards and reconstructing the ADF from
  `fromString`/`toString` deltas. **This reconstruction is lossy**
  — `fromString`/`toString` discard ADF marks, links, and embedded
  media. Byte-equality under NFC will *not* hold on round-trip.

**Gotchas:**

- **The "Acceptance Criteria" custom field.** In standard Jira
  Software it's a `customfield_NNNNN` (often `customfield_10100`
  through `customfield_10299` depending on instance) of type
  `com.atlassian.jira.plugin.system.customfieldtypes:textarea` →
  also stored as ADF. The changelog records changes to it the same
  way as `description`, but it's a per-instance field id, so the
  adapter must accept the field id as configuration. Plain-text
  custom-field plugins exist (e.g. "Easy Agile") that store
  criteria as separate sub-records — adapter must support either
  shape.
- Filter `expand=changelog` is incompatible with some `fields=*all`
  variants (the changelog disappears when both are set per Jira
  Cloud community reports); use `expand=changelog,renderedFields`
  separately.
- Comment edits are versioned per-comment, not in the issue
  changelog. If criteria live in comments, the adapter must walk
  `comments[].updated` (no built-in diff per comment).
- ADF normalization is non-trivial; recommend the adapter stores
  the *rendered text view* (`renderedFields.description`, which Jira
  computes as HTML→stripped text) and that becomes the canonical
  body. L0 byte-compares the rendered text, not the ADF JSON.
- The new points-based rate limits enforced 2026-03-02 ([Atlassian
  rate-limit evolution][jira-ratelimits]) charge per-app, not
  per-token; orchestrator must measure cost in points, not requests.

**Rate limits:** points-based, per-app, shared across all tenants
using the app. API-token traffic still uses legacy per-minute burst
limits. Adapter MUST honor `Retry-After` and the `X-RateLimit-*`
headers; ask for the `429` documentation page on first non-200.

**Webhooks:** at-least-once with up to 5 retries on 5xx/429/timeout
with 5–15min randomized backoff. Each delivery has
`X-Atlassian-Webhook-Identifier` for idempotency. No native replay
API; failed deliveries can be inspected via "Get failed webhooks" up
to 3 months ([Jira webhook delivery][jira-webhook]).

**Verdict:** **degraded-mode.** Use Jira when the org mandates it;
warn loudly in the onboarding doc that the immutability guarantee
holds only at the rendered-text level, not at the ADF level.

---

## 5. Linear

**Immutability model:** GraphQL audit log via `IssueHistory`, similar
in spirit to GitHub's `userContentEdits` but field-typed instead of
content-typed.

**API endpoints:**

- `query { issue(id) { history { nodes { id, createdAt, actor,
  fromDescription, toDescription, fromTitle, toTitle, ... } } } }`
  — Linear's `IssueHistory` carries `fromDescription` and
  `toDescription` as full prior/next strings (markdown text, not
  ADF). This is the diff-capable surface.
- `issue.updatedAt` — fine-grained timestamp; advances on any field
  change.
- `issue.history.nodes[]` is paginated; ordered newest-first by
  default.

**What the adapter can offer:**

- `SourceRef{Kind:"ticket", Locator:"TEAM-42", SHA:<updatedAt
  RFC3339Nano>}`. Because Linear gives you full prior text via
  `fromDescription`, the adapter can reconstruct the body at
  read-time-T exactly, no lossiness — unlike Jira.
- L0 verifies the criterion text by walking back through
  `history.nodes` until it finds the entry where
  `createdAt <= read_time`, and reads `toDescription` from that node
  (which is the body the agent saw).

**Acceptance-criteria modeling:** Linear has two common shapes for
criteria:

1. **Markdown checkboxes within `description`.** Simple, but the
   adapter must parse `- [ ]` / `- [x]` lines out of the markdown
   description. State flips become description edits — which means
   *every* state flip looks like a body mutation to L0. **L0 must
   special-case the `[ ]`↔`[x]` byte change as a legal mutation,**
   exactly as it does for the markdown_catalog adapter (see
   `gates/l0/testdata/README.md` for the canonical exception). For
   parity with the markdown_catalog schema, this is the
   recommended shape.
2. **Sub-issues, one per criterion.** Stronger separation — each
   criterion is its own Linear issue with its own state, history,
   and assignee. The parent's `description` carries only the
   narrative spec; criterion text lives on the sub-issue title and
   is mutable only by changing a different resource. **For repos
   using Linear, sub-issues are arguably the more honest mapping** —
   the WorkItem maps to the parent, each `Criterion` maps to a
   sub-issue. The adapter SHOULD support both modes and select via
   `regatta.yaml: work_item_source.linear.criteria_mode:
   "checkbox" | "subissue"`.

**Gotchas:**

- `fromDescription`/`toDescription` are populated only when the
  description actually changed in that history entry; entries for
  state-changes, label-changes, etc. have them as `null`. Filter
  appropriately.
- Linear's GraphQL is complexity-priced (5,000 requests/hr *and*
  3,000,000 points/hr per API key; leaky bucket). Walking a deep
  `history` is expensive — adapter should cache the read-time
  snapshot in `regatta.db` rather than re-walk for L0 ([Linear rate
  limits][linear-ratelimits]).
- No conditional-request support (it's GraphQL).

**Webhooks:** at-least-once; up to 3 retries with backoff
1min/1hr/6hr. `Linear-Delivery` header is a v4 UUID per payload —
use as idempotency key. `webhookTimestamp` body field for replay-
attack guard. No documented Linear API for redelivering past
webhooks; if a delivery is dropped beyond the 3-retry window, it's
gone — orchestrator must reconcile via polling
([Linear webhooks][linear-webhook]).

**Verdict:** **degraded-to-first-class** depending on `criteria_mode`.
In `subissue` mode the guarantee is as strong as GitHub Issues; in
`checkbox` mode it's strong-but-requires-L0-special-casing for the
`[ ]`↔`[x]` flip. Recommend `subissue` as default.

---

## 6. Cross-platform: concurrency

What happens if a human edits the spec while a Regatta agent is
mid-work?

| Platform | Detection mechanism | Optimistic-concurrency token |
|---|---|---|
| GitHub Issues | ETag (REST) or `userContentEdits[last].editedAt` (GraphQL) | weak ETag, suitable for `If-None-Match` re-read |
| GitLab Issues | `updated_at` timestamp only | none — adapter polls and compares timestamps |
| markdown_catalog | git commit SHA on `main` | commit SHA |
| Jira | `issue.version` integer + last `changelog.id` | numeric version; supports `If-Match: <version>` on PUT |
| Linear | `issue.updatedAt` | timestamp; no `If-Match` |

**Recommended adapter behavior:** every read records the token in
`SourceRef.SHA`. Before the agent flips state, the adapter re-reads
and confirms the token unchanged. If changed, the adapter returns
`ErrSourceMutated`; the agent halts and the orchestrator logs a
"needs-rework" with the diff.

This already matches the design doc's L0 contract. The new piece
this research surfaces: **L0 must run at *PR-open time* against the
recorded `SourceRef.SHA`, not just at merge-time.** Without that, the
agent may waste an iteration writing tests against criteria a human
has already rewritten. Recommend adding this to `gates/l0/testdata/README.md`
as a normative rule.

---

## 7. Cross-platform: rate-limit budget

For an orchestrator that polls every 60s for ready items across N
work items:

| Platform | Cost / poll cycle | Headroom @ 5k/hr | Notes |
|---|---|---|---|
| GitHub Issues | 1 conditional request per item + 1 list req; 304s free | very high | use ETag-cached reads |
| GitLab Issues | 1 list req + 1 detail per item if `updated_at` advanced | very high (2k/min default) | rarely the bottleneck |
| markdown_catalog | 1 `git fetch` + diff; no API | effectively free | bounded by git server |
| Jira | depends on `expand=changelog` cost (points-priced) | medium | budget in points, not reqs; new model enforced 2026-03 |
| Linear | 1 list query + 1 detail per stale item; complexity points dominate | low if walking history | cache history walks in `regatta.db` |

Polling cadence floor (`Capabilities.MinPollInterval`) per platform:

- GitHub: 30s (1 list req / 30s = 120/hr, ≪ 5k budget)
- GitLab: 30s (default user limit is 2,000/min — generous)
- markdown_catalog: 10s (it's a local git fetch)
- Jira: 60s (points-priced; conservative until orchestrator measures
  actual cost in prod)
- Linear: 60s (complexity points; conservative)

---

## 8. Cross-platform: webhook reliability

| Platform | Delivery | Idempotency header | Replay | Notes |
|---|---|---|---|---|
| GitHub | at-least-once | `X-GitHub-Delivery` (UUID) | REST API, 30-day window | best-in-class; `changes.body.from` in payload |
| GitLab | at-least-once | `webhook-id` | REST API, project-webhooks resend | auto-disable after 4 (temp) / 40 (perm) failures |
| markdown_catalog | n/a — git push hook → use commit SHA | commit SHA | n/a | not webhook-driven; orchestrator pulls |
| Jira | at-least-once | `X-Atlassian-Webhook-Identifier` | no API; 3-month retention of failed | 5 retries, 5–15min backoff |
| Linear | at-least-once | `Linear-Delivery` (UUID) | none | only 3 retries; reconcile via poll beyond that |

`spec_event_id` should be the platform's native idempotency header
verbatim — `X-GitHub-Delivery`, `webhook-id`, `commit-sha:<sha>`,
`X-Atlassian-Webhook-Identifier`, or `Linear-Delivery`. SpecWatcher
keys its dedupe table on `(adapter_kind, spec_event_id)`. This is
the spec analogue of the orchestrator's `(pr_sha, gate_id)` gate
keying.

---

## 9. Capabilities matrix

Proposed rows (some already in `Capabilities`, some new):

| Capability | GitHub | GitLab | Markdown | Jira | Linear |
|---|---|---|---|---|---|
| `Webhook` | ✓ | ✓ | ✗ (git push) | ✓ | ✓ |
| `BulkUpdate` | ✗ (per-issue) | ✗ | ✓ (one commit) | partial (bulk-edit API exists, transactional within issues only) | partial (GraphQL multi-mutation, not atomic) |
| `SupportsAuditedEditHistory` (new) | ✓ (GraphQL `userContentEdits`) | ✗ (no public API) | ✓ (git log) | partial (changelog stores plain-text projection of ADF) | ✓ (`IssueHistory.fromDescription`) |
| `SupportsImmutableSnapshot` (new) | ✓ (ETag + audit log) | ✗ (timestamp only) | ✓ (commit SHA) | partial (`version` + lossy changelog) | ✓ (timestamp + audit log) |
| `SupportsOptimisticConcurrency` (new) | ✓ (`If-None-Match`) | ✗ | ✓ (SHA pin) | ✓ (`If-Match: <version>`) | ✗ (timestamp only) |
| `SupportsCriterionState` (new — per-criterion state flip without rewriting parent body) | ✗ (state lives in body unless using task-list API) | ✗ (state in body) | partial (`[ ]`↔`[x]` is a body edit, L0 special-cases) | ✓ (sub-task or custom field) | ✓ (sub-issue mode) |
| `SupportsAtomicTransitions` (new — multiple criterion flips in one operation) | partial (tasklist API allows updating multiple checkboxes in one PATCH) | ✗ | ✓ (one commit) | ✓ (bulk-edit API) | partial (GraphQL aliases, no transaction) |
| `SupportsWebhookReplay` (new) | ✓ (30 days) | ✓ (project webhooks API) | n/a | partial (3-month *inspection* only, no replay) | ✗ |
| `RateLimitModel` (new — enum: per-token / per-app / per-ip / per-points) | per-token + per-app | per-user + per-ip | n/a | per-app + points | per-key + points |

Legend: ✓ full support · partial (one-line caveat) · ✗ no support.

---

## 10. Recommendations

**Tiering for v1:**

- **First-class** (Regatta makes the full immutability promise):
  - `github_issues`
  - `markdown_catalog`
- **First-class conditional on config:**
  - `linear` (only in `criteria_mode: subissue`)
- **Degraded-mode** (Regatta detects mutation, may not recover prior
  text; documented gap):
  - `gitlab_issues`
  - `jira`
  - `linear` (in `criteria_mode: checkbox`)

This matches the user's instinct on GitHub + markdown first-class. It
also identifies that GitLab is *not* at GitHub parity despite both
being "issues APIs" — GitLab has no equivalent to
`userContentEdits`, and that gap is structural, not fixable by the
adapter.

**Operator-visible UX:** the `regatta init` wizard should print the
tier when the operator picks an adapter:

> You selected `gitlab_issues`. Regatta runs in **degraded-mode** for
> this adapter: it will detect that a human edited the description
> while an agent was working, but cannot recover the prior text.
> Consider tracking specs in `markdown_catalog` mode instead.

**Per-repo override:** `regatta.yaml: work_item_source.tier_override:
strict|warn|off` lets the operator pick how L0 reacts on
`ErrSourceMutated`. Default = `strict` (fail PR); `warn` (gate-comment
only) is appropriate for degraded-mode adapters during rollout.

---

## 11. Concrete change-list for `schemas/work_item_source.go`

```go
type Capabilities struct {
	Webhook           bool
	BulkUpdate        bool
	MinPollInterval   time.Duration
	SupportedStatuses []Status

	// New (this research).

	// SupportsImmutableSnapshot reports whether SourceRef.SHA is a
	// strong immutability token (commit SHA, ETag, or version number)
	// vs a weak timestamp. L0 trusts strong tokens for byte-equality
	// checks; weak tokens fall through to live re-read + warn.
	SupportsImmutableSnapshot bool

	// SupportsAuditedEditHistory reports whether the adapter can
	// retrieve the body text as it was at SourceRef.SHA, even after
	// subsequent edits. Required for L0 to recover and diff prior
	// versions on ErrSourceMutated.
	SupportsAuditedEditHistory bool

	// SupportsOptimisticConcurrency reports whether the adapter can
	// pass an If-Match / If-None-Match style precondition on
	// UpdateStatus. When false, UpdateStatus is a blind write and
	// the orchestrator may clobber concurrent human edits.
	SupportsOptimisticConcurrency bool

	// SupportsCriterionState reports whether the adapter can flip
	// a single criterion's state without rewriting the parent
	// WorkItem body. When false (GitHub free-form body, GitLab body,
	// markdown checkbox), the body itself records state and L0 must
	// special-case the [ ]↔[x] byte change.
	SupportsCriterionState bool

	// SupportsAtomicTransitions reports whether multiple criterion
	// state flips can be applied in one upstream operation. Useful
	// for batched completion at PR-merge time.
	SupportsAtomicTransitions bool

	// SupportsWebhookReplay reports whether the platform exposes an
	// API to redeliver a webhook by id. Affects SpecWatcher's
	// recovery strategy after orchestrator restart.
	SupportsWebhookReplay bool

	// RateLimitModel reports the rate-limit shape so the
	// orchestrator can budget correctly.
	RateLimitModel RateLimitModel
}

type RateLimitModel string

const (
	RateLimitPerToken  RateLimitModel = "per_token"
	RateLimitPerApp    RateLimitModel = "per_app"
	RateLimitPerIP     RateLimitModel = "per_ip"
	RateLimitPerPoints RateLimitModel = "per_points"
	RateLimitNone      RateLimitModel = "none" // markdown_catalog
)
```

Also propose a new sentinel error:

```go
// ErrSourceUnverifiable is returned when the adapter cannot prove
// criterion text has not been mutated since SourceRef.SHA was
// recorded. Distinct from ErrSourceMutated (which is a positive
// detection of change). Degraded-mode adapters return this when the
// best they can do is "I don't know."
var ErrSourceUnverifiable = errors.New("regatta: source verifiability degraded")
```

L0 treats `ErrSourceUnverifiable` per `work_item_source.tier_override`:
`strict` → fail; `warn` → gate-comment; `off` → ignore.

---

## References

- [GitHub REST best-practices (conditional requests)][gh-rest-best]
- [GitHub REST rate limits][gh-ratelimits]
- [GitHub Issues timeline API][gh-timeline]
- [Octokit conditional-request thread][gh-octokit]
- [GitHub webhook delivery (X-GitHub-Delivery, redelivery)][gh-webhook]
- [GitLab description-edit-history requests][gl-history-issue]
- [GitLab resource state events][gl-state-events]
- [GitLab user/IP rate limits][gl-ratelimits]
- [GitLab webhooks docs][gl-webhook]
- [Jira changelog (`expand=changelog`) format][jira-changelog]
- [Jira ADF migration (description field)][jira-adf]
- [Jira new points-based rate limits (2026-03)][jira-ratelimits]
- [Jira Cloud webhook delivery][jira-webhook]
- [Linear rate-limiting][linear-ratelimits]
- [Linear webhook delivery][linear-webhook]

[gh-rest-best]: https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api
[gh-ratelimits]: https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api
[gh-timeline]: https://docs.github.com/en/rest/issues/timeline
[gh-octokit]: https://github.com/octokit/octokit.js/issues/2563
[gh-webhook]: https://docs.github.com/en/webhooks/using-webhooks/handling-webhook-deliveries
[gl-history-issue]: https://gitlab.com/gitlab-org/gitlab/-/issues/10103
[gl-state-events]: https://docs.gitlab.com/ee/api/resource_state_events.html
[gl-ratelimits]: https://docs.gitlab.com/administration/settings/user_and_ip_rate_limits/
[gl-webhook]: https://docs.gitlab.com/ee/user/project/integrations/webhooks.html
[jira-changelog]: https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issues/
[jira-adf]: https://developer.atlassian.com/cloud/jira/platform/apis/document/structure/
[jira-ratelimits]: https://www.atlassian.com/blog/platform/evolving-api-rate-limits
[jira-webhook]: https://developer.atlassian.com/cloud/jira/platform/webhooks/
[linear-ratelimits]: https://linear.app/developers/rate-limiting
[linear-webhook]: https://linear.app/developers/webhooks
