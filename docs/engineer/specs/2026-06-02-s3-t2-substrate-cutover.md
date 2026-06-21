---
title: "S3-T2 — substrate Phase B+C cutover (approvals only)"
status: shipped
shipped_at: 2026-06-04
summary: "Phase S3-T2: shadow-write + read-from-substrate cutover for approvals only. Forward-fits the W8 tenant_id seam without absorbing it."
---

# Spec: S3-T2 — substrate Phase B+C cutover (approvals only)

_Author: design subagent, 2026-06-02. Source: `docs/engineer/briefs/2026-06-01-self-host-first.md` §3 S3-T2. Companion specs: `2026-06-01-unified-substrate-design.md` §3 (the four phases A → B → C → D), `2026-06-01-cost-governor-design.md` §A6 (Phase-B runbook hook). Memory: `feedback_research_design_principles`, `feedback_grade_rubric`, `feedback_pr_body_file_only`, `feedback_test_godoc_one_line`._

Self-host-first cutover: ship Phase B (shadow-write) + Phase C (read-from-substrate, legacy fallback) for the **approvals** state store ONLY. Cost-governor is already substrate-native (`internal/cost/spend/{reader,writer}.go` writes and reads `substrate_events` directly — no legacy table) so the cost-gov half of S3-T2 reduces to **documenting that fact + adding a divergence-check harness** for token_spend continuity; no code cutover required there. Approvals (`approvals` + `approval_events` tables, MVP-2 W1) is the only state store with both a legacy row-mutation table AND a substrate-eligible event stream. The W7 web-UI cutover, the conditional-DAG cutover, the work_item_outputs cutover, and the blackboard cutover are EXPLICITLY OUT OF SCOPE per self-host-first §3.

---

## 1. Prior art adopted (≥2 OSS per `feedback_research_design_principles`)

| Pattern | Adopted from | What we take | Why not bespoke |
|---|---|---|---|
| **Expand → migrate → contract (write-shadow → read-shadow → read-cutover → drop)** | [Stripe's online schema migration playbook](https://stripe.com/blog/online-migrations) | Four named phases; "every release adds one phase, never two"; rollback at any phase = revert one flag flip | Stripe ran this on billions of `charges` rows; their four-step framing is the safest known shape for cutover migrations. We use it verbatim (A=create, B=dual-write, C=dual-read, D=drop) |
| **Read-shadow comparison + divergence alerting** | [Datadog "shadow read" pattern](https://www.datadoghq.com/blog/engineering/database-migration-without-downtime/) | Phase C variant: every read returns the legacy answer to callers AND simultaneously runs the substrate-side read; a comparator emits a metric on divergence | Datadog migrated their write-heavy timeseries metadata this way. The pattern catches read-path bugs (missing indexes, fold semantics) without exposing them to callers — exactly what self-host single-operator needs because the operator IS the integration test |
| **GitHub's "trickle-cutover" via flag fan** | [GitHub's MySQL → Vitess migration](https://github.blog/2021-09-27-partitioning-githubs-relational-databases-scale/) | Phase B per-callsite opt-in (one writer at a time, not all-or-nothing) so a buggy callsite doesn't poison the substrate mirror | GitHub cut over thousands of callsites this way without taking outages. Mirrors our self-host-first need: one approval flow site at a time, no fleet-wide blast radius |
| **Independent-transaction shadow-write** | Substrate W1 spec §3 (this repo) | Substrate write is a SEPARATE tx from the legacy write — no atomic dual-write — reconciliation handles partial failure | Already proven in substrate W1; we inherit the contract |

**Rejected alternatives (defended):**
- **Atomic dual-write inside one tx** — was rejected by substrate W1 spec §3 R2 (sqlite WAL forbids nested tx; refactor crosses 3+ packages). Not revisited.
- **Trigger-based mirror** (sqlite trigger copies `approval_events` → `substrate_events`) — rejected because the substrate row needs HMAC signing that pulls a per-process keyring; triggers run in sqlite's session, not Go's, and have no keyring access. Application-layer shadow-write is the only signing-correct shape.
- **Big-bang cutover** (skip Phase B entirely, flip Phase C in one PR) — rejected because Phase B's divergence-check is the ONLY mechanism that catches fold-semantics bugs before they hit the read path. Skipping Phase B forfeits the safety net.

---

## 2. Scope — exact stores being cutover

### 2.1 In scope (this spec)

| Store | Legacy shape | Substrate shape | Phase before this PR | Phase after this PR |
|---|---|---|---|---|
| `approvals` (mutable row per approval) | `migrations/0004_approvals.sql` lines 11-30: PK `id`, status mutates pending→approved\|rejected\|timed_out, decided_at + decided_by mutate on decision | DERIVED via `fold(substrate_events WHERE kind=approval_event AND payload.approval_id=?)` — the row IS the latest-state projection of its event stream | A (substrate writes nothing) | C (substrate read primary; legacy row fallback) |
| `approval_events` (append-only history) | `migrations/0004_approvals.sql` lines 43-59: AUTOINCREMENT id, FK to approvals, kind enum, token_jti for single-use callback | `substrate_events` rows with `kind='approval_event'`; payload `{approval_id, transition, actor}` already validated in `internal/orchestrator/state/substrate/validate.go::validateApprovalEvent` | A | C |

### 2.2 Out of scope (NOT this spec — per self-host-first §3)

| Store | Why deferred |
|---|---|
| `work_item_outputs` (MVP-2 W4) | Self-host doesn't need fold-from-substrate for outputs; markdown adapter + scheduler read the row store; no operator pain. Deferred to Phase X when external buyer asks for replay-from-history |
| `work_item_edges` (conditional DAG) | Same — single-operator self-dispatch doesn't need substrate-backed routing replay |
| `events` (per-agent stdout/stderr log, MVP-1 `0001_initial.sql`) | Already append-only; no row-mutation to cut over. Substrate forward-fit not load-bearing for self-host |
| Blackboard (W11) | Deferred entirely to Phase X per self-host-first §4 |
| W7 UI (htmx admin) | Deferred entirely to Phase X |

### 2.3 Cost-gov — already substrate-native (no cutover work)

`internal/cost/spend/writer.go` writes `kind='token_spend'` rows directly to `substrate_events` via `substrate.AppendEvent` (verified: `internal/cost/spend/writer_test.go:99` reads `FROM substrate_events WHERE kind='token_spend'`). `internal/cost/spend/reader.go:78-82` reads budget state from `substrate_events` exclusively — no legacy `cost_*` or `token_spend_legacy` table exists in `migrations/`. Phase C is therefore already complete for cost-gov by design.

**What this spec adds for cost-gov:** a **divergence-watchdog test** (§5.3) that re-runs the cost-gov reader against a synthetic 1000-row scenario AND asserts the result is byte-identical when computed via two query plans (one direct, one via fold). This is a regression net, not a cutover. No new migration, no new flag.

---

## 3. Phase B mechanics — shadow-write for approvals

### 3.1 The seam

Every approval write today goes through two paths:
- `state.DB.CreateApproval` (mutates `approvals` row) — `internal/orchestrator/state/approvals.go:101`
- `state.DB.AppendApprovalEvent` (appends `approval_events` row) — `internal/orchestrator/state/approvals.go:185`

The Phase B seam is **one wrapper per write** that calls `substrate.AppendEvent` AFTER the legacy write commits. Two independent transactions per substrate W1 spec §3 R2 (no atomic dual-write).

```go
// internal/orchestrator/state/approvals_shadow.go (NEW file in Task 1)
//
// Phase B: shadow-write to substrate AFTER the legacy write commits.
// Per substrate W1 spec §3 R2: two independent tx. Failure of the
// substrate-side write does NOT roll back the legacy write — it
// records a divergence row (§3.3) and surfaces a metric.
//
// Flag: SUBSTRATE_APPROVALS_SHADOW_WRITE=true gates the mirror call.
// Default OFF for one release cycle; flip ON in the next release.
func (d *DB) AppendApprovalEventWithShadow(ctx context.Context, ev ApprovalEvent) error {
    if err := d.AppendApprovalEvent(ctx, ev); err != nil {
        return err  // legacy write authoritative; surface as today
    }
    if !d.shadowEnabled() {
        return nil
    }
    return d.shadowMirror(ctx, ev)  // independent tx; logs+metric on failure
}
```

The legacy write stays the only error-returning path. The substrate-side mirror is best-effort — failures are observed (metric + log + reconciliation cron picks up) but do not propagate to the caller. This is load-bearing: Phase B's correctness contract is that the legacy write remains source-of-truth; the substrate is shadow.

### 3.2 What the substrate row looks like

A single `approval_events` row → one `substrate_events` row:

| legacy column | substrate column / payload |
|---|---|
| `id` (AUTOINCREMENT) | (discarded; substrate.id is a fresh ULID) |
| `approval_id` | `payload_json.approval_id` |
| `ts` | `written_at` (ms; legacy stores seconds — multiply by 1000) |
| `kind` (e.g. `requested`, `approved`, `token_consumed`) | `payload_json.transition` |
| `actor` | `payload_json.actor` + duplicated to `written_by` for keyring lookup |
| `payload_json` | merged into `payload_json` per-kind |
| `token_jti` | `payload_json.token_jti` (used by replay-check, NOT used as substrate nonce — see §3.5) |
| `trace_id` | `trace_id` column (already on `approval_events` per W6 T5) |

The `approvals` row (mutable status) is NOT directly mirrored — the substrate model is event-stream-derives-state, so fold-over-approval-events IS the substrate's representation. The legacy `approvals.status` row is the projection legacy callers see; substrate readers compute the same projection from the event fold.

### 3.3 Divergence detection (the test-time read-old → read-new compare)

Three layers, each independently detects drift:

**Layer 1 — write-time invariant check (cheap, every write):**
After the shadow mirror commits, the writer runs `verifyShadowParity(ctx, approval_id)` which:
1. SELECTs the last 5 `approval_events` rows for the approval (legacy).
2. Folds the last 5 substrate `approval_event` rows for the same approval_id.
3. Asserts byte-equal payloads (after kind/transition rename) and count.
On mismatch: increment `regatta_substrate_approvals_divergence_total` counter, log structured warning with approval_id + diff summary, write a `substrate_divergence_audit` row (small table; one row per detection). Does NOT fail the write.

**Layer 2 — per-test compare harness (CI/dev, every test):**
A test helper `assertSubstrateMatchesLegacyApproval(t, db, approvalID)` runs in `internal/gates/approval/*_test.go` test teardown when the test exercised an approval flow. Bare reuse of Layer 1's compare, called as a `t.Cleanup`. CI fails on any test that leaves divergence. This is the test-time read-old → read-new compare called out in the task brief.

**Layer 3 — reconciliation cron (production, hourly):**
`internal/orchestrator/substrate_recon/approvals_recon.go` (NEW in Task 3) walks `approvals` rows touched in the last hour, folds substrate, compares. Divergences open an audit issue (or surface a Honeycomb alert at A+ tier). The cron uses the **backfill helper** to repair: legacy-has + substrate-missing → re-emit the substrate row from legacy data; substrate-has + legacy-missing → operator-investigated audit (legacy is authoritative; substrate is wrong, manual repair).

The three layers are intentionally redundant. Layer 1 catches programmer error in the shadow seam at the speed of the test suite; Layer 2 catches accumulated drift across the test corpus; Layer 3 catches partial-failure drift in production.

### 3.4 Per-callsite opt-in (the GitHub trickle-cutover variant)

The flag `SUBSTRATE_APPROVALS_SHADOW_WRITE` controls all approval-events shadow-write at once. But the cutover-flag-state machine has THREE positions:

- `off` (default Wave 1 ship) — no substrate writes; substrate table stays empty for approvals.
- `dry_run` — substrate write happens; failures are LOGGED but the writer returns success even if legacy write happened to fail (use only in operator-driven smoke tests; gated by `SUBSTRATE_APPROVALS_SHADOW_DRY_RUN=true` env).
- `on` — substrate write happens; failures are observed (metric + audit row) but legacy write authoritative.

Self-host operator flips to `on` after one release cycle of `off` with the table created. This matches GitHub's trickle pattern: ship the code dark, observe in CI, flip when the test suite is clean.

### 3.5 Why approval-events token_jti is NOT the substrate nonce

The substrate `nonce` column is replay-protection for the substrate signature (substrate W1 spec §5). The legacy `token_jti` is replay-protection for the approval callback URL — a different concern. The shadow-write seam generates a FRESH 16-byte nonce per substrate row and stores token_jti inside `payload_json`. This is load-bearing: collapsing them would mean a substrate replay would also retire the token, and vice versa, coupling two replay-prevention surfaces with different lifetimes. `TestSubstrate_ApprovalShadow_NonceIndependentOfTokenJTI` pins the invariant.

### 3.6 The `substrate_divergence_audit` table

```sql
-- migrations/0007_substrate_divergence_audit.sql (Task 1 owner)
CREATE TABLE substrate_divergence_audit (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    detected_at     INTEGER NOT NULL,                              -- unix ms
    detector        TEXT    NOT NULL CHECK (detector IN ('layer1_write','layer2_test','layer3_cron')),
    store           TEXT    NOT NULL CHECK (store IN ('approvals','token_spend')),
    primary_key     TEXT    NOT NULL,                              -- approval_id or call_id
    legacy_summary  TEXT    NOT NULL,                              -- hash + count
    substrate_summary TEXT  NOT NULL,
    diff_summary    TEXT    NOT NULL,                              -- short human-readable diff
    repaired_at     INTEGER,                                       -- NULL until reconciliation cron repairs
    repair_action   TEXT    NOT NULL DEFAULT ''                    -- 'backfill_substrate'|'manual'|''
);
CREATE INDEX idx_substrate_divergence_audit_unrepaired
    ON substrate_divergence_audit(detected_at)
    WHERE repaired_at IS NULL;
```

Migration `0007_substrate_divergence_audit.sql` is per `feedback_migration_number_lock` pinned at 0007 — implementer subagents may not pick.

---

## 4. Phase C mechanics — read-from-substrate with legacy fallback

### 4.1 The read seam

Today's approval reads:
- `GetApproval(ctx, id)` — `internal/orchestrator/state/approvals.go:154` — `SELECT … FROM approvals WHERE id = ?`
- `ListApprovalEvents(ctx, approvalID)` — `internal/orchestrator/state/approvals.go:212` — `SELECT … FROM approval_events WHERE approval_id = ?`
- `ListPending(...)`, `ListExpired(...)` — `internal/orchestrator/state/approvals.go:243+258`

Phase C introduces ONE read flag — `SUBSTRATE_APPROVALS_READ_FROM=substrate|legacy` (default `legacy` Wave 1; flip to `substrate` after Phase B has been `on` for one full release cycle with zero unrepaired divergences). When `substrate`:

```go
// internal/orchestrator/state/approvals_substrate_read.go (NEW Task 2)
//
// Phase C: read from substrate fold; fall back to legacy on miss.
// Self-host invariant: legacy table still receives writes (Phase B
// continues), so fallback is zero-loss.
func (d *DB) GetApprovalSubstrate(ctx context.Context, id string) (Approval, error) {
    if d.readFrom() == "legacy" {
        return d.GetApproval(ctx, id)
    }
    a, err := d.foldApprovalFromSubstrate(ctx, id)
    if errors.Is(err, ErrSubstrateNoEvents) {
        // Substrate miss: fall back to legacy. Layer-3 cron will
        // eventually backfill substrate; meanwhile reads keep working.
        return d.GetApproval(ctx, id)
    }
    return a, err
}
```

### 4.2 The fold

`foldApprovalFromSubstrate(ctx, id)` queries:

```sql
SELECT payload_json, written_at, written_by
FROM substrate_events
WHERE kind = 'approval_event'
  AND json_extract(payload_json, '$.approval_id') = ?
ORDER BY written_at ASC, id ASC
```

Reducer is `append` (substrate W1 spec §4). The fold state machine: `requested` → `pending`; first `approved` (with quorum reached, computed by counting unique `actor` values across `approved` transitions until count == quorum) → `approved`; first `rejected` → `rejected`; any `timed_out` → `timed_out`. The quorum logic mirrors `internal/gates/approval/fold.go::Fold` verbatim — Task 2 must call into the existing fold, NOT reimplement it. This is load-bearing per `feedback_research_design_principles` (adopt, don't rebuild).

### 4.3 Fallback semantics (Datadog shadow-read variant)

When substrate fold returns empty AND legacy row exists, the fallback returns the legacy row AND emits a `substrate_fallback_total{store="approvals", reason="empty_fold"}` counter increment. This is observed-but-not-fatal — Wave 1 substrate is shadow, so empty fold is expected for any approval written before Phase B flipped on.

A second counter `substrate_fallback_total{store="approvals", reason="fold_disagrees_with_legacy"}` fires when substrate fold yields a state that differs from legacy `approvals.status`. This is the post-Phase-C version of Layer 3 divergence — fires only after read-from-substrate is the default. Pre-cutover the counter stays zero (because read path uses legacy).

### 4.4 No silent mutation

Phase C's read path is read-only against substrate; the fallback path is read-only against legacy. Neither path writes. Backfill (substrate-missing) is the cron's job (§3.3 Layer 3), not the read path's. This is a hard invariant — `TestSubstrate_ApprovalReadPath_NoWrites` pins it with sqlite query-log assertion.

---

## 5. Rollback — required (substrate W1 spec §3 mandates)

### 5.1 The flag matrix

| Failure mode | Flip | Recovery |
|---|---|---|
| Substrate writes fail / divergence spikes | `SUBSTRATE_APPROVALS_SHADOW_WRITE=off` | Legacy write continues uninterrupted. Substrate table stops growing for approvals. No data loss (legacy is source-of-truth). |
| Read-from-substrate returns wrong state in production | `SUBSTRATE_APPROVALS_READ_FROM=legacy` | One env var flip + process restart. Legacy row is authoritative (Phase B still writes it). No data loss. |
| Both above + suspected substrate-side corruption | `SUBSTRATE_APPROVALS_SHADOW_WRITE=off` + `SUBSTRATE_APPROVALS_READ_FROM=legacy` | Equivalent to Phase A. Substrate table stays as historical record; no new rows; reads ignore it. |

### 5.2 The rollback test (grade B requirement)

`TestSubstrate_ApprovalRollback_ReadFromFlipsBack` exercises the full sequence: Phase A → flip to B → write some approvals → flip C → read works → corrupt the substrate row directly (UPDATE inside the test, simulating bug) → fold gives wrong state → flip C back to `legacy` → reads now serve the correct legacy state → assert no exception, no data loss. This test is the proof-of-rollback per the task brief's "Required" line.

### 5.3 The cost-gov divergence-watchdog (the §2.3 carry-over)

`TestCostGov_SubstrateFold_BudgetParity` (NEW, internal/cost/spend/) generates 1000 synthetic token_spend rows in a temp db, then computes the per-DAG budget total via two query plans:
- Plan A: `SELECT SUM(json_extract(payload_json, '$.usd')) FROM substrate_events WHERE kind='token_spend' AND json_extract(payload_json,'$.dag_id')=?` (today's reader).
- Plan B: same set folded in Go via `substrate.Fold(ctx, db, runID, KindTokenSpend)` + Go-side SUM.

Assert byte-equal totals to 1e-9 USD. This is the cost-gov analog of approval Phase-C readback parity: even though cost-gov has no cutover, a future refactor that switches `reader.go` to use `substrate.Fold` directly must not break the budget computation. The test is the regression net.

### 5.4 What rollback does NOT cover (acknowledged gaps)

- **Substrate row already written, legacy write rolled back** (mid-process crash between the two tx). Phase B's Layer-3 cron detects (substrate-has + legacy-missing) and opens an audit issue. Manual repair: operator inspects, either re-inserts legacy or marks substrate row stale. This is rare and the audit table preserves enough data to repair.
- **Phase D (legacy table drop)** — explicitly NOT in scope per task brief and substrate W1 spec §3 Phase D. Approvals legacy table stays forever in this spec's horizon. Phase D ships when an external customer triggers Phase X re-entry.

---

## 6. File-disjoint task breakdown

Per `feedback_dispatch_strategy` + `feedback_plan_subagent_dup_files`: every task names exact output paths up front. 4 file-disjoint subagent tasks.

| # | Task | Files (output paths pinned) | Owner | Depends on |
|---|---|---|---|---|
| **0** | Migration `0007_substrate_divergence_audit.sql` + lint-rule that pins `0007` to this purpose | `internal/orchestrator/state/migrations/0007_substrate_divergence_audit.sql`, `internal/orchestrator/state/migrations/migrations_test.go` (extend existing) | Z | substrate W1 (shipped) |
| **1** | Phase B shadow-write seam + Layer-1 invariant check + audit row writer | `internal/orchestrator/state/approvals_shadow.go`, `internal/orchestrator/state/approvals_shadow_test.go`, `internal/orchestrator/state/approvals_shadow_helpers_test.go` | A | Task 0 |
| **2** | Phase C read-from-substrate + fold-state-machine + fallback + read-only invariant test | `internal/orchestrator/state/approvals_substrate_read.go`, `internal/orchestrator/state/approvals_substrate_read_test.go`, `internal/orchestrator/state/approvals_rollback_test.go` | B | Task 1 (reuses helpers) |
| **3** | Reconciliation cron + Layer-2 test helper + Layer-3 backfill helper + cost-gov watchdog test | `internal/orchestrator/substrate_recon/approvals_recon.go`, `internal/orchestrator/substrate_recon/approvals_recon_test.go`, `internal/orchestrator/substrate_recon/backfill.go`, `internal/orchestrator/substrate_recon/backfill_test.go`, `internal/cost/spend/budget_parity_test.go` | C | Task 1, Task 2 |
| **D** | Docs-only follow-up batch (operator runbook entry + flag doc) | `docs/operator/quickstart.md` (small section), `docs/engineer/specs/2026-06-02-s3-t2-substrate-cutover.md` (this file — no edit), `docs/runbook/substrate-cutover.md` (NEW) | D | Tasks 1+2+3 merged |

Tasks 1, 2, 3 are file-disjoint among themselves; Task 1 must ship first (Task 2 imports the shadow seam's helpers; Task 3 imports both). Task 0 is a pre-Wave migration gate.

Per `feedback_test_godoc_one_line`: every Test/Fuzz/Benchmark godoc in the above is 1 line max. The implementer dispatch prompt for each task explicitly cites this rule.

---

## 7. Grade rubric (B / A / A+ — tool-checkable per `feedback_grade_rubric`)

| Tier | Criterion | Tool check |
|---|---|---|
| **B** | Migration 0007 ships; Phase B shadow-write seam lands; Phase C read+fallback lands; rollback test `TestSubstrate_ApprovalRollback_ReadFromFlipsBack` passes; Layer-1 divergence-counter increments on injected mismatch; cost-gov budget-parity watchdog passes; no UPDATE/DELETE on substrate_events from any new file. | `make check && go test ./internal/orchestrator/state/... ./internal/orchestrator/substrate_recon/... ./internal/cost/spend/...` passes; `go test -run TestSubstrate_ApprovalRollback ./...` passes; `go test -run TestCostGov_SubstrateFold_BudgetParity ./...` passes; `grep -rE '\b(UPDATE\|DELETE)\b' internal/orchestrator/state/approvals_shadow.go internal/orchestrator/state/approvals_substrate_read.go` zero matches in non-test files. |
| **A** | All B + Layer-2 test-cleanup helper integrated into existing approval test suite (every approval_test exercises the parity assertion); reconciliation cron lands with end-to-end test (`TestApprovalsRecon_BackfillRepairsMissingSubstrate`); read-from-substrate flag flip exercised via env-var test; fold-state-machine matches `internal/gates/approval/fold.go::Fold` byte-equal on 100 randomised event sequences. | `go test -run TestApprovalsRecon_BackfillRepairsMissingSubstrate ./...` passes; property test `TestApprovalsFold_SubstrateMatchesLegacy` (100 random sequences) passes; `grep -rEn 'assertSubstrateMatchesLegacyApproval' internal/gates/approval/*_test.go` returns ≥5 callsites. |
| **A+** | All A + Layer-3 cron metrics surface to OTel per W6 (`regatta.substrate.divergence_count` gauge); automated audit issue auto-filing path (cron + gh-cli helper); cost-gov reader refactored to OPTIONALLY use `substrate.Fold` behind a flag with byte-equal output (proves the abstraction holds for future cutovers); rollback flag flip documented in `docs/runbook/substrate-cutover.md` with screenshot of the metric drop. | OTel exporter emits the gauge (verified via `go test -run TestSubstrate_DivergenceMetricExported ./...`); flag `SPEND_READ_VIA_FOLD=true` toggles fold-path in reader.go and `TestCostGov_FoldReader_ByteEqualToDirectQuery` passes; runbook file exists with the documented procedure. |

---

## 8. Adversarial red-team

Per `feedback_adversarial_review`. Reviewer subagent must verify before Wave dispatches.

1. **Shadow-write partial failure: legacy commit succeeds, process crashes before substrate write.** Defense: Layer-3 cron detects (legacy-has + substrate-missing) and backfills. Time-to-detect ≤ 1 hour (cron cadence). `TestApprovalsRecon_BackfillRepairsMissingSubstrate`.

2. **Shadow-write partial failure: substrate commit succeeds, legacy write rolls back.** This shape is impossible by ordering — substrate write is AFTER legacy commit; if legacy rolls back, substrate write never runs. The shape only arises if a future refactor reorders; `TestSubstrate_ApprovalShadow_OrderingInvariant` pins it via a code-pattern lint check (rejects substrate write before legacy commit in `approvals_shadow.go`).

3. **Phase C fold gives wrong state because event ordering is wrong.** Defense: fold ORDER BY `written_at ASC, id ASC` matches substrate W1 spec §10 #16. Property test with concurrent writers in randomised order — same final state on every run.

4. **Phase C fold gives wrong state because a `kind` value differs between legacy (`approved`) and substrate (`transition=approved`).** Defense: explicit rename table in `approvals_shadow.go`'s `legacyKindToTransition()`; reverse `transitionToLegacyKind()` for parity check. Both are exhaustive (compile-time switch with default-panic).

5. **Rollback flip but in-flight reads have already started — does a request started under `substrate` mode finish under `legacy` mode coherently?** Defense: the flag is read ONCE per request at entry (`d.readFrom()` is called from the top-level `GetApprovalSubstrate`, not deep in the fold). Mid-request flip is invisible to that request. `TestSubstrate_ApprovalReadPath_FlagReadOnce` instruments the flag-getter and asserts call count == 1 per public-API invocation.

6. **Substrate fold over a huge approval (1000+ events).** Defense: substrate W1 spec §7 performance budget covers Fold ≤ 10ms for ≤100 events. For approvals, real events per approval are ≤ 5 in practice (requested + ≤quorum approved + decision). Wave 1 doesn't need optimisation. Followup issue: `[s3-t2-followup]` "Fold pagination for high-event approvals (post-Phase-X enterprise scale)."

7. **Token JTI used for both callback replay AND substrate nonce — silent coupling.** Defense: §3.5 invariant test pins they are independent.

8. **Operator flips read flag to `substrate` BEFORE shadow-write has run a release cycle — read returns empty fold for every legacy approval.** Defense: fallback (§4.3) returns legacy row, so no operator-visible breakage. Counter `substrate_fallback_total{reason="empty_fold"}` spikes — runbook documents this signal as "you flipped read too early; flip back."

9. **Reconciliation cron itself diverges (bug repairs wrong direction).** Defense: cron writes audit rows, never deletes; manual repair is always recoverable. `TestApprovalsRecon_NeverDeletesLegacyData` pins.

10. **Backfill helper re-emits a substrate row with a DIFFERENT nonce than the original (because the original was never written) — does the signature still verify against the legacy data?** Defense: backfill signs with the per-process keyring + a fresh nonce; the substrate row is freshly authoritative. The KEY id may differ from the original writer (if key rotated between original write and backfill); that is acceptable because backfill is a reconciliation event, not a forge. Audit row records the keyID drift.

11. **Phase C cutover order under multi-tenant (post-W8).** OUT OF SCOPE — self-host single-tenant. `[s3-t2-followup]` tracks: "per-tenant Phase C flag for post-Phase-X multi-tenant cutover."

12. **Migration 0007 collides with an unrelated migration filed in parallel by another wedge.** Defense: `feedback_migration_number_lock` — implementer subagent dispatch prompts MUST pin 0007 to this spec. No other 2026-06 spec claims 0007.

---

## 9. Followup issues (cited per `feedback_unaddressed_load_bearing`)

Each filed before Wave 1 PR opens. Prefix `[s3-t2-followup]`.

| # | Title | Why deferred | Pre-condition |
|---|---|---|---|
| F1 | `[s3-t2-followup]` Phase D drop of `approvals` + `approval_events` tables | Self-host doesn't need it; legacy table is cheap | External-customer Phase X re-entry OR self-host running 90 days clean on Phase C |
| F2 | `[s3-t2-followup]` Fold pagination for high-event approvals | Realistic approvals stay ≤ 5 events | Enterprise pilot where approvals chain ≥ 100 events |
| F3 | `[s3-t2-followup]` Per-tenant Phase C flag for multi-tenant cutover | Self-host single-tenant | W8 multi-tenant cutover (Phase X) |
| F4 | `[s3-t2-followup]` Honeycomb dashboard tile for `regatta_substrate_approvals_divergence_total` | A+ rubric item; manual today | A+ tier promotion |
| F5 | `[s3-t2-followup]` Auto-file GitHub issue from Layer-3 cron divergence | A+ rubric item; manual today | A+ tier promotion |
| F6 | `[s3-t2-followup]` Extend cutover pattern to work_item_outputs | Out-of-scope per self-host-first §3 | Phase X external-customer trigger |
| F7 | `[s3-t2-followup]` Extend cutover pattern to work_item_edges | Out-of-scope per self-host-first §3 | Phase X external-customer trigger |

---

## 10. Open questions for adversarial reviewer

1. **Layer-1 invariant check cost.** §3.3 Layer 1 runs a `SELECT last 5 + fold last 5 + compare` after every shadow write. p95 cost on a hot path? Substrate spec §7 says fold ≤ 10ms for ≤ 100 events; we're folding 5. Estimate ≤ 200µs per write. Acceptable? Or should Layer 1 be sample-rate-limited (1 in 10) to lower cost?

2. **Flag-flip rollout cadence.** §3.4 says "flip to `on` after one release cycle." Self-host has no release cadence — it's continuous. Suggested rule: after Phase B has been ON in main for `≥7 calendar days with zero unrepaired divergences`, flip read-from-substrate. Reviewer to confirm.

3. **Cost-gov budget-parity test scope.** §5.3 generates 1000 synthetic rows. Is 1000 enough to surface fold-vs-direct divergence at scale? Or should it be 100K (which is the substrate W1 A+ tier number)? Suggested: 1000 for B-tier, 100K for A+-tier behind `-tags=load` build flag.

4. **Layer-3 cron cadence.** §3.3 says hourly. Self-host single-operator likely processes ≤ 5 approvals/day. Hourly is overkill. Suggested: hourly is fine; cron overhead is one query per hour; not worth optimising.

5. **Fallback counter cardinality.** §4.3 counters use `reason="empty_fold"` and `reason="fold_disagrees_with_legacy"`. Are two reason labels enough or should we split `empty_fold` by approval-age bucket (≤1d, ≤7d, >7d)? Suggested: two reasons for Wave 1; add bucketing as `[s3-t2-followup]` if dashboard reveals need.

---

_Spec authority: per `feedback_spec_pattern_authority`, implementer subagent deviation from this spec requires re-spawning the design subagent. Open questions §10 must be resolved by adversarial reviewer before Wave dispatches. Followup issues §9 MUST be filed and cited in the Wave 1 PR body per `feedback_unaddressed_load_bearing`._

## Resolution (2026-06-02)

Phase B+C cutover shipped across #369 (`feat(state): Phase B — approvals shadow-write seam (migration 0009)`) and #378 (`feat(state): Phase C — approvals read-from-substrate seam (migration 0011)`). Cost-governor stays substrate-native (no cutover needed); approvals are the only legacy table touched in this scope.
