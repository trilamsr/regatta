---
title: "docs/engineer/dispatch-templates/*.md modular split — Design Spec"
status: active
summary: "Split the four monolithic dispatch templates (`implementer.md`, `reviewer.md`, `designer.md`, `triage.md`) into per-rule snippet files under `docs/engineer/dispatch-templates/snippets/{shared,implementer,reviewer,designer,triage}/<rule-slug>.md`. Templates become a short manifest of snippet includes that the dispatch-prompt reader concatenates in declared order, so future rule-tweaks edit ONE snippet instead of patching the same monolithic file in 4-5 parallel PRs."
---

# `docs/engineer/dispatch-templates/*.md` modular split — Design Spec

Status: ready for review
Date: 2026-06-08
Author: design session <tri@maydow.com>

Memory rules in force: `feedback_decision_priority`, `feedback_default_simpler`, `feedback_cascade_rebase_root_cause`, `feedback_deletion_default`, `feedback_research_design_principles`, `feedback_adversarial_review`, `feedback_spec_pattern_authority`, `feedback_no_signatures`, `feedback_unaddressed_load_bearing`, `feedback_dispatch_brief_only`.

---

## §0 Closing trigger

Done when: slice-3 lands AND each of `docs/engineer/dispatch-templates/{implementer,reviewer,designer,triage}.md` is either (a) a manifest of snippet includes plus the per-dispatch `## Variables` / `## Per-dispatch payload` / `## Definition of done` blocks, OR (b) removed in favor of a generated artifact — AND the concatenation of the manifest-resolved snippets is byte-equal to the pre-split template body for each role, verified by `scripts/check-template-render.sh` (new, see §3.5).

Reopen-trigger: if ≥3 PRs in one session land on the same individual snippet file, that snippet is the new god-file — split it further or merge it back into a shared snippet. Same predicate as `feedback_cascade_rebase_root_cause`.

---

## §1 Problem

`docs/engineer/dispatch-templates/{implementer,reviewer,designer,triage}.md` are four monolithic files (150 / 101 / 92 / 64 LOC respectively at `origin/main@a83f6c7`). Each holds 10-20 independent rules glued by markdown headings:

- `implementer.md` — WORKTREE, TDD, ADVERSARIAL REVIEW, SELF-GRADE, DOC-CHECK, RELEASE NOTES, NO SIGNATURES, MEMORY CITES, CI-CHECK OUTPUT COMPRESSION, SHARED-PRIMITIVE OWNERSHIP, WINDOWS PATH TESTS, COMMENT BUDGET, REVIEWER-SKIP CONDITIONS, LOAD-BEARING LEFTOVERS, INDEPENDENT REVIEW MEASURES, plus a `## Anchored rules (worker-prompt parity)` slug list + RECURRING-FAILURE TRAPS appendix.
- `reviewer.md` — WORKTREE, ROLE, AUTO-SKIP CHECK, LENSES (9 lenses), RUN LOCAL LINTS, AUTOMERGE GATE, LOAD-BEARING LEFTOVERS → ONE AGGREGATE TRACKING ISSUE PER PR, OUTPUT FORMAT, NO SIGNATURES, RECURRING-FAILURE TRAPS.
- `designer.md` — WORKTREE, RESEARCH + DESIGN, GRADE RUBRIC, SELF-HOST FILTER, ADVERSARIAL REVIEW ON SPEC, DOC-CHECK, DELETION DEFAULT, RELEASE NOTES, NO SIGNATURES, MEMORY CITES, OUTPUT-PATH SLUG, CROSS-DOC LINK PHASING, DESIGN ITERATION LOCAL, UMBRELLA SPEC, RECURRING-FAILURE TRAPS.
- `triage.md` — ROLE, DECISION PRIORITY, SELF-HOST FILTER, VERDICTS, ROOT CAUSE, DEDUPE, REVIEWER-FINDING + SLICE AGGREGATION, OUTPUT FORMAT, NO CODE, DROP CEREMONY.

Observed in this session alone, rule-tweak PRs hit cascade-rebase 4–5 times: any two parallel PRs that each touch one rule (e.g. one PR adds a memory citation to the COMMENT BUDGET block, another rewords the RELEASE NOTES block) collide on the same template file. `git log --oneline docs/engineer/dispatch-templates/implementer.md` shows ~30+ commits in the last 30 days, each amending one block.

Same defect class as `internal/orchestrator/state` (#737), `cmd/regatta/serve.go` (#985 / #1002), `contracts/schemas/regatta.v1.cue` (cue-schema-modular-split spec, 2026-06-08), and `docs/engineer/specs/README.md` (untracked in #957). Per `feedback_cascade_rebase_root_cause`: ≥3 PRs DIRTY on a shared anchor = design defect, not normal merge math. Fix structurally.

Symptom shape: rules are independently mutable but glued in one file with no extraction seam.

Cross-cutting cost: 8 rules currently appear in 2+ templates (WORKTREE preamble, NO SIGNATURES, DOC-CHECK, RELEASE NOTES, SELF-HOST FILTER, ADVERSARIAL REVIEW, MEMORY CITES, RECURRING-FAILURE TRAP entries). A rule update means N-way prose-sync across templates today; `scripts/check-prose-dup.sh` already flags this drift but cannot auto-fix.

---

## §2 Decision priority

Per CLAUDE.md §"Decision priority": UX > ease > performance > best-practices > speed > velocity. Long-term > short-term.

For one operator + autonomous-loop subagent dispatch + per-rule mutation cadence:

- **Operator UX**: today the operator opens `implementer.md` and scrolls 150 lines to find the COMMENT BUDGET rule. Post-split, the operator opens `snippets/implementer/comment-budget.md` or `snippets/shared/comments.md`. `grep -rn 'comment-density' snippets/` continues to work; file-jumping by rule slug is the new affordance. Net wash for read-cost, win for mutation-cost.
- **Ease**: pure markdown concat, no new format. Per `feedback_default_simpler`: simplest viable mechanism — a manifest is a list of file paths in include-order; concatenation is `cat $(jq -r '.[]' manifest.json)` or equivalent. No template engine, no Mustache/Jinja, no front-matter directives. (Adversarial alternative considered + rejected in §6.)
- **Best practice** (one-rule-per-file cohesion): real win for diff-locality.
- **Velocity**: 3-slice incremental migration; each slice independently shippable; byte-equal acceptance gate prevents semantic drift.

Long-term: every future rule-tweak PR's diff fits in one snippet file. Cross-template prose-duplication moves from drift-prone copy-paste to single-source-of-truth `snippets/shared/`. Subagent dispatch (`feedback_dispatch_brief_only` — main thread already pastes per-task brief, NOT the full template) is unchanged; only the authoring surface changes.

Long-term anti-win: if snippet count balloons past ~40 entries per role, navigation degrades. Bound by §5 "Reopen-trigger" predicate.

---

## §3 Design

### §3.1 Target layout

```
docs/engineer/dispatch-templates/
├── implementer.md            # manifest: lists snippet includes + per-dispatch payload
├── reviewer.md               # manifest
├── designer.md               # manifest
├── triage.md                 # manifest
└── snippets/
    ├── shared/               # rules used by ≥2 roles
    │   ├── worktree-preamble.md
    │   ├── no-signatures.md
    │   ├── release-notes-fence.md
    │   ├── doc-check.md
    │   ├── self-host-filter.md
    │   ├── memory-cites.md
    │   ├── adversarial-review.md
    │   ├── pr-body-file-flag.md           # gh pr create --body-file rule
    │   └── recurring-traps-shared.md      # cross-role failure traps
    ├── implementer/
    │   ├── comments-zero-by-default.md
    │   ├── tdd-failing-first.md
    │   ├── ci-check-compress.md
    │   ├── shared-primitive-ownership.md
    │   ├── windows-path-tests.md
    │   ├── comment-budget.md
    │   ├── reviewer-skip-conditions.md
    │   ├── load-bearing-leftovers.md
    │   ├── independent-review-measures.md
    │   ├── self-grade.md
    │   ├── anchored-rules-worker-parity.md
    │   └── recurring-traps.md
    ├── reviewer/
    │   ├── role.md
    │   ├── auto-skip-check.md
    │   ├── lenses-1-edge-cases.md
    │   ├── lenses-2-refactor.md
    │   ├── lenses-3-risk.md
    │   ├── lenses-4-spec-fidelity.md
    │   ├── lenses-5-tdd-trace.md
    │   ├── lenses-6-doc-check.md
    │   ├── lenses-7-subagent-verification.md
    │   ├── lenses-8-load-bearing-leftovers.md
    │   ├── lenses-9-comment-sweep.md
    │   ├── run-local-lints.md
    │   ├── automerge-gate.md
    │   ├── load-bearing-aggregate-issue.md
    │   ├── output-format.md
    │   └── recurring-traps.md
    ├── designer/
    │   ├── research-and-design.md
    │   ├── grade-rubric.md
    │   ├── deletion-default.md
    │   ├── output-path-slug.md
    │   ├── cross-doc-link-phasing.md
    │   ├── design-iteration-local.md
    │   ├── umbrella-spec-task-list.md
    │   └── recurring-traps.md
    └── triage/
        ├── role.md
        ├── decision-priority.md
        ├── verdicts.md
        ├── root-cause.md
        ├── dedupe.md
        ├── reviewer-finding-slice-aggregation.md
        ├── output-format.md
        ├── no-code-no-pr.md
        └── drop-ceremony.md
```

One rule per file. Filename = rule slug (kebab-case). Each snippet is a markdown fragment — heading level `##` at top so concatenation produces today's section layout. No frontmatter (snippets are not standalone docs).

### §3.2 Manifest format

The template manifest is plain Markdown — the same file an agent reads today, just shorter. Includes use a fenced HTML-style directive that is invisible to GitHub render but parsed by the resolver:

```markdown
# Implementer dispatch template

Code-writing subagent. Substitute `<VARS>` then paste into Task dispatch.

## Variables
- `<TASK-ID>` — wave/task tag (e.g. `S1-T2`, `cost-gov-W3-T7`).
- `<SPEC-PATH>` — canonical spec under `docs/engineer/specs/`.
- `<BRANCH-NAME>` — `feat/...` | `fix/...` | `chore/...`.
- `<FILE-SCOPE>` — paths this dispatch may touch (file-disjoint vs siblings).
- `<MEMORY-RULES>` — comma-separated `feedback_*` filenames to cite.
- `<PR-TYPE>` — `feat` | `fix` | `refactor` | `chore` | `docs` | `ci`.

<!-- include: snippets/implementer/comments-zero-by-default.md -->

## Preamble blocks (paste verbatim)

<!-- include: snippets/shared/worktree-preamble.md -->
<!-- include: snippets/implementer/tdd-failing-first.md -->
<!-- include: snippets/shared/adversarial-review.md -->
<!-- include: snippets/implementer/self-grade.md -->
<!-- include: snippets/shared/doc-check.md -->
<!-- include: snippets/shared/release-notes-fence.md -->
<!-- include: snippets/shared/no-signatures.md -->
<!-- include: snippets/shared/memory-cites.md -->
<!-- include: snippets/implementer/ci-check-compress.md -->
<!-- include: snippets/implementer/shared-primitive-ownership.md -->
<!-- include: snippets/implementer/windows-path-tests.md -->
<!-- include: snippets/implementer/comment-budget.md -->
<!-- include: snippets/implementer/reviewer-skip-conditions.md -->
<!-- include: snippets/implementer/load-bearing-leftovers.md -->
<!-- include: snippets/implementer/independent-review-measures.md -->

<!-- include: snippets/implementer/anchored-rules-worker-parity.md -->

## Per-dispatch payload
- Task: `<TASK-ID>`
- Spec: `<SPEC-PATH>` (canonical; deviations require design-subagent re-spawn per `feedback_spec_pattern_authority`)
- File scope: `<FILE-SCOPE>` (stay disjoint with sibling implementers)
- PR type: `<PR-TYPE>`
- Migration number (if schema): pinned in dispatch prompt, never picked by implementer (`feedback_migration_number_lock`).

## Definition of done
- [ ] worktree branch, not primary
- [ ] failing test landed first (commit log shows it)
- [ ] `make pre-push-check` green locally
- [ ] reviewer subagent cleared OR auto-skip condition met
- [ ] release-notes fence present
- [ ] no banned phrases
- [ ] no signatures
- [ ] memory rules cited
- [ ] worktree removed after merge (`CLAUDE.md` §Worktree discipline)

<!-- include: snippets/implementer/recurring-traps.md -->
<!-- include: snippets/shared/recurring-traps-shared.md -->
```

Resolver rule: any line matching `^<!-- include: (snippets/[^ ]+\.md) -->\s*$` is replaced by the file contents (no trailing newline trim). All other lines pass through verbatim. The resolver is a 15-line awk/sh script (see §3.5). Order matters: snippets concatenate in include-directive order; the manifest is the single source of truth for ordering.

The `## Variables`, `## Per-dispatch payload`, and `## Definition of done` blocks stay in the manifest because they are role-specific scaffolding, not reusable rules.

### §3.3 Snippet shape

Every snippet starts with a `##` heading matching the block title in today's monolithic template. Example — `snippets/shared/no-signatures.md`:

```markdown
NO SIGNATURES
- No `Co-Authored-By`, no AI footer, no "Generated with" tags. Anywhere. Per `feedback_no_signatures`.
```

(Note: today's templates use ALL-CAPS section headers inline rather than `##` — the snippet preserves the same shape to satisfy the byte-equal acceptance gate. §3.5.)

### §3.4 Snippet hierarchy

Promotion rule: a rule moves from `snippets/<role>/` to `snippets/shared/` when ≥2 role manifests would include it. Demotion rule: a `snippets/shared/` rule moves back to role-specific if only one role still includes it. Maintained by `scripts/check-snippet-hierarchy.sh` (new, advisory only — no CI failure, just a `make check`-emitted warning when a `shared/` snippet is referenced by exactly one manifest).

Initial promotions (≥2 roles reference):
- `worktree-preamble.md` — implementer, reviewer, designer (3 roles).
- `no-signatures.md` — all 4 roles.
- `doc-check.md` — implementer, designer.
- `release-notes-fence.md` — implementer, designer (reviewer references inside its recurring-traps).
- `self-host-filter.md` — designer, triage.
- `memory-cites.md` — implementer, designer.
- `adversarial-review.md` — implementer, designer.
- `pr-body-file-flag.md` — referenced in all 4 templates' RECURRING-FAILURE TRAPS.

Triage stays standalone for ROLE / VERDICTS / OUTPUT FORMAT (no overlap with the other 3).

### §3.5 Resolver + byte-equal acceptance gate

New script: `scripts/render-template.sh <template.md>` — emits the resolved template to stdout. Implementation (≤25 LOC of POSIX sh + awk):

```sh
#!/bin/sh
# render-template.sh — resolve <!-- include: snippets/... --> directives.
set -eu
root="$(dirname "$1")"
awk -v root="$root" '
  match($0, /^<!-- include: (snippets\/[^ ]+\.md) -->[[:space:]]*$/, m) {
    path = root "/" m[1]
    while ((getline line < path) > 0) print line
    close(path)
    next
  }
  { print }
' "$1"
```

New gate: `scripts/check-template-render.sh` — for each of `{implementer,reviewer,designer,triage}.md`, runs the resolver and compares the output against a per-role golden file checked in at `docs/engineer/dispatch-templates/_render/<role>.golden.md`. Golden files are captured ONCE at slice-1 from the pre-split monoliths (byte-equal). On every PR, `scripts/check-template-render.sh` re-renders and `diff -u` against the golden — non-zero exit fails CI. Wired into `make check`.

Acceptance gate (byte-equal pre/post):
```
git show origin/main:docs/engineer/dispatch-templates/implementer.md > /tmp/pre.md
scripts/render-template.sh docs/engineer/dispatch-templates/implementer.md > /tmp/post.md
diff -u /tmp/pre.md /tmp/post.md   # must be empty
```

Per CLAUDE.md §CI gates "Byte-equal-refactor pin" (closes #985): any refactor whose correctness story is "concat is byte-equal pre/post" MUST ship a mechanical drift gate. `scripts/check-template-render.sh` is that gate; this is a hard requirement, not a nice-to-have.

### §3.6 Operator-override path

Operators occasionally need to paste a one-off override into a single dispatch (e.g. "for this task, skip the comment-density check because the file is a generated stub"). Today they edit the monolithic template in their head and paste a modified copy.

Post-split: the manifest's per-dispatch payload section is preserved as the override surface. Operator adds a free-text block under `## Per-dispatch payload`:

```markdown
## Per-dispatch payload
- Task: foo-W3-T1
- ...

### Override (this dispatch only)
- Skip COMMENT BUDGET density gate; file is auto-generated and ≥30% comment-density by design. Justify in PR body with `<!-- comment-density-justified: ... -->`.
```

The override block is plain markdown; the resolver does not interpret it. Operators paste manifest + overrides verbatim into the subagent dispatch. No new mechanism, no new format.

### §3.7 Missing-snippet fallback

If a `<!-- include: snippets/<missing>.md -->` directive references a non-existent file, the resolver MUST fail loud: exit 1 + emit `render-template: missing snippet: snippets/<missing>.md` to stderr. `scripts/check-template-render.sh` propagates the failure to CI.

Rejected alternative: silent skip OR substitute a default placeholder. Both make broken manifests survive merge and surface later as missing rules at dispatch time. Loud-failure is the only safe default — per `feedback_root_cause`, fix the manifest, not the resolver.

The same rule covers typos in the include path. Adversarial review §6 lens 4 catches accidental snippet deletions.

---

## §4 Slices (implementer brief, 3-slice)

Each slice is independently shippable and PR-mergeable. File-disjoint where possible; serial where the resolver/gate is the dependency.

### §4.1 Slice 1 — snippet directory + resolver + golden capture

Owner: implementer.
File scope:
- ADD `docs/engineer/dispatch-templates/snippets/` (empty directory + `.gitkeep` initially).
- ADD `scripts/render-template.sh` (≤25 LOC POSIX sh + awk per §3.5).
- ADD `scripts/check-template-render.sh` (≤20 LOC; loops over the 4 roles, diffs against golden).
- ADD `docs/engineer/dispatch-templates/_render/{implementer,reviewer,designer,triage}.golden.md` — golden capture of CURRENT monolith content (verbatim).
- EDIT `Makefile.d/ci.mk::check` — append `check-template-render` to the `check` target.
- EDIT `CLAUDE.md` §CI gates list — add `check-template-render` to the `make check` enumeration.

TDD order:
1. RED commit: `scripts/check-template-render.sh` exists, exits 0 when manifests are empty resolver-passthrough (no `<!-- include -->` directives yet — manifest ≡ golden today is byte-equal). Add a failing test that asserts the gate fails when a snippet is missing.
2. GREEN: implement resolver + gate so the failing test passes.
3. No snippet extraction in this slice. Monoliths still live; goldens equal monoliths byte-for-byte; gate is a no-op against current state (passes).

Definition of done:
- [ ] `make check` runs `check-template-render` and passes against current `main`.
- [ ] failing test for missing-snippet detection landed first.
- [ ] no behavior change for agents reading the templates.

### §4.2 Slice 2 — extract rules per-role into snippets

Owner: implementer.
File scope:
- ADD all `snippets/shared/*.md` + `snippets/{implementer,reviewer,designer,triage}/*.md` per §3.1.
- EDIT `docs/engineer/dispatch-templates/{implementer,reviewer,designer,triage}.md` — replace the inline rule prose with `<!-- include: snippets/... -->` directives.
- KEEP `docs/engineer/dispatch-templates/_render/*.golden.md` unchanged.

TDD order:
1. Extract one snippet (e.g. `snippets/shared/no-signatures.md`) + edit one manifest (e.g. `implementer.md`) to include it. Run `scripts/check-template-render.sh` — must pass (byte-equal because the extracted snippet contains exactly the prose that lived inline).
2. Iterate snippet-by-snippet across all 4 roles. Each step keeps the byte-equal gate green.
3. End state: every manifest is a short outline of includes + per-dispatch scaffolding; no inline rule prose.

Acceptance:
- `diff -u <(scripts/render-template.sh docs/engineer/dispatch-templates/implementer.md) docs/engineer/dispatch-templates/_render/implementer.golden.md` must be empty. Same for the other 3 roles.

Definition of done:
- [ ] all snippets created per §3.1.
- [ ] all 4 manifests reduced to includes + per-dispatch scaffolding.
- [ ] `make check` green; byte-equal acceptance gate green.
- [ ] CLAUDE.md "Pointers" / `docs/engineer/pointers.md` cross-refs unchanged (the manifest filenames stay the same).

### §4.3 Slice 3 — drop monoliths, finalize hierarchy

Owner: implementer.
File scope:
- DELETE `docs/engineer/dispatch-templates/_render/*.golden.md` — replace with a regenerated `${ROLE}.golden.md` produced by re-rendering each manifest. The golden is now the resolver's output, not a snapshot of the pre-split monolith.
- EDIT `scripts/check-template-render.sh` — after deletion, the gate's role flips: it now confirms that the resolver is deterministic (rendering twice produces identical output). Acceptance is "renders without error + no missing snippets" rather than "matches a pre-split snapshot". Implementation: render to a temp file + diff against a second render in the same run.
- ADD `scripts/check-snippet-hierarchy.sh` (advisory) — emits a warning when a `snippets/shared/` snippet is referenced by exactly one manifest. Wired into `make check` (warn-only).
- EDIT `docs/engineer/pointers.md` if cross-refs reference template line numbers (they should not; pointer entries should be by section heading).

TDD order:
1. RED: stub `scripts/check-snippet-hierarchy.sh` failing on a synthetic `snippets/shared/foo.md` that no manifest includes.
2. GREEN: implement the advisory check.
3. Drop the pre-split goldens; flip the gate's role to determinism.

Definition of done:
- [ ] pre-split goldens deleted.
- [ ] `scripts/check-template-render.sh` validates determinism + no missing snippets.
- [ ] `scripts/check-snippet-hierarchy.sh` advisory wired in.
- [ ] each manifest still reads coherently top-to-bottom (operator UX check — read each one start to finish, confirm no orphaned scaffolding).

---

## §5 Acceptance criteria

- **Byte-equal pre/post** (slice 2 gate): `scripts/render-template.sh docs/engineer/dispatch-templates/<role>.md` matches the pre-split monolith for each of the 4 roles at slice-2 merge. Enforced by `scripts/check-template-render.sh` in `make check`. Per CLAUDE.md §CI gates "Byte-equal-refactor pin".
- **One rule per file**: every `snippets/**/*.md` contains exactly one rule block (one top-level title + its prose). Hierarchy check (advisory) catches drift.
- **Manifest stability**: the 4 template filenames + their on-disk locations are unchanged from pre-split. Operators + autonomous-loop dispatch flows (`internal/orchestrator/spawner/claude.go::defaultPromptBuilder`) reference templates by these exact paths.
- **Worker-prompt parity preserved**: `snippets/implementer/anchored-rules-worker-parity.md` keeps the `feedback_*` slug list verbatim. `scripts/check-prompt-parity.sh` already greps for these slugs; the resolver renders the slug list back into the manifest at read time. Parity gate continues to pass without modification.
- **No new tooling beyond resolver + gates**: no Mustache, no Jinja, no template engine. Pure markdown + comment-directive resolution.
- **Missing-snippet = loud failure**: `scripts/render-template.sh` and `scripts/check-template-render.sh` exit non-zero on a missing snippet reference.

Reopen-trigger: if ≥3 PRs in one session land on the same individual snippet file, the snippet is the new god-file — split it further. Same predicate as `feedback_cascade_rebase_root_cause`.

---

## §6 Adversarial pass (run before slice 1 dispatch)

Adversarial reviewer findings (pre-empted, addressed inline):

1. **Snippet-order matters — concatenation order is load-bearing**.
   The manifest's include-directive order IS the source of ordering. Resolver concatenates in declared order. Lens: if a future PR alphabetizes the manifest by accident, rule presentation breaks (e.g. `## Comments: zero by default` ends up after `## Per-dispatch payload`). Mitigation: `scripts/check-template-render.sh` golden compare (slice 2) catches reordering against pre-split layout. Post-slice-3 the golden flips to determinism-only; operator-UX read-through of each manifest end-to-end is the only ordering check. ACCEPTED RISK: post-slice-3 reordering drift relies on operator review. If this becomes a recurring problem, file a tracker to add a stable-order-manifest pin (e.g. block reorderings via per-template `<!-- order-pin: HASH -->` directive comparing to a checked-in hash).

2. **Missing snippet falls back to bundled default?** — REJECTED. §3.7 establishes loud-failure as the only safe default. A bundled-default fallback masks broken manifests and surfaces missing rules at dispatch time when the operator is already mid-session. Loud-failure forces the fix in the PR that broke the manifest.

3. **Operator-override path preserved?** — Yes, §3.6. Overrides live in the per-dispatch payload block, which is manifest-local (not snippet-local). Override prose is free-form markdown; resolver passes it through verbatim.

4. **What if a snippet is accidentally deleted?**
   `scripts/check-template-render.sh` fails closed (§3.7). PR cannot merge. Same gate catches typos in include paths.

5. **Cross-template prose drift on shared rules?**
   Slice 2 promotes 8 cross-cutting rules to `snippets/shared/`. After promotion, the rule has ONE source-of-truth file; cross-template drift becomes impossible by construction. `scripts/check-prose-dup.sh` (existing) keeps an eye on unintentional duplication.

6. **Resolver is a new shell script — bug-surface added.**
   25 LOC of awk. Add a fixture test under `scripts/testdata/render-template/` with: (a) a minimal manifest + snippet, (b) a missing-snippet manifest (expects exit 1), (c) a manifest with no includes (expects passthrough). Tests run in `make check` via `bats` or plain shell. Test path: `scripts/render-template_test.sh`. Mechanical, low-risk. Per `feedback_default_simpler`: prefer 25 LOC of awk over a Go binary; reopen-trigger is "resolver edits ≥3 in one quarter".

7. **Why not generate the templates from snippets at build time + commit the rendered output?**
   Rejected: two source-of-truth files diverge silently when an operator edits the rendered file directly (forgets the snippet was the source). Manifest-as-source + on-read resolution is the simpler invariant. Per `feedback_default_simpler`.

8. **Why not just split the 4 templates into 4 files per role instead of N snippets?**
   That is what they already are — one file per role. The problem is intra-role rule churn AND cross-role rule duplication. Further sub-splitting only helps if rules are the unit of mutation, which they are.

9. **Reviewer 9-lens block is one heading but contains 9 sub-rules — split into 9 snippets or 1?**
   Split into 9 (`snippets/reviewer/lenses-{1..9}-<name>.md`). Lens 9 (comment sweep) was the most-edited block in the last 30 days — exactly the rule-churn pattern this spec exists to fix.

10. **What about `## Variables` and `## Definition of done` blocks — do they move into snippets too?**
    No. Both are role-specific scaffolding tied to the per-dispatch contract. They stay in the manifest. The spec is explicit about this in §3.2.

11. **The recurring-traps section grew 2x last month — what's the read-time impact when 4 trap snippets stack?**
    Per role, the trap snippet is one file (e.g. `snippets/implementer/recurring-traps.md`). Cross-role shared traps (e.g. `pr-body-file-flag` appears in every role's traps) move to `snippets/shared/recurring-traps-shared.md`. Net: per-role read length stays comparable; cross-role duplication drops to zero.

---

## §7 Out of scope (Phase X / deferred)

- **Template render at dispatch time inside the harness**. Today the operator pastes the resolved template into a subagent dispatch by hand (or the orchestrator does it on their behalf). Auto-rendering at dispatch invocation is a Phase-X concern — reopen when (a) external contributors land regularly, OR (b) the manual paste step itself becomes a friction point. Out of scope here.
- **Per-snippet metadata (owner, last-reviewed date, criticality tier)**. Tempting but speculative. Add only if `scripts/check-snippet-hierarchy.sh` warnings prove insufficient at catching stale rules. Per `feedback_default_simpler` + `feedback_recognize_session_end`.
- **Generated table-of-contents** at the head of each manifest. Markdown auto-rendering on GitHub handles the rendered output; pre-render TOC is cosmetic.
- **Replacing the markdown include directive with a YAML-frontmatter mechanism**. Frontmatter is heavier; rejected per `feedback_default_simpler`.

---

## §8 Risks + mitigations

| Risk | Tier | Mitigation |
| --- | --- | --- |
| Snippet reorder breaks rule presentation | Med | Slice 2 byte-equal gate against pre-split golden. Post-slice-3: operator read-through + reopen-trigger. |
| Missing snippet ships to main | High | `scripts/check-template-render.sh` fails closed (§3.7). |
| Cross-role drift on shared rule | Low | `snippets/shared/` collapses N copies to 1 file by construction. |
| Resolver bug surfaces a wrong rule body | Low | 25 LOC of awk + fixture tests under `scripts/testdata/render-template/`. |
| Slice 2 produces a 30-PR sprawl as each snippet extraction goes through review | Med | Slice 2 is ONE PR, not N. The implementer extracts all snippets in one branch; byte-equal gate is the correctness invariant. Per `feedback_design_iteration_local`. |
| Manifest comment-directive (`<!-- include: ... -->`) collides with an existing comment-directive parser | Low | None known in repo today. `scripts/doc-check.sh` already passes over these comments transparently. |
| Worker-prompt parity gate breaks because anchored-rules slug list got hidden inside a snippet | High | Anchored-rules snippet is included verbatim in the implementer manifest. `scripts/check-prompt-parity.sh` already greps `internal/orchestrator/spawner/claude.go` against the implementer manifest's resolved output — render before grep. Slice 1 updates `check-prompt-parity.sh` to invoke `scripts/render-template.sh implementer.md` before its existing grep. |

---

## §9 Memory rules referenced

`feedback_decision_priority`, `feedback_default_simpler`, `feedback_cascade_rebase_root_cause`, `feedback_deletion_default`, `feedback_research_design_principles`, `feedback_adversarial_review`, `feedback_spec_pattern_authority`, `feedback_no_signatures`, `feedback_unaddressed_load_bearing`, `feedback_dispatch_brief_only`, `feedback_design_iteration_local`, `feedback_root_cause`, `feedback_recognize_session_end`.

---

## §10 Pointer updates

- `docs/engineer/pointers.md` — confirm dispatch-template entries reference filenames (not line numbers). No update expected.
- `CLAUDE.md` §CI gates — append `check-template-render` to the `make check` enumeration (slice 1).
- `CLAUDE.md` §Pointers — no change (still points to `docs/engineer/dispatch-templates/{implementer,reviewer,designer,triage}.md`).
