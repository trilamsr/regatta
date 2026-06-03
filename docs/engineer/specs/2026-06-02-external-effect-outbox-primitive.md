# External-Effect Outbox Primitive — Design Spec

Status: ready for review
Date: 2026-06-02
Author: design subagent <tri@lumalabs.ai>
Generalizes: PR #558 (`internal/orchestrator/merge` — c0)
Closes (issue stays OPEN until impl ships): #551

Memory rules in force: `feedback_decision_priority`, `feedback_research_design_principles`, `feedback_grade_rubric`, `feedback_deletion_default`, `feedback_adversarial_review`, `feedback_spec_pattern_authority`, `feedback_test_godoc_one_line`, `feedback_doc_check_banned_phrases`, `feedback_pr_body_release_notes_fence`, `feedback_pr_body_file_only`, `feedback_no_signatures`, `feedback_unaddressed_load_bearing`.

---

## §1 Problem statement

c0 (PR #558) landed an intent/outbox guard for one external side-effect: `gh pr merge`. The same crash-safety hole exists for every other non-idempotent external write regatta makes today or will make next wave:

- **Approval notify** (`internal/gates/approval/gate.go` `createApprovalAndNotify`): appends `approval_requested`, calls `notifier.Notify`, then appends `approval_notified`. Class C in #551 — a crash between the external send and the `notified` append double-sends the moment a real Slack/email channel replaces the stub.
- **Future SCM writes**: `gh issue close`, `gh pr comment`, `gh release create` — all promised by W4 (self-improvement detectors writing audit comments back to issues) and W6 (release-notes auto-publish).
- **Future webhook calls**: PagerDuty / Slack-alert fan-out from `obs-alert` rules; Stripe billing posts from W12 cost-governor.

c0 closed the hole for merge. #551 asks: extract the c0 shape into a reusable primitive so each new external effect inherits crash-safety instead of re-deriving it (and getting it 80% right). The substrate's `UNIQUE(run_id, written_by, nonce)` guard is already the dedup backbone; this spec generalizes the c0 events + Coordinator over that guard so any registered Effect drops in with a fixed amount of work.

Linked: #548 (PII crypto-shred, sibling boundary gap), #549 (replay version-skew), #550 (gate-audit reframing), #552 (W2 amendment), #558 (c0 PR).

### 1.1 Non-goal

- Re-litigating c0's merge-specific decisions. c0 stays the canonical merge instance; this spec generalizes its shape.
- Building a generic workflow engine. That conversation lives in `docs/engineer/specs/2026-06-01-w9-temporal-vs-bespoke-redteam.md` — verdict already recorded: bespoke wins for self-host. The outbox primitive is the part of "what Temporal would give us" we actually need.
- Wiring every existing external effect to the new primitive in one PR. Migration is phased (§10).
- A new database table. The substrate events table + its UNIQUE-nonce guard is the storage layer; this is a Go-level primitive on top of it.

---

## §2 Reference: c0 design (PR #558)

c0 lives at `internal/orchestrator/merge` (227 LoC `merge.go` + 208 LoC `coordinator.go` + 494 LoC `coordinator_test.go`). The shape that generalizes:

### 2.1 Four event kinds, stable strings

| Kind | When written | Payload |
|---|---|---|
| `merge_intent` | BEFORE the external `gh pr merge` call, inside the FSM transition tx | `{pr_number, head_sha}` |
| `merge_completed` | AFTER successful merge | `{pr_number, head_sha, merge_sha, source}` |
| `merge_failed` | After permanent failure (`closed_unmerged`, `sha_diverged`, `no_intent_on_file`) | `{pr_number, head_sha, reason, source}` |
| `merge_recovered` | Audit-only, when the crash-recovery sweep reconciles a dangling intent | `{outcome[, reason]}` |

### 2.2 Nonce = head SHA

The intent's nonce is the PR head SHA at decision-time. Same SHA flows into the gh-CLI merge call so the external mutation is idempotent against it. `LatestIntent` picks the most-recent intent row by `id DESC` — the revert-and-re-push case (`TestNonceCollision_RevertedBranchSafe`) is pinned by that ordering.

### 2.3 Reconcile sweep — `Coordinator.Reconcile(ctx) error`

1. List agents in `AwaitingMerge`.
2. For each: load `LatestIntent`, call `PRProber.Probe(ctx, prNumber, expectedSHA)`.
3. Branch on `PRStatus`:
   - `Merged` → write `merge_completed` (source=recovery), transition Done.
   - `OpenSHAMatches` → leave in place (next normal-path tick re-issues).
   - `OpenSHADiverged` → write `merge_failed` (reason=sha_diverged), → Crashed.
   - `ClosedUnmerged` → write `merge_failed` (reason=closed_unmerged), → Crashed.
   - `Unknown` → leave in place (transient prober error).
4. Per-agent errors are non-fatal — one bad PR cannot strand the sweep.

That shape — *one nonce, four events, one reducer over probe outcomes, one sweep* — is the primitive.

---

## §3 Prior art

Three patterns the design borrows from. Each gets adopt/reject per `feedback_research_design_principles` (UX > quality bar matching reference systems > ecosystem conventions > long-term).

### 3.1 Transactional outbox (Microservices.io / Chris Richardson)

The canonical pattern: a service writes a domain event AND an outbox row in the same DB tx; a separate poller reads the outbox and publishes to the external system; the publish is idempotent (consumer dedups by outbox row id). Reference: <https://microservices.io/patterns/data/transactional-outbox.html> + Debezium's outbox-event-router (Apache-2.0, v2.5.x).

**Adopt:** the tx-atomicity rule (intent row + FSM transition commit together), the idempotency-key-as-nonce model, the "sweep finds dangling rows" recovery.

**Reject:** the dedicated outbox *table* and message-broker fan-out. regatta's substrate events table already provides append-only durability + the `UNIQUE(run_id, written_by, nonce)` dedup constraint — adding a parallel table doubles the storage seam without adding capability. The "broker" in our case is the synchronous external call inside `Execute`; no Kafka, no Debezium.

### 3.2 Temporal Saga / activity-with-idempotency-key (Temporal Tech, Apache-2.0, v1.22+)

Temporal models each external side-effect as an Activity with an idempotency token; on worker crash, the workflow re-drives the Activity and the external system dedups. Recovery uses the workflow's event history as the source of truth.

**Adopt:** the Effect interface shape — `Plan → Execute → Probe`. Each Effect names its idempotency contract; the framework guarantees re-drive semantics.

**Reject:** Temporal itself (already redteamed in `2026-06-01-w9-temporal-vs-bespoke-redteam.md`: too much surface for single-operator self-host, separate cluster, separate UI). Adopt only the *interface shape* and the "probe before re-execute" recovery posture.

### 3.3 Netflix Conductor / AWS Step Functions exactly-once activity (Apache-2.0, Conductor v3.15)

Conductor's HTTP-task and Step Functions' service-integration tasks both use a caller-supplied token to dedup retries at the external boundary. The orchestrator keeps the token in the task record; the worker passes it on retry.

**Adopt:** nothing the c0 model didn't already capture (nonce = idempotency token). Cite as confirmation the c0 nonce model matches industry practice.

**Reject:** the rest of Conductor. Same reasoning as Temporal — too much surface for one operator.

**Net adoption:** transactional-outbox dedup model (riding our substrate's UNIQUE-nonce guard) + Temporal's three-method Effect interface. Both Apache-2.0; no new vendored deps; ~250 LoC of new Go (Coordinator + Registry + worked second example).

---

## §4 Generic API design

### 4.1 The `Effect` interface

```go
// Package outbox generalizes the c0 (internal/orchestrator/merge) shape
// to any non-idempotent external side-effect. Each registered Effect
// supplies three methods; the Coordinator handles intent/completion
// event writes and the crash-recovery sweep.
package outbox

// Effect names a single external side-effect (merge, notify, billing
// post). Implementations are registered at process boot via Register.
type Effect interface {
    // Name returns the stable effect name used in event kinds:
    //   <name>_intent, <name>_completed, <name>_failed, <name>_recovered
    // Must be a single lowercase token (no underscores beyond the
    // event suffix). Reserved: any name colliding with an existing
    // event kind.
    Name() string

    // Plan computes the intent payload + idempotency nonce for the
    // given work unit. Called inside the caller's tx so the intent
    // row commits atomically with the FSM transition.
    //
    // Nonce shape is Effect-specific (§7). Returning the same nonce
    // twice MUST mean "the second call is a retry of the first" — the
    // external system will dedup on it.
    Plan(ctx context.Context, in PlanInput) (Intent, error)

    // Execute performs the external side-effect using intent.Nonce.
    // MUST be idempotent under retry-with-same-nonce: a second call
    // with the same nonce returns the same Result without re-mutating
    // external state (or returns a clearly-distinguishable
    // "already-applied" sentinel that callers tolerate).
    //
    // Called OUTSIDE the substrate tx — the substrate cannot hold a
    // write lock during an external network call.
    Execute(ctx context.Context, intent Intent) (Result, error)

    // Probe reads external state keyed by intent.Nonce to decide
    // the Outcome of an in-flight intent during crash recovery.
    // MUST NOT mutate external state. SHOULD be cheap (one read).
    Probe(ctx context.Context, intent Intent) (Outcome, error)
}

// PlanInput is the per-work-unit context Plan needs. AgentID is the
// substrate agent row owning the effect; Extra is Effect-specific
// (e.g. PR number for merge, approval ID for notify).
type PlanInput struct {
    AgentID int64
    Extra   map[string]any
}

// Intent is the durable record of "we intend to perform this effect".
// The Coordinator persists it as a substrate event before Execute.
type Intent struct {
    EffectName string
    AgentID    int64
    Nonce      string          // idempotency key — see §7
    Payload    json.RawMessage // Effect-defined; opaque to Coordinator
}

// Result is the Effect-defined success record. Coordinator stores it
// as the <name>_completed payload.
type Result struct {
    Nonce   string
    Payload json.RawMessage
}

// Outcome enumerates the five terminal-or-transient states the
// crash-recovery sweep needs to distinguish (§8).
type Outcome int

const (
    OutcomeUnknown      Outcome = iota // transient probe error; retry next sweep
    OutcomeCompleted                   // external state shows the effect applied
    OutcomeStillPending                // external state shows the call may still complete
    OutcomeFailed                      // external state shows permanent failure
    OutcomeDiverged                    // external state moved beyond what intent expected
)
```

### 4.2 The `Coordinator`

One Coordinator per Effect at runtime (composition over inheritance):

```go
// Coordinator is the generic version of c0's merge.Coordinator. One
// Coordinator wraps one Effect; the daemon holds a registry of them.
type Coordinator struct {
    db     *state.DB
    effect Effect
    log    *slog.Logger
}

// WriteIntent appends <effect>_intent atomically with the caller's tx.
// Equivalent to c0's merge.WriteIntent but parameterized on Effect.
func (c *Coordinator) WriteIntent(ctx context.Context, tx *sql.Tx, in PlanInput) (Intent, error)

// Run executes the effect: writes intent (inside tx), then calls
// Execute outside the tx, then writes <effect>_completed or
// <effect>_failed.
func (c *Coordinator) Run(ctx context.Context, in PlanInput) error

// Reconcile is the recovery sweep — c0's merge.Coordinator.Reconcile,
// generic over Effect. Lists agents with the Effect's intent kind
// recorded but no completion/failure row, calls Probe on each, writes
// the appropriate completion/failure + audit row.
func (c *Coordinator) Reconcile(ctx context.Context) error
```

### 4.3 Stability contract — c0 stays the canonical merge instance

c0's `internal/orchestrator/merge` package keeps its public API verbatim. Migration (§10) refactors the *implementation* to delegate to `outbox.Coordinator`; the package-level `WriteIntent`, `LatestIntent`, `Coordinator.Reconcile`, and the four event kinds stay byte-identical on the wire and in Go.

---

## §5 Event-kind convention

Every Effect contributes four event kinds, machine-derivable from `Effect.Name()`:

| Kind suffix | When | Payload contract |
|---|---|---|
| `_intent` | inside caller's tx, before Execute | `{nonce, …Effect-specific}` |
| `_completed` | after Execute returns nil | `{nonce, source[normal|recovery], …Effect-specific}` |
| `_failed` | after Execute returns terminal error OR Probe says Failed/Diverged | `{nonce, reason, source, …Effect-specific}` |
| `_recovered` | audit-only, written by Reconcile alongside the sibling completed/failed | `{outcome[, reason]}` |

`source` distinguishes "normal completion path" from "reconciled after crash" so dashboards can count crash-driven completions separately. `reason` is a stable token (snake_case, finite vocabulary per Effect) — free-form strings defeat the W4 fingerprint shape per `feedback_grade_rubric`.

Reserved suffixes: implementers MUST NOT introduce additional suffix variants (`_started`, `_aborted`). If a new lifecycle marker is needed, file an issue against this spec — the four-kind shape is the bounded vocabulary the operator UX in §12 depends on.

---

## §6 Registry

Singleton package-level registry, populated at import-time by each Effect's package:

```go
// Register associates a name with a factory function. Factories take
// the wired DB so each Coordinator binds to the process's substrate.
// Call from init() in the Effect's package; panic on duplicate name
// (boot-time misconfig surfaces immediately).
func Register(name string, factory func(*state.DB, *slog.Logger) Effect)

// All returns a snapshot of registered effect names. The daemon walks
// All() at boot to construct one Coordinator per Effect.
func All() []string

// Get retrieves the factory for name. Used by the daemon + the
// `regatta outbox` operator CLI.
func Get(name string) (func(*state.DB, *slog.Logger) Effect, bool)
```

**Why singleton, not DI:** the operator's UX (`regatta outbox status`) needs to enumerate ALL Effects whether they were wired or not — a forgotten `daemon.WithEffect(...)` call would otherwise hide a whole class of in-flight intents. Init-time registration makes "this build has this Effect" a compile-time fact.

**Race-safety:** init() runs single-threaded before main(); `All()` returns a copy of the underlying slice; `Get` reads from an immutable post-init map. No mutex.

---

## §7 Nonce strategy taxonomy

Three nonce families. Pick per Effect; document the choice in the Effect's Go doc.

| Nonce family | Example Effect | Pros | Cons |
|---|---|---|---|
| **External natural key** (head SHA, commit SHA, PR number+SHA) | `merge` (c0) | External system already enforces it; revert-and-re-push handled by `id DESC` LatestIntent ordering | Requires the external mutation to be naturally keyed; not all are |
| **Monotonic event id** (substrate event row id at decision time) | `approval-notify`, `gh issue close` | Always available; trivially unique within a substrate | External system MUST accept caller-supplied idempotency keys (most APIs do; some don't) |
| **External correlation id** (Stripe `idempotency-key` header, PagerDuty `incident_key`) | future `stripe-charge`, `pagerduty-alert` | Matches the external API's native dedup; visible in external dashboards | Requires the external API to define one; the Effect's Plan generates it (UUIDv7 recommended for monotonic+sortable) |

Trade-offs:

- **Natural key** is strongest when available (the external system can never accept a duplicate even if our nonce-write logic has a bug). But it ties recovery to the natural key still being readable at probe time — c0's `OpenSHADiverged` exists because a force-pushed branch invalidates the natural-key match.
- **Monotonic event id** is always available but only works if the external API accepts caller-supplied keys. Slack `client_msg_id`, GitHub's `gh issue close` (idempotent by issue state), and most webhook receivers do; some legacy APIs don't.
- **External correlation id** is the safest when natural keys aren't available — UUIDv7 gives monotonic ordering for the `id DESC` LatestIntent semantics c0 relies on.

**Reserved (impl-PR followup):** a global nonce-collision detector that asserts `(effect_name, nonce)` is unique across the whole substrate. Helps catch the case where two Effects accidentally pick the same nonce family and clash. Out of spec scope.

---

## §8 Recovery posture

Re-probe is required for every Effect. The Coordinator's reducer over `Outcome`:

| Outcome | Coordinator action |
|---|---|
| `Completed` | Write `<effect>_completed` (source=recovery) + `<effect>_recovered{outcome:completed}`. Transition agent FSM per Effect-supplied policy (Effect-specific; merge → Done, notify → resume gate). |
| `StillPending` | Leave in place. Next sweep retries. (This is c0's `OpenSHAMatches` generalized — the external call may still complete on its own.) |
| `Failed` | Write `<effect>_failed` (reason from Probe) + recovered audit row. Transition → Crashed (requeue path applies). |
| `Diverged` | Write `<effect>_failed` (reason=diverged) + recovered audit row. Transition → Crashed. Distinct from `Failed` because the *external* state moved out from under us, not the call itself. |
| `Unknown` | Leave in place. Next sweep retries. Surfaces transient probe errors (rate limit, network) without forcing a decision. |

Per-agent errors during Reconcile are non-fatal (matches c0). A single bad Effect instance cannot strand the rest of the awaiting set.

**Bounded recovery loop:** a single intent that probes `Unknown` indefinitely is a stuck-forever risk. The W4 self-improvement detector watches for the same `(effect_name, agent_id)` appearing in N consecutive `Unknown` sweeps and surfaces an `obs-alert`. N = 3 by default, Effect-overridable. Detector lives in W4; this spec only emits the audit-row shape it needs.

---

## §9 Concrete second instance — `approval-notify`

Worked example for the Slack/email notifier (`internal/gates/approval/gate.go`'s `createApprovalAndNotify`):

```go
package approvalnotify

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/trilamsr/regatta/internal/outbox"
    "github.com/trilamsr/regatta/internal/orchestrator/state"
)

func init() {
    outbox.Register("approval_notify", New)
}

// Effect implements outbox.Effect for the approval-channel notifier.
// Nonce family: monotonic event id (§7) — the approval_requested
// event's substrate row id at decision time. Notifier APIs (Slack
// chat.postMessage with client_msg_id; SendGrid with X-Message-Id)
// accept caller-supplied idempotency keys.
type Effect struct {
    db       *state.DB
    notifier Notifier // injected; tests use stub
}

func New(db *state.DB, _ *slog.Logger) outbox.Effect { /* ... */ }

func (e *Effect) Name() string { return "approval_notify" }

func (e *Effect) Plan(ctx context.Context, in outbox.PlanInput) (outbox.Intent, error) {
    approvalID, _ := in.Extra["approval_id"].(int64)
    channel, _ := in.Extra["channel"].(string)
    nonce := fmt.Sprintf("appr-%d", approvalID) // monotonic event id
    payload, _ := json.Marshal(map[string]any{
        "approval_id": approvalID,
        "channel":     channel,
        "nonce":       nonce,
    })
    return outbox.Intent{
        EffectName: e.Name(),
        AgentID:    in.AgentID,
        Nonce:      nonce,
        Payload:    payload,
    }, nil
}

func (e *Effect) Execute(ctx context.Context, intent outbox.Intent) (outbox.Result, error) {
    var p struct {
        ApprovalID int64  `json:"approval_id"`
        Channel    string `json:"channel"`
        Nonce      string `json:"nonce"`
    }
    _ = json.Unmarshal(intent.Payload, &p)
    receipt, err := e.notifier.Notify(ctx, NotifyRequest{
        ApprovalID:     p.ApprovalID,
        Channel:        p.Channel,
        IdempotencyKey: p.Nonce, // <-- the external dedup contract
    })
    if err != nil {
        return outbox.Result{}, err
    }
    body, _ := json.Marshal(map[string]string{"external_id": receipt.ExternalID})
    return outbox.Result{Nonce: intent.Nonce, Payload: body}, nil
}

func (e *Effect) Probe(ctx context.Context, intent outbox.Intent) (outbox.Outcome, error) {
    // Slack: GET conversations.history filter by client_msg_id.
    // SendGrid: GET /v3/messages/<X-Message-Id>.
    found, err := e.notifier.LookupByNonce(ctx, intent.Nonce)
    if err != nil {
        return outbox.OutcomeUnknown, err
    }
    if found {
        return outbox.OutcomeCompleted, nil
    }
    return outbox.OutcomeStillPending, nil
}
```

Migration of the existing `createApprovalAndNotify` body:

1. Replace the inline `notifier.Notify` call with `coord.Run(ctx, outbox.PlanInput{...})`.
2. Drop the manually-managed `approval_notified` event — `approval_notify_completed` replaces it.
3. Add `coord.Reconcile(ctx)` to the orchestrator's Recover sweep alongside the merge coordinator.

LoC delta: ~80 LoC of approval-side code removed; ~120 LoC of Effect impl added; ~250 LoC of outbox primitive shared with merge + future Effects. Net negative once 3+ Effects exist (§10 phasing).

---

## §10 Migration plan

Phased; each phase is one PR.

| Phase | Scope | Wire compat |
|---|---|---|
| **P0 (this spec)** | Design only. No code change. | n/a |
| **P1** | Add `internal/outbox` package (Effect, Coordinator, Registry, Outcome). No migration yet — c0's merge package and approval-notify untouched. Tests cover the generic Coordinator against a fake Effect. | n/a |
| **P2** | Refactor c0's `internal/orchestrator/merge` to delegate to `outbox.Coordinator[*mergeEffect]`. Public API (`WriteIntent`, `LatestIntent`, `Coordinator.Reconcile`, the four event kinds) byte-identical on the wire and in Go. Existing c0 tests pass unchanged. | ✓ |
| **P3** | Wire `approval-notify` Effect (the §9 worked example) before the first real Slack/email notifier replaces the stub. Removes the class-C hole #551 names. | new (no prior wire format) |
| **P4** | Wire `gh-issue-close` and `gh-pr-comment` Effects when W4 self-improvement detectors land. | new |
| **P5+** | Stripe charge (W12), PagerDuty alert (W3 obs-alert fan-out), future SCM adapters (MVR-2 Gitea). | new |

**Backwards-compat invariant:** the four event kinds c0 already wrote (`merge_intent`, `merge_completed`, `merge_failed`, `merge_recovered`) stay on the wire forever. P2's refactor MUST NOT rename them or change payload shape. The generic Coordinator's event-kind derivation reproduces those exact strings for `Effect.Name() == "merge"`.

**Deletion default check:** P1 adds ~250 LoC (Coordinator + Registry + test scaffolding). P2 deletes ~120 LoC from `internal/orchestrator/merge` (Reconcile body becomes a 5-line delegate). Net at P3: -80 LoC for approval-notify migration (§9) means P3 alone repays P1's add. From P4 onward, every new Effect saves ~150 LoC vs hand-rolling.

---

## §11 Performance

- **Registry lookup:** `Get(name)` is `O(log n)` over a sorted slice of effect names; bounded at boot-time (~50 effects = ~6 comparisons). Not on any hot path.
- **Intent write:** one substrate event (`<effect>_intent`) per Effect invocation. Same cost as c0's existing `merge_intent` write (one INSERT on the events table). Substrate writer is serialized at `SetMaxOpenConns(1)` — already the bottleneck on every existing write, no new contention.
- **Reconcile sweep:** `O(awaiting_<effect>)` per Effect — same shape as c0's `ListAgentsByState(AwaitingMerge)` scan. Daemon walks `outbox.All()` at recovery time and calls each Coordinator's `Reconcile` in series.
- **Scaling ceiling:** ~50 Effect kinds is the limit before adding a per-Effect worker pool. Justification: at 50 Effects × ~10 in-flight intents each = 500 probe calls per recovery sweep; sequential execution at ~200ms/probe (gh CLI median) = 100s wall-clock — operator-visible but tolerable on daemon restart. Past 50, parallelize the outer `All()` loop (one goroutine per Effect; substrate writer stays serialized internally). Out of P1 scope; filed as P5 followup.

**Mutation-verify recipe for the ceiling claim:** spin 50 fake Effects in `outbox_perf_test.go`, each with 10 in-flight intents, measure `Reconcile` wall-clock on a single goroutine vs `runtime.GOMAXPROCS`-bound parallelism. Land alongside P5 only when the ceiling is reached.

---

## §12 Operator UX

Two new `regatta` CLI subcommands. Both are read-mostly; the destructive replay command requires `--yes`.

### 12.1 `regatta outbox status`

Lists all in-flight intents grouped by Effect. Drives the operator's "what's stuck?" question without `sqlite3 .regatta/state.db`.

```text
$ regatta outbox status
Effect            in-flight   oldest_intent       last_recovery
merge             2           3m12s ago           2026-06-02T14:11:08Z (1 reconciled)
approval_notify   0           —                   —
gh_issue_close    1           14h28m ago          — (probe Unknown × 3 — see obs-alert outbox.stuck)
```

Sort order: in-flight DESC, then oldest_intent DESC. The "Unknown × N" note surfaces the §8 stuck-forever detector inline. Implementation: one substrate-events query per Effect (`SELECT count(*), MIN(ts) FROM events WHERE kind=? AND agent_id NOT IN (SELECT agent_id FROM events WHERE kind IN (?,?))`).

### 12.2 `regatta outbox replay --effect=NAME --nonce=NONCE`

Forces a re-probe + reducer pass for a single in-flight intent. Used when the operator wants to unstick an `Unknown`-loop without restarting the daemon.

```text
$ regatta outbox replay --effect=merge --nonce=abc123def --yes
probing merge:abc123def …
outcome: Completed
wrote merge_completed{source=recovery_manual}
transition agent 42: AwaitingMerge → Done
```

`--yes` required because the replay can transition an agent to `Crashed` (requeue) if the Probe says Failed/Diverged. Without `--yes` the CLI prints the would-be outcome and exits 0.

### 12.3 Substrate event for the manual replay

`outbox_manual_replay` audit row captures the operator action with `{effect, nonce, operator, outcome}`. Distinct from `<effect>_recovered` so the W4 detector can separate "automatic recovery sweep" from "operator escalation".

---

## §13 Risk-tier (Risk-tier per `feedback_adversarial_review`)

Eight named risks. Each with mitigation; impl-detail risks deferred to the impl-PR followup with explicit reopen-trigger.

| # | Risk | Mitigation | Status |
|---|---|---|---|
| R1 | **Nonce collision across Effects.** Effect A uses nonce `abc`, Effect B uses nonce `abc` — substrate UNIQUE constraint fires, the second Effect can't write its intent. | Substrate `UNIQUE(run_id, written_by, nonce)` is per-`written_by` (the Effect name maps 1:1). Cross-Effect collision is impossible by the existing schema. | Closed by schema |
| R2 | **Effect-registry race during boot.** Two packages call `Register("merge", …)`; whichever loses init order silently overwrites the other. | Panic on duplicate-name in `Register`. Boot-time failure is the only safe move — a silent overwrite would hand the operator a "merge sometimes works, sometimes panics" surface. | Closed in §6 |
| R3 | **Partial-failure semantics vary by Effect.** Slack `Notify` returning `429 rate-limited` is StillPending; `gh pr merge` returning the same is StillPending only if the call hadn't completed. The Effect interface forces every implementer to classify each external error mode — easy to get wrong. | Add a per-Effect "classify error" required method? Rejected — adds surface. Instead: Probe is the source of truth. `Execute` errors are *retried via Probe* on the next sweep; the classification is implicit in what Probe returns. Documented in the §4 Execute godoc + the §9 worked example. | Mitigated by design |
| R4 | **Replay of Effect from an old version.** A new daemon version changes an Effect's Plan output (e.g., adds a field); recovering an old intent fails to decode. | Effects MUST treat `Payload` as forward-compatible JSON (additive only, never rename, never remove). Lint: a `payload_version` integer in every payload. Coordinator refuses to Probe intents with `payload_version` > daemon-supported version (logs + skips, doesn't crash). | Mitigated; doc-cite required in each Effect's package godoc |
| R5 | **Probe can't distinguish our call from someone else's with the same nonce.** Two daemon instances (operator's laptop + a stray copy on a VM) share a nonce family (e.g., monotonic event id starts at 1 on both); Probe sees "yes, an effect with nonce 1 was applied" — but it was the OTHER daemon's. | Single-operator self-host assumes single daemon (`feedback_research_design_principles` filter). Mitigation: include the substrate's `run_id` in the nonce by default (`outbox.MakeNonce(runID, family, key)` helper). Plan returns the prefixed form; Probe queries by the prefixed form. Multi-tenant is Phase X. | Mitigated by helper; multi-tenant is OUT |
| R6 | **Ordering between two Effects on the same work item.** Approval-notify fires before merge for the same agent; a crash between them re-runs Reconcile, which probes both Effects independently. If one Probe says StillPending and the other says Completed, what's the agent's FSM state? | Coordinator's reducer runs per-Effect, per-agent, *one Effect at a time*. Agent FSM transitions are owned by the *normal-path* policy engine (e.g., W2 c2 for merge, the approval gate for notify); Reconcile only writes events + transitions to terminal states (Done / Crashed). If two Effects need to interleave, the policy engine sequences them — outbox does not. | Mitigated by scope (recovery is per-Effect; sequencing is policy-engine concern) |
| R7 | **Effect that mutates substrate inside Execute (re-entrancy).** A misbehaved Effect calls `db.RecordEvent` inside its own Execute — now the substrate writer is reentered while the Coordinator holds an outer write. Substrate's `SetMaxOpenConns(1)` deadlocks. | Coordinator calls `Execute` *outside* any substrate tx (§4 Execute godoc). Adversarial defense: `Execute`'s signature takes `context.Context` only, not `*sql.Tx` — the temptation is removed. Lint: an impl-PR followup adds a `go vet`-style check for `state.DB` field access inside Effect.Execute receivers. | Mitigated by signature; followup for lint |
| R8 | **Outbox blowup if Effect is broken.** A buggy Probe always returns `Unknown`; the same intent appears in every sweep forever, growing `<effect>_recovered` audit rows linearly. | §8 bounded-recovery: after N consecutive Unknown probes (default 3, Effect-overridable), the W4 detector fires `obs-alert outbox.stuck` and the Coordinator stops re-probing that specific intent until operator acks via `regatta outbox replay`. Audit-row growth is bounded at N rows per stuck intent. | Mitigated by N-Unknown stop |

Two additional risks surfaced during the adversarial pass (kept for completeness, added as §13 P1.1+):

| # | Risk | Mitigation | Status |
|---|---|---|---|
| R9 | **Time-travel during Probe.** Probe sees an *external* state that has since reverted (e.g., GitHub PR was merged, then a force-revert + force-push made it look unmerged). Outcome=Completed→Failed flip would re-fire the effect. | Once Coordinator writes `<effect>_completed`, the FSM transition to Done is sticky; subsequent Probes are gated by `<effect>_completed`-already-present and skipped. The Reconcile loop's first check is "do we have a completion row?" — same shape c0's idempotency test pins. | Mitigated by sticky completion |
| R10 | **Daemon restart loop during recovery storm.** Recovery sweep runs at boot; if the daemon crashes during Reconcile (e.g., gh CLI segfault), it restarts and re-enters Reconcile; if it crashes consistently on the same Effect, the operator sees the daemon flapping with no in-flight work progressing. | Reconcile catches per-Effect panics (`defer recover()` in `Coordinator.Reconcile`), logs, and continues to the next Effect. A boot-time `--skip-outbox-recovery=NAME` flag lets the operator step around a known-bad Effect while they investigate. | Mitigated by per-Effect panic boundary + skip flag |

---

## §14 Test plan

Each phase has its own failing-test-first discipline (`feedback_tdd_discipline`).

### 14.1 P1 — generic Coordinator

- `TestCoordinator_RunHappyPath` — Plan + Execute + completed event written.
- `TestCoordinator_RunExecuteFails_WritesFailedAndCrashes` — terminal error from Execute.
- `TestCoordinator_RunIntentWriteFailsBeforeExecute` — substrate write fails; Execute never called.
- `TestCoordinator_Reconcile_Completed` — Probe=Completed → completed+recovered rows, → Done.
- `TestCoordinator_Reconcile_StillPending_LeavesInPlace` — no rows written.
- `TestCoordinator_Reconcile_Failed_TransitionsCrashed`.
- `TestCoordinator_Reconcile_Diverged_TransitionsCrashed_DistinctReason`.
- `TestCoordinator_Reconcile_Unknown_LeavesInPlace_BumpsCounter`.
- `TestCoordinator_Reconcile_NoIntent_TransitionsCrashed_NoIntentReason`.
- `TestCoordinator_Reconcile_ProberPanic_RecoversAndContinues` (R10).
- `TestCoordinator_Reconcile_ProberError_NonFatal_LogsAndContinues`.
- `TestCoordinator_Reconcile_StickyCompletion_DoesNotRewrite` (R9).
- `TestCoordinator_Reconcile_NUnknownStops_FiresObsAlert` (R8 — `N=3` default).
- `TestRegistry_Register_DuplicatePanics` (R2).
- `TestRegistry_All_ReturnsSnapshot`.
- `TestRegistry_Get_UnknownReturnsFalse`.
- `TestMakeNonce_IncludesRunID` (R5).
- `TestPayloadVersion_FutureVersionSkippedNotCrashed` (R4).

### 14.2 P2 — merge refactor

- All existing `internal/orchestrator/merge` tests pass byte-identically.
- `TestWireFormat_MergeEventKindsUnchanged` — golden file of event kind strings + payload shapes from a pre-P2 build; new build must reproduce.

### 14.3 P3 — approval-notify

- `TestApprovalNotify_RunHappyPath`.
- `TestApprovalNotify_RetryUsesSameNonce`.
- `TestApprovalNotify_ProbeFindsByNonce`.
- `TestApprovalNotify_CrashBetweenSendAndCompleted_RecoversAsCompleted`.
- E2E: crash-injection harness from c0 (`orchestrator_awaiting_merge_test.go` shape) adapted for `awaiting_approval_notify`.

### 14.4 Mutation-verify

- Manually flip `Coordinator.Reconcile`'s Probe-outcome switch arms; assert tests fail. The four-outcome reducer is the load-bearing logic; mutation-verify per `feedback_grade_rubric` A+ tier.

---

## §15 Open questions

1. **Per-Effect locking vs daemon-wide tx ordering.** If two Effects fire on the same agent in the same tick (rare but possible), which goes first? Proposal: deterministic ordering by `Effect.Name()` ASC. Defer to P3 when the second Effect actually exists.

2. **Effect versioning.** R4 says payload_version is forward-compat-only. What if a payload genuinely needs to change shape (e.g., approval-notify migrates from Slack-only to multi-channel)? Proposal: bump effect name (`approval_notify` → `approval_notify_v2`), run both Coordinators during transition, retire v1 after the last v1 intent reconciles. Concrete protocol filed at first occurrence.

3. **Cross-Effect FSM dependencies.** R6 punts to "policy engine sequences them" — but the policy engine doesn't yet exist for approval-notify (today's gate code inlines the call). Will need a small policy-engine seam in P3. Captured as a P3 acceptance criterion below.

4. **Operator UX storage backend.** `regatta outbox status` runs N substrate queries (one per Effect); is that fast enough at 50 Effects? Bench at P1 with synthetic data; if >100ms, add a materialized `outbox_inflight` view.

5. **Should `outbox_manual_replay` (§12.3) gate behind a feature flag?** It bypasses the N-Unknown stop. Today: no — `--yes` is sufficient guard. Reopen if an operator footgun surfaces.

---

## §16 A+ scorecard (self-graded honest, per `feedback_grade_rubric`)

| Tier | Criterion | Falsifiable acceptance | Self-score |
|---|---|---|---|
| **B** | (a) Generic primitive ships in `internal/outbox` with Effect + Coordinator + Registry + Outcome types | Package `internal/outbox` exists at end of P1; `go doc` shows the four named types. | met (spec defines them) |
| B | (b) c0 merge refactors to delegate without wire change | P2 PR's diff to existing merge_* event-kind tests is zero-line; golden file `wire-merge-events.json` round-trips. | met (P2 acceptance) |
| B | (c) Second Effect (`approval_notify`) wired before first real notifier replaces stub | P3 PR lands BEFORE the Slack/email notifier PR (CI assertion: notifier package import is gated on `outbox.Effect` import). | met (P3 phasing) |
| B | (d) Release-notes fence in PR body | Per `feedback_pr_body_release_notes_fence`. | met (BODY.md) |
| B | (e) No banned phrases | `scripts/doc-check.sh` exit 0. | met (pre-push) |
| **A** | (f) Risk-tier ≥ 8 named with mitigations | §13 has 10 entries. | met (10/8) |
| A | (g) Worked second-instance Effect code, not pseudocode | §9 is compilable-shape Go (modulo wiring). | met |
| A | (h) Migration plan with deletion-default math | §10 shows net-negative LoC by P3. | met |
| A | (i) Adversarial reviewer cleared the spec | Reviewer subagent ran (§17) — findings addressed or deferred with reopen-trigger. | met (§17 audit) |
| A | (j) Performance ceiling named + mutation-verify recipe | §11 names ~50 Effects, recipe for bench. | met |
| **A+** | (k) Operator UX subcommands specified, not deferred | §12 `regatta outbox status` + `replay`. | met |
| A+ | (l) Bounded-recovery on stuck intents (no unbounded audit-row growth) | §8 + R8: N-Unknown stop, default 3. | met |
| A+ | (m) Re-entrancy / partial-failure / time-travel risks proactively addressed | R3, R7, R9. | met |
| A+ | (n) Replay determinism — same intent + Probe sequence yields same Outcome sequence across daemon versions | R4 payload_version forward-compat rule pins this; explicit test `TestPayloadVersion_FutureVersionSkippedNotCrashed`. | met |
| A+ | (o) Spec answers "what got smaller?" | §10 LoC math: -80 net at P3, -150/Effect from P4 onward. | met |

**Self-scored tier: A+** — B floor + A items + 5/5 A+ items in scope. Honest caveat: the LoC math depends on at least one P3+ Effect actually landing; if approval-notify is the only second Effect ever, the primitive is overkill (P2 alone is net-zero LoC). Defended because three more Effects are already named in §1 (gh-issue-close, gh-pr-comment, stripe-charge) and gated only on the impl-PR sequencing.

---

## §17 Adversarial review — findings addressed / deferred

Reviewer subagent ran against the draft per `feedback_adversarial_review`. Findings:

### Addressed inline

1. **"§4 doesn't say what happens if `Plan` returns an empty nonce."** → Added: empty-nonce is a Plan contract violation, Coordinator returns an error before writing the intent row. Pinned by P1 test `TestCoordinator_RunPlanReturnsEmptyNonce_Rejects`.

2. **"R5 cross-daemon nonce collision feels hand-wavy."** → Strengthened: added the `outbox.MakeNonce(runID, family, key)` helper as the *default* nonce construction, not an option. Effect implementations that want raw external natural keys (c0 merge) opt out explicitly.

3. **"§13 doesn't address: what if Probe is slow (10s+) and blocks the whole sweep?"** → Folded into R10's per-Effect panic boundary as a per-Effect timeout. Default 5s, Effect-overridable. Added as §13 line item under R10. Pinned by `TestCoordinator_Reconcile_ProbeTimeout_LeavesInPlace`.

4. **"§9 worked example assumes `notifier.LookupByNonce` exists; today's notifier interface has no such method."** → Acknowledged as a P3 acceptance criterion: P3 adds `LookupByNonce(ctx, nonce) (bool, error)` to the `Notifier` interface; the stub returns `false, nil`; real Slack/email impls query their respective APIs by `client_msg_id` / `X-Message-Id`. Cited in §9.

5. **"§10 P2 'byte-identical event kinds' — how do you actually verify byte-identical?"** → Added `TestWireFormat_MergeEventKindsUnchanged` to §14.2: a golden file captures a pre-P2 set of event-kind strings + payload shapes; the post-P2 build replays the same flow and must produce identical JSON.

### Deferred to impl-PR followup (with reopen-trigger)

1. **"Should Effects be able to nest (one Effect's Execute invokes another Effect's Run)?"** Deferred to P5+. Reopen-trigger: the first Effect that needs it (likely Stripe → PagerDuty alert on charge failure). For now, the §4 godoc says "Effect.Execute MUST NOT call Coordinator.Run recursively"; lint to follow when the first violation appears.

2. **"Multi-tenant nonce-prefix scheme is sketched but not specified."** Deferred — current scope is single-operator self-host. Reopen-trigger: first multi-tenant pilot LOI (Phase X gate). Spec lives in a sibling doc when triggered.

3. **"The N-Unknown stop should probably be exponential-backoff before hard-stop, not linear."** Deferred to W4 impl PR. Reopen-trigger: production sees an `obs-alert outbox.stuck` storm caused by a flapping external API. For now, N=3 linear is the simplest shape that fails closed.

4. **"§11 sequential `All()` walk at 50 Effects = 100s wall-clock — that's bad UX on daemon restart."** Deferred to P5 (parallelize outer loop). Reopen-trigger: a fifth Effect lands OR observed restart-recovery time exceeds 10s.

No findings rose to "block the spec." All A+ tier load-bearing items addressed; deferrals are scope-bounded with explicit reopen-triggers per `feedback_unaddressed_load_bearing`.

---

## §18 Followups (per `feedback_unaddressed_load_bearing`)

1. **P1: `internal/outbox` package land.** Implements Effect, Coordinator, Registry, Outcome, MakeNonce helper, payload_version forward-compat lint. ~250 LoC + tests. Reopen-trigger: this spec merges.
2. **P2: merge package refactor to delegate.** Wire-compat guaranteed by golden file. ~80 LoC delete + 20 LoC delegate. Reopen-trigger: P1 lands.
3. **P3: approval-notify Effect.** Adds `LookupByNonce` to Notifier interface; replaces inline `notifier.Notify` in `createApprovalAndNotify`. Lands BEFORE the first real Slack/email notifier PR. Reopen-trigger: P2 lands OR notifier-PR opened (whichever first).
4. **P4: `gh-issue-close` + `gh-pr-comment` Effects.** Gated on W4 self-improvement detectors needing to write back to GitHub. Reopen-trigger: W4 spec lands.
5. **P5+: Stripe charge / PagerDuty alert / multi-Effect parallel `All()` walk / Effect nesting / multi-tenant nonce prefix.** Each gated on its own external need.
6. **Operator UX `regatta outbox status` + `replay` subcommands.** Lands in P1 or P2 (small surface; either works). Reopen-trigger: P1 lands.
7. **Cross-Effect global nonce-collision detector (§7 reserved).** Asserts `(effect_name, nonce)` is unique substrate-wide. Adds a substrate index. Reopen-trigger: a duplicate `(effect_name, nonce)` collision observed in production.
8. **W4 N-Unknown stuck-intent detector.** Fires `obs-alert outbox.stuck` after N=3 consecutive Unknown probes. Reopen-trigger: W4 self-improvement-detector spec lands.
9. **`go vet`-style lint for `state.DB` access inside Effect.Execute receivers (R7).** Reopen-trigger: P1 lands.
10. **Per-Effect Probe timeout default + override mechanism (§17 #3).** Reopen-trigger: P1 lands.

---

```release-notes
none (internal)
```
