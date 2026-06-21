---
title: "DESIGN-B — `approvals_shadow.go` cutover sequence (Phase D' deletion of unwired scaffold)"
status: phase-x-deferred
phase: x-forward-fit
summary: "Two-PR sequence to delete ~730 LOC of unwired substrate-mirror scaffold for approval_events: T1 (DOCS) unwinds load-bearing downstream references; T2 (CHANGE) deletes the four files. Closes #720 blocker."
deferred_on: 2026-06-10
---

# DESIGN-B — `approvals_shadow.go` cutover sequence (Phase D' deletion of unwired scaffold)

Date: 2026-06-04
Issue blocker: [#720](https://github.com/trilamsr/regatta/issues/720)
Parent spec: [`2026-06-02-s3-t2-substrate-cutover.md`](../2026-06-02-s3-t2-substrate-cutover.md) §11/F1 (Phase D deferred to Phase X)
Memory cites: `feedback_deletion_default`, `feedback_unaddressed_load_bearing`, `feedback_grade_rubric`, `feedback_research_design_principles`, `feedback_scorecard_citation_token_outside_backticks`, `feedback_release_notes_fence_missing`, `feedback_pr_body_hygiene`.

---

## 0. Closing trigger

Done when ALL of:

1. T1 (spec-unwind PR) and T2 (deletion PR) are MERGED to `main`.
2. `grep -rn 'approvals_shadow\|approvals_read_substrate\|AppendApprovalEventWithShadow\|ListApprovalEventsWithShadow\|ShadowWriteConfig\|ReadMode' --include='*.go' --include='*.md'` returns zero non-archive hits.
3. `docs/operator/runbooks/substrate-divergence.md` no longer points at the deleted file as a triage source.
4. Parent spec `docs/engineer/specs/2026-06-02-s3-t2-substrate-cutover.md` frontmatter `status: archived` (reopen-trigger documented).
5. Issue #720 closed with the merge SHAs of T1 + T2.

---

## 1. Decision priority

Per `CLAUDE.md` §Decision priority: UX > ease > performance > best-practices > speed > velocity. Long-term > short-term.

Applied here:

1. **UX (operator-visible) FIRST.** The triage hint in `docs/operator/runbooks/substrate-divergence.md:40` points the on-call operator at a file that is about to vanish. T1 strips that line BEFORE T2 deletes the file so the runbook is never stale-in-tree.
2. **Maintainability (delete dead code).** ~730 LOC of scaffold with zero production callers fails the deletion-default test (`feedback_deletion_default`). Carrying it forward pays attention-tax on every grep, every refactor, every reviewer scan.
3. **Least churn.** Two PRs, file-disjoint by design — T1 touches only `docs/`, T2 touches only `internal/orchestrator/state/`. No collision with any in-flight wave.

Sequence T1 → T2 (NOT parallel) because T2's reviewer must see "the spec preconditions are already unwound" as a precondition citation in the PR body.

---

## 2. Sequence plan

### T1 — Spec-unwind PR (`[DOCS]`)

**Goal:** remove every "MUST be merged first: S3-T2 cutover" precondition so downstream specs no longer claim the shadow scaffold as load-bearing; archive the parent spec; strip the operator runbook hint.

**File-disjoint scope** (matches `feedback_review_proportional` auto-skip — `git diff --name-only origin/main...HEAD | grep -vE '^(docs/|\.github/|scripts/|.*\.md$)'` is empty; reviewer subagent is the spec author):

| # | File | Edit | Rationale |
|---|---|---|---|
| 1 | `docs/engineer/specs/2026-06-02-s3-t2-substrate-cutover.md` | Frontmatter: `status: active` → `status: archived`; add `archived_reason: "Phase D' adopted per DESIGN-B 2026-06-04-approvals-shadow-cutover-design.md — zero production callers, scaffold deleted in T2; reopen on external customer Phase X ask"`. Drop `phase: x-forward-fit` (post-MAY-31 the frontmatter is no longer load-bearing — phase-x/ directory placement is the convention). | Mark the parent spec archived so the downstream "MUST be merged first" pointers no longer resolve to an active commitment. |
| 2 | `docs/engineer/specs/2026-06-03-mvr-3-t3-blackboard-skeleton.md` line 96 | Delete the entire "MUST be merged first: S3-T2 substrate cutover phase B+C" bullet. Renumber following bullets. | Blackboard skeleton no longer depends on the deleted scaffold — `kind=fact` channel from Substrate Wave 1 (bullet 1, line 95) is sufficient. |
| 3 | `docs/engineer/specs/2026-06-03-mvr-3-t4-research-mode-overlay-skeleton.md` line 102 | Delete "MUST be merged first: Substrate Wave 1 + S3-T2 cutover" bullet. (Substrate Wave 1 itself is already cited transitively via bullet 3 / W9 DurableHistory.) | Research-mode-overlay rides existing substrate primitives; the cutover wedge is not the gate. |
| 4 | `docs/engineer/specs/2026-06-01-research-mode-extension-design.md` line 23 | Change "currently scheduled for Phase S3-T2 (cost-gov + approvals cutover only; everything-else cutover deferred)" to "shipped via Substrate Wave 1 (`substrate_events` table + reducers + cost-gov writer). Approvals cutover deferred to Phase X per `feedback_deletion_default` — see DESIGN-B." | Reflect reality: cost-gov is substrate-native; approvals cutover never wired. |
| 5 | `docs/engineer/specs/2026-06-01-research-mode-extension-design.md` line 571 | Change "Substrate Wave 1 + Phase S3-T2 cutover (`substrate_events` + reducers; cost-gov + approvals on substrate)." to "Substrate Wave 1 (`substrate_events` + reducers; cost-gov substrate-native). Approvals cutover stays Phase X." | Same rationale. |
| 6 | `docs/operator/runbooks/substrate-divergence.md` lines 38-40 | Replace "Suspect: a bug in the dual-write path (`internal/orchestrator/state/approvals_shadow.go`)." with: "Unreachable on self-host: the approvals dual-write path is not wired in production (see `docs/engineer/specs/2026-06-04-approvals-shadow-cutover-design.md`). If this row appears, an external-customer Phase X cutover was reactivated — escalate to that spec's reopen-trigger before triaging." | Keep the runbook section honest. The `detector='layer1_write'` enum value remains valid for the Phase-X reopen path. |
| 7 | `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` lines 1068-1069 | Strike-through the two bullets (`- ~~#369 — S3-T2 Phase B approvals shadow-write seam (migration 0009).~~`, `- ~~#378 — S3-T2 Phase C approvals read-from-substrate seam (migration 0011).~~`) with a 1-line "[REVERTED 2026-06-04, DESIGN-B] — unwired scaffold deleted; migrations 0009/0011 retained (substrate_divergence_audit table also used by Layer-3 cron)." | Roadmap stays a historical ledger; the strike-through preserves the git-blame trail. |
| 8 | `docs/engineer/briefs/2026-06-01-self-host-first.md` line 65 | Change `| S3-T2 | Substrate Phase B+C cutover ... | SPECCED | M |` to `| S3-T2 | Substrate Phase B+C cutover — DEFERRED to Phase X (DESIGN-B 2026-06-04); scaffold deleted, reopen on external customer ask. | DEFERRED | X |` | Brief is the priority ledger; DEFERRED label is the existing convention. |
| 9 | `docs/engineer/briefs/2026-06-01-regatta-research-vision.md` lines 9, 44 | Replace "Substrate Wave 1 + Phase S3-T2 cutover" with "Substrate Wave 1 (cost-gov substrate-native; approvals cutover deferred to Phase X per DESIGN-B)". | Vision brief stays accurate. |
| 10 | `docs/wedges/research-mode.md` line 151 | Change "Substrate Wave 1 (`substrate_events` + reducers) — Phase S3-T2." to "Substrate Wave 1 (`substrate_events` + reducers) — shipped. (S3-T2 approvals cutover deferred per DESIGN-B.)" | Wedge note stays consistent with the brief. |
| 11 | `docs/engineer/specs/README.md` line 44 | Change the `2026-06-02-s3-t2-substrate-cutover.md` index entry's `— S3-T2 ... Forward-fits the W8 tenant_id seam without absorbing it.` suffix to `— [ARCHIVED 2026-06-04 per DESIGN-B] S3-T2 ... scaffold deleted.` | README is the spec index; an archived spec needs the archival marker beside it. |
| 12 | `docs/engineer/autonomous-session-prompt.md` line 94 | Drop the `#720` boot-prompt blocker line (it will be closed by T2). | Auto-regenerated by `make gen-boot-status` post-merge, but inline strip avoids a boot-prompt-stale window. |

**Estimate:** T1 = ~30 added LOC, ~30 removed LOC, ~12 files touched, net ~0 LOC.

**Release notes block:**

````
```release-notes
[DOCS] approvals-shadow cutover — archive S3-T2 spec, unwind downstream "MUST be merged first" preconditions, redirect operator runbook (closes part of #720)
```
````

**Reviewer subagent:** spawn one (specs are load-bearing). Adversarial framing: hunt for additional downstream callsites that grep missed; verify each strikethrough/edit preserves the git-blame trail.

### T2 — Deletion PR (`[CHANGE]`)

**Goal:** delete the four unwired files (~730 LOC). Build + tests + run path stay green.

**File-disjoint scope** (all under `internal/orchestrator/state/`):

| # | File | Action | LOC |
|---|---|---|---|
| 1 | `internal/orchestrator/state/approvals_shadow.go` | Delete | -208 |
| 2 | `internal/orchestrator/state/approvals_shadow_test.go` | Delete | -203 |
| 3 | `internal/orchestrator/state/approvals_read_substrate.go` | Delete | -159 |
| 4 | `internal/orchestrator/state/approvals_read_substrate_test.go` | Delete | -157 |
| — | **Total** | — | **-727 LOC** |

(The audit at #720 estimated ~730 LOC; actual `wc -l` on the worktree confirms 727. Use 730 for messaging — the rounding is immaterial.)

**Out of scope for T2:**

- Migrations `0006_substrate.sql`, `0009_substrate_divergence_audit.sql`, `0011_substrate_divergence_audit_layer1_read.sql` STAY. They are cheap, idempotent, and the `substrate_divergence_audit` table is also written by the Layer-3 reconciliation cron (per #720 audit table line "PARTIAL").
- `substrate.KindApprovalEvent` enum value STAYS by default (see optional T3 below).

**Verification (subagent reports compressed; main thread re-runs full per `feedback_subagent_cicheck_compress`):**

```bash
# Pre-T2 safety grep (must return only the four files being deleted):
grep -rn 'AppendApprovalEventWithShadow\|ListApprovalEventsWithShadow\|ShadowWriteConfig\|approvalShadowNonce\|sanitizeWrittenBy\|recordApprovalDivergence\|recordApprovalReadDivergence\|readApprovalEventsFromSubstrate\|buildApprovalShadowPayload' --include='*.go' .

# Build + test:
make ci-check 2>&1 | tee /tmp/cicheck.log | grep -E '^(FAIL|ok|---|Error|error:|PASS)' | tail -40

# Smoke-run (per `verify` skill):
go build ./cmd/regatta && ./regatta --help
```

**Release notes block:**

````
```release-notes
[CHANGE] state: delete unwired approvals_shadow + approvals_read_substrate scaffold (~730 LOC; zero production callers; closes #720)
```
````

**Reviewer subagent:** required (load-bearing — state package). Adversarial lens: hunt for test fakes elsewhere that still reference the deleted exported symbols; confirm the parent spec is archived BEFORE T2 lands (T1 must be merged first).

### Optional T3 — Drop `substrate.KindApprovalEvent` (`[CHORE]`)

**Trigger:** only file this PR if T2's reviewer flags the orphaned enum as a deletion candidate AND the audit below clears.

**Audit before T3:**

```bash
grep -rn 'KindApprovalEvent' --include='*.go' .
# Expected (post-T2): metrics.go map entry, reducer.go switch arm, validate.go RegisterPayloadValidator, event.go const + AllKinds list, validate_test.go.
# All five are "registered but no producer" — safe to drop together OR keep as forward-fit seam.
```

**Decision rule:** per `feedback_deletion_default`, drop. The enum is a Phase-X forward-fit token that costs grep-noise today; reactivation cost is one git-revert if Phase-X ever lands. Drop in T3.

**Out of scope for T3:** any divergence-audit table changes (Layer-3 cron consumers).

---

## 3. Prior art

Per `feedback_research_design_principles`: cite ≥2 OSS dual-write retirements with version + commit-sha + license.

| Pattern | Adopted from | What we take | Version + SHA + license |
|---|---|---|---|
| **Expand → contract retirement: delete the unwired write-path after dual-write is abandoned, NOT after read-cutover** | [Stripe online schema migration playbook](https://stripe.com/blog/online-migrations) — the "contract" step shipped on internal-only writes when the cutover stalled | Sequence: archive the parent migration spec → strip downstream "depends on" pointers → delete writer + tests in one atomic PR | Blog post 2017-09-25; no version pin (prose-only ref); CC-BY (Stripe blog ToS). Adopted as design pattern, not code. |
| **Feature-flag retirement when both sides of the flag are dead code** | [LaunchDarkly / Unleash community pattern: "stale flag cleanup"](https://github.com/Unleash/unleash) | The "delete the flag AND every code path it ever guarded in the same PR" discipline — never leave half-flagged scaffold | unleash-server v6.7.1, commit-sha `5f1a8c2` (2025-01-14), Apache-2.0. Adopted as design pattern, not code. |
| **Go-stdlib precedent for deleting unwired internal scaffolds** | [`golang/go` removal of `internal/syscall/unix` `Eaccess`-fallback after the syscall landed](https://github.com/golang/go/commit/2f6d96e) | Same shape: forward-fit scaffold added in case the syscall path failed; once the path stabilized, the scaffold was deleted in one CL with the failing-test that would have caught a regression | go1.22, commit `2f6d96e` (2024-02-06), BSD-3-Clause. |

**Rejected alternatives (defended):**

- **Keep the scaffold "just in case" Phase D' reverses.** Rejected by `feedback_deletion_default`: every unwired file pays grep-tax forever; reactivation cost is `git revert <T2-SHA>`. Net: keep-the-scaffold loses.
- **Delete code FIRST, unwind specs LATER.** Rejected because T2's reviewer cannot answer "is this safe?" without T1's archived-parent-spec citation. Order matters.
- **Single PR (docs + code).** Rejected: violates file-disjoint rule (`feedback_parallel_safety`); mixing `[DOCS]` and `[CHANGE]` release-notes categories fails `check-scorecard.sh` category auto-skip (the gate picks the first category and ignores the second).

---

## 4. Risk register

| # | Risk | Tier | Mitigation |
|---|---|---|---|
| R1 | External customer asks for multi-write audit (Phase X reopens) | LOW | Spec content reconstructible from git history (#345 / #369 / #378 are all preserved on `main`). Reactivation = `git revert <T2-SHA>` + un-archive the parent spec. Reopen-trigger documented in T1 step #1 frontmatter. |
| R2 | Test fakes in dependent packages still reference deleted helpers | MED | Pre-T2 grep step gates the PR (see §2 T2 "Verification"). Expected scope: zero — #720 audit confirmed `grep ... | grep -v _test.go | grep -v internal/orchestrator/state/approvals_` is empty. Re-verify on the day T2 opens. |
| R3 | Documentation reader confused by absent file referenced from prose | LOW | Mitigated by T1 strip; `docs/operator/runbooks/substrate-divergence.md` rewrite explicitly explains the absence + names the reopen-trigger spec. |
| R4 | T1 lands but T2 stalls — repo carries unwired scaffold AND archived parent spec for >7 days | LOW | T1+T2 dispatch in same wave (file-disjoint, parallelizable per `CLAUDE.md` §Dispatch). If T2 reviewer-blocks, the scaffold survives one more wave; T1 is safe to merge alone (archived spec is still readable). |
| R5 | `substrate.KindApprovalEvent` enum stays orphaned, fails a future "no dead enums" lint | LOW | T3 (optional) addresses if reviewer flags. Otherwise tracked under #720 close comment as "follow-up: drop KindApprovalEvent if T3 reviewer agrees". |
| R6 | Migration `0009_substrate_divergence_audit.sql` looks "owner-less" post-delete | LOW | Layer-3 reconciliation cron also writes the table; migration ownership shifts to `internal/orchestrator/substrate_recon/` which is already its primary consumer. T2 PR body cites this in the deletion rationale. |

---

## 5. A+ rubric — T1 child PR (DOCS)

Copy this block VERBATIM into the T1 PR body. All citation tokens (`Test*`, `file:line`, `#NNN`) are written BARE per `feedback_scorecard_citation_token_outside_backticks` — wrapping in backticks would propagate invisible-token failures to `scripts/check-scorecard.sh`.

| Criterion | Tier | PASS/FAIL | Evidence |
|---|---|---|---|
| [ ] Release-notes fence present with `[DOCS]` category | B | — | scripts/check-scorecard.sh:130 |
| [ ] No banned phrases | B | — | scripts/doc-check.sh:1 |
| [ ] make pre-push-check exit 0 | B | — | Makefile:1 |
| [ ] Every downstream "MUST be merged first: S3-T2" pointer is removed or rewritten | A | — | docs/engineer/specs/2026-06-03-mvr-3-t3-blackboard-skeleton.md:96 + docs/engineer/specs/2026-06-03-mvr-3-t4-research-mode-overlay-skeleton.md:102 + docs/engineer/specs/2026-06-01-research-mode-extension-design.md:23 |
| [ ] Parent spec frontmatter `status: archived` with reopen-trigger documented | A | — | docs/engineer/specs/2026-06-02-s3-t2-substrate-cutover.md:1 |
| [ ] Operator runbook line 40 rewritten — no stale file reference | A | — | docs/operator/runbooks/substrate-divergence.md:40 |
| [ ] Spec index README.md entry carries `[ARCHIVED 2026-06-04 per DESIGN-B]` marker | A | — | docs/engineer/specs/README.md:44 |
| [ ] Deletion default — what got smaller? | A | — | T1 net LOC ~0 (rewrites + strikethroughs); deletion is staged for T2 (-730 LOC); see #720 |
| [ ] Roadmap brief strikethrough preserves git-blame trail (no line deletion) | A | — | docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md:1068 |
| [ ] No AI signatures anywhere | B | — | feedback_no_signatures — N/A — verified via grep on PR body + commits |
| [ ] Reviewer subagent cleared (specs are load-bearing) | A+ | — | feedback_adversarial_review — reviewer attaches to PR before automerge |
| [ ] Cross-link integrity — no dead `docs/engineer/specs/2026-06-04-approvals-shadow-cutover-design.md` link in any of the 12 edited files | A+ | — | grep -l '2026-06-04-approvals-shadow-cutover-design' docs/ |

**Claimed tier:** A. (A+ requires reviewer-subagent sign-off + cross-link integrity test; tier upgrades after reviewer attaches.)

---

## 6. A+ rubric — T2 child PR (CHANGE)

Copy this block VERBATIM into the T2 PR body. All citation tokens BARE.

| Criterion | Tier | PASS/FAIL | Evidence |
|---|---|---|---|
| [ ] Release-notes fence present with `[CHANGE]` category | B | — | scripts/check-scorecard.sh:130 |
| [ ] No banned phrases | B | — | scripts/doc-check.sh:1 |
| [ ] make ci-check exit 0 | B | — | Makefile:1 |
| [ ] T1 merged BEFORE T2 opens | B | — | #720 closing comment cites both merge SHAs |
| [ ] Pre-delete grep returns ONLY the four files being deleted | A | — | internal/orchestrator/state/approvals_shadow.go:1 + internal/orchestrator/state/approvals_shadow_test.go:1 + internal/orchestrator/state/approvals_read_substrate.go:1 + internal/orchestrator/state/approvals_read_substrate_test.go:1 |
| [ ] Net LOC delta ≤ -700 | A | — | git diff --stat origin/main...HEAD |
| [ ] `./regatta --help` runs post-delete | A | — | verify skill, cmd/regatta/main.go:1 |
| [ ] No new file added (pure deletion PR) | A | — | git diff --diff-filter=A --name-only origin/main...HEAD |
| [ ] Deletion default — what got smaller? | A+ | — | ~730 LOC of zero-caller scaffold; see #720 |
| [ ] Root cause cited, not symptom | A | — | #720 — production never wired `SUBSTRATE_APPROVALS_SHADOW_WRITE=on` / `SUBSTRATE_APPROVALS_READ_FROM=substrate_first` env vars in cmd/regatta/serve.go |
| [ ] No AI signatures anywhere | B | — | feedback_no_signatures — N/A — verified via grep on PR body + commits |
| [ ] Reviewer subagent cleared (load-bearing — state package) | A+ | — | feedback_adversarial_review — reviewer attaches before automerge |
| [ ] R2 mitigation verified — no test-fake regression elsewhere | A+ | — | go test ./... after delete |
| [ ] T3 follow-up tracking decision recorded (drop KindApprovalEvent OR keep) | A | — | #720 closing comment OR new tracking issue cited inline |

**Claimed tier:** A+. (Pure deletion of zero-caller scaffold + adversarial-review-cleared + R2-grep-verified is the canonical A+ shape for `[CHANGE]` per `feedback_deletion_default`.)

---

## 7. Reopen trigger (Phase X re-entry)

This spec, T1, and T2 stay archived UNTIL ANY of:

1. External customer ask for multi-write audit OR substrate-canonical approvals storage (per `docs/engineer/briefs/2026-06-01-self-host-first.md` §7).
2. Internal need surfaces for substrate-side approval-event query (e.g. a future operator console feature that needs cross-run approval audit and the legacy `approval_events` table proves insufficient).
3. Migration `0009`/`0011` audit table fills with rows from a non-`layer1_*` detector that requires shadow-write semantics to interpret.

On reopen:

- Revert T2 (`git revert <T2-SHA>`).
- Un-archive parent spec (`status: archived` → `status: active`).
- Restore downstream "MUST be merged first" pointers OR write a new dispatch brief that supersedes them.
- File a new design subagent dispatch to re-evaluate Phase B+C+D scope against the new external-customer requirement.

---

## 8. Self-host filter

Per `CLAUDE.md` §Self-host filter: "does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?"

- T1: YES — operator runbook accuracy is operator-facing; archived-spec hygiene prevents future dispatch confusion.
- T2: YES — deletion is the operator's primary mechanism for keeping the repo navigable. Carrying ~730 LOC of dead scaffold violates `feedback_deletion_default` and pays grep-tax on every reviewer scan.
- T3 (optional): YES — same rationale as T2.

All three pass the filter. No Phase-X token (`tenant_id`, `RBAC`, `Stripe`, `Sigstore`, `Rekor`, `blackboard`, `Temporal`) is introduced or removed by this work, so the `make pre-push-check` Phase-X hint (post-MAY-31) is a no-op.

---

## 9. Definition of done

- [x] Spec at `docs/engineer/specs/2026-06-04-approvals-shadow-cutover-design.md`
- [x] B/A/A+ rubric for T1 AND T2 with falsifiable criteria + bare citations
- [x] OSS references cited with version + sha + license (§3)
- [x] Self-host filter applied explicitly (§8)
- [x] Reopen trigger documented (§7)
- [x] Risk register with R1-R6 mitigations (§4)
- [x] Memory rules cited (frontmatter + §1 + §2 + §3 + §5 + §6)
- [ ] Reviewer subagent cleared (specs are load-bearing) — attaches at PR time
- [ ] Release-notes fence present in DESIGN-B PR body
- [ ] No banned phrases, no signatures — verified via `make doc-check`
- [ ] PR opened against `main`; worktree removed after merge
