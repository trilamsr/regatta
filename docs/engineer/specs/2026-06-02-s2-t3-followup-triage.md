# Spec: S2-T3 — GitHub `[followup]` issues → work_item briefs

_Author: design subagent, 2026-06-02. Source-of-truth: `docs/engineer/briefs/2026-06-01-self-host-first.md` §3 S2-T3 + `internal/orchestrator/adapter/markdown.go` + `contracts/schemas/spec_adapter.go`._

Cites: `memory/feedback_research_design_principles` (adopt > reimplement), `memory/feedback_grade_rubric` (B/A/A+ scorecard), `memory/feedback_pr_body_file_only` (`--body-file` only), `memory/feedback_test_godoc_one_line` (1-line max test godoc), `memory/feedback_self_improvement` (lessons cross-applied from S1-T3 PR #331), `memory/feedback_root_cause` (no symptom suppression), `memory/feedback_spec_pattern_authority` (deviation re-spawns design).

Sibling of S1-T3 (PR #331, `cmd/boot-prompt-to-items`). Where S1-T3 inhales `docs/engineer/autonomous-session-prompt.md`, this spec inhales the operator's `[followup]`-labeled GitHub issues — closing the loop _"operator files issue → regatta picks up → PR opened"_ from the self-host §3 acceptance gate.

---

## 0. Goal

Convert every `[followup]`-labeled GitHub issue in this repository into a `.regatta/items/gh-issue-<n>-<slug>.md` brief so the existing `internal/orchestrator/adapter/markdown.go::List` ingests it as a `WorkItem`. Run as `make followups`. Re-run is idempotent (no-op when the GH issue body sha256 is unchanged). Closed issues delete their brief file (root-cause: see §6).

This closes the self-host §3 S2-T3 loop with **zero new schemas, zero new adapters, zero new orchestrator wiring** — same shape as S1-T3.

## 1. Prior art adopted (no bespoke invention)

Per `memory/feedback_research_design_principles`, every primitive cites a proven OSS source OR a regatta primitive already shipped.

| Primitive | Adopted from | What we take | Why not bespoke |
|---|---|---|---|
| GH issue enumeration | [`gh issue list --label followup --state all --json number,title,body,labels,state,url`](https://cli.github.com/manual/gh_issue_list) | JSON shape; auth via `GH_TOKEN`; pagination via `--limit` | Shelling out to `gh` reuses the operator's existing auth context; no new GitHub-API client code, no new third-party Go dep. `gh` is the OSS reference impl. |
| `[followup]` label convention | Existing repo label (`gh label list` shows `followup — Deferred during review; trigger condition documented in body`, color `C5DEF5`) + 5 live issues at spec-time (#324, #329, #333 open; #261, #265 closed) | Use the live label as-is; do not introduce new labels | Adopting the existing label that operators already file on issues is free; inventing `[autonomous]` or `[regatta-pickup]` is YAGNI per `feedback_deletion_default` |
| Frontmatter generation | [Hugo `archetype` files](https://gohugo.io/content-management/archetypes/) + [Jekyll `_drafts` collection](https://jekyllrb.com/docs/posts/#drafts) | YAML frontmatter + body separator (`---`); one file per ingestable item; convention-over-config | Both static-site generators converged on the same shape because it is the smallest schema that round-trips through `text/template`. Reusing the shape means `parseMarkdownItem` already handles it. |
| Round-trip validator | `internal/orchestrator/adapter/parse.go::ParseMarkdownItem` (this repo) | The exported parser is the test oracle for every generated brief | Same trick as S1-T3 — the adapter is the spec; if the converter writes a file the adapter cannot read, the test fails. Single source of schema truth. |
| Idempotency sentinel | `cmd/boot-prompt-to-items` (S1-T3 PR #331) — embedded HTML comment `<!-- source-sha256: ... -->` placed in the body region (NOT under `## Acceptance criteria`) so `criterionRE` does not reject it | Same sentinel mechanism, hashed over canonical GH issue body bytes | Sibling-tool consistency. Operator who learned S1-T3 idempotency need not re-learn. Root-cause-fix lesson from PR #331 (sentinel in body, not under criteria) is reused. |
| Slugification | [Hugo `urlize`](https://gohugo.io/functions/urlize/) + the slug rule already used by `cmd/boot-prompt-to-items` (S1-T3 §2) | First 5 dash-joined lower-cased tokens of the title (drop punctuation, drop stop-words like `the/of/and/a/an`) | Cross-tool slug consistency — operator scanning `.regatta/items/` sees the same convention regardless of source. |

**Rejected alternatives (defended in §10):** custom `go-github` client + OAuth; webhook listener; GraphQL `issuesDependsOn` graph traversal; introducing a new `[autonomous]` label; deleting the markdown adapter and writing a `github_issues` adapter; emitting GH issues as `Kind=program` requiring planner expansion; cron-running `make followups` via `regatta serve` boot.

## 2. Decision: extend `cmd/boot-prompt-to-items` vs new `cmd/gh-followup-to-items`?

Decision: **NEW Go cmd at `cmd/gh-followup-to-items/main.go`**.

Decision priority per `memory/feedback_decision_priority` (UX → ease → performance → best-practices → speed → velocity; long-term > short-term):

| Axis | Extend S1-T3 cmd | New sibling cmd | Verdict |
|---|---|---|---|
| Operator UX | `make items` does TWO things (file + GH); failure mode is opaque ("did the boot-prompt fail or the gh call?") | `make items` and `make followups` are separate; each fails independently with a clear blame target | Sibling wins |
| Long-term maintenance | One binary with TWO source-readers behind a `--source-kind={file,gh}` flag is a god-mode CLI; the next sibling (e.g. Slack-thread → brief) bloats it further | One binary per source kind; each ≤300 LOC; tests stay focused | Sibling wins |
| Best-practices | Mixing read-from-disk + shell-out-to-`gh` in one main means tests need both a fixture file AND a `gh` mock | Each binary has ONE side-effect surface to mock | Sibling wins |
| Performance | `make items && make followups` runs two `go run`s (~2× cold start, ~80ms) | Same | Tie (negligible) |
| Velocity | Slightly faster: share regex helpers from S1-T3 | Slightly slower: re-write helpers OR carve a `internal/itemsgen/` package | Extend marginally |

Tie-broken by **long-term maintenance > velocity** per the decision-priority rule. New cmd.

Anti-bias check (`memory/feedback_research_design_principles`): we are NOT reinventing `gh`. We are NOT writing a GraphQL client. We shell out to `gh issue list --json ...` and parse the JSON it emits. The "proven OSS" we adopt is `gh` itself for transport + auth, and the markdown adapter for round-trip validation.

### 2.1 Shared helpers: do we extract a package?

No. Per `memory/feedback_parallel_dup_followups`, when two tools share ~30 lines of slug/idempotency-sentinel logic, the cheaper move is **copy + file a tracking issue** rather than premature abstraction. Carving an `internal/itemsgen/` package now means a third caller (the next sibling) drives the abstraction, not two. Both tools live under `cmd/`; if a third sibling appears, the first PR of that sibling extracts the shared package and back-fills S1-T3 + S2-T3.

Tracking issue (filed at spec-time, not at PR time): `[followup] extract internal/itemsgen/ once a 3rd brief-source converter appears` — labeled `followup`. The very label this spec ingests.

## 3. Output shape

One file per `[followup]`-labeled issue: `.regatta/items/gh-issue-<n>-<slug>.md`.

- `<n>`: the GitHub issue number, no padding. (`gh-issue-333-...`, not `gh-issue-00333-...`.)
- `<slug>`: first 5 dash-joined lower-cased tokens of the issue title with the leading `[followup]` / `[followup ...]` bracket-prefix stripped (drop punctuation, drop stop-words). Identical rule to S1-T3.

Example for live issue #333:

`gh-issue-333-doc-check-reviewer-tag-regex-over.md`

### 3.1 Frontmatter mapping (GH issue → WorkItem)

```yaml
---
id: GH-ISSUE-333                                # WorkItemID; deterministic from issue number
title: doc-check reviewer-tag regex over-matches 'Reviewer <Capital>' prose
kind: feature                                   # default; never "program" (planner expansion is out of scope — see §10)
lane: followup                                  # see §3.2
status: planned                                 # while issue is OPEN
dependencies:                                   # see §3.3
linked_artifact: https://github.com/trilamsr/regatta/issues/333
---
```

- `id` format: `GH-ISSUE-<n>` (upper-case, matches `WorkItemID` convention used elsewhere — e.g. `S1-T2`).
- `title` strips the leading `[followup]` / `[followup retro-audit]` / `[followup W7.0-T1]` bracket-tag from the GH issue title (a screen-display affordance, not signal for the work item).
- `kind=feature` always. GH issues are leaf work items; planner expansion (`Kind=program`) is out of scope (§10).
- `linked_artifact` is the GH issue URL — `parse.go::parseMarkdownItem` accepts any string; the URL form is how the operator clicks through.

### 3.2 Lane derivation from labels

`lane` is auto-derived from the most-specific GH label after `followup`:

| Label set on issue | `lane:` value |
|---|---|
| `followup` only | `followup` |
| `followup` + `W6-followup` | `w6-followup` |
| `followup` + `cost-governor-followup` | `cost-governor-followup` |
| `followup` + `tech-debt` | `tech-debt` |
| `followup` + any other `*-followup` label | that label's name, lower-cased |

Algorithm: of all labels on the issue, pick the first label matching `[a-z0-9][a-z0-9-]*-followup$` (lower-cased), else fall back to `followup`. Sorted alphabetically for determinism. If two `*-followup` labels are present (rare but possible), the alphabetically-first wins and a one-line warning prints (root cause per `feedback_root_cause`: GH labels are unordered; the converter MUST pick deterministically; the warning surfaces the ambiguity without failing the run).

### 3.3 Dependencies from GH issue links — **not** in this PR

GitHub's REST API exposes [issue dependencies](https://docs.github.com/en/issues/tracking-your-work-with-issues/about-issues#about-issue-dependencies) via `/repos/{owner}/{repo}/issues/{n}/sub_issues` and `parent_issue_url`. `gh issue view --json` does NOT yet surface these fields as of `gh` v2.59 (Jan 2026 cutoff). Probing the REST API would mean a second tool surface and a second auth path.

Decision: **omit `dependencies` entirely in this PR**. Tracking issue at spec-time: `[followup] gh-followup-to-items: emit Dependencies when gh CLI surfaces sub_issues JSON OR when an operator hits a real ordering bug`. YAGNI per `feedback_deletion_default`: no live `[followup]` issue today (5 issues sampled) has a sub_issue dependency. Ship without; add when the first real dep appears.

Root cause sanity check per `feedback_root_cause`: are we ignoring a missing field because it is hard, or because the use case is absent? Use case is absent: 5 of 5 sampled `[followup]` issues are leaf items. Confirmed absent — not papered over.

### 3.4 Body (satisfies `parse.go::parseCriteria`)

The adapter REQUIRES at least one acceptance criterion under `## Acceptance criteria`. The converter emits:

```markdown
Source: https://github.com/trilamsr/regatta/issues/333

<full GH issue body text, verbatim, fenced or unfenced as-was, EXCEPT
that the trailing `<!-- source-sha256: ... -->` sentinel is appended
AFTER this body and BEFORE the `## Acceptance criteria` heading>

<!-- source-sha256: deadbeef... -->

## Acceptance criteria

- [planned] c1: Resolve GH issue #333 "doc-check reviewer-tag regex over-matches 'Reviewer <Capital>' prose" per the trigger condition documented in the issue body.
```

Sentinel placement: in the **body region** (before `## Acceptance criteria`), per the root-cause lesson from S1-T3 PR #331 — placing the sentinel under `## Acceptance criteria` breaks `criterionRE`. Body region captures into `rest` at `parse.go:130` without complaint. We are NOT re-learning that lesson — citing `feedback_self_improvement`.

Single criterion only. Splitting issue bodies heuristically would invent structure GH issues do not carry. Operator hand-edits get clobbered when the source SHA changes; protected when it does not (see §4).

### 3.4.1 Body collisions (adversarial-review)

Two collision risks the reviewer flagged:

1. **GH issue body contains its own `## Acceptance criteria` heading** → adapter would parse the operator's words as criteria. Mitigation: the converter wraps the GH issue body in a fenced verbatim block (` ```text ... ``` `) when ANY line of the body matches `^##\s+Acceptance\s+criteria\s*$` (case-insensitive). Fenced block escapes the heading from the parser. The fence is omitted in the common case where no collision exists, keeping briefs human-readable.

2. **GH issue body contains its own `<!-- source-sha256: ... -->` literal** (e.g. an operator copy-pasted a regatta brief into an issue body) → false-positive on the idempotency check. Mitigation: the converter extracts the sentinel using a regex anchored to **end-of-body** — `(?m)^<!-- source-sha256: ([0-9a-f]{64}) -->\s*\z` — and only the converter-emitted last-line position counts. A sentinel-shaped string appearing mid-body is treated as ordinary content. Tested via `TestParse_BodyContainsLiteralSentinel` (§11).

## 4. Idempotency

Re-run MUST NOT clobber an unchanged file's mtime, and MUST overwrite when the GH issue body changes.

Mechanism (mirrors S1-T3 sentinel):

1. Converter computes `sha256(canonical GH issue body bytes)` where canonical = LF-normalized + trailing-whitespace-trimmed + final newline.
2. Embedded as HTML comment: `<!-- source-sha256: <64-hex> -->` in the **body region** (before `## Acceptance criteria`).
3. On re-run:
   - File missing → write.
   - File exists + embedded hash matches → no-op (do not touch mtime).
   - File exists + hash mismatches → rewrite the WHOLE file. Print one-line diagnostic.
   - File exists + no embedded hash → assume hand-authored; skip + warning.
4. Closed-issue cleanup runs AFTER the open-issue pass: see §6.

The hash is over the GH issue body bytes ONLY — not the title, not the labels, not the converter template. So a converter format change does NOT trigger spurious rewrites; a title-typo fix DOES NOT either (deliberate — title is rendered but not load-bearing for the criterion text). A real body edit by the operator on GitHub DOES trigger a rewrite. If the operator wants a title-typo fix to also re-render, they edit the body too OR re-run with `--force` (added when the first operator hits this, not now — YAGNI).

## 5. Auth

`gh issue list --json ...` inherits the operator's existing auth — `gh auth status` checks via `GH_TOKEN` env OR `~/.config/gh/hosts.yml` OR `GITHUB_TOKEN` env (same lookup order as `gh` itself).

The converter **never reads, logs, or sees** the token directly. It shells out to `gh` and consumes the JSON on stdout. No inline credentials, no `--token=...` flag (which would log via `ps`).

Failure modes:
- `gh` not installed → exit 2 with message `gh CLI not found; install from https://cli.github.com`.
- `gh auth status` failing → `gh issue list` exits non-zero with its own message; converter forwards to stderr + exits 1.
- Repo cannot be resolved (no remote `origin`) → `gh` exits non-zero; converter forwards.

CI / GH Actions: if used (NOT scope of this PR — `make followups` is operator-invoked), `GH_TOKEN` from `${{ secrets.GITHUB_TOKEN }}` is the standard env. No converter-side change required.

## 6. Closed issues: emit `status: completed` OR delete?

Decision: **delete the file**.

Options considered:

| Option | Behavior on issue close | Decision-priority verdict |
|---|---|---|
| A. Set `status: completed` | File stays, frontmatter rewritten | Schema has no `completed` — `Status` is `{planned, in_progress, done}` only (`spec_adapter.go:99-101`). Adding `completed` would be a schema change for one downstream use; rejected. |
| B. Set `status: done` | File stays, frontmatter rewritten | `done` means "L0 verified criterion text" in this repo; a closed GH issue has not been L0-verified by the orchestrator. Lying about state. Rejected per `feedback_root_cause`. |
| C. Delete the file | File removed; brief stops appearing in `markdownCatalog.List()` output | Clean. Re-opens the issue → next `make followups` re-creates the file. No semantic mismatch. |

Picked **C**. Mechanism: after the open-issue pass, list `.regatta/items/gh-issue-*.md`, extract `<n>` from the **filename** (`gh-issue-<n>-<slug>.md`, matched by regex `^gh-issue-(\d+)-`), check membership in the closed-issues set (from the same `gh issue list --state all` JSON), `os.Remove` the matched files. Print one line per deletion.

Adversarial-review finding: an earlier draft proposed parsing the `linked_artifact:` frontmatter URL to recover the issue number. Filename parsing is simpler, has no YAML-parse failure mode, and survives a hand-edit that corrupts the frontmatter URL.

This is the SAME root-cause fix discipline S1-T3 used for Phase X (`feedback_root_cause`): rather than inventing a sentinel status (`blocked`, `completed`) and a schema migration to support it, we don't emit the artifact at all. The schema stays minimal; the converter stays write-side-only.

Idempotency: closed-issue cleanup is itself idempotent — second run finds zero files to delete.

Hand-edits + closure: if an operator hand-edited a brief (sentinel stripped), §4 step "no embedded hash → skip + warning" applies on the open pass. On the close pass, the deletion is unconditional (the GH issue is the source-of-truth for existence; a hand-edited file referencing a closed issue is stale by definition). Print a one-line warning before deletion so the operator can recover from `git restore` if needed.

## 7. CLI surface

```
$ gh-followup-to-items [--label <name>] [--out <dir>] [--repo <owner/name>] [--dry-run]
```

Defaults:
- `--label followup`
- `--out .regatta/items/`
- `--repo` defaults to `gh`'s own resolution (origin remote → `gh repo set-default`); pass explicitly when the converter runs outside a checkout. Adversarial-review finding: without this flag, a stale or missing default-repo config makes `gh` prompt interactively and the converter hangs.
- `gh issue list` is always called with `--state all` internally; there is no operator-facing `--state` flag because the §6 cleanup pass REQUIRES the closed set to do its work. Adversarial-review finding: exposing `--state` would let an operator pass `--state open` and silently skip the cleanup pass, which is the §6 root-cause failure mode in reverse.
- `--dry-run` lists actions (create / no-op / rewrite / skip / delete) without touching disk OR shelling to `gh` (the dry-run mode uses a `--source-json <file>` escape hatch for tests; otherwise it still calls `gh issue list` because that is a network read, not a disk write).

Exit codes:
- 0: success (zero or more files written/deleted; idempotent re-run is success).
- 1: parse error (gh returned malformed JSON; duplicate sluggy filenames; unreadable issue body).
- 2: tool error (`gh` not installed; filesystem permission denied).

One line per action on stdout. Errors on stderr. No verbosity flag.

## 8. Section parser shape — NOT applicable

Unlike S1-T3 (which walks freeform markdown with 3 regexes), S2-T3 consumes structured JSON from `gh issue list --json number,title,body,labels,state,url --label followup --state all --limit 200`. The "parser" is `encoding/json` against this typed struct:

```go
type ghIssue struct {
    Number int    `json:"number"`
    Title  string `json:"title"`
    Body   string `json:"body"`
    URL    string `json:"url"`
    State  string `json:"state"`  // "OPEN" or "CLOSED"
    Labels []struct {
        Name string `json:"name"`
    } `json:"labels"`
}
```

No regexes, no awk, no yq. The label / lane derivation in §3.2 is one `for` loop + a regex match on the label name (`*-followup$`).

`--limit 200` is the ceiling; if a repo accumulates more than 200 `[followup]`-labeled issues we exit 1 with "increase --limit" (failure-loud rather than silent-truncate per `feedback_root_cause`). Today the count is 5; the ceiling is 40× headroom.

## 9. Makefile target

```make
followups:  ## Regenerate .regatta/items/gh-issue-*.md from GH `[followup]`-labeled issues. Idempotent.
	go run ./cmd/gh-followup-to-items
```

Placed alphabetically near `items` from S1-T3. Both `items` and `followups` listed in the existing `.PHONY` list at the top of the Makefile.

## 10. Out of scope (defended)

- **Cron-running via `regatta serve` boot.** `make followups` is operator-invoked. A serve-time poller adds a second source of `gh` calls AND a per-tick latency hit; YAGNI until the operator measures real friction running the make target manually.
- **`Kind=program` planner expansion.** GH issues are leaf items. Treating them as `program` means the planner spawns sub-items, which means the converter's output is no longer round-trippable through `parseMarkdownItem`. Rejected per `feedback_deletion_default`.
- **`go-github` client + OAuth.** Adding a third-party Go dep when shelling to `gh` works is bias-toward-NIH per `feedback_research_design_principles`.
- **Webhook listener.** Operator changes are infrequent (5 issues over months). Polling via `make followups` matches the cadence; webhooks add infrastructure (a public URL, a secret) for zero operator-visible win.
- **GraphQL `issuesDependsOn` traversal.** No live `[followup]` issue has a sub_issue dep (§3.3). Tracking issue filed; ship when the first real dep appears.
- **New `[autonomous]` label.** The `followup` label is already canonical in this repo. Inventing a parallel label means two surfaces for the operator to keep in sync. Rejected per `feedback_deletion_default`.
- **Deleting the markdown adapter for a `github_issues` adapter.** The markdown adapter is the single source of schema truth; rewriting it in `github_issues` shape means schema fork. Rejected per `feedback_research_design_principles`.
- **Round-tripping converter-generated files back into GH issue comments.** One-way only.

## 11. Test plan (TDD strict)

Tests live in `cmd/gh-followup-to-items/main_test.go`. Failing tests FIRST; implementation second. Per `memory/feedback_test_godoc_one_line`, every test godoc is 1 line MAX (or omitted):

| Test | Input (via `--source-json` fixture) | Expected |
|---|---|---|
| `TestParse_EmitsOnePerOpenIssue` | 3 open `[followup]` issues, 1 closed | 3 files written; closed issue's file NOT created |
| `TestParse_FrontmatterIsAdapterIngestable` | Fixture with 1 issue | Generated file round-trips through `adapter.ParseMarkdownItem` and yields expected ID + Title + Lane + Status + LinkedArtifact |
| `TestParse_LaneDerivedFromLabel` | Issues labeled `followup`+`W6-followup`, `followup`+`cost-governor-followup`, `followup` only, `followup`+`tech-debt` | Lanes: `w6-followup`, `cost-governor-followup`, `followup`, `tech-debt` |
| `TestParse_LaneAmbiguity_Warns` | Issue with two `*-followup` labels | Alphabetically-first wins; warning printed to stderr |
| `TestParse_IdempotentNoOp` | Run twice on same fixture | Second run touches zero files; mtimes preserved |
| `TestParse_BodyChange_Rewrites` | Run, edit fixture issue body, re-run | File rewritten; new sha256 embedded |
| `TestParse_HandEdit_Skipped` | Run, delete `source-sha256` sentinel from a file, re-run | File untouched; warning printed |
| `TestParse_ClosedIssue_DeletesFile` | Run, mark issue closed in fixture, re-run | File removed; cleanup logged |
| `TestParse_BracketTagStrippedFromTitle` | Issue title `[followup retro-audit] PR #261 shipped without explicit A+ rubric scorecard` | Generated `title:` field has the bracket-prefix stripped |
| `TestParse_SluggyFilenameDeterministic` | Two runs on same input | Same filename emitted both times |
| `TestParse_LimitExceeded_Errors` | Fixture with 201 issues, `--limit 200` | Exit code 1; no files written |
| `TestParse_DryRun` | `--dry-run` flag | Action lines printed; no files written; no deletions |
| `TestParse_BodyContainsAcceptanceHeading` | Issue body literally contains `## Acceptance criteria` | Body wrapped in fenced ` ```text ` block; adapter still parses; criterion still recognized |
| `TestParse_BodyContainsLiteralSentinel` | Issue body literally contains `<!-- source-sha256: aaaa... -->` mid-text | Sentinel mid-body ignored; converter writes its own end-of-body sentinel; idempotent re-run still no-ops |

Failing-test capture: the first `go test ./cmd/gh-followup-to-items/` MUST print red. Output captured in the PR body of the implementer PR (NOT this spec PR — this PR is spec-only).

The `--source-json <file>` fixture path is the test escape hatch: tests construct fixture JSON resembling `gh issue list --json ...` output and feed it without touching the network. Production runs (no `--source-json`) shell to `gh`.

## 12. B / A / A+ grade rubric

| Criterion | B (must) | A (should) | A+ (aspirational) |
|---|---|---|---|
| Round-trip | Generated files parse via `adapter.ParseMarkdownItem` | Round-trip is asserted in the test suite, not just spec prose | Round-trip is asserted against the LIVE `gh issue list` output (impl-PR test, gated behind `-tags=integration` to keep CI hermetic) |
| Idempotency | Re-run does not corrupt files | Re-run is a no-op (zero writes) when issue bodies unchanged | Re-run preserves hand-edits via the `source-sha256` sentinel + warns when the sentinel is missing |
| Adapter compatibility | One file per open issue under `.regatta/items/*.md` | Frontmatter contains every field `parseMarkdownItem` reads | Body satisfies `parseCriteria` with at least one criterion AND the `## Acceptance criteria` heading exactly matches the regex; sentinel placed in body region per S1-T3 lesson |
| Closed-issue handling | Closed issues do not pollute new briefs | Closed issues' existing briefs deleted on re-run | Deletion is idempotent + warns before clobbering hand-edited briefs |
| Auth | Inherits `gh auth` (no inline tokens) | Documented in §5 + error message points to `gh auth login` | Token never reaches process args (no `--token=...`), never reaches converter memory |
| TDD | Failing test exists before impl | Failing-test output captured in impl-PR body | Test suite covers all 12 cases in §11 including lane-derivation + bracket-strip + closed-cleanup |
| Maintenance | Go cmd under `cmd/`; same `go test` harness | No new third-party Go deps (gh shell-out only) | Total LOC < 350 (cmd + test) |
| Decision priority | Decision documented (extend vs sibling) with reasoning | Reasoning cites `feedback_decision_priority` axes | Reasoning addresses anti-bias check vs `feedback_research_design_principles` |
| Sibling parity | Tool naming + slug rule + sentinel mechanism mirror S1-T3 | Cross-tool consistency documented in §1 | Shared-helper extraction deferred via filed `[followup]` tracking issue (eats own dog food) |
| Scope (deps) | `dependencies` omitted with reasoning | Tracking issue filed for the deferred dep traversal | Tracking issue itself becomes a self-host test case (the spec is filed as a `[followup]` issue and the converter MUST self-ingest it) |
| Banned-phrase clean | `make pre-push-check` passes | doc-check.sh banned-phrase lint clean | Spec + PR body intentionally falsifiable (versions, counts, file:line refs, label-color hex) |
| Deletion default | PR answers "what got smaller?" | Net new code documented as proportional to net new capability | Scope reduction via §6 option C (don't emit closed) + §3.3 (don't emit deps until needed) |
| Test godoc | Every `func Test*` godoc is 1 line max OR omitted | Per `feedback_test_godoc_one_line` | All 12 test names self-document (no godoc needed) |
| PR body format | `gh pr create --body-file <path>` ONLY | Per `feedback_pr_body_file_only` | Body-file lives at worktree-local path (not `/tmp/`) so it's reviewable in `git status` until the PR opens |

## 13. What got smaller

- The dispatch-prompt's "extend `cmd/boot-prompt-to-items`" branch is DROPPED in favor of a sibling binary (§2). Maintenance surface stays additive, not multiplicative.
- The dispatch-prompt's "closed issues: emit `status: completed`" branch is DROPPED in favor of deletion (§6). One less schema state to add; one less semantic mismatch.
- The dispatch-prompt's "deps from issue dependencies" branch is DEFERRED via tracking issue (§3.3). Zero live cases today; ship when the first real case appears.
- No new schema. No new adapter. No new orchestrator wiring. No new third-party Go dep. No new GH label.
- No `--include-closed` flag, no `--force` flag, no verbosity flag — added when the first operator hits the case (YAGNI).
- No serve-time cron integration — added when manual `make followups` measurably hurts (YAGNI).
- Shared-helper package extraction deferred to the third sibling-binary's PR — premature abstraction avoided (§2.1).

## 14. Adversarial-review summary

Findings raised and folded back into the spec (no findings left as PR comments because the spec PR is design-only):

| # | Finding | Section folded into |
|---|---|---|
| AR-1 | `--state {open,all}` flag would let an operator silently skip the §6 cleanup pass | §7 — `--state` flag removed; `--state all` is internal |
| AR-2 | `gh issue list` prompts interactively if default repo is unresolved | §7 — `--repo` flag added with adopt-`gh`-defaults policy |
| AR-3 | GH issue body could itself contain `## Acceptance criteria`, breaking the adapter parser | §3.4.1 mitigation #1 — fenced wrap when heading detected |
| AR-4 | GH issue body could itself contain `<!-- source-sha256: ... -->`, false-positive on idempotency | §3.4.1 mitigation #2 — sentinel regex anchored to end-of-body |
| AR-5 | Parsing `linked_artifact:` URL to recover issue number on cleanup is brittle vs. filename regex | §6 — switched to filename `gh-issue-(\d+)-` regex |
| AR-6 | Two new test cases needed for AR-3 + AR-4 mitigations | §11 — `TestParse_BodyContainsAcceptanceHeading`, `TestParse_BodyContainsLiteralSentinel` |

Findings NOT folded back (defended in-place):

| # | Finding | Rationale |
|---|---|---|
| AR-7 | "Should `make followups` run inside `regatta serve` boot?" | §10 — YAGNI; operator-invoked only |
| AR-8 | "Should we add a `--force` flag for hand-edit overwrite?" | §4 — YAGNI; add when the first operator hits the case |
| AR-9 | "Should sibling tools share an `internal/itemsgen/` package now?" | §2.1 — defer to third caller; tracking issue filed |

## 15. Memory-rule citations (this spec)

- `memory/feedback_research_design_principles` — §1 adopts `gh` + Hugo/Jekyll frontmatter + the markdown adapter itself; §2 anti-bias check rejects `go-github` + GraphQL custom client.
- `memory/feedback_grade_rubric` — §12 + impl-PR body MUST post the scorecard verbatim.
- `memory/feedback_pr_body_file_only` — §12 row "PR body format" mandates `--body-file`; impl + spec PRs both bound by it. This spec PR uses a body-file at `/tmp/pr-body-s2-t3.md`.
- `memory/feedback_test_godoc_one_line` — §11 + §12 row "Test godoc" enforce the 1-line max on all 12 test names.
- `memory/feedback_self_improvement` — §3.4 reuses the S1-T3 sentinel-placement root-cause lesson (PR #331); §1 reuses the S1-T3 slug rule + sentinel-mechanism + Makefile-target pattern.
- `memory/feedback_root_cause` — §3.2 picks deterministic-with-warning over silent-truncate; §3.3 confirms the absent-use-case before deferring; §6 picks deletion over a fabricated `completed` status; §8 picks fail-loud over silent-truncate at the `--limit` ceiling.
- `memory/feedback_spec_pattern_authority` — implementer deviations from this spec (e.g. "let's add status: completed", "let's emit closed issues with kind: program", "let's introduce internal/itemsgen/ now") re-spawn the design subagent.
- `memory/feedback_deletion_default` — §10 + §13 enumerate every cut.
- `memory/feedback_parallel_dup_followups` — §2.1 defers shared-helper extraction to the third caller.

## Resolution (2026-06-02)

Shipped via #368 (`feat(gh-followup-to-items): GH [followup] issues → work_item briefs (S2-T3)`). `make followups` emits one `.regatta/items/F-<NNN>-<slug>.md` per open `[followup]` GH issue.
