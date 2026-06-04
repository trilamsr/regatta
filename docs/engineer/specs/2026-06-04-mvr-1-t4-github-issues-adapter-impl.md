---
status: draft
phase: mvr-1
spec_id: MVR-1-T4
supersedes: docs/engineer/specs/2026-06-02-mvr-1-t2-regatta-init-bundle.md (subset — T4 portion only)
companion: contracts/schemas/regatta.v1.cue (SpecAdapter.github_issues)
date: 2026-06-04
---

# MVR-1-T4 — `github_issues` spec adapter — impl-ready

## 0. TL;DR

`internal/orchestrator/adapter/github_issues.go` implements `schemas.SpecAdapter` over `gh issue list` so persona-A operators can use GitHub Issues as the work-item source instead of `.regatta/items/*.md`. The adapter consumes issues labelled `autonomous`, projects them deterministically into `schemas.WorkItem`, and dedups via a `<!-- regatta-dedup-key: <hex> -->` HTML-comment marker written into the issue body — the same convention `internal/selfimprove/detector.go:131-143` (`extractDedupKey`) already ships. Closed/mutated issues surface as tombstones (the same shape `markdown_catalog` uses for vanished files). `markdown_catalog` stays in tree as the in-repo authoring path; `github_issues` is the opt-in for operators whose plan-of-record lives in their issue tracker. Pure addition to the existing CUE discriminator (`#SpecAdapter.type = "github_issues"`) — schema already shipped at `contracts/schemas/regatta.v1.cue:64`; only wiring + impl are new. Substrate writes (observability audit trail) are deferred to a F-followup PR (§13 F20) because the existing substrate seam at `internal/orchestrator/state/substrate/event.go:137` requires `RunID + TenantID + HMAC key` fields the adapter cannot synthesize; the dedup oracle (GH body marker) does not depend on substrate, so the adapter ships without that dep. Deterministic projection (no LLM inference); rejected ambiguous extractions surface as `ErrPermanent` so the scheduler never silently drops a malformed issue. Canonical acceptance heading is `## Acceptance criteria` (matches markdown_catalog's existing parser convention); metadata syntax is HTML-comment YAML (`<!--regatta ... -->`) — single pick, no precedence rule needed.

## 1. Contract

### 1.1 Public surface

One constructor returning the canonical interface. Mirrors `NewMarkdownCatalog` shape (`internal/orchestrator/adapter/markdown.go:73`).

```go
// Package adapter — additions to existing file github_issues.go.

// GitHubIssuesConfig configures a GH-Issues-backed adapter.
//
// File format expected on the GH side:
//   Issue title:   "ID: human title"        (required; ID matches ^[A-Z][A-Z0-9_-]{1,40}$)
//   Issue body:    free prose + HTML-comment metadata block (see §3.5)
//                  <!--regatta
//                  lane: server                (optional; default "")
//                  dependencies: ID-A, ID-B    (optional; comma-sep)
//                  linked_artifact: path.md    (optional)
//                  -->
//                  ## Acceptance criteria
//                  - [planned] c1: First criterion
//                  - [planned] c2: Second criterion
//
// Labels required for List() to include the issue:
//   "autonomous"           — discriminator (see §2)
//   "source:<producer>"    — provenance tag (operator / self-improve / alarm)
//
// SourceRef.Kind="issue"; SourceRef.Locator="github://<owner>/<repo>/issues/<n>";
// SourceRef.SHA="sha256:<hex of body bytes after CRLF→LF + NFC>".
type GitHubIssuesConfig struct {
    // Client is the ghclient.Client seam; production wires the HTTP impl.
    // Extends today's 3-method interface (§10.2) with a paginated list +
    // single-issue read + body-edit append. Nil → constructor returns an error.
    Client ghclient.Client

    // Repo names the owner/name pair the adapter scans. Required.
    Repo schemas.Repo

    // Selector is the gh-flavoured label expression from regatta.yaml
    // spec_adapter.selector (e.g. "label:autonomous,label:source:operator").
    // Always conjunctively ANDed with the implicit "label:autonomous" guard
    // even if absent — see §2.2.
    Selector string

    // AcceptanceSection is the H2 heading the criterion parser anchors on.
    // Defaults to "## Acceptance criteria" matching the markdown_catalog
    // convention (see §3.4).
    AcceptanceSection string

    // MinPoll caps how often List may hit GH. Defaults to 30s (vs
    // markdown_catalog's 5s — disk vs network). Respects gh's
    // 5000-req/hour primary REST budget.
    MinPoll time.Duration

    // Logger / Tracer follow the markdown_catalog convention.
    Logger func(format string, args ...any)
    Tracer trace.Tracer
}

// NewGitHubIssues returns a schemas.SpecAdapter backed by GH Issues
// labelled "autonomous". Returns ErrInvalidConfig on missing required
// fields (Client / Repo).
func NewGitHubIssues(cfg GitHubIssuesConfig) (schemas.SpecAdapter, error)
```

The returned value satisfies the existing `schemas.SpecAdapter` interface (`contracts/schemas/spec_adapter.go:22`). No interface change — additive only.

WHY no `SubstrateRecorder` field: the dedup oracle is the GH body marker (§4), not substrate. The existing substrate write seam at `internal/orchestrator/state/substrate/event.go:137` (`AppendEvent(ctx, tx, e Event, key, keyID)`) requires a 15-field signed `Event` struct including `RunID`, `TenantID`, and HMAC-key inputs the adapter has no way to synthesize from its spawn context. Rather than block this PR on a precursor `RecordObservation(kind, payload)` wrapper, the adapter ships substrate-write-free; the audit-trail wiring is F20 §13. The dedup oracle does not depend on substrate (§4), so this defer is load-bearing-safe.

### 1.2 Method semantics

| Method | Behaviour |
|---|---|
| `List(ctx)` | One paginated `gh issue list --label autonomous --state open --json number,title,body,labels,updatedAt`. Filter via selector AND-guard. Project each issue (§6). Skip-and-WARN any issue that fails projection. Returns sorted by `WorkItem.ID`. |
| `Get(ctx, id)` | One `gh issue view <n>` where `<n>` resolved via local map built from last `List` call (cached for `MinPoll`). On cache miss → adapter re-issues a single `gh issue list --label autonomous --state open --search "id:<ID>"` to rebuild the map, then re-resolves once; if still unresolved → `ErrNotFound`. Cache TTL is exactly `MinPoll`; oldest-first eviction. See test `TestGitHubIssues_Get_CacheMissAfterMinPoll_Refetches` (§11). `ErrPermanent` if >1 hit on rebuild (ID collision — operator error). |
| `UpdateStatus(ctx, id, status, citation)` | No-op for the MVR-1 cut. Status lives in the title prefix or a body marker today is rejected — operator closes issues manually (matches `markdown_catalog`'s "operator edits the file" model). Returns `ErrAdapterUnsupported` so callers know not to retry. Reopen criterion: see §13 F1. |
| `Capabilities()` | `{Webhook: false, BulkUpdate: false, MinPollInterval: cfg.MinPoll, SupportedStatuses: [StatusPlanned, StatusInProgress, StatusDone]}`. `closed-resolved` omitted — see §7. |

`List` MUST honour `ctx` cancellation between pages. Rate-limit responses (HTTP 403 with `X-RateLimit-Remaining: 0`) wrap `ErrRateLimited` with `RateLimitHint.RetryAfter` typed as `time.Duration` (see §7.4 for format pick and scheduler behaviour).

## 2. Label + filter conventions

### 2.1 The single required label: `autonomous`

The adapter consumes ONE label as the consume-vs-ignore discriminator: `autonomous`. This matches the existing producer convention in `internal/alarmwebhook/dedup.go:20` (`const autonomousLabel = "autonomous"`). The selfimprove detector today files under `self-improvement` only (`internal/selfimprove/rule.go:29`); the adapter's tracking issue F4 (§13) widens selfimprove to dual-label `[autonomous, source:self-improve]` so the producer surface unifies.

WHY `autonomous` (not `regatta-work-item`, `task`, etc.) — the label is the operator's contract: "this issue is fair game for the autonomous loop to pick up unattended." Naming preserves the same semantic the alarm webhook already shipped. Persona-A maintainers who file an issue and DO NOT want regatta to pick it up simply omit the label — same opt-in shape as Renovate's `automerge` (§6.6 roadmap precedent).

### 2.2 Per-source provenance label: `source:<producer>`

| Producer | Source label | Title prefix (existing) | Schema migration in this PR |
|---|---|---|---|
| Operator (filed by hand or `regatta init` template) | `source:operator` | `<ID>:` | — |
| Self-improve detector (`internal/selfimprove`) | `source:self-improve` | `[self-improve]` (existing) | Add `autonomous` + `source:self-improve` to label set; keep `self-improvement` for one release for rollback. F4 §13. |
| Alarm webhook (`internal/alarmwebhook`) | `source:alarm` | `[obs-alert]` (existing) | Already files `autonomous`; add `source:alarm` per F5 §13. |
| Future (T7 LLM-authored, T5 SCM-replay) | `source:<producer>` | per-producer | reserved. |

Source labels are advisory — used for L4 reviewer prompt routing and operator filtering, NEVER for filter inclusion. The discriminator is `autonomous` alone. WHY single-key discriminator: a per-source allowlist is a 4-row truth table that grows every wave; a single guard label keeps the truth table at one row and pushes per-source nuance into the projection layer where it can be tested deterministically.

### 2.4 Producer-issue projection rejection (R6 fold)

Selfimprove + alarmwebhook today file `[self-improve] ...` and `[obs-alert] ...` issues — both carry (or will carry per F4/F5) the `autonomous` label for unified labelling, BUT their titles do not satisfy the `^[A-Z][A-Z0-9_-]{1,40}:\s` ID-prefix shape required for `WorkItem.ID` extraction (§6.1). These issues are intentionally NOT projected as WorkItems:

- They are observability artifacts (rule-fire records, alert audits), not units of work.
- An operator who decides to turn one into a work item edits the title to add an `ID:` prefix and updates the body to match the §3 template.
- Until then, the projection layer skips them with a WARN log carrying `{adapter, repo, issue_number, reason}` (substrate audit emission is deferred — F20 §13).

WHY dual-label rather than separate labels: a single discriminator (§2.1) keeps the operator filter UX simple. The projection layer is the right place to reject observability-shaped titles, not the label filter. Pushes nuance into the deterministic, test-able layer.

### 2.3 Reject patterns (adversarial)

| Pattern | Adapter action | Reason |
|---|---|---|
| Issue closed | Skip silently (List filters `--state open`). | Closed = operator done; never re-enqueue (matches `StatusClosedResolved` semantics, `spec_adapter.go:88`). |
| Issue has `autonomous` label but no `source:*` | Project anyway. WARN-log missing source. | Operator-filed without template; do not block. F6 §13 may tighten later. |
| Issue has `source:operator` but no `autonomous` | NOT consumed. | Operator-filed bug reports MUST NOT auto-dispatch silently. Discriminator-leak guard. |
| Issue has `autonomous` but title fails `^[A-Z][A-Z0-9_-]{1,40}:` ID parse | Skip + WARN with skip-payload (§7.6). | Ambiguous projection — fail closed. |
| Issue has duplicate `ID:` prefix with another open `autonomous` issue | Both skipped + WARN + comment-on-both-issues (§7.8). `ErrPermanent` on `Get`. | Operator error — refuse to silently pick one. |
| Issue body lacks `## Acceptance criteria` section | Project with empty criteria + WARN. | Soft-fail — adapter still surfaces title/lane/deps. L0 will block dispatch when criteria are empty (existing gate); the adapter's job is the projection, not the policy. |

## 3. Acceptance-criterion extraction

### 3.1 Required issue-body shape (operator-facing template)

The `regatta init` wizard (MVR-1-T2 §4 wizard step 7) writes a `.github/ISSUE_TEMPLATE/autonomous.yml` that produces:

```markdown
<free-form description prose>

<!--regatta
lane: server
dependencies: ID-A, ID-B
linked_artifact: docs/rfc/foo.md
-->

## Acceptance criteria

- [planned] c1: First criterion text
- [planned] c2: Second criterion text
```

Three load-bearing sections:

1. **HTML-comment YAML metadata block** — `<!--regatta ... -->` wrapping YAML key/value pairs the adapter parses for `lane`, `dependencies`, `linked_artifact`. All three optional; missing block = all defaults. See §3.5 for the syntax pick rationale.
2. **`## Acceptance criteria` H2** — anchors criterion extraction. Same parser as `parseMarkdownItem` (`internal/orchestrator/adapter/markdown.go` — `extractAcceptance` helper). Default heading is `cfg.AcceptanceSection` matching the markdown_catalog convention (§3.4).
3. **Bullet criteria** — `- [<state>] <id>: <text>` shape, same as markdown_catalog. `<state>` ∈ `{planned, in_progress, done, closed}`. `<id>` ∈ `^c[0-9]+$` or `^[a-z][a-z0-9_-]{0,20}$`.

### 3.2 Default-fallback projection

| Field | Default when absent |
|---|---|
| `lane` | `""` (empty = default lane, matches markdown_catalog) |
| `dependencies` | empty slice |
| `linked_artifact` | `""` |
| `## Acceptance criteria` block | empty `AcceptanceCriteria` (NOT an error — see §2.3 last row) |
| `kind` | `KindFeature` (always; programs require explicit operator override in §3.3 reopen item) |

### 3.4 Canonical heading: `## Acceptance criteria` (R3 fold — DEFINITIVE PICK)

Markdown catalog docstring (`internal/orchestrator/adapter/markdown.go` lines 36-42) reads `## Acceptance criteria` with a " criteria" suffix. The CUE schema default at `contracts/schemas/regatta.v1.cue:68` reads `## Acceptance` (no suffix). The two disagree today.

**Pick: `## Acceptance criteria`** (markdown_catalog-aligned, WITH `criteria` suffix).

WHY changed from the prior draft pick:

- (a) The markdown_catalog parser is ALREADY SHIPPED and consumed by every existing operator config; changing it is a real-world migration, while changing the CUE default is a one-line edit.
- (b) Operators porting their existing `.regatta/items/*.md` templates to GH issue templates need ZERO heading rewrites — single heading across both adapters, no migration silent-breakage.
- (c) The CUE default at `contracts/schemas/regatta.v1.cue:68` is amended in this PR's companion patch to read `*"## Acceptance criteria" | string` (alongside the schema row addition for `github_issues`).

Operator-override: `cfg.AcceptanceSection` (from `regatta.yaml::spec_adapter.acceptance_section`) lets operators pin a custom heading if they need to. Default is `"## Acceptance criteria"`.

The prior-draft alternative `## Acceptance` (no-suffix) is **DELETED** — single canonical heading, no migration table, no per-adapter divergence. F18 (prior-draft markdown_catalog dual-heading cleanup) is rescinded because no parser-side change is needed.

### 3.5 Metadata syntax: HTML-comment YAML — DEFINITIVE PICK (R4 fold)

The metadata block uses HTML-comment-wrapped YAML and NOTHING else:

```markdown
<!--regatta
lane: server
dependencies: ID-A, ID-B
linked_artifact: docs/rfc/foo.md
-->
```

The prior-draft fenced ` ```regatta ` alternative is **DELETED**. No precedence rule needed — there is one syntax.

WHY HTML comment (not fenced block, not raw frontmatter):

| Surface | Fenced ```regatta | Raw `---` frontmatter | HTML comment |
|---|---|---|---|
| Renders cleanly on GH | Yes (as a code block) | NO — renders as `<hr>` + corrupted body | Yes (invisible to reader) |
| Survives GitHub mobile renderer | Yes | Sometimes | Yes |
| Parser-friendly (YAML.Unmarshal-able) | No (need custom fence parser) | Yes | Yes (after stripping comment markers) |
| Ambiguous with release-notes fence convention (`feedback_pr_body_hygiene`) | YES — visually identical to a triple-fenced block; HEREDOC mis-escaping breaks both | No | No |
| Invisible to human readers (clean issue body) | No (renders as code) | Body-corrupting | Yes |

HTML-comment YAML wins on every row except "parser-friendly" where the gap is one `bytes.TrimPrefix` + `bytes.TrimSuffix` call. The release-notes-fence collision is the load-bearing reason — adopting a fenced syntax that mimics the project's release-notes convention is an unforced footgun.

Parser: locate first `<!--regatta` and matching `-->`, extract bytes between, run `yaml.Unmarshal`. Reject malformed YAML with skip-payload (§7.6) + WARN.

### 3.3 Rejected ambiguity

- Two `## Acceptance criteria` sections in one body → skip + WARN with skip-payload (§7.6). Operator must collapse.
- A criterion bullet outside the `## Acceptance criteria` block → ignored. The H2 is the only anchor.
- Markdown lists nested inside the HTML-comment metadata block → ignored. The block is YAML, not markdown.
- Unknown keys inside the metadata block → ignored with WARN. Forward-compat: future versions may add `kind: program`, `priority: P0`, etc.

WHY deterministic parsing without LLM fallback: per `feedback_drop_ceremony` + `feedback_research_design_principles`, projection MUST be auditable + reproducible. A future "natural-language acceptance criteria" wedge (LLM-extracted) lives behind a flag, not in the default projection path — see §13 F2.

## 4. Dedup

### 4.1 Dedup oracle: GH body marker (DEFINITIVE PICK)

The dedup oracle is a `<!-- regatta-dedup-key: <hex> -->` line inside the issue body. This is the same convention `internal/selfimprove/detector.go:131-143` (`extractDedupKey`) already ships for selfimprove dedup. Pattern alignment with the existing producer convention is the load-bearing reason — adding a third dedup primitive (after selfimprove's body marker and alarmwebhook's body marker) for the same logical concern is a `feedback_research_design_principles` violation.

The prior-draft §4.1–§4.2 substrate-as-source-of-truth wording is **DELETED**. Substrate writes are deferred entirely to F20 §13 (audit-trail-only, not load-bearing for dedup).

### 4.2 Adapter-computed dedup key

`dedup_key = sha256_hex(<owner>/<repo>:<issue_number>:<body_sha256>)`

where `body_sha256` is computed over body bytes after CRLF→LF + NFC normalization (matches `Criterion.Text` normalization at `spec_adapter.go:66`), with the `<!-- regatta-dedup-key: ... -->` line itself elided from the hash input to keep the key stable across the backfill write.

Storage / lifecycle:

- The `regatta init` issue template embeds an initial `<!-- regatta-dedup-key: <hex> -->` line with placeholder hex.
- On first `List` sighting of an issue whose body lacks the marker (or carries the placeholder), the adapter computes the key and back-fills via a single `gh issue edit <n> --body-file -` call. WHY a single backfill write: gh CLI is the only write surface (§10.1), so the adapter trades one-time write quota for permanent dedup determinism.
- On subsequent `List` ticks the adapter reads the marker; in-process `map[dedup_key]issue_number` accumulates across the tick.
- On collision (two open issues, same key) → WARN + comment-on-both-issues (§7.8). Operator filed dup.

### 4.3 Body-mutated-mid-flight policy: re-queue, not fail-closed

Operator could edit a `autonomous`-labelled issue body while the agent is mid-dispatch. Two-policy bake-off:

| Policy | Pro | Con | Pick |
|---|---|---|---|
| Re-queue (recompute dedup key from new body, surface `ErrSourceMutated` on `Get`, scheduler restarts) | Operator's edit takes effect; matches `markdown_catalog`'s file-edit-restarts semantic (markdown adapter re-reads on each List). | One in-flight cycle wasted. | **Yes — default.** |
| Fail-closed (skip until operator force-acks) | No wasted cycles. | Operator edit silently ignored; trust hole. | No. |

The adapter recomputes the dedup key, the next `List` emits the updated WorkItem, the scheduler observes `ErrSourceMutated` on its in-flight `Get` and aborts the current attempt (existing handling in `internal/orchestrator/state/machine.go` per the `ErrSourceMutated` sentinel at `spec_adapter.go:132`). L0 (which verifies criterion-text-at-SHA) blocks the in-flight PR from merging since its captured SHA no longer matches — the existing immutability invariant carries the load.

WHY re-queue (not fail-closed): mid-flight edits are operator-driven by definition; the trust contract is that the operator's most recent intent wins. Markdown catalog already ships this semantic implicitly (file re-read every tick) — choosing fail-closed for GH-Issues would create a producer-asymmetry against `markdown_catalog` that violates `feedback_research_design_principles` (consistency over per-adapter discretion).

### 4.4 Why GH body marker (not in-memory map, not substrate)

Three reasons GH body marker is load-bearing here vs an adapter-local `sync.Map` vs substrate writes:

1. **Pattern alignment** — selfimprove + alarmwebhook BOTH dedup via body markers; adopting a third primitive for the same concern is a `feedback_research_design_principles` violation.
2. **Process restart survives** — the body marker lives on GitHub, not in adapter memory; restart loses nothing.
3. **No substrate dependency** — the substrate seam at `internal/orchestrator/state/substrate/event.go:137` requires RunID + TenantID + HMAC key the adapter cannot synthesize from its spawn context. Body marker decouples dedup from substrate-wiring entirely. Substrate is then optional audit (F20 §13) rather than a precursor blocker.

### 4.5 (DELETED — replaced by §4.1)

§4.5 in the prior draft proposed dual-oracle (substrate + body marker). The dual proposal is rescinded; §4.1 is single-source.

### 4.6 Substrate write is audit-only, not load-bearing (NEW)

The adapter does NOT write to substrate in this PR. WHY: the only existing substrate write seam (`AppendEvent` at `internal/orchestrator/state/substrate/event.go:137`) requires a 15-field signed `Event` struct (`ID, RunID, TenantID, Nonce`, signature fields) that the adapter has no spawn-context source for. Adding a `RecordObservation(kind, payload string)` wrapper that synthesizes those fields is a precursor PR the implementer SHOULD NOT block on.

The adapter ships substrate-free. Skip / tombstone / observed signals surface via:

- WARN logs with structured fields (`adapter, repo, issue_number, reason`) — operator-console can scrape logs in MVR-1.
- GH issue comments for high-severity cases (collision §7.8) — operator-visible without any backend.

F20 §13 tracks the wrapper PR. When it lands, the adapter's WARN path emits the same structured payload to substrate; nothing observable changes for operators except richer operator-console queries.

## 5. Polling + pagination

### 5.1 Cadence

`MinPoll` defaults to 30s (vs `markdown_catalog`'s 5s). WHY higher: gh REST has a 5000-req/hour primary budget; one full `List` is typically 1-2 calls (gh CLI batches up to 100 issues/page). 30s ceiling = 120 ticks/hour × 2 calls = 240 calls/hour, leaving headroom for `Get`, the alarm webhook, and the selfimprove detector all sharing the same token.

The orchestrator scheduler (existing `internal/orchestrator/scheduler/`) reads `Capabilities().MinPollInterval` and obeys it — no adapter-side throttle needed.

### 5.2 Full-scan vs cursor

**Full-scan with body-marker dedup**, matching `markdown_catalog`'s file-scan pattern. Operators with N>1000 open `autonomous` issues are out of scope for MVR-1 (and would themselves bounce off persona-A definition of "solo or small-team maintainer"). Cursor-based pagination is a §13 F3 reopen item.

Implementation: single `gh issue list --label autonomous --state open --json ... --limit 1000`. gh CLI handles internal pagination via its `paginate` flag transparently. Adapter aborts with `ErrPermanent` if response carries >1000 issues — forces operator to file F3 before continuing.

### 5.4 Scheduler tick budget (R8 fold)

`Capabilities().MinPollInterval = 30s` is read by the scheduler's adapter-aware loop. Implementer MUST verify (integration-test) that the scheduler honours per-adapter `MinPollInterval` rather than running a fixed 5s tick that ignores the value. Existing markdown_catalog ships 5s which means the scheduler may have been hardcoded to that — if so, fix the scheduler (cite F19) before this adapter merges. The test: `TestScheduler_RespectsGitHubIssuesMinPoll30s` in `internal/orchestrator/scheduler/`.

### 5.3 No webhook for MVR-1

`Capabilities.Webhook = false`. GH webhook receiver belongs to a separate W4-style ingress (which already exists for alarm); reusing it for issue-events doubles the surface that the L4-gate's adversarial reviewer hunts. Reopen on §13 F7.

## 6. WorkItem projection (deterministic-only)

### 6.1 Field mapping table

| `WorkItem` field | Source on GH issue | Deterministic? |
|---|---|---|
| `ID` | Title prefix `^([A-Z][A-Z0-9_-]{1,40}):\s` → match group 1 | Yes |
| `Kind` | Always `KindFeature` (MVR-1) | Yes |
| `Title` | Title with the `ID:` prefix stripped, leading/trailing WS trimmed | Yes |
| `Body` | Issue body bytes, CRLF→LF + NFC; `## Acceptance criteria` section onwards stripped (criteria own that block); the `<!-- regatta-dedup-key: ... -->` line elided | Yes |
| `AcceptanceCriteria` | Bullet-parsed under `## Acceptance criteria` (§3) | Yes |
| `Dependencies` | Metadata-block `dependencies` field, comma-split, trimmed | Yes |
| `Lane` | Metadata-block `lane` field, or `""` | Yes |
| `Status` | Always `StatusPlanned` on `List` (in-flight state lives in orchestrator). GH `--state open` already filters closed. | Yes |
| `LinkedArtifact` | Metadata-block `linked_artifact` field, or `""` | Yes |
| `Source.Kind` | `"issue"` | Yes |
| `Source.Locator` | `"github://<owner>/<repo>/issues/<n>"` | Yes |
| `Source.SHA` | `"sha256:" + hex(sha256(body_nfc_with_dedup_key_line_elided))` | Yes |

### 6.2 Rejected fields (LLM-inference forbidden at projection)

- **`Kind = KindProgram` inference** — would require parsing the body for "this is a program-of-work" signal. Operator must override via a future metadata-block `kind: program` field (§13 F2). Until then, every GH issue is a feature.
- **Lane inference from labels** — tempting to read `lane:server` GH label → `WorkItem.Lane = "server"`. Rejected: GH labels are a flat namespace where operators set them for view-filtering reasons that may not align with regatta's lane semantics. Explicit metadata-block field wins.
- **Acceptance-criteria from prose** — natural-language extraction is a knob behind `--enable-llm-projection` (off by default, never on for MVR-1).

WHY deterministic-only at projection: per `feedback_drop_ceremony` + `feedback_research_design_principles`, every projection must be re-derivable from inputs alone. LLM inference in the projection path means the W9 replay harness produces non-determinism — adversarial-review fail.

## 7. Failure modes

### 7.1 Issue closed mid-work (tombstone)

GH issue transitions to closed while regatta has an in-flight PR keyed on that issue. Sequence:

1. Next `List` tick: issue absent from `--state open` output.
2. Adapter emits a WARN log with `{adapter, repo, issue_number, observed_at, kind:"tombstone"}`. Substrate emission is deferred (F20).
3. Scheduler sees the work item vanish on its next `List`-snapshot diff and emits the existing tombstone WARN path (matches `markdown_catalog`'s file-vanish semantics).
4. In-flight PR carries on — operator decides whether to close it manually. Adapter does NOT auto-close PRs.

WHY no auto-close: the closed GH issue may have been operator-triage-close-as-duplicate; abandoning the in-flight PR would discard work the operator wanted. Tombstone-as-WARN preserves both intents.

### 7.2 Body edited mid-work

See §4.3. Re-queue.

### 7.3 Label removed mid-work

Operator strips `autonomous`. Same handling as 7.1 — vanishes from `List`, tombstone WARN, in-flight PR continues. Symmetric with closure.

### 7.4 Rate limit hit (DEFINITIVE)

GH returns HTTP 403 with `X-RateLimit-Remaining: 0` and `X-RateLimit-Reset: <unix-seconds>`. Adapter parses the reset header, computes `retry_after := reset_time.Sub(time.Now())`, and returns:

```go
return nil, fmt.Errorf("github_issues list: %w", schemas.WrapErrRateLimited(
    schemas.RateLimitHint{RetryAfter: retry_after},
))
```

`RetryAfter` is typed `time.Duration` (Go-native, deterministic, comparable in tests via `time.Duration` equality). NOT a unix timestamp, NOT an ISO string — those would require time-source mocking in every consumer.

**Scheduler behaviour**: per `feedback_research_design_principles`, the scheduler treats `ErrRateLimited` as non-fatal for the offending adapter only. Behaviour:

- Scheduler backs off the github_issues adapter polling by `max(RetryAfter, MinPoll)`.
- Other adapters (markdown_catalog, future SCM adapters) continue ticking at their own `MinPollInterval`.
- The backoff is per-adapter-instance, NOT process-global.

Deterministic test pattern (see §11): `TestGitHubIssues_List_RateLimitWrapsErrRateLimited` injects a fake `ghclient.Client` that returns `&schemas.RateLimitError{Reset: time.Unix(1700000060, 0)}` against a fake clock pinned at `time.Unix(1700000000, 0)`; asserts the wrapped error carries `RetryAfter == 60 * time.Second` exactly.

### 7.5 gh CLI exit-code 1 / 2 / 4

Per gh CLI conventions: 1 = generic error (transient — `ErrTransient`), 2 = auth error (permanent — `ErrPermanent`), 4 = no matches (NOT an error — empty `List`).

### 7.6 Body fails projection — skip-payload schema (DEFINITIVE)

Skip + WARN. The WARN log carries a structured payload that is the same shape substrate WILL receive when F20 lands:

```json
{
  "adapter":      "github_issues",
  "repo":         "trilamsr/regatta",
  "issue_number": 590,
  "reason":       "title prefix did not match ID regex",
  "issue_url":    "https://github.com/trilamsr/regatta/issues/590"
}
```

`reason` is one of a closed enum: `bad_id_prefix`, `dup_id_prefix`, `dup_acceptance_section`, `bad_metadata_yaml`, `body_marker_backfill_failed`.

WHY structured payload: the operator who filed the issue needs a feedback loop — the existing operator-console v5.1 dashboard (per `2026-06-02-operator-console-design.md`) will render this once F20 wires substrate. Silent skip would leave the operator wondering why their issue is invisible. Test `TestGitHubIssues_Skip_RecordsObservation` (§11) asserts the payload schema verbatim.

### 7.7 Issue absent on `Get` (cache miss + refetch)

`Get` first consults the in-memory map built from the most recent `List` (TTL = `MinPoll`). On cache miss:

1. Adapter issues a single `gh issue list --label autonomous --state open --search "id:<ID>"` to rebuild the map for that ID.
2. If exactly one match → return projected `WorkItem`, prime the cache.
3. If zero matches → `ErrNotFound`.
4. If >1 matches → `ErrPermanent` (ID collision; matches §7.8).

After a successful refetch the cache TTL resets to `MinPoll` from the refetch time. Test `TestGitHubIssues_Get_CacheMissAfterMinPoll_Refetches` (§11) asserts: (a) cache hit within TTL avoids gh call; (b) cache expiry triggers exactly one `gh issue list --search` invocation; (c) refetch failure surfaces `ErrNotFound`, not a stale cache hit.

### 7.8 Duplicate `ID:` prefix (R9 fold)

§2.3 row 5 declares both issues skipped. To prevent silent stranding, the adapter additionally:

1. Logs at ERROR severity (not WARN) with both issue numbers + the collided ID.
2. Files a comment on BOTH issues: "duplicate work-item ID `<ID>` — regatta will not consume either issue until the collision is resolved". One comment per detection (in-process dedup-key: `collision:<owner>/<repo>:<ID>:<earliest-iso-date>`); the adapter keeps an in-memory bloom-filter-style set of (ID, day) tuples to avoid re-commenting on each `List` tick.
3. Surfaces via operator-console adapter-health pane once F20 + F6 land.

Test `TestGitHubIssues_List_DupIDPrefix_BothSkipped_CommentsOnBothIssues` (§11) asserts: (a) both issues skipped from `List` output; (b) `ghclient.CommentOnIssue` called exactly once per issue with the collision message; (c) re-running `List` on the same un-resolved pair does NOT re-comment (in-memory dedup holds).

Reopen: collision-auto-resolve (pick lower issue number) gated on operator opt-in flag. Not in MVR-1.

## 8. Security + trust boundary

### 8.1 Token scopes

`GH_TOKEN` already in use repo-wide (alarm + selfimprove). Required scopes:

| Operation | Scope on classic PAT | Fine-grained PAT permission |
|---|---|---|
| `List` open issues | `repo:read` or `public_repo` | `Issues: Read` |
| `Get` single issue | `repo:read` | `Issues: Read` |
| Body-marker backfill (§4.2) | `repo` (issue write) | `Issues: Write` |
| Collision comment (§7.8) | `repo` (issue write) | `Issues: Write` |
| (Future) update-status | `repo` | `Issues: Write` |

WHY write scope is in-scope NOW: the dedup oracle is a body marker the adapter back-fills on first sighting (§4.2), and the collision-feedback path comments on dup issues (§7.8). Both require `Issues: Write`. The init wizard (MVR-1-T2 §3 PAT prompt) is amended to request `Issues: Write` minimum (was `repo:read` in the prior draft); this is a load-bearing scope upgrade and the wizard CHANGELOG entry calls it out.

### 8.2 Malicious-issue defense — adapter-side per-field safety (R5 fold)

An operator-filed (or external-contributor-filed-and-operator-labelled) issue with shell-injection in title or body MUST NOT escape the projection boundary. The adapter's per-field safety is NECESSARY BUT NOT SUFFICIENT for end-to-end safety — downstream consumers (worker prompt builder, branch-name renderer) own the rest.

| Surface | Defense | Location of enforcement |
|---|---|---|
| Adapter→scheduler (Go types) | `WorkItem.Title` / `WorkItem.Body` are `string` — no shell interpolation. Adapter NEVER passes raw issue text to `os/exec`. | `internal/orchestrator/adapter/github_issues.go` (this PR) |
| Scheduler→prompt builder | Implementer worker prompt (currently in flight, see PR #834 — worker prompt enrichment) MUST treat issue body as untrusted input. Body is JSON-encoded into the prompt template, not string-interpolated. | `internal/dispatch/prompt.go` (out of scope for this spec — F9 §13 cross-cuts) |
| Scheduler→shell (worktree creation, gh CLI calls) | All shell args use `exec.Command(name, args...)` not `sh -c`. Issue title→branch name passes through `regexp.MustCompile("[^a-z0-9-]").ReplaceAllString` first. | `internal/orchestrator/state/machine.go` (existing) |

**Conditional safety statement (NEW)**: Adapter's per-field safety is necessary but NOT sufficient. The worker prompt-builder MUST sanitize issue-sourced fields. See PR #834 (worker prompt enrichment) for downstream defense. The F9 split is intentional: adapter owns extraction safety (this PR); orchestrator owns prompt-injection defense (F9). Both gates must hold to claim end-to-end safety; failure of either makes the system unsafe regardless of the other.

The adapter's load-bearing contribution: **only emit typed Go fields, never shell strings**. The downstream prompt-sanitization invariant is owned by the dispatch package and verified by an L4 reviewer prompt addition (F9 §13).

### 8.3 PII / secret bleed

Issue bodies may contain operator-pasted secrets. Adapter MUST NOT log full body content at INFO level; only `issue_number + body_sha256` are loggable. Test: `TestGitHubIssues_ListWarn_NeverLogsBodyBytes` (§11).

## 9. Migration story

### 9.1 Both adapters live in parallel

`markdown_catalog` stays in tree. Operators flip between adapters via `regatta.yaml`:

```yaml
# markdown_catalog (existing default)
spec_adapter:
  type: markdown_catalog
  root: "."

# github_issues (MVR-1-T4 new option)
spec_adapter:
  type: github_issues
  selector: "label:autonomous"
  acceptance_section: "## Acceptance criteria"
```

### 9.2 Operator switch flow

1. Operator runs `regatta init --reconfigure` (existing flag from MVR-1-T2 §6 idempotency).
2. Wizard asks "where do your work items live?" → `markdown_catalog` (default) | `github_issues`.
3. If `github_issues` picked: wizard installs `.github/ISSUE_TEMPLATE/autonomous.yml` (§3 template), confirms `GH_TOKEN` has `Issues: Write` scope (per §8.1 — load-bearing scope upgrade in this PR), writes the YAML.
4. `regatta serve` reads `spec_adapter.type` → instantiates the right adapter.

No data migration. The two adapters address different operator workflows: in-repo `.regatta/items/*.md` (markdown_catalog) vs in-issue-tracker authoring (github_issues). Neither subsumes the other.

A standalone runbook (F-NEW2, §13 F21) walks operators through markdown_catalog → github_issues port: harvest existing `.regatta/items/*.md` files into `gh issue create` calls with the canonical metadata block + acceptance heading; verify body-marker backfill on first `List` tick.

### 9.3 Concurrent-adapter rejection

MVR-1 forbids running both adapters simultaneously (one `regatta.yaml` `spec_adapter:` block, one type). Reopen as §13 F10 when a paying customer demands multi-source aggregation.

## 10. Adoption-first audit

### 10.1 OSS to adopt: gh CLI subprocess (pick) vs go-github (track) vs raw HTTP (reject)

| Option | Score | Verdict |
|---|---|---|
| **gh CLI subprocess** | ★★★★★ | **Adopt.** Already on operator's PATH (it's how persona-A authenticates), already wired in alarm + selfimprove via `internal/ghclient`. Honours `gh auth status` env (`GH_TOKEN`, `GITHUB_TOKEN`, keyring). Zero new dep. JSON output mode is contract-stable (`gh issue list --json` documented stable since 2.0). Spec pins gh CLI ≥ 2.40 (see §10.3 + §11.4 property-test toolchain assertion). |
| go-github (`google/go-github/v66`) | ★★★☆☆ | Track. Adds a 200KB+ dep, requires its own token wiring, duplicates auth surface gh CLI already owns. Adopt only if the gh CLI dep becomes the bottleneck (e.g. webhooks land in §13 F7 and need real-time event parsing). Roadmap §4 row T4 names go-github as the adopted lib; this spec deviates with cited justification: existing `ghclient` is gh-CLI-backed and adding a parallel go-github path violates `feedback_research_design_principles` (one path per concern). |
| Raw HTTP via `net/http` | ★☆☆☆☆ | Reject. Reinvents auth, pagination, rate-limit parsing. Anti-`feedback_research_design_principles`. |

Reinforcing argument (R5 fold): alarm webhook (`internal/alarmwebhook/github.go:133` `httpGitHubClient.CreateIssue`) and selfimprove both wire through `ghclient.Client` which today is gh-CLI-backed. The gh CLI path IS the established repo convention; adopting go-github here would FORK the GitHub-IO surface into two parallel auth/retry/rate-limit stacks. That's a `feedback_research_design_principles` violation in the opposite direction from what the roadmap §4 row T4 implies. The roadmap entry should be amended (F11 §13) — pick gh CLI for consistency with existing two-producer convention.

### 10.2 ghclient interface extension

Current `internal/ghclient/client.go:15` is 3 methods (List/Create/Comment). Adapter needs three additions:

```go
type Client interface {
    // existing
    ListOpenIssuesByLabel(ctx context.Context, label, titleSubstr string) ([]Issue, error)
    CreateIssue(ctx context.Context, title, body string, labels []string) (int, error)
    CommentOnIssue(ctx context.Context, number int, body string) error

    // new in this PR
    ListIssuesByLabelPaginated(ctx context.Context, label string, opts ListIssuesOpts) ([]Issue, error)
    GetIssue(ctx context.Context, number int) (Issue, error)
    EditIssueBody(ctx context.Context, number int, body string) error  // body-marker backfill (§4.2)
}

type Issue struct {
    Number    int       `json:"number"`
    Title     string    `json:"title"`
    Body      string    `json:"body"`
    Labels    []string  `json:"labels"`     // new field — labels carried over for source:* routing
    UpdatedAt time.Time `json:"updatedAt"`  // new field — debug + future cursor
}
```

`Issue` struct extension is backward-compatible (existing callers unaware of new fields). Stub clients in `internal/ghclient/client_test.go:31` need updating (one-line method additions returning empty slices).

### 10.3 Adopt cited

| Component | Version pinned | License |
|---|---|---|
| gh CLI | ≥ 2.40 (json-output stability + `--search "id:<ID>"` flag stability) | MIT |
| Existing `internal/ghclient` | sha at PR base | (in-tree) |

No new third-party Go modules added by this PR.

## 11. Test plan

### 11.1 File layout

```
internal/orchestrator/adapter/github_issues.go
internal/orchestrator/adapter/github_issues_test.go
internal/orchestrator/adapter/github_issues_fuzz_test.go
internal/orchestrator/adapter/testdata/github_issues/
    issue-590-valid.json           # gh issue view output, canonical metadata + acceptance
    issue-591-no-acceptance.json   # missing ## Acceptance criteria H2
    issue-592-dup-id.json          # paired with issue-595-dup-id.json — same ID prefix
    issue-593-bad-title.json       # title fails ID regex
    issue-594-malicious.json       # shell-meta in body
    issue-595-dup-id.json          # collision peer to 592
    issue-list-bulk.json           # 50 issues, golden List output
```

The 6 fixture files cover the load-bearing-by-fixture cases (canonical-shape, empty-acceptance, dup-id-pair, bad-title, shell-meta, bulk-list). The remaining 14 tests below use programmatic mock input (in-test inline `Issue{}` literals) — fixture files would be ceremony for one-line cases. See §11.4 for property/mock seam.

### 11.2 Golden fixtures (recorded gh CLI output, NOT live API)

Tests inject a `ghclient.Client` fake reading from `testdata/`. Same shape as `internal/selfimprove/source_test.go:22`. Golden re-record via `go test -update` (existing convention in markdown_test.go). The fake is a deterministic mock; tests NEVER hit live api.github.com.

### 11.3 Test names (1-line godocs, per `feedback_test_godoc_one_line`)

| Test | Source (fixture/mock) | One-line assertion |
|---|---|---|
| `TestGitHubIssues_List_FiltersToAutonomousLabel` | mock | `List` skips issues missing `autonomous` label (#590). |
| `TestGitHubIssues_List_SkipsClosedIssues` | mock | `List` honours `--state open` and never returns closed issues. |
| `TestGitHubIssues_List_ProjectsTitleIDPrefix` | fixture 590 | `WorkItem.ID` equals title-prefix capture group. |
| `TestGitHubIssues_List_SkipsBadIDPrefix_WarnsAndRecords` | fixture 593 | Bad title-prefix records skip-payload (§7.6) + WARN log. |
| `TestGitHubIssues_List_ExtractsMetadataFields` | fixture 590 | `lane`, `dependencies`, `linked_artifact` parse from HTML-comment YAML. |
| `TestGitHubIssues_List_AcceptanceSectionParsedAsCriteria` | fixture 590 | `## Acceptance criteria` bullets project to `[]Criterion` with `state, id, text`. |
| `TestGitHubIssues_List_MissingAcceptance_EmptyCriteria` | fixture 591 | Missing H2 yields empty `AcceptanceCriteria`, not error. |
| `TestGitHubIssues_List_DupIDPrefix_BothSkipped_CommentsOnBothIssues` | fixtures 592+595 | Two open issues sharing prefix both skipped + WARN + `CommentOnIssue` fires exactly once per issue per (ID, day) tuple. |
| `TestGitHubIssues_List_DedupesViaBodyMarker` | mock | Repeated `List` on same body never re-back-fills the marker. |
| `TestGitHubIssues_List_BodyEditChangesKey_ReprojectsWorkItem` | mock | Body edit between ticks yields fresh dedup key + scheduler sees `ErrSourceMutated` on `Get`. |
| `TestGitHubIssues_List_RateLimitWrapsErrRateLimited` | mock | HTTP 403 + `X-RateLimit-Reset` 60s ahead → wrapped error carries `RetryAfter == 60 * time.Second`. |
| `TestGitHubIssues_List_TombstoneOnClosure` | mock | Issue closed between ticks emits tombstone WARN with `kind:"tombstone"` payload. |
| `TestGitHubIssues_List_NeverLogsBodyBytes` | mock | INFO logs carry only `(issue_number, body_sha256)`, never raw body. |
| `TestGitHubIssues_Get_NotFound_WrapsErrNotFound` | mock | Missing issue surfaces `ErrNotFound`. |
| `TestGitHubIssues_Get_IDCollision_WrapsErrPermanent` | fixtures 592+595 | Two open issues sharing prefix surfaces `ErrPermanent` on `Get`. |
| `TestGitHubIssues_Get_CacheMissAfterMinPoll_Refetches` | mock | Cache hit within `MinPoll` avoids gh call; expiry triggers exactly one refetch; failure → `ErrNotFound`. |
| `TestGitHubIssues_Skip_RecordsObservation` | fixture 593 | Skip WARN payload matches `{adapter, repo, issue_number int, reason, issue_url}` schema verbatim. |
| `TestGitHubIssues_UpdateStatus_AlwaysErrAdapterUnsupported` | mock | `UpdateStatus` returns `ErrAdapterUnsupported` for every input. |
| `TestGitHubIssues_Capabilities_NoWebhookNoBulk_MinPoll30s` | mock | Capabilities zero for Webhook + BulkUpdate, MinPoll = 30s. |
| `FuzzGitHubIssues_ParseMetadata_NoPanic` | mock | Random HTML-comment-bytes never panic the metadata parser. |
| `FuzzGitHubIssues_ParseAcceptance_NoPanic` | mock | Random body bytes never panic acceptance extraction. |
| `TestGitHubIssues_PropertyTest_ProjectionDeterministic` | mock (deterministic seeded RNG) | Same input → same WorkItem byte-for-byte across N=100 runs against the in-process `ghclient` fake (NOT live API). |

**Mock-vs-fixture split (R7 fold)**: 6 fixtures cover the canonical-shape + edge-case bodies that need golden re-record on schema bumps; 14 mock-driven tests use in-test `Issue{}` literals because the input is one-line and the assertion is on adapter logic, not body parsing. Property test §11.4 runs against the same `ghclient.Client` fake.

### 11.4 Property test

`TestGitHubIssues_PropertyTest_ProjectionDeterministic` runs 100 random body-byte inputs through projection twice and asserts byte-equality of the resulting `WorkItem` JSON. Property invariant: deterministic projection (§6). Seam: the `ghclient.Client` fake from §11.2; the test NEVER hits live api.github.com. Toolchain assertion: test setup calls `exec.LookPath("gh")` and asserts `--version` stdout matches `gh version 2.4` or higher; on older gh the test skips with a clear message rather than producing flaky results.

## 12. Adversarial-review findings

This section captures the inline self-review + the fresh-slot reviewer round-trip status. Fresh-slot reviewer findings (CRITICAL + HIGH) MUST round-trip into §1–§11 inline before merge per `feedback_reviewer_dispatch_enforcement`.

### 12.1 Pre-emptively addressed in initial draft

The author ran a single-pass adversarial sweep before the initial draft; the following gaps were closed inline.

| Finding | Severity (author self-rate) | Address location |
|---|---|---|
| Operator-filed issue w/o `autonomous` label silently consumed | CRITICAL | §2.3 reject-pattern row 3 + §2.1 single-discriminator argument |
| Body-edit-mid-flight policy undefined | HIGH | §4.3 re-queue-not-fail-closed decision + L0 SHA invariant carries load |
| LLM-inference creeping into projection | HIGH | §6.2 rejected fields + §3.3 ambiguity rejection |
| Token scope creep | MED | §8.1 read+write scope reasoned + load-bearing for §4.2 body backfill |
| Shell injection via title/body | CRITICAL | §8.2 three-surface defense table + F9 cross-cut + conditional-safety statement |
| Selfimprove uses `self-improvement` not `autonomous` — adapter would miss its issues | HIGH | §2.2 producer table + F4 tracking issue |
| Two `## Acceptance criteria` sections silently picks one | MED | §3.3 reject ambiguous |
| Closed-mid-work auto-closes in-flight PR | MED | §7.1 no-auto-close decision |
| Rate-limit wedges scheduler | MED | §7.4 + §1.1 `MinPoll` default 30s budget math + per-adapter backoff |
| OSS-adoption-claim mismatch (roadmap names go-github; spec picks gh CLI) | LOW | §10.1 deviation cited; roadmap §4 row T4 amendment via F11 §13 |
| In-memory dedup vs body marker | MED | §4.4 three-reason body-marker-load-bearing |
| Issue body PII bleed via logs | HIGH | §8.3 log-discipline + dedicated test |
| Concurrent adapter run | LOW | §9.3 forbidden + F10 reopen |
| Empty acceptance criteria not an error | LOW | §2.3 soft-fail row + L0 carries load |

### 12.2 Fresh-slot reviewer dispatch checklist

When dispatching the reviewer:

- Path: this file.
- Prompt include: CLAUDE.md decision-priority + `feedback_research_design_principles` + the 9 design questions verbatim.
- Reviewer output expected: severity-tagged findings folded into §12.3.
- Critical / High findings MUST round-trip into §1–§11 inline before merge.

### 12.3 Fresh-slot reviewer findings — fold-status table

Reviewer dispatched 2026-06-04. Verdict: AUTHOR-REVISION-NEEDED. Findings table below; each row maps to either an inline § that addressed it OR an F-followup that defers it with rationale.

| # | Severity | Finding | Resolution | Status |
|---|---|---|---|---|
| C1 | CRITICAL | Substrate seam mismatch: spec §1.1 defined `SubstrateRecorder.Record(ctx, kind, payload)` but the real seam at `internal/orchestrator/state/substrate/event.go:137` is `AppendEvent(ctx, tx, e Event, key, keyID)` requiring 15-field signed Event with RunID/TenantID/HMAC key. Adapter has no source for those. | Substrate writes removed from this PR entirely. Dedup oracle is GH body marker (§4.1) which has no substrate dep. Audit-trail substrate writes deferred to F20 §13 (RecordObservation wrapper). | **ADDRESSED §1.1 + §4 + F20** |
| H1 | HIGH | Dedup oracle contradiction §4.2 (substrate) vs §4.5 (body marker) — both can't be source of truth. | Single oracle pick: GH body marker. §4.1–§4.2 rewritten; prior dual-oracle §4.5 deleted. | **ADDRESSED §4.1, §4.2, §4.5-DELETED** |
| H2 | HIGH | Heading drift §3.4 unresolved — spec picked `## Acceptance` but markdown_catalog parser reads `## Acceptance criteria`; silent empty-criteria on adapter switch. | Canonical heading pinned to `## Acceptance criteria` (markdown_catalog-aligned). CUE default amended in companion patch. F18 (dual-heading cleanup) rescinded. | **ADDRESSED §3.4** |
| M1 | MED | Metadata syntax churn §3.5 (HTML-comment) vs §3.1 (fenced block) — no precedence rule. | Single pick: HTML-comment YAML. Fenced-block alternative deleted from §3.1 + §3.5. | **ADDRESSED §3.1 + §3.5** |
| M2 | MED | Rate-limit handling §7.4 undefined — `RetryAfter` format unspecified (unix vs ISO); no deterministic test pattern. | `RetryAfter time.Duration` pick + scheduler per-adapter-backoff behaviour + deterministic test pattern with fake clock. | **ADDRESSED §7.4** |
| M3 | MED | Golden fixtures incomplete §11.1–§11.3: 20 test names vs 6 fixture files mismatch; §11.4 property test mock-or-live ambiguity. | 6-fixture list explicit; 14 mock-driven tests called out in §11.3 source column. Property test §11.4 mock-only with `gh ≥ 2.40` toolchain assertion. | **ADDRESSED §11.1 + §11.3 + §11.4** |
| M4 | MED | Shell-injection defense §8.2 incomplete — adapter safe, but downstream (prompt-builder F9) can re-inject. | Conditional safety statement added: adapter safety is necessary-but-not-sufficient; both gates required. F9 cross-cut + PR #834 cited. | **ADDRESSED §8.2** |
| L1 | LOW | Collision feedback §7.8 + §11.3 untested — spec says comment on both issues, no test name. | Test `TestGitHubIssues_List_DupIDPrefix_BothSkipped_CommentsOnBothIssues` added §11.3; assertion includes once-per-(ID, day) dedup. | **ADDRESSED §7.8 + §11.3** |
| L2 | LOW | Skip-event payload §7.6 undefined — no schema. | Schema pinned §7.6: `{adapter, repo, issue_number int, reason, issue_url}`. `reason` is a closed enum. Test `TestGitHubIssues_Skip_RecordsObservation` asserts verbatim. | **ADDRESSED §7.6 + §11.3** |
| L3 | LOW | Cache-miss behavior §1.2 + §6.1 incomplete — `Get` refetch path unspecified. | §7.7 added: gh search-by-id refetch, TTL reset, `ErrNotFound` on miss. Test `TestGitHubIssues_Get_CacheMissAfterMinPoll_Refetches` added §11.3. | **ADDRESSED §7.7 + §11.3** |

**Round-trip summary**: 10/10 fresh-slot findings addressed inline (CRITICAL: 1/1; HIGH: 2/2; MED: 4/4; LOW: 3/3). Two items deferred to F-followups (F20 substrate-write wiring; F21 markdown_catalog→github_issues migration runbook); both are non-load-bearing for the adapter itself (substrate is audit-only per §4.6; runbook is operator docs).

### 12.4 Critical/High round-trip status

- C1 (CRITICAL) — addressed. Substrate dep removed; F20 tracks wrapper PR for audit-trail-only emission.
- H1 (HIGH) — addressed. Single dedup oracle (body marker §4.1); no contradiction.
- H2 (HIGH) — addressed. `## Acceptance criteria` is the single canonical heading.

## 13. A+ rubric self-score

Per `feedback_grade_rubric`. Solo author ships at B by default; reviewer pulls toward A. This is the **revision-pass self-score** — honest about what changed from the initial draft.

| Criterion | B (table-stakes) | A (production-ready) | A+ (load-bearing teaching surface) | Self-rate (post-revision) |
|---|---|---|---|---|
| Spec answers all 9 design questions | All 9 answered | Each cites source file path:line | Each names the trade-off pair considered | **A** — `internal/orchestrator/adapter/markdown.go`, `contracts/schemas/spec_adapter.go:22`, `internal/alarmwebhook/dedup.go:15,20`, `internal/orchestrator/state/substrate/event.go:137`, `internal/selfimprove/detector.go:131-143` cited; trade-off pairs in §4.3, §6.2, §10.1 |
| Deterministic projection | Stated | Defended against LLM creep | Property test | **A+** — §6.2 + §11.4 `TestGitHubIssues_PropertyTest_ProjectionDeterministic` with mock seam + toolchain assertion |
| Failure modes covered | Listed | Each has observable signal | Operator-console pane wired | **A** — §7 + skip-payload + tombstone WARN; substrate emission deferred (F20) but signal still operator-visible via logs + GH comments |
| Dedup scheme | Defined | Policy-defended | Mid-flight invariant proven | **A** — §4.1 single-oracle (body marker) + §4.3 re-queue policy + L0 carries SHA invariant; formal proof N/A |
| Trust boundary | Token scopes named | Sanitization tested | Cross-cut to dispatch prompt | **A+** — §8.1 scope-load-bearing + §8.2 three-surface defense + conditional-safety statement + F9 cross-cut + `TestGitHubIssues_List_NeverLogsBodyBytes` |
| OSS adoption claim | Named | Versioned | Deviation from roadmap cited | **A+** — §10.1 + spec-vs-roadmap deviation cited + F11 §13 + gh ≥ 2.40 pin |
| Test plan | Test names | Golden fixtures | Fuzz + property | **A+** — §11.1 6-fixtures + §11.3 fuzz + §11.4 property + mock-vs-fixture split documented |
| Migration story | Path described | Both adapters live | Forbidden combo named + runbook | **A** — §9 + §9.3 multi-source rejection + F10 reopen + F21 runbook tracked |
| Adversarial sweep | Self-review | Independent reviewer | Reviewer prompt pinned + folded | **A+** — §12.1 self-sweep + §12.3 fresh-slot reviewer fold-status table (10/10 findings addressed inline) |
| Comment discipline (`feedback_comments_lint_reconcile`) | WHY-not-WHAT prose | Lint-clean exported godocs | Test godocs 1-line | **A** — §11.3 test-godoc table follows 1-line rule; impl godocs to be drafted by implementer |
| Self-host filter | Persona-A pass | Internal-operator-direct payoff named | Phase-X surface explicit | **A** — persona-A self-host operator filing issues in their own repo is the canonical use; no Phase-X surface (no multi-tenant, no SaaS) |

**Claimed tier: A** (with five A+ criteria — deterministic projection + trust boundary + OSS adoption + test plan + adversarial sweep). The two pulls-down: implementer godocs not yet drafted (deferred to impl PR); formal proof of SHA invariant not attempted. The revision pass closed all 10 fresh-slot findings inline, lifting adversarial-sweep from A → A+; trust-boundary from A → A+ via conditional-safety statement; test-plan A+ retained with explicit mock seam.

## §14. Followups (tracking issues to file pre-merge, per `feedback_unaddressed_load_bearing`)

Every load-bearing leftover gets a tracking issue filed at PR merge. Universal rule — no PR-type exempt.

- **F1 — `UpdateStatus` for github_issues.** Currently `ErrAdapterUnsupported`. Adopt-pattern: append `Status: <status>` HTML comment marker to issue body, dedup-key tolerant. Tracking issue: "github_issues adapter: implement UpdateStatus via body-marker append".
- **F2 — `kind: program` opt-in via metadata-block field.** Today every issue projects to `KindFeature`. Trigger: operator files an issue too large for a single PR. Tracking issue: "github_issues adapter: support kind:program via metadata-block override".
- **F3 — Cursor pagination for >1000 open issues.** Trigger: any operator hits the `ErrPermanent` 1000-cap. Tracking issue: "github_issues adapter: switch to cursor pagination above 1000 issues".
- **F4 — Selfimprove dual-label migration.** Detector files `self-improvement` today; needs `autonomous` + `source:self-improve` so the adapter consumes. Tracking issue: "selfimprove detector: dual-label with autonomous + source:self-improve for github_issues adapter consumption".
- **F5 — Alarm webhook `source:alarm` label.** Already files `autonomous`; needs `source:alarm` for L4 reviewer routing. Tracking issue: "alarmwebhook: add source:alarm label alongside obs-alert".
- **F6 — Operator-console pane for skip / tombstone signals.** Depends on F20 (substrate writes). Reopen on `operator-console v5.1` S3 dispatch. Tracking issue: "operator-console: render skip + tombstone signals under adapter-health pane".
- **F7 — GH webhook receiver for issue events.** Capabilities.Webhook stays false until reopen. Trigger: a persona-A operator reports >5min staleness on issue-create-to-dispatch round trip. Tracking issue: "github_issues adapter: webhook receiver for sub-30s latency".
- **F8 — Wizard scope ask refinement.** Init wizard now asks for `Issues: Write` (§8.1) load-bearing for body-marker backfill. F8 covers the future expansion to `repo:write` when `UpdateStatus` lands (F1). Tracking issue: "init wizard: expand GH_TOKEN scope ask when UpdateStatus lands".
- **F9 — Prompt-builder sanitization invariant (cross-cut to PR #834).** L4 reviewer prompt must add a lens: "if any field passed to the implementer prompt originated from a GH issue body, assert it was JSON-encoded not string-interpolated." Tracking issue: "L4 reviewer: add untrusted-input-sanitization lens for spec_adapter-sourced fields".
- **F10 — Multi-source aggregation (markdown_catalog + github_issues simultaneously).** Reopen on first paying customer with split work-item authority. Tracking issue: "spec_adapter: support multi-source aggregation".
- **F11 — Roadmap §4 row T4 amendment.** Roadmap names go-github as the adopt; this spec picks gh CLI subprocess. File doc-PR. Tracking issue: "roadmap §4 MVR-1-T4: amend Adopt column from go-github to gh CLI subprocess (cite spec §10.1 deviation)".
- **F12 — `closed-resolved` status surface.** GH closed issues today vanish from `List`; some operators may want them surfaced for human read-only inspection. Tracking issue: "github_issues adapter: optional --include-closed-resolved for read-only audit".
- **F13 — RESCINDED.** Prior-draft "locate substrate seam" — resolved: seam located at `internal/orchestrator/state/substrate/event.go:137`; mismatch with adapter context is the reason §4.6 defers substrate writes entirely. No standalone followup needed.
- **F14 — RESCINDED.** Prior-draft "port selfimprove body-marker convention" — folded into §4.1 as the single dedup oracle. No standalone followup needed.
- **F15 — RESCINDED.** Prior-draft "ratify metadata syntax pick at impl time" — §3.5 makes the definitive pick (HTML-comment YAML); no impl-time deferral remains.
- **F16 — Scheduler tolerance for `ErrAdapterUnsupported` from UpdateStatus.** Integration test pre-merge. Tracking issue: "scheduler: verify ErrAdapterUnsupported on UpdateStatus does not block github_issues lifecycle".
- **F17 — RESCINDED.** Prior-draft "collision feedback followup" — addressed inline §7.8 + test name in §11.3.
- **F18 — RESCINDED.** Prior-draft "markdown_catalog dual-heading" — markdown_catalog parser does not need to change; CUE default is amended in this PR's companion patch (§3.4).
- **F19 — Scheduler MinPollInterval honour audit.** Verify scheduler respects per-adapter `Capabilities().MinPollInterval`. Tracking issue: "scheduler: audit MinPollInterval honour per spec_adapter (markdown_catalog 5s vs github_issues 30s)".
- **F20 — NEW — Substrate-write wiring via RecordObservation wrapper.** Adapter ships substrate-free (§4.6); this followup adds a `RecordObservation(kind, payload string)` wrapper around `AppendEvent` (`internal/orchestrator/state/substrate/event.go:137`) that synthesizes `RunID + TenantID + HMAC key` from the adapter's spawn context (or accepts an explicit signing-key seam). Once landed, the adapter's WARN-only skip / tombstone / observed signals emit substrate events of `kind ∈ {spec_adapter_observed, spec_adapter_skip, spec_adapter_tombstone}` with the §7.6 payload schema. Unblocks F6. Tracking issue: "substrate: add RecordObservation wrapper for spec_adapter audit-trail kinds (unblocks github_issues skip/tombstone observability)".
- **F21 — NEW — markdown_catalog → github_issues migration runbook.** Standalone operator doc walking through harvest of `.regatta/items/*.md` files into `gh issue create` calls with canonical metadata block + acceptance heading; verifies body-marker backfill on first List tick; documents rollback. Tracking issue: "docs/runbook: markdown_catalog → github_issues adapter migration runbook".

## §15. Comment sweep

Per `feedback_comments_discipline` + `feedback_comments_lint_reconcile`. This section is the implementer's pre-push checklist:

- Drop any `// what:` narration; default no comment.
- Every exported `func`/`type` godoc opens with the symbol name AND captures WHY in 1 sentence.
- Every `Test*`/`Fuzz*` godoc is exactly one line.
- Drop ceremony banners ("---- region ----", per-commit linting noise, mid-stream CHANGELOG bumps).

## §16. Cites

- Roadmap §4 MVR-1-T4: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md:143` (table row).
- CUE schema: `contracts/schemas/regatta.v1.cue:63-69` (`#SpecAdapter` block).
- Go interface: `contracts/schemas/spec_adapter.go:22-37` (`SpecAdapter`).
- Go work item: `contracts/schemas/spec_adapter.go:39-50` (`WorkItem`).
- Go source ref: `contracts/schemas/spec_adapter.go:106-110` (`SourceRef`).
- Markdown adapter reference impl: `internal/orchestrator/adapter/markdown.go` (full file).
- Self-improve producer (body-marker dedup convention): `internal/selfimprove/detector.go:131-143` (`extractDedupKey`) + `internal/selfimprove/rule.go:29` (`LabelSelfImprovement`) + `internal/selfimprove/detector.go:145` (`renderIssue`).
- Alarm-webhook producer: `internal/alarmwebhook/handler.go:340-343` (issue creation) + `internal/alarmwebhook/dedup.go:15,20` (label constants).
- GH client seam: `internal/ghclient/client.go:15-29` (`Client` + `Issue`).
- Substrate write seam (real surface; basis for §1.1 + §4.6 defer decision): `internal/orchestrator/state/substrate/event.go:137` (`AppendEvent(ctx, tx, e Event, key, keyID)`).
- Config wiring: `internal/config/validate/load.go:75,108-142` (`SpecAdapter` typed view).
- Init wizard (T2): `docs/engineer/specs/2026-06-02-mvr-1-t2-regatta-init-bundle.md`.
- SCM adapter (T5): `docs/engineer/specs/2026-06-02-mvr-1-t3-p38-scm-adapter-gitea-first.md`.
- Self-host filter: `docs/engineer/briefs/2026-06-01-self-host-first.md`.
- Decision priority, deletion default, dispatch brief only, unaddressed load-bearing: `CLAUDE.md`.
- Worker prompt enrichment (F9 cross-cut downstream sanitization owner): PR #834.
