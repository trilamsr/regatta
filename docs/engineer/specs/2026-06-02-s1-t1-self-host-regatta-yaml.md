# S1-T1 — `regatta.yaml` for this repo + `.regatta/items/` bootstrap

_Phase-S1 dogfood-ready core, brief `docs/engineer/briefs/2026-06-01-self-host-first.md` §3._

Memory rule cites: `feedback_research_design_principles`, `feedback_decision_priority`, `feedback_grade_rubric`, `feedback_self_improvement`.

## 1. Problem

regatta cannot self-dispatch today because:

1. There is no `regatta.yaml` at the repo root — `regatta serve` boots only via the `--items-root` CLI flag, so an operator who just runs `regatta serve` against this repo gets an empty catalog.
2. There is no `.regatta/items/` directory — even with `--items-root=.` the adapter lists zero items because the bootstrap content is missing.
3. The CUE schema's `spec_adapter` block for `type: markdown_catalog` declares a `path` field (line `regatta.v1.cue:51`) that no Go consumer reads. The runtime `MarkdownCatalogConfig.Root` is wired only from the `--items-root` flag (`cmd/regatta/serve.go:115`, `:160`). Schema-to-runtime drift.

Fix all three at once so a fresh `git clone && go run ./cmd/regatta serve` against this repo lists the seed work items without any extra flags.

## 2. Scope (in)

1. Add a `spec_adapter.root` field to the CUE schema for `type: markdown_catalog` — the directory under which the adapter resolves `.regatta/items/*.md`. Defaults to `.`. Remove the no-op `path` field (one test consumer, no runtime); no backwards-compatibility tax during self-host phase.
2. Plumb the field through `internal/config/validate.Config` so `cmd/regatta/serve.go::runServe` can read it and feed `adapter.NewMarkdownCatalog`. The `--items-root` flag stays — it becomes a per-invocation override of `spec_adapter.root`. Resolution priority: explicit flag > yaml field > flag default (`.`).
3. Write a self-host `regatta.yaml` at the repo root: markdown adapter pointing at `.regatta/items/`, `ci: make check`, two-gate minimum (one deterministic doc-check, one approval-gate stub), modest safety caps for a single-operator OSS repo.
4. Seed `.regatta/items/` with 2–3 hand-crafted work items pointing at trivial open `[followup]` GH issues (#182, #195, #198).

## 3. Scope (out)

- The boot-prompt → work_item converter (that's S1-T3, deliberately separate).
- The GH-issue adapter (`type: github_issues`) — markdown adapter only for S1.
- Approval-gate notifier production wiring (stub-notifier suffices for the seed).
- Migration tooling for the schema rename — there are no production consumers in the wild because we are pre-v1 self-host phase.
- Adding multi-tenant fields (single-operator, single-tenant per brief §1).

## 4. Schema delta

`contracts/schemas/regatta.v1.cue` lines 50–53 today:

```cue
if type == "markdown_catalog" {
    path:    string                          // path relative to repo root
    format:  *"github_checkbox" | "rubric"   // - [ ] / - [x]  vs.  ☐/⧗/☑
}
```

New shape:

```cue
if type == "markdown_catalog" {
    // Directory containing .regatta/items/*.md. Relative to repo root.
    // Defaults to "." — i.e. items live at <repo>/.regatta/items/.
    root:    *"." | string
    format:  *"github_checkbox" | "rubric"
}
```

Rationale:

- `root` is the field name the Go runtime already uses (`MarkdownCatalogConfig.Root`); naming the YAML field identically removes one layer of translation tax.
- Default `"."` keeps the minimal-yaml ergonomics — operators get the spec adapter working with one declarative `type: markdown_catalog` line.
- `path` had no runtime consumer; the load_test that referenced it is updated to use `root` instead.
- `format` left alone; it is read by the parser (already implemented).

## 5. Runtime wiring delta

Two edits:

1. `internal/config/validate/load.go::Config` — surface a typed `SpecAdapter` block exposing `Type` + `Root` (markdown-specific). Mirrors the existing `Prompts` accessor pattern.
2. `cmd/regatta/serve.go::runServe` — load `regatta.yaml` once (already loaded for approval gates) and use its `spec_adapter.root` as the resolved items-root unless the operator passed `--items-root` explicitly. Detection: `flag.Visit` on the `items-root` flag.

`--items-root` flag default remains `"."` so the CLI behaviour for repos without a `regatta.yaml` is unchanged.

## 6. Seed content

Repo-root `regatta.yaml`:

```yaml
version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
  default_branch: main
spec_adapter:
  type: markdown_catalog
  root: .
ci:
  command: make check
gates:
  - id: doc_check
    type: deterministic
    command: bash scripts/doc-check.sh
  - id: human_merge
    type: approval_gate
    name: human-merge
    risk_class: low
    reviewers: [trilamsr]
    quorum: 1
    timeout: 24h
    decision_window: 12h
    on_timeout: fail
safety:
  destructive_ops_deny: ['rm -rf /', 'git push --force']
  agent_creds_scope: dev_only
  iteration_cap: 50
  spend_cap_usd: 50
  spend_cap_usd_per_day: 200
  canary_rate: 0.05
```

`.regatta/items/` seed (three items, each referencing one open `[followup]` issue):

- `S1-SEED-001.md` — references #182 (approval-gates end-to-end test + property fold≡status).
- `S1-SEED-002.md` — references #195 (minted token JTIs never land in approval_events; reaper revocation is dead code).
- `S1-SEED-003.md` — references #198 (tighten Notifier interface to enforce contract invariants).

Each seed item has frontmatter (`id`, `title`, `lane`, `status: planned`, `linked_artifact: https://github.com/trilamsr/regatta/issues/N`) and one acceptance criterion. The body free-text cites the issue and the trigger condition that promotes it from "seed" to "dispatch this now."

## 7. Tests (TDD-first)

1. **Failing test in `internal/orchestrator/adapter/markdown_test.go`** — `TestMarkdownCatalog_RootFromYAMLDefault` is N/A here (adapter does not read yaml). Move this test to `internal/config/validate/load_test.go` instead: `TestLoad_MarkdownCatalog_RootField_Valid` (accepts the new `root` field) + `TestLoad_MarkdownCatalog_PathField_Errors` (rejects the dead `path` field).
2. **Failing test in `cmd/regatta/serve_yaml_root_test.go`** — `TestServe_YAMLRoot_DrivesItemsRoot` writes a fixture `regatta.yaml` with `spec_adapter.root: <tmp>` and `.regatta/items/*.md` under it, then runs `serve -tick-once` WITHOUT `--items-root` and asserts items are listed (proxy: state DB rows in `work_items` after one tick). This is the load-bearing integration test for the whole feature.
3. **Failing CLI smoke** — extend `TestCLI_Serve_TickOnceStub` or add `TestCLI_Serve_TickOnceStub_NoItemsRootFlag` that drops the `-items-root` arg and relies on the seed `regatta.yaml`.
4. **Repo-root `regatta.yaml` validates** — `TestSelfHost_RegattaYAML_Validates` reads the actual `regatta.yaml` written in §6 and runs `validate.LoadBytes` on it; this anchors the seed against the schema across future schema edits.
5. **Seed items parse** — `TestSelfHost_SeedItems_Parse` reads every file under `.regatta/items/` and asserts `ParseMarkdownItem` returns nil error. Anchors the seeds against the adapter's parser across future parser edits.

Capture failing output for each before implementing.

## 8. Failure modes

- **YAML missing → no behavior change.** `regatta serve` falls back to the `--items-root` flag default `.`; if no `.regatta/items/` exists, the adapter returns an empty list, the orchestrator loops, no error. Matches today's contract.
- **YAML present but `spec_adapter.type != markdown_catalog`** — out of scope for S1; serve.go logs a warning and continues with the flag-default. Adapter selection logic for non-markdown types lands when those adapters do (post-MVP-2 per the brief's Phase X list).
- **Operator passes both `--items-root` and a yaml `spec_adapter.root`** — explicit flag wins. Documented in the flag help text.
- **Seed item references a closed issue** — adapter ignores GH state; the item stays "planned" in the catalog. Not a failure mode per se; a stale-seed cleanup task lands as a separate follow-up if it matters.

## 9. Adversarial reviewer pass

After the design subagent finished the §1-§8 above, an adversarial reviewer was spawned with the spec doc + the brief + the relevant source files (`serve.go`, `markdown.go`, `regatta.v1.cue`). The reviewer raised three points:

1. **R1 — "Why not keep `path` for back-compat?"** The CUE field `path` has zero runtime readers. Keeping a dead field tax operators with confusion + lint exceptions. Self-host phase has no production deployments to break. Decision: drop `path`. (Decision-priority: ease > best-practices when "best practice" means deprecation overhead for zero benefit.)
2. **R2 — "Acceptance criterion for `TestServe_YAMLRoot_DrivesItemsRoot` proxies through the DB; brittle if the orchestrator polls async."** Use `-tick-once` so the test is synchronous (proven in `TestCLI_Serve_TickOnceStub`). Reviewer cleared.
3. **R3 — "Seed items should not encode the `linked_artifact` field as a GH URL — schema treats it as a repo-relative path."** Verified: `WorkItem.LinkedArtifact` is a free string today, no schema constraint. A URL is acceptable. But the L0 verifier currently runs against on-disk artifacts — a URL won't resolve. Decision: file the L0-URL-handling delta as a follow-up issue if/when L0 trips on the seed items; for S1 the seeds are catalog entries only, no L0 run scheduled against them. Reviewer cleared with one tracking note: open issue if L0 fires on a URL-linked item.

All three closed in this revision.

## 10. Grade rubric (B / A / A+)

Per `feedback_grade_rubric` — every PR posts this scorecard verbatim in the body with PASS/FAIL/N-A + evidence.

### B (must-have, baseline)

- B1. `regatta.yaml` at repo root validates against `contracts/schemas/regatta.v1.cue` via `regatta validate-config`.
- B2. `.regatta/items/` contains ≥2 hand-crafted seed items each referencing an open `[followup]` GH issue.
- B3. `MarkdownCatalog` resolves seed items end-to-end via `regatta serve --tick-once` against the repo root without the `--items-root` flag (i.e., yaml drives it).
- B4. Failing test captured + included in PR body BEFORE implementation diff. TDD-strict.
- B5. `make pre-push-check` clean (CUE vet, golangci-lint, go test ./...).
- B6. No banned phrases (`bash scripts/doc-check.sh` clean).
- B7. No AI signatures / Co-Authored-By footers.

### A (good-quality, expected)

- A1. CUE schema delta (rename `path` → `root`) lands in same PR; old test updated to reference the new field.
- A2. `cmd/regatta/serve.go` flag-resolution priority documented in flag help text: `--items-root` explicit > yaml `spec_adapter.root` > default `.`.
- A3. Two integration tests pin the new behaviour: one Go-level (yaml field round-trip), one CLI-smoke (serve --tick-once without flag).
- A4. Seed items use the same frontmatter format the parser already accepts (`id`, `title`, `lane`, `status`, `dependencies`, `linked_artifact`); seed-parse test (`TestSelfHost_SeedItems_Parse`) anchors them.
- A5. Spec doc cites the four memory rules called out in the dispatch prompt (`feedback_research_design_principles`, `feedback_decision_priority`, `feedback_grade_rubric`, `feedback_self_improvement`).
- A6. PR body includes the captured failing-test output verbatim under a "TDD evidence" section.

### A+ (best-of-class)

- A+1. Adversarial reviewer subagent finds ≥3 nontrivial issues; spec §9 captures the findings + how they were addressed in this revision.
- A+2. The seed `regatta.yaml` is itself the integration fixture for `TestSelfHost_RegattaYAML_Validates` — single source of truth, no drift between "example yaml" and "self-host yaml."
- A+3. Schema rename closes a real gap (no runtime consumer for `path`) rather than just adding a parallel field; net file count delta is negative or zero against the pre-PR baseline (deletion default per `feedback_deletion_default`).
- A+4. PR description includes a one-paragraph "what got smaller" answer per `feedback_deletion_default`.
- A+5. Followup-issue references in seed items include the trigger condition that promotes "seed only" → "dispatch now" (so a future operator can decide programmatically when to flip a seed's status from `planned` to live).
- A+6. CUE delta uses CUE's native default (`*"."`) so an operator who declares `type: markdown_catalog` with no other field gets a working adapter.

## 11. Implementation order

1. Worktree exists (`../regatta-s1-t1`).
2. Write spec (this doc).
3. Spawn adversarial reviewer; capture findings in §9.
4. Write failing tests (§7 #1, #2, #3, #4, #5); run; capture failure output for PR body.
5. CUE schema edit (`contracts/schemas/regatta.v1.cue`).
6. Update `internal/config/validate/load.go` (typed `SpecAdapter` accessor).
7. Update `internal/config/validate/load_test.go` (rename `path` → `root` in the existing test).
8. Update `cmd/regatta/serve.go::runServe` to read yaml.spec_adapter.root when `--items-root` not set.
9. Write `regatta.yaml` at repo root.
10. Write `.regatta/items/S1-SEED-001.md`, `…002.md`, `…003.md`.
11. Run `make check`, `bash scripts/doc-check.sh`, `make pre-push-check`.
12. Open PR with scorecard + release-notes fence + TDD evidence. Enable automerge.

## 12. Out-of-band risk

- The repo-root `regatta.yaml` declares `gates.human-merge` as an `approval_gate` with `reviewers: [trilamsr]`. If the orchestrator runs unattended against an item that triggers the gate, it will block waiting for `trilamsr` to approve via `regatta approval decide`. That is the intended self-host behavior (human-merge enforced through the gate). Operators who want pure-automation can drop the gate. Not a defect.
- Switching the schema field from `path` to `root` is a breaking change for any consumer outside the repo. Brief §1 establishes there are none. If discovery proves otherwise, file a v2-migration follow-up rather than reverting.
