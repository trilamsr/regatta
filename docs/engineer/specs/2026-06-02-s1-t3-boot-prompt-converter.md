# Spec: S1-T3 — boot-prompt → work_item brief converter

_Author: design subagent, 2026-06-02. Source-of-truth: `docs/engineer/autonomous-session-prompt.md` PRIORITY block + `internal/orchestrator/adapter/markdown.go` + `contracts/schemas/spec_adapter.go`._

Cites: `memory/feedback_research_design_principles` (adopt > reimplement), `memory/feedback_decision_priority` (UX > ease > performance > best-practices > speed > velocity), `memory/feedback_doc_check_banned_phrases` (11-token list), `memory/feedback_grade_rubric` (B/A/A+ scorecard required), `memory/feedback_root_cause` (no symptom suppression).

---

## 0. Goal

Convert the PRIORITY block of `docs/engineer/autonomous-session-prompt.md` into one `.regatta/items/<phase>-<id>-<slug>.md` brief per entry so the existing `internal/orchestrator/adapter/markdown.go::List` ingests them as `WorkItem`s. Run as `make items`. Re-run is idempotent (no-op when source unchanged).

This closes the loop in self-host §3 S1-T3: the operator updates ONE source (the boot prompt) and the binary inhales briefs.

## 1. Decision: shell+yq vs Go cmd

Decision: **Go cmd at `cmd/boot-prompt-to-items/main.go`**.

Decision priority (`memory/feedback_decision_priority`):

| Axis | Go cmd | shell+yq | Verdict |
|---|---|---|---|
| Long-term maintenance | Same `go test` harness as the rest of the repo; refactors caught by the compiler; reuses `parseMarkdownItem` so the schema can never drift from the adapter | Bash + yq is two dependencies; yq is unpinned on most dev machines; brittle awk for section parsing | Go wins |
| Ease (operator-side) | `make items` — one target | `make items` — one target after installing yq | Tie at the make layer; Go drops the yq install step |
| Performance | <50ms cold start; irrelevant for a <1KB file | <100ms; irrelevant | Tie |
| Best-practices | Idempotency via content-hash diff is 5 lines of Go | Same logic in bash needs a temp-file dance | Go wins |
| Velocity | ~150 LOC + a test file | ~80 LOC bash + yq install doc | Bash looks shorter but the test surface is wider because every `yq` edge case is its own bug |

Anti-bias check (`memory/feedback_research_design_principles` — "adopt proven OSS over reimplementation"): we are NOT reimplementing yq or sed. We are writing a regex-based section walker for ONE document shape (the boot-prompt PRIORITY block) plus an idempotent frontmatter writer. The "proven OSS" we are adopting is the markdown adapter itself — `parse.go::ParseMarkdownItem` is the round-trip validator. The converter writes; the adapter reads; the round-trip is the test.

## 2. Output shape

One file per PRIORITY entry: `.regatta/items/<phase>-<id>-<slug>.md`.

- `<phase>`: `s1`, `s2`, `s3`, or `x`. Lowercase. Drawn from the section header (`PHASE S1 — ...`, `PHASE X — ...`).
- `<id>`: the bracketed token at the start of the bolded entry — `s1-t2`, `s2-t4`, etc. Lowercase. Phase X entries get synthesized IDs `x-1`, `x-2`, ... in source-order because the source uses prose bullets (no explicit ID).
- `<slug>`: first 5 dash-joined lower-cased tokens of the title (drop punctuation, drop stop-words like `the/of/and/a`).

Example: `s1-t2-close-282-spawner-callback-wiring.md`.

### Frontmatter (satisfies `WorkItem` schema per `contracts/schemas/spec_adapter.go:57-68`)

```yaml
---
id: S1-T2                                    # upper-cased phase-id; matches WorkItemID convention
title: close #282 spawner-callback wiring   # exactly the entry's title prose, # preserved
lane: self-host                              # constant for boot-prompt-derived items
status: planned                              # Phase S1/S2/S3 → planned; Phase X → planned with dependencies set to trigger
dependencies:                                # comma-separated; empty when none
linked_artifact: docs/engineer/autonomous-session-prompt.md#L22
---
```

- `lane` is constant `self-host` because every boot-prompt item is part of the self-host-first program. A future converter that ingests non-self-host briefs MAY widen this; not in scope here.
- `status` is always `planned` — the markdown adapter only accepts `planned|in_progress|done` (per `parse.go::validStatus`). The "blocked" notion the dispatch prompt brief mentioned does not exist in the schema; what gates Phase X entries from being picked is the `Dependencies` field pointing at unmet triggers. See §2.1.
- `linked_artifact` carries the source location as `<path>#L<line>` so an operator clicking through sees the originating PRIORITY entry.

### Body (satisfies `parse.go::parseCriteria`)

The adapter REQUIRES at least one acceptance criterion under `## Acceptance criteria`. The converter emits a single criterion per item, deterministic, derived from the PRIORITY entry's prose tail:

```markdown
Source: docs/engineer/autonomous-session-prompt.md#L22

## Acceptance criteria

- [planned] c1: Land the PRIORITY entry "S1-T2 close #282 spawner-callback wiring" per the boot prompt; entry's prose tail recorded in body.
```

This is intentionally a single criterion. The PRIORITY entry's prose is one paragraph; splitting it heuristically would invent structure the source does not carry. When a richer brief is needed, the operator hand-edits the generated file — and the converter's idempotency check (content-hash) will refuse to clobber the hand-edit (see §3).

### 2.1 Phase X "blocked" encoding

The dispatch brief asked for `status: blocked` with `deps: [<trigger>]`. The schema does not allow `blocked` (`parse.go:60`). Root cause: the markdown adapter today encodes "blocked-because-dep" via the `Dependencies` field + scheduler dep-graph check. There is no separate `blocked` state.

Resolution: Phase X items emit with `status: planned` and `dependencies: PHASE-X-TRIGGER`. We create ONE sentinel item `.regatta/items/_phase-x-trigger.md` (leading `_` so the adapter SKIPS it per `markdown.go:135`) — wait, that won't work because the dep then references a missing ID and `checkAcyclic` won't complain but the scheduler will never find a `PHASE-X-TRIGGER` item to mark done. Two options:

- **A**: emit Phase X items with `status: done` so they never get picked. Lie about state.
- **B**: emit Phase X items with `dependencies: PHASE-X-TRIGGER` and emit ONE non-underscored `phase-x-trigger.md` with `status: planned` and a single criterion the operator manually marks done when the trigger fires. The trigger item itself depends on nothing, so the scheduler will TRY to pick it up. The operator's responsibility is "don't dispatch Phase X work" — which is exactly what the self-host-first brief §7 already says.
- **C**: don't emit Phase X items at all. The boot prompt's Phase X section is human-readable backlog, not dispatch-ready work.

Decision: **C**. Per `memory/feedback_decision_priority` (long-term maintenance > velocity): a sentinel that exists to be skipped is a sharp edge an operator WILL accidentally dispatch one day; better to not emit at all. Phase X items remain in the boot prompt as prose. When the trigger fires the operator re-runs `make items` with a `--include-phase-x` flag (added when the trigger actually fires, not now). YAGNI on the flag too.

PHASE-S-RELAX note: this matches the gate-relaxation memory entry's framing — defer ceremony that is not load-bearing today.

## 3. Idempotency

Re-running the converter MUST NOT clobber a hand-edited file. Mechanism:

1. The converter computes `sha256(canonical PRIORITY-entry prose)` and embeds it as an HTML comment at the bottom of the generated file: `<!-- source-sha256: deadbeef... -->`.
2. On re-run, for each target path:
   - If file does NOT exist → write.
   - If file exists AND embedded `source-sha256` matches the recomputed hash → no-op (do not touch mtime).
   - If file exists AND hash mismatches → the source moved; rewrite the WHOLE file (operator hand-edits to the body get overwritten — by design, because the source-of-truth changed). Print a one-line diagnostic so the operator notices.
   - If file exists AND has NO embedded hash → assume hand-authored (not converter-generated); skip and print a one-line warning.

The hash is over the PRIORITY entry prose ONLY — not over the generated frontmatter or body template. So a converter format change (e.g. new frontmatter field) does NOT trigger spurious rewrites; only a real source-prose change does.

## 4. Section parser (no yq, no awk-juggling)

The boot-prompt PRIORITY block has a stable shape, captured by 3 regexes:

- Phase header: `^PHASE (S[123]|X) — `
- Numbered entry (S-phases): `^\d+\.\s+\*\*(S[123]-T\d+) — ([^*]+)\*\*\s+—?\s*(.*)$`
- Bulleted entry (X-phase): `^- \*\*([^*]+)\*\*\s+—?\s*(.*)$` — captured only when `--include-phase-x` flag is on (§2.1: not in this PR).

The walker advances line by line, tracks `current_phase`, emits an entry per match, stops when it hits a line matching `^OPEN FOLLOWUPS\b` or `^Already shipped\b` or `^WORKFLOW per item\b`.

Robustness: if zero entries are found, exit non-zero with a clear error. If the same ID appears twice, exit non-zero. No silent success.

## 5. CLI surface

```
$ regatta-boot-prompt-to-items [--source <path>] [--out <dir>] [--dry-run]
```

Defaults:
- `--source docs/engineer/autonomous-session-prompt.md`
- `--out .regatta/items/`
- `--dry-run` lists the actions (create/no-op/rewrite/skip) without touching disk.

Exit codes:
- 0: success (zero or more files written; idempotent re-run is success).
- 1: parse error (no entries found, duplicate ID, unreadable source).
- 2: filesystem error (permissions, etc.).

No flags for verbosity. One line per action on stdout; errors on stderr.

## 6. Makefile target

```make
items:  ## Regenerate .regatta/items/*.md from docs/engineer/autonomous-session-prompt.md. Idempotent.
	go run ./cmd/boot-prompt-to-items
```

Placed alphabetically near `install-hooks`. No `.PHONY` change required if we add `items` to the existing `.PHONY` list.

## 7. Test plan (TDD strict)

Tests live in `cmd/boot-prompt-to-items/main_test.go`. Failing tests FIRST; implementation second. Test cases:

| Test | Input | Expected |
|---|---|---|
| `TestParse_EmitsOnePerPriorityEntry` | Fixture boot prompt with 2 S1, 2 S2, 1 S3 entries | 5 files written under tempdir |
| `TestParse_FrontmatterIsAdapterIngestable` | Same fixture | Each generated file round-trips through `adapter.ParseMarkdownItem` without error and yields the expected ID + Title + Lane + Status |
| `TestParse_IdempotentNoOp` | Run twice on same source | Second run touches zero files; mtimes preserved |
| `TestParse_SourceChange_Rewrites` | Run, edit fixture entry's prose, re-run | File rewritten; new sha256 embedded |
| `TestParse_HandEdit_Skipped` | Run, delete the `source-sha256` comment from a file, re-run | File untouched; warning printed |
| `TestParse_PhaseXSkipped` | Fixture includes Phase X bullets | Zero Phase X files generated |
| `TestParse_DuplicateID_Errors` | Fixture with two `S1-T2` entries | Exit code 1, no files written |
| `TestParse_DryRun` | `--dry-run` flag | Action lines printed; no files written |
| `TestParse_RealBootPrompt` | The actual checked-in `docs/engineer/autonomous-session-prompt.md` | All entries parse; output ingests through adapter round-trip; count matches manual count |

Failing-test capture: the first run of `go test ./cmd/boot-prompt-to-items/` must print red. Captured in the PR body.

## 8. B / A / A+ grade rubric

| Criterion | B (must) | A (should) | A+ (aspirational) |
|---|---|---|---|
| Round-trip | Generated files parse via `adapter.ParseMarkdownItem` | Round-trip is asserted in the test suite, not just spec prose | Round-trip is asserted against the LIVE checked-in boot prompt (test fails when boot-prompt drifts past the parser) |
| Idempotency | Re-run does not corrupt files | Re-run is a no-op (zero writes) when source unchanged | Re-run preserves hand-edits via the `source-sha256` sentinel |
| Adapter compatibility | One file per PRIORITY entry under `.regatta/items/*.md` | Frontmatter contains every field `parseMarkdownItem` reads | Body satisfies `parseCriteria` with at least one criterion AND has the `## Acceptance criteria` heading exactly as the regex expects |
| TDD | Failing test exists before impl | Failing-test output captured in PR body | Test suite covers all 9 cases in §7 including the live-boot-prompt round-trip |
| Maintenance | Go cmd under `cmd/`; same `go test` harness as the rest of the repo | No new third-party deps | Total LOC < 250 (cmd + test) |
| Decision priority | Decision documented (Go vs shell) with reasoning | Reasoning cites `feedback_decision_priority` axes | Reasoning explicitly addresses anti-bias check vs `feedback_research_design_principles` (no reinvention of yq) |
| Banned-phrase clean | `make pre-push-check` passes | doc-check.sh banned-phrase lint clean | Spec + PR body intentionally falsifiable (versions, counts, file:line refs) |
| Scope | Phase X not emitted | Phase X resolution documented (`§2.1`) | Phase X resolution explains the YAGNI on the `--include-phase-x` flag |
| Deletion default | PR answers "what got smaller?" | Net LOC removed elsewhere OR scope reduction documented | Scope reduction via §2.1 (don't emit Phase X) is the deletion |

## 9. What got smaller

- The dispatch-brief's `status: blocked` notion was DROPPED (the schema does not have it; §2.1 picks option C: don't emit Phase X at all).
- The dispatch-brief's `deps: [<trigger>]` mechanism was DROPPED (same root cause: there is no "trigger" item to depend on).
- The dispatch-brief allowed shell+yq OR Python; §1 picks Go and removes those branches from the maintenance surface.
- No new schema. No new adapter. No new orchestrator wiring. The converter is a write-side tool only.
- No flag for `--include-phase-x` until the Phase-X trigger fires (YAGNI).

## 10. Out of scope

- Round-tripping converter-generated files back into the boot prompt (one-way only).
- Watching the boot prompt for changes (`make items` is operator-invoked).
- Cross-referencing PR numbers from "Already shipped" section to mark done. (Operator marks done via the adapter's `UpdateStatus` after PR merges — that flow exists already.)
- Linking the converter into `regatta serve` boot. The converter is a build-time helper, not a runtime dependency.

## 11. Memory-rule citations (this spec)

- `memory/feedback_research_design_principles` — §1 anti-bias check; we adopt the markdown adapter as the round-trip validator instead of reinventing yq.
- `memory/feedback_decision_priority` — §1 table picks long-term-maintenance > velocity.
- `memory/feedback_root_cause` — §2.1 fixes the schema mismatch at root (don't emit Phase X) instead of papering over with a sentinel.
- `memory/feedback_doc_check_banned_phrases` — §8 grade rubric requires banned-phrase clean.
- `memory/feedback_grade_rubric` — §8 + PR body MUST post the scorecard verbatim.
- `memory/feedback_deletion_default` — §9 enumerates the cuts.
