---
title: "internal/orchestrator/spawner/claude.go::defaultPromptBuilder section split (cascade-rebase anchor reduction)"
status: draft
summary: "defaultPromptBuilder is THE most-touched cascade source per 2026-06-04 session retro (#834, #851, #856, #966, #1000). Every prompt-rule tweak collides on the same monolithic strings.Builder body. Split into composable sections under internal/orchestrator/prompt/sections/*.go — each section is a pure `func(Request) string` returning a Markdown chunk; defaultPromptBuilder becomes a thin composition root that calls them in a pinned order. Acceptance: rendered prompt byte-equal pre/post under existing golden tests + a new fixed-order test that pins section composition."
---

# defaultPromptBuilder section split — Design Spec

Date: 2026-06-08
Trigger source: `feedback_cascade_rebase_root_cause` — session retro 2026-06-04 names `defaultPromptBuilder` as THE most-touched cascade source (#834, #851, #856, #966, #1000). Every prompt-rule tweak collides on the same `strings.Builder` body.
Prior art: `cmd/regatta/serve.go` per-subsystem split (#737, #744, `docs/engineer/specs/phase-x/2026-06-08-serve-go-subsystem-split.md`) — same pattern, different composition root.
Memory rules in force: `feedback_default_simpler`, `feedback_deletion_default`, `feedback_root_cause`, `feedback_cascade_rebase_root_cause`, `feedback_adversarial_review`, `feedback_audit_main_before_implementing`, `feedback_spec_pattern_authority`, `feedback_no_signatures`.

```release-notes
[DOCS] Design spec for splitting internal/orchestrator/spawner/claude.go::defaultPromptBuilder
into composable per-rule sections under internal/orchestrator/prompt/sections/.
defaultPromptBuilder becomes a thin composition root that assembles the sections
in a pinned order. Acceptance: rendered prompt byte-equal pre/post under existing
golden tests + a new fixed-order test. No code in this PR.
```

## §0 Closing trigger

Done when ALL of:

1. Impl PR(s) (separate from this spec) merge referencing this spec.
2. `internal/orchestrator/spawner/claude.go::defaultPromptBuilder` body is composition-only: section calls in a pinned order, ItemBody sentinel guard, optional bundled CLAUDE.md prefix, optional enrichment suffix. No inline `b.WriteString("- TDD: ...")` rule literals.
3. Each section file `internal/orchestrator/prompt/sections/<name>.go` exports ONE `func(Request) string` and stays under 80 LOC.
4. Existing golden tests in `internal/orchestrator/spawner/claude_test.go` (the seven `defaultPromptBuilder(Request{...})` assertions at lines 187, 198, 237, 267, 298, 319, 337, 347, 357, 365, 379, 430, 444) pass without modification.
5. New `TestDefaultPromptBuilder_SectionOrder` pins the section sequence by asserting substring index ordering for one anchor per section (the order test is the drift gate per `feedback_byte_equal_refactor_pin`).
6. `scripts/check-prompt-parity.sh` continues to pass (every `feedback_*` slug in `implementer.md::Anchored rules (worker-prompt parity)` is still cited in the assembled prompt).
7. `make ci-check` green on impl PR.

## §1 Problem

`internal/orchestrator/spawner/claude.go::defaultPromptBuilder` (lines 228-294, 66 LOC) is a single `strings.Builder` that linearly appends six logical sections:

- **bundled-CLAUDE.md prefix** (lines 231-235) — emitted only when `prompt.ResolveClaudeMd` returns `SourceBundled`.
- **item-body fence** (lines 236-251) — work-item header + sentinel-fenced ItemBody with collision rejection.
- **comments-budget** (lines 252-259) — "## COMMENTS: zero by default" block (7 rule bullets).
- **discipline anchors** (lines 260-265) — 5-bullet CLAUDE.md slug cite list (TDD, comments, deletion, PR hygiene, scorecard).
- **scorecard citation gate** (lines 266-278) — 13 lines of token-shape rules + release-notes fence requirement + auto-skip carve-out.
- **PR shape contract** (lines 279-282) — title format + body sections + reviewer-skip predicate.
- **adaptive enrichment** (lines 283-291) — L2 enrichment plumbed through `prompt.Enrich`, gated by env kill-switch.
- **terminator** (line 292) — `Begin now. Do not summarize the brief back.`

Five of the last 20 PRs touched the file specifically to tweak ONE of these blocks:

- #834 — comments-budget block (added the test-godoc one-liner rule).
- #851 — scorecard citation gate (added the `N/A — <reason>` form).
- #856 — discipline anchors (added the deletion-default cite).
- #966 — scorecard citation gate (REJECTED-forms list).
- #1000 — discipline anchors (added the PR hygiene cite).

Each PR re-touched the same function body. Per `feedback_cascade_rebase_root_cause` (≥3 PRs DIRTY simultaneously on shared-anchor changes = design defect, not "normal merge math"), the monolith is the defect — fix structurally, not per-PR.

`defaultPromptBuilder` also has a second cost: the worker-prompt parity gate (`scripts/check-prompt-parity.sh`) regexes the function body for slug cites. Adding a new anchored rule requires editing both the dispatch template AND a specific line range here. Section files give the parity gate a tighter grep target.

## §2 Goal

Split the monolith. After this work:

- `internal/orchestrator/spawner/claude.go::defaultPromptBuilder` is a 25-line composition root: ItemBody sanitation + sentinel guard inline (security-load-bearing — do NOT move to a separate package), then a fixed-order call list to `sections.*`, then the enrichment suffix + terminator.
- Each rule-bearing section lives at `internal/orchestrator/prompt/sections/<name>.go` as `func <Name>(req prompt.Request) string`. Pure function, no side effects, no globals.
- New rule tweaks edit ONE section file. Parallel rule tweaks are file-disjoint by construction.

Self-host filter: cascade-rebase is a single-operator velocity tax; `defaultPromptBuilder` is on the hot path of every dispatch. Keep in scope.

## §3 Pattern (authoritative)

Match the #737 / `wire_*.go` pattern adapted for pure-function sections.

### §3.1 Package layout

```
internal/orchestrator/prompt/
├── sections/
│   ├── sections.go         // Request type re-export + section interface assertion
│   ├── itembody.go         // §3.4 §1 — work-item header + sentinel-fenced brief
│   ├── comments.go         // §3.4 §2 — comments-zero-by-default block
│   ├── discipline.go       // §3.4 §3 — CLAUDE.md slug-cite anchors
│   ├── scorecard.go        // §3.4 §4 — scorecard citation gate + release-notes fence
│   ├── prshape.go          // §3.4 §5 — PR title / body / reviewer-skip contract
│   └── *_test.go           // per-section unit tests (golden chunk per section)
├── enrich.go               // (existing) L2 adaptive enrichment
├── embed.go                // (existing) bundled CLAUDE.md resolver
└── ...
```

### §3.2 Section signature

Every section file exports ONE function with this exact shape:

```go
// <Name> returns the <topic> block of the worker prompt; section ordering is
// pinned at internal/orchestrator/spawner/claude.go::defaultPromptBuilder.
func <Name>(req Request) string
```

Where `Request` is a package-local type alias re-exporting `spawner.Request` (avoid the import cycle: `spawner` imports `prompt/sections`, not the reverse). Concretely: `internal/orchestrator/prompt/sections/sections.go` defines `type Request = prompt.Request` and `prompt.Request` is a NEW struct introduced in slice 1 that captures the subset of `spawner.Request` fields any section reads (`AgentID`, `WorkItemID`, `Lane`, `ItemBody`, `RepoRoot`). `spawner.defaultPromptBuilder` constructs a `prompt.Request` value from its inbound `spawner.Request` and passes it to each section.

Rationale (decision priority — best-practices < UX): a separate request type buys two things — (1) the section package never depends on `spawner` (no cycle, no `internal/` visibility games), (2) future non-spawner callers (e.g. the operator-console "preview prompt" path sketched in `docs/engineer/specs/2026-06-08-svelte-console-ux-design.md`) can build a `prompt.Request` directly.

### §3.3 Composition root shape

After the split, `defaultPromptBuilder` reads:

```go
func defaultPromptBuilder(req Request) string {
    var b strings.Builder
    if rules, source, err := prompt.ResolveClaudeMd(req.RepoRoot); err == nil && source == prompt.SourceBundled {
        b.WriteString("## Operating rules (bundled default — target has no CLAUDE.md)\n\n")
        b.WriteString(rules)
        b.WriteString("\n\n")
    }
    pr := prompt.Request{
        AgentID:    req.AgentID,
        WorkItemID: req.WorkItemID,
        Lane:       req.Lane,
        ItemBody:   sanitizeItemBody(req.WorkItemID, req.ItemBody),
        RepoRoot:   req.RepoRoot,
    }
    b.WriteString(sections.ItemBody(pr))
    b.WriteString(sections.Comments(pr))
    b.WriteString(sections.Discipline(pr))
    b.WriteString(sections.Scorecard(pr))
    b.WriteString(sections.PRShape(pr))
    if req.RepoRoot != "" && !enrichmentDisabled() {
        if enrich := prompt.Enrich(context.Background(), req.RepoRoot, prompt.DefaultOptions()); enrich != "" {
            b.WriteString(enrich)
            if !strings.HasSuffix(enrich, "\n") {
                b.WriteString("\n")
            }
            b.WriteString("\n")
        }
    }
    b.WriteString("Begin now. Do not summarize the brief back.\n")
    return b.String()
}
```

`sanitizeItemBody` is a package-local helper that wraps the existing `stripControlChars` + `bodyContainsSentinel` + reject-banner logic. It stays in `spawner` (security-load-bearing — sentinel constants + log line live next to the only call site).

### §3.4 Section contracts (one heading per section file)

Each section's contract names the EXACT trailing-newline shape so the byte-equal golden test holds.

1. **`ItemBody(req)` → `internal/orchestrator/prompt/sections/itembody.go`**
   - Emits `"regatta worker: work item %s on lane %s (agent %d).\n\n"` (always).
   - If `req.ItemBody != ""` after trim, emits the `## Item brief …` heading + sentinel-fenced body + trailing `\n\n`.
   - Sentinel constants imported from `spawner` are NOT moved — section receives the sanitised body string and the BEGIN/END markers from `prompt.Sentinels()` (a thin accessor introduced in slice 1; the literal constants stay in `spawner/claude.go` as the security boundary).

2. **`Comments(req)` → `comments.go`**
   - The 8-line `## COMMENTS: zero by default.` block verbatim.
   - Trailing `\n\n`.

3. **`Discipline(req)` → `discipline.go`**
   - The `## Discipline (anchors into CLAUDE.md)` block + 5 slug-cite bullets verbatim.
   - Trailing `\n\n`.

4. **`Scorecard(req)` → `scorecard.go`**
   - The `### Scorecard citation gate` block — token list + REJECTED forms + release-notes fence + auto-skip carve-out.
   - Trailing `\n\n`.

5. **`PRShape(req)` → `prshape.go`**
   - The `## PR shape contract` block + reviewer-skip predicate cite.
   - Trailing `\n\n`.

### §3.5 Per-section invariants (all enforced by impl PR tests)

- Pure function: no I/O, no globals, no `time.Now`, no env reads, no logger.
- Idempotent: `f(req) == f(req)` byte-equal across calls.
- Trailing-newline shape fixed in the contract above; `TestSection_TrailingShape` per section asserts.
- ≤80 LOC per section file (matches §3.5 of the serve.go spec ceiling, scaled down for smaller blocks).

## §4 Acceptance — byte-equal pin

The correctness story is "the rendered prompt is byte-equal pre/post". Per `feedback_byte_equal_refactor_pin`, the PR ships a mechanical drift gate, not a prose claim.

### §4.1 Existing golden tests (UNTOUCHED)

The 13 `defaultPromptBuilder(Request{...})` assertions in `internal/orchestrator/spawner/claude_test.go` (lines 187, 198, 237, 267, 298, 319, 337, 347, 357, 365, 379, 430, 444) MUST pass unmodified. If any test edits its expected substring, the refactor is wrong — STOP and re-spawn the design subagent (`feedback_spec_pattern_authority`).

### §4.2 New byte-equal golden test (slice 1, RED FIRST)

`internal/orchestrator/spawner/claude_test.go::TestDefaultPromptBuilder_ByteEqualPreSplit` captures the FULL rendered output for three canonical requests (no-RepoRoot, with-RepoRoot, with-ItemBody) into testdata golden files BEFORE any code moves, then the refactor PR replays the same three requests and asserts byte-equality against the captured goldens. Goldens live at `internal/orchestrator/spawner/testdata/prompt_golden_*.txt`.

Capture command (run on `main` before slice 1):
```sh
REGATTA_GOLDEN_CAPTURE=1 go test ./internal/orchestrator/spawner/ -run TestDefaultPromptBuilder_ByteEqualPreSplit
```

### §4.3 Section-order pin (slice 1)

`internal/orchestrator/prompt/sections/sections_test.go::TestSectionOrder_Pinned` builds the assembled prompt and asserts substring-index ordering for one anchor per section:

```go
order := []string{
    "regatta worker: work item",            // ItemBody
    "## COMMENTS: zero by default",         // Comments
    "## Discipline (anchors into CLAUDE.md)", // Discipline
    "### Scorecard citation gate",          // Scorecard
    "## PR shape contract",                 // PRShape
    "Begin now. Do not summarize the brief back.", // terminator
}
```

Index of each MUST be strictly increasing. This is the drift gate — a future PR that swaps section order without updating the anchor list fails fast.

### §4.4 Section unit tests

Per `feedback_test_coverage_audit_per_wave`: each section file gets a sibling `_test.go` with golden chunks (one per section), exercised independently of the composition root.

## §5 Implementer brief (3 slices)

Per `feedback_dispatch_brief_only` — slices are sequential (chained output: slice 2 depends on slice 1's interface). NOT parallel.

### Slice 1 — interface + composition root (~150 LOC net add)

**Files**: `internal/orchestrator/prompt/sections/sections.go` (new), `internal/orchestrator/prompt/request.go` (new), `internal/orchestrator/prompt/sentinels.go` (new), `internal/orchestrator/spawner/claude.go` (modify defaultPromptBuilder only — keep all section bodies inline still, just construct `prompt.Request` and call NO section functions yet), `internal/orchestrator/spawner/testdata/prompt_golden_{no_repo,with_repo,with_itembody}.txt` (new golden captures), `internal/orchestrator/spawner/claude_test.go` (add `TestDefaultPromptBuilder_ByteEqualPreSplit`).

**Deliverable**: Section package exists with the `Request` type, `Sentinels()` accessor, and ONE stub section (`ItemBody`) extracted. Existing tests + new byte-equal test pass. NO behaviour change — every existing rule string still in `defaultPromptBuilder` or in `sections.ItemBody`. RED commit first (golden file present, `ItemBody` not yet extracted → test fails on diff between assembled output and golden), then GREEN.

**Reviewer dispatch**: REQUIRED (load-bearing — `internal/orchestrator/spawner/` is on the reviewer-verdict-gate path list in CLAUDE.md::TDD+review).

### Slice 2 — extract sections one-at-a-time (~200 LOC net delete from claude.go, +200 LOC across 4 new files)

**Files**: `internal/orchestrator/prompt/sections/comments.go`, `discipline.go`, `scorecard.go`, `prshape.go` + sibling `_test.go` per. `internal/orchestrator/spawner/claude.go` shrinks (each section block replaced with a `b.WriteString(sections.Xxx(pr))` call).

**Deliverable**: Four sections extracted. `defaultPromptBuilder` body shrinks to the §3.3 shape. Byte-equal goldens from slice 1 still pass. Per-section trailing-shape tests pass.

**Order of extraction within slice 2**: Comments → Discipline → Scorecard → PRShape. Each section is one commit; CI green between commits (the RED-first rule applies to the first commit only — subsequent extractions are mechanical and the golden test is the regression gate).

**Reviewer dispatch**: REQUIRED.

### Slice 3 — drop monolith + sweep (~50 LOC net delete)

**Files**: `internal/orchestrator/spawner/claude.go` (final shrink), `docs/engineer/dispatch-templates/implementer.md` (update §Anchored rules to point at sections — `scripts/check-prompt-parity.sh` is already grep-based, no script edit needed unless the section file paths shift the grep target), `CLAUDE.md` (one bullet under Token economy: "Prompt sections under `internal/orchestrator/prompt/sections/`; tweak in ONE section file, not the monolith.").

**Deliverable**: `defaultPromptBuilder` is ≤30 LOC. Inline section literals all gone (the four `b.WriteString("- TDD: ...")` lines and friends). Parity gate still green.

**Reviewer dispatch**: PROPORTIONAL — auto-skip per `feedback_review_proportional` if slice-3 diff is `claude.go` shrink + dispatch-template path update + one CLAUDE.md bullet. Builder posts the auto-skip predicate cite in the PR body.

## §6 Adversarial pass

Per `feedback_adversarial_review_every_step` — design briefs get the same adversarial sweep as PRs. The hunt:

### §6.1 Section-order drift (HIGH)

**Risk**: A future PR adds a new section but inserts it in the middle of the composition root without updating `TestSectionOrder_Pinned`'s anchor list. The order changes silently because the test only sees the anchors it knows about.

**Mitigation**: Make the order test ALSO assert section COUNT — render the prompt, count the number of section-heading anchors (`## ` lines that match a known prefix set), assert equal to `len(order)`. New section MUST register both an anchor AND increment count. (Implementer slice 1 includes this assertion.)

### §6.2 Operator-override hook lost (MED)

**Risk**: `ClaudeSpawnerConfig.Prompt` is a `PromptBuilder` — operators can swap the whole function. After the split, an operator who wants to drop ONE section (e.g. scorecard for an internal-only dispatch) has to re-implement the entire composition root.

**Mitigation**: Out of scope for this spec — `PromptBuilder` remains a whole-function swap. The section package is callable from custom builders (sections are exported), so an operator can copy the §3.3 composition body and omit one call. If demand surfaces (≥2 operator-asks), file a follow-up for a `Sections []SectionFunc` config field. Trigger: external customer ask OR ≥2 internal asks. Tracker: file at slice-3 merge IFF an operator request lands during impl; otherwise SKIP per `feedback_default_simpler` (don't pre-build for hypothetical drift).

### §6.3 Section-cycle risk (LOW)

**Risk**: A section file imports another section file (e.g. `scorecard.go` reads a slug list from `discipline.go`), creating an intra-package dependency graph. Tomorrow's reviewer thinks they can edit Scorecard in isolation; today's import says otherwise.

**Mitigation**: Hard rule in slice 2 implementer brief: "Section files MUST NOT import each other. Each section is a standalone function. If two sections share a literal (e.g. the slug name `feedback_tdd_discipline`), duplicate the literal — three similar lines beat a premature abstraction (`feedback_default_simpler`)." Reviewer lens 1 (file scope) enforces.

### §6.4 Bundled-CLAUDE.md prefix coupling (LOW)

**Risk**: The bundled-CLAUDE.md prefix is emitted BEFORE the ItemBody section. If a future change moves the prefix into a section, the section ordering changes and `TestSectionOrder_Pinned` may not catch it (prefix is conditional).

**Mitigation**: Pin the bundled prefix INSIDE the composition root, NOT in a section file. Spec §3.3 codifies this. The §6.1 count-assertion also catches a stray ordering change.

### §6.5 ItemBody sanitation drift (HIGH — security)

**Risk**: Splitting `sanitizeItemBody` into a section function could let a future refactor move the sentinel collision check away from the only call site, opening a fence-escape injection (the original threat from #837).

**Mitigation**: HARD constraint in §3.3 — `stripControlChars` + `bodyContainsSentinel` + `itemBodyRejectedBanner` constants STAY in `internal/orchestrator/spawner/claude.go`. The section receives the already-sanitised body string. Slice 1 reviewer brief lists this as a CRITICAL invariant to verify.

### §6.6 Parity gate target shift (MED)

**Risk**: `scripts/check-prompt-parity.sh` greps `internal/orchestrator/spawner/claude.go` for slug literals. After the split, slugs live in `internal/orchestrator/prompt/sections/*.go` — the script's grep target moves.

**Mitigation**: Slice 3 updates the parity script's grep paths (or extends it to walk both files). Acceptance §0 #6 makes parity-gate-green a closing criterion. Implementer slice 3 brief MUST run `scripts/check-prompt-parity.sh` locally before push.

## §7 What got smaller (deletion default)

- `internal/orchestrator/spawner/claude.go::defaultPromptBuilder`: 66 LOC → ~25 LOC (-41 LOC inline).
- `internal/orchestrator/spawner/claude.go` total: 342 LOC → ~280 LOC (-62 LOC).
- Inline rule-string literals in claude.go: 18 `b.WriteString(...)` calls → 5 (-13).
- Cascade-rebase surface: 1 hot file (claude.go) → 1 thin composition root + 5 file-disjoint section files. PRs tweaking ONE rule no longer touch the composition root.

Pure-addition cost: 6 new files in `internal/orchestrator/prompt/sections/`. Per the deletion-default rule, this is an A+-defensible add: the addition is the unit-of-isolation that kills the cascade-rebase root cause. Each new file's existence is justified by the §3.4 contract; an empty section package would not solve the problem.

## §8 Out of scope

- `prompt.Enrich` L2 adaptive enrichment — already lives in its own file (`internal/orchestrator/prompt/enrich.go`), no change.
- `prompt.ResolveClaudeMd` bundled-default resolver — already isolated in `prompt/embed.go`.
- `genai`-side prompt rendering — separate code path, not on the spawner hot path.
- `ClaudeSpawnerConfig.Prompt` whole-function override seam — preserved as-is per §6.2.
- A future `Sections []SectionFunc` config seam — deferred (`feedback_default_simpler`); file follow-up only on second operator ask.

## §9 References

- `internal/orchestrator/spawner/claude.go` lines 228-294 — the monolith.
- `internal/orchestrator/spawner/claude_test.go` lines 187-444 — existing golden tests (preserved unchanged).
- `internal/orchestrator/prompt/embed.go` — bundled CLAUDE.md resolver (unchanged).
- `internal/orchestrator/prompt/enrich.go` — L2 adaptive enrichment (unchanged).
- `docs/engineer/dispatch-templates/implementer.md` §Anchored rules (worker-prompt parity) — touched by slice 3.
- `scripts/check-prompt-parity.sh` — parity gate (slice 3 verifies, may need grep-path update).
- `docs/engineer/specs/phase-x/2026-06-08-serve-go-subsystem-split.md` — sibling pattern (composition-root split).
- #834, #851, #856, #966, #1000 — the five recent PRs whose collisions motivate this split.
