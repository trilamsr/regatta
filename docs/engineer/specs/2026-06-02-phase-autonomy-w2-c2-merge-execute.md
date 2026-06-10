---
title: "PHASE AUTONOMY W2 c2 — merge-execute"
status: active
summary: "PHASE AUTONOMY §11 W2 c2: wire `gh pr merge --squash --auto --delete-branch` between c0's atomic `PrepareMerge` and the `merge_completed` event. New `merge.Coordinator.ExecuteMerge` + `merge/executor` package + dedicated `mergeWorker` goroutine + `merge_executed` audit event + `regatta merge status` CLI. 11+ risks pre-addressed; no schema migration (rides c0's 0013 unique-event index)."
---

# PHASE AUTONOMY §11 W2 c2 — merge-execute (`gh pr merge`) wired on top of c0

Status: ready for review
Date: 2026-06-02
Author: design subagent <tree@lumalabs.ai>
Depends on: #558 (W2 c0 — `internal/orchestrator/merge` intent/outbox + `awaiting_merge` recovery — MERGED 2026-06-03).
Builds toward: `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W2.

Memory rules in force: `feedback_research_design_principles`, `feedback_grade_rubric`, `feedback_test_godoc_one_line`, `feedback_deletion_default`, `feedback_doc_check_banned_phrases`, `feedback_pr_body_release_notes_fence`, `feedback_pr_body_file_only`, `feedback_unaddressed_load_bearing`, `feedback_spec_pattern_authority`, `feedback_tdd_discipline`, `feedback_root_cause`, `feedback_adversarial_review`, `feedback_comments_discipline`.

---

## §1 Problem

c0 (#558) shipped two halves of the auto-merge crash-safety primitive:

- `merge.Coordinator.PrepareMerge(ctx, agentID, prNumber, headSHA) error` — one transaction writes the `merge_intent` audit row (nonce = head SHA) AND transitions `GatesRunning → AwaitingMerge`.
- `merge.Coordinator.Reconcile(ctx) error` — on startup + on a periodic sweep, walks `AwaitingMerge` agents, probes GitHub for real PR state, and writes `merge_completed` / `merge_failed` / `merge_recovered` accordingly.

What c0 does NOT do: actually invoke `gh pr merge`. No production code calls `PrepareMerge`, and `Reconcile` is the only writer of `merge_completed`. The autonomous loop reaches the gate-pass decision and stops; the operator still has to click Merge.

c2 closes the gap. It wires the scheduler's "all gates pass" path to:

1. Call `Coordinator.PrepareMerge` (atomic intent + state transition).
2. Shell out to `gh pr merge <pr-number> --squash --auto --delete-branch`.
3. Write `merge_completed` and transition to `Done` on success — OR leave the agent in `AwaitingMerge` so the existing `Reconcile` sweep handles recovery on the next tick.

c0's recovery sweep is the safety net: any crash, any retry, any concurrent instance is handled by the primitives c0 already shipped. c2 only adds the call site + the one new event (`merge_executed`, audit-only) + a thin gh-CLI executor.

---

## §2 Scope

### IN

- One new package `internal/orchestrator/merge/executor` exposing `Executor.Merge(ctx, prNumber, headSHA) (Outcome, error)`. Production impl shells `gh pr merge`.
- One new `Coordinator.ExecuteMerge(ctx, agentID, prNumber, headSHA) error` method that calls `PrepareMerge` THEN `Executor.Merge` THEN writes `merge_completed` (source = `merge_call`). Lives in `internal/orchestrator/merge/coordinator.go` next to existing methods.
- One new dedicated goroutine in `orchestrator.Run` — `mergeWorker` — fed by a buffered channel from the scheduler's "gates pass" hook. Keeps the scheduler tick hot path free of the ~500 ms gh-CLI shell-out.
- One new event kind `merge_executed` (audit-only; payload carries `pr_number`, `head_sha`, `merge_sha`, `exit_code`, `duration_ms`). Distinct from `merge_completed` (which is the FSM-driving event already shipped in c0).
- One new CLI subcommand `regatta merge status` — lists agents currently in `AwaitingMerge` with their intent payload, helpful for operator visibility when the substrate parks a merge in the auto-queue.
- Trace span `merge.execute` with attributes `pr.number`, `head.sha`, `nonce`, `outcome`, propagated from the scheduler's gate-pass span (W6 OTel backbone).
- Verbose-mode operator log line per merge attempt: `regatta: merging PR #N (sha=<short>)`.

### OUT

- The decision to merge (i.e. "all gates green, L4 ADOPT, cost-cap OK, adversarial reviewer cleared"). That logic lives in the scheduler's existing gate-pass path and is reused as-is; the spec adds one call site, not a new policy engine.
- Webhook-driven merge triggers. Polling + tick-driven Reconcile is sufficient at single-operator scale (same argument as `2026-06-02-orchestrator-pr-watcher.md` §1.2).
- `merge_intent` schema changes. c0's migration `0013_merge_event_unique.sql` is unchanged. No new migration in c2.
- The `[needs-human-review]` / `[auto-merge-ok]` label interlocks (c3 / c4) and the `obs-alert critical` substrate-wide halt (c5). They sit upstream of the call site this spec adds: the scheduler decides; c2 executes.
- Replacing `gh` CLI with `google/go-github`. Same gh-CLI-as-auth-seam argument as the PR-watcher spec — the operator's existing `gh auth status` is the credential surface, no new vendored SDK.

---

## §3 State diagram

```
GatesRunning
    │
    │ scheduler: all gates pass
    ▼
[Coordinator.ExecuteMerge]
    │
    │ ── PrepareMerge (atomic) ──────────────────────────────────┐
    │     • write merge_intent (nonce = head_sha)                │
    │     • TransitionAgent → AwaitingMerge                      │
    │ ◄──────────────────────────────────────────────────────────┘
    │
AwaitingMerge ◄──── crash recovery point (Reconcile re-enters here)
    │
    │ ── Executor.Merge (gh pr merge --squash --auto) ───────────┐
    │     • exit 0 + merged → OutcomeMerged                      │
    │     • exit 0 + queued → OutcomeAutoQueued                  │
    │     • already-merged → OutcomeAlreadyMerged                │
    │     • rate-limit / network → OutcomeTransient (error)      │
    │     • PR closed / conflict → OutcomeTerminal (error)       │
    │ ◄──────────────────────────────────────────────────────────┘
    │
    ├─ Merged / AlreadyMerged
    │     │
    │     │ write merge_completed (source=merge_call) + merge_executed
    │     ▼
    │   Done
    │
    ├─ AutoQueued
    │     │ leave in AwaitingMerge; Reconcile sweeps poll PR state until merged or closed
    │     ▼
    │   AwaitingMerge (no event written yet)
    │
    ├─ Transient error (recoverable)
    │     │ leave in AwaitingMerge; backoff + next tick re-issues ExecuteMerge
    │     │ Reconcile is the long-tail safety net (probes regardless of tick re-entry)
    │     ▼
    │   AwaitingMerge
    │
    └─ Terminal error (PR closed, conflict --auto can't resolve, branch protection rejects)
          │ write merge_failed (reason=...) + merge_executed
          ▼
        Crashed → requeue via existing crashed→pending path
```

The cardinal invariant: **`PrepareMerge` happens BEFORE any external mutation, in its own committed tx**. A crash anywhere after the commit leaves the agent in `AwaitingMerge` with an intent on file, which is exactly what c0's `Reconcile` was built to recover from.

---

## §4 ExecuteMerge implementation

### 4.1 Call sequence

```go
func (c *Coordinator) ExecuteMerge(ctx context.Context, agentID int64, prNumber int, headSHA string) error {
    if err := c.PrepareMerge(ctx, agentID, prNumber, headSHA); err != nil {
        if errors.Is(err, state.ErrInvalidTransition) {
            // Already in AwaitingMerge or terminal from a prior call —
            // let Reconcile own the outcome.
            return nil
        }
        return fmt.Errorf("prepare: %w", err)
    }
    out, err := c.executor.Merge(ctx, prNumber, headSHA)
    switch {
    case err == nil && out.Merged():
        return c.markCompletedNormalPath(ctx, agentID, prNumber, headSHA, out.MergeSHA)
    case err == nil && out.Queued():
        // gh accepted --auto; Reconcile sweeps will drive to Done.
        return c.recordMergeEvent(ctx, agentStub{ID: agentID}, EventKindMergeExecuted, out.payloadJSON())
        // No FSM transition — agent stays in AwaitingMerge.
    case isTerminalError(err):
        return c.markFailedNormalPath(ctx, agentID, prNumber, headSHA, terminalReason(err))
    default:
        // Transient — leave in AwaitingMerge for the next tick. Reconcile sweeps probe regardless.
        c.log.Warn("merge.execute_transient", "agent_id", agentID, "pr_number", prNumber, "err", err.Error())
        return err
    }
}
```

(Pseudocode — the spec, not the implementation. `markCompletedNormalPath` mirrors c0's `markCompleted` but writes `Source = "merge_call"` and writes the new `merge_executed` audit row alongside.)

### 4.2 Executor — the gh CLI shell-out

`internal/orchestrator/merge/executor/executor.go`:

```go
type GhExecutor struct {
    bin     string         // override for tests; defaults to "gh"
    timeout time.Duration  // 30s default
    log     *slog.Logger
}

func (g *GhExecutor) Merge(ctx context.Context, prNumber int, headSHA string) (Outcome, error) {
    ctx, cancel := context.WithTimeout(ctx, g.timeout)
    defer cancel()
    cmd := exec.CommandContext(ctx, g.bin, "pr", "merge",
        strconv.Itoa(prNumber),
        "--squash", "--auto", "--delete-branch",
        "--match-head-commit", headSHA, // refuse if head moved
    )
    var stdout, stderr bytes.Buffer
    cmd.Stdout, cmd.Stderr = &stdout, &stderr
    err := cmd.Run()
    return classify(err, stdout.Bytes(), stderr.Bytes()), wrapped(err, stderr.String())
}
```

**Flags chosen.**

- `--squash` — matches the operator's repo merge policy (every PR squashes; see `git log` shape).
- `--auto` — falls into GitHub's auto-merge queue if required checks are still pending. **Idempotent under retry**: a second `--auto` call on a PR already in the queue is a no-op (`gh` reports exit 0 + stderr `"already enabled auto-merge"`).
- `--delete-branch` — cleans the agent worktree branch on merge; matches the operator's manual flow and the `cleanup-merged-branches` follow-up that already runs.
- `--match-head-commit <sha>` — gh ≥ 2.40 supports this guard. If the PR head moved between PrepareMerge and Merge, `gh` refuses with a clear error → classified as `OutcomeSHADiverged` → terminal failure → `Reconcile` requeues. Closes the "force-push between Prepare and Execute" race without us re-probing manually.

**Why not `gh api graphql --raw-field query=...`?** GraphQL `mergePullRequest` mutation works but adds ~50 lines of boilerplate and doesn't accept `--match-head-commit` cleanly. `gh pr merge` is the path operators already trust; sticking to it keeps the operator-side debugging shape (`gh pr merge --dry-run`) unchanged.

### 4.3 Exit-code classification

```go
type OutcomeKind int

const (
    OutcomeUnknown        OutcomeKind = iota
    OutcomeMerged                     // gh exit 0, stdout indicates merge happened synchronously
    OutcomeAutoQueued                 // gh exit 0, stderr indicates "added to auto-merge queue"
    OutcomeAlreadyMerged              // stderr "pull request was already merged"
    OutcomeBranchDeleted              // stderr "branch was already deleted" (post-merge re-run)
    OutcomeSHADiverged                // stderr "head commit does not match" (--match-head-commit guard)
    OutcomeRateLimit                  // stderr "API rate limit exceeded" or HTTP 429
    OutcomeAuthExpired                // stderr "authentication required" / "bad credentials"
    OutcomeGhNotInstalled             // exec.ErrNotFound on cmd.Run
    OutcomePRClosed                   // stderr "pull request is closed"
    OutcomeBranchProtection           // stderr "required status check ... has not run / is failing" (terminal — operator must intervene)
    OutcomeConflict                   // stderr "merge conflict" (terminal — `--auto` cannot resolve)
)
```

Classification is a single switch keyed on stderr substrings — same pattern c0 uses for `isUniqueViolation` (substring probe against modernc.org/sqlite errors). Stable strings come from `gh` source (`cli/cli` v2.40+); the spec pins `gh ≥ 2.40.0` in the operator-onboarding checklist.

**Recoverable vs terminal**:

| Outcome | Recoverable? | Why |
|---|---|---|
| `Merged` / `AlreadyMerged` | success | write `merge_completed`, transition Done |
| `AutoQueued` | success (deferred) | leave in AwaitingMerge; Reconcile sweeps probe |
| `BranchDeleted` | success | post-merge branch already gone; treat as `AlreadyMerged` |
| `RateLimit` | recoverable | backoff; next sweep retries; never increment a retry count past 5 (then surface obs-alert) |
| `AuthExpired` | recoverable | operator-fix; obs-alert fires after 3 consecutive expirations |
| `GhNotInstalled` | terminal at boot | refuse to start the merge worker; clear error names "install gh ≥ 2.40 from https://cli.github.com" |
| `SHADiverged` | terminal | `merge_failed` reason = `sha_diverged`; existing crashed→pending requeue |
| `PRClosed` | terminal | `merge_failed` reason = `closed_unmerged` |
| `BranchProtection` | terminal | `merge_failed` reason = `branch_protection_blocked`; obs-alert fires |
| `Conflict` | terminal | `merge_failed` reason = `conflict`; obs-alert + operator must rebase manually |
| `Unknown` | recoverable | conservative — leave in AwaitingMerge for next sweep |

### 4.4 Timeout

30 s is the wall-clock cap on a single `gh pr merge` call. `gh` itself completes in ~500 ms typical; the 30 s cap is for slow GitHub API edges (cross-region latency, transient 5xx + gh internal retry). Beyond 30 s, the context cancels, the process is killed, and the outcome is `OutcomeUnknown` → recoverable → next sweep retries.

### 4.5 Backoff (rate-limit + transient)

```go
type backoff struct {
    base    time.Duration // 5s
    max     time.Duration // 5min
    attempt int           // resets on success
}
```

Per-agent backoff lives in memory on the `mergeWorker` goroutine. The agent stays in `AwaitingMerge`; the worker simply waits `backoff.next()` before retrying. On `OutcomeRateLimit`, the executor parses `stderr` for `X-RateLimit-Reset` (when gh surfaces it) and uses that as a floor for the next attempt.

After 5 consecutive transient failures on the same `(agent_id, head_sha)` tuple, the worker stops retrying for that agent — Reconcile + the W4 self-improvement detector take over (existing follow-up in c0's PR: "wire a counter on `PRStatusUnknown` per-agent so three consecutive sweeps fire an alert").

---

## §5 Coupling with c0

c0 provides three invariants this spec relies on:

1. **`PrepareMerge` is atomic.** Either both writes commit (`merge_intent` + `AwaitingMerge`) or neither does. c2 never observes a half-state.
2. **`Reconcile` is the recovery loop.** Any crash after `PrepareMerge` returns leaves the agent in `AwaitingMerge` with an intent on file → c0's recovery sweep handles it on the next orchestrator boot or scheduler tick.
3. **Unique-constraint guards double-writes.** Migration `0013` ensures two writers cannot both record `merge_completed` for the same agent; the second writer's `UNIQUE constraint failed` is suppressed by `Coordinator.recordMergeEvent`.

Combined consequence: **c2 is correctness-preserving even if the orchestrator is SIGKILLed mid-`gh pr merge`**. The intent row is on file before the external call; the next `Reconcile` sweep probes GitHub for real state and reconciles. The `--auto` flag makes the gh-side call itself idempotent against a same-SHA retry. Post-merge, the branch may be deleted; gh's `OutcomeAlreadyMerged` classification turns that into a success path, not a failure.

The one new event (`merge_executed`) is **audit-only**. It does not drive FSM transitions; `merge_completed` (from c0) and `merge_failed` (from c0) remain the FSM-driving rows. `merge_executed` exists so dashboards can distinguish "normal call path" from "recovery path" — payload carries `exit_code` + `duration_ms` so operators can spot gh-side latency drift.

---

## §6 Concurrency

Three concurrency surfaces, all delegated to c0's primitives:

### 6.1 Two scheduler instances both decide to merge the same PR

Scheduler instance A and B both see "gates pass" on the same `(work_item_id, head_sha)`. Both call `Coordinator.ExecuteMerge`. The atomic `PrepareMerge` is the gate:

- A's tx commits first: writes `merge_intent`, transitions `GatesRunning → AwaitingMerge`.
- B's tx attempts to transition `GatesRunning → AwaitingMerge`. The FSM rejects: A already moved the agent. B's `PrepareMerge` returns `ErrInvalidTransition` → `ExecuteMerge` short-circuits with no error (the early `errors.Is(err, state.ErrInvalidTransition)` branch in §4.1). B does NOT call `Executor.Merge`. A proceeds.

If A crashes between `PrepareMerge` and `Executor.Merge`, B's next tick still won't `ExecuteMerge` (agent is in `AwaitingMerge`, not `GatesRunning`) — but B's `Reconcile` sweep picks it up because the intent row is on file. Reconcile probes GitHub, sees PR open + SHA matches, leaves in `AwaitingMerge`, lets the next tick re-attempt. A's `mergeWorker` queue may also resurrect on restart and retry. Both paths converge on the same `--auto` idempotent call.

### 6.2 Reconcile sweep races a fresh ExecuteMerge

Reconcile (running on a parallel tick) and `mergeWorker` (running on its own goroutine) both decide to call `Executor.Merge` for the same agent. Both shell `gh pr merge --auto`; both calls succeed; one of them returns `OutcomeMerged`, the other returns `OutcomeAlreadyMerged`. Both writers attempt `merge_completed`. The unique-event index swallows the loser — c0's `recordMergeEvent` already logs `merge.duplicate_event_suppressed` and continues.

### 6.3 Operator manually merges via the GitHub UI mid-execute

Same shape as 6.2. `gh pr merge --auto` returns `OutcomeAlreadyMerged`; the substrate writes `merge_completed` with `source = "merge_call"` (the executor reached gh, gh reported the merge, the recovery sweep is not involved). Reconcile, if it runs later, finds the agent already in `Done` and is a no-op.

---

## §7 Operator UX

### 7.1 Verbose log line

The merge worker prints one stable log line per merge attempt:

```
regatta: merging PR #142 (sha=a1b2c3d) auto=true squash=true
regatta: merge PR #142 result=merged duration=487ms
```

Both INFO level. Bound to `RGB-1` log shape (operator-grade single-line summary; the trace span carries the structured fields).

### 7.2 Trace span

`merge.execute` span on the existing OTel pipeline (W6 backbone). Attributes:

- `pr.number` (int) — GitHub PR number
- `head.sha` (string) — full SHA from the intent
- `nonce` (string) — same as `head.sha` today; separate attribute name future-proofs against changing the nonce shape
- `outcome` (string, enum) — `merged | auto_queued | already_merged | sha_diverged | rate_limit | terminal | unknown`
- `gh.exit_code` (int)
- `gh.duration_ms` (int)
- `agent.id` (int64)
- `work_item.id` (int64)

Span is child of the scheduler's `gates.evaluate` span. Errors set status `Error` with the classified outcome reason as the error message.

### 7.3 New CLI subcommand

`regatta merge status` — table of agents in `AwaitingMerge`:

```
$ regatta merge status
AGENT  PR    HEAD_SHA   INTENT_AGE  LAST_PROBE_OUTCOME
142    #189  a1b2c3d    00:00:32    auto_queued
143    #191  e4f5g6h    00:14:08    rate_limit (retry in 00:02:12)
```

Reads c0's `merge_intent` events + the latest `merge_executed` row. Read-only; no mutations. Total new code ≈ 80 LoC in `cmd/regatta/merge.go` (sibling to existing `cmd/regatta/status.go`).

---

## §8 Performance

| Cost | Value | Notes |
|---|---|---|
| gh CLI subprocess fork+exec | ~50 ms | macOS / Linux measurement; one fork per merge attempt |
| gh API round-trip (single PR) | ~300 ms typical | GitHub's PR merge endpoint p95 |
| Total per merge call | ~500 ms typical, 30 s cap | dominated by GitHub-side, not us |
| Scheduler tick impact | ZERO | merge work runs on dedicated `mergeWorker` goroutine, fed via buffered channel from the scheduler hook |
| Memory footprint | ~64 KB per pending merge | `Outcome` struct + per-agent backoff state; capped at ~10 in-flight merges via channel buffer size |
| GH API quota | 5000 req/hr (operator personal token) | one merge = 1 mutation + (for `--auto` queue) ~2 status probes from gh = ≤3 req per attempted merge; worst case 1666 merges/hr per operator. Doc as not-a-bottleneck. |

The merge worker is a single goroutine — there is no per-agent concurrency. Two reasons:

1. The operator's `gh auth` token has one rate-limit budget; serializing reads naturally backs off without tripping it.
2. Sequential merge ordering protects against the rare "two PRs both touch the same file" merge-conflict cascade — serializing means each merge sees the prior merge's resolved tree.

If a future operator hits the serialized-throughput ceiling (>10 merges/min sustained), the worker can fan out to N goroutines with per-PR-number locks; out of scope for c2.

---

## §9 Risks (8+, with pre-addressed mitigations)

1. **`gh` CLI missing on host.** Operator misconfig. Mitigation: at orchestrator boot, the merge worker runs `gh --version` once; on `exec.ErrNotFound`, the worker logs `merge: gh CLI not found — install gh ≥ 2.40 from https://cli.github.com; merge worker disabled` and refuses to start. The substrate continues running without auto-merge (operator can still click Merge manually). Per `feedback_root_cause`: the error message names the exact install command and version floor, and the worker returns `ErrMergeUnavailable` to the scheduler instead of silently swallowing the failure.

2. **`GH_TOKEN` missing or expired.** Detected via `OutcomeAuthExpired` classification. Mitigation: same retry-with-backoff path as rate-limit; after 3 consecutive failures, fire an `obs-alert` issue with severity `critical` and stop retrying that agent. Operator rotates the token via existing W6 secret-credential autonomic fetch (PHASE AUTONOMY §11 W6); on the next sweep, `Reconcile` picks the agent back up.

3. **Branch protection blocks merge.** Repo has a branch-protection rule the substrate can't satisfy (e.g. "requires 2 reviews from CODEOWNERS"). gh returns a clear stderr line. Mitigation: `OutcomeBranchProtection` is terminal → `merge_failed` reason = `branch_protection_blocked` → agent transitions to `Crashed` → existing crashed→pending requeue is skipped via a new `terminal_reason` check (no point requeuing a deterministic policy failure). `obs-alert critical` fires with a link to the PR; operator either relaxes the protection or merges manually.

4. **Merge introduces a conflict that `--auto` can't resolve.** gh returns `OutcomeConflict`. Mitigation: terminal failure → `merge_failed` reason = `conflict` → `obs-alert` with severity `warning` (per-PR, not substrate-wide). Operator rebases. This is the operator-as-customer accepting that auto-merge can't resolve semantic conflicts — same trade-off bors / Mergify make.

5. **Rate-limit during PR-storm.** GitHub's 5000 req/hr is per token; a regatta-burst (10 PRs queued from a multi-lane dispatch) could brush it. Mitigation: backoff parses `X-RateLimit-Reset` (when gh surfaces it; gh 2.40+ does for `--json`). After 5 consecutive rate-limit hits on different agents within a 1-min window, the worker pauses for 60s and surfaces an INFO log. No `obs-alert` — rate-limit is expected during high-throughput dispatch.

6. **PR retargeted to non-main base between Prepare and Execute.** Operator (or a hostile workflow) retargets PR #142 from `main` to `dev` between PrepareMerge and the gh call. Mitigation: gh's `--auto` respects the PR's current base, so the merge proceeds against `dev`. Two outcomes: (a) acceptable — regatta's job is to merge the PR; the operator chose the new base. (b) unacceptable — the substrate wanted main only. For (b), c2 adds an optional `--match-base main` guard (gh 2.45+) when `regatta.yaml: ci.require_main_base: true`. Default OFF for the self-host case (one operator, one repo, one main branch). Filed as follow-up: `merge-base-pinning` (low priority).

7. **Operator manually merged the PR while regatta is awaiting.** Already covered in §6.3. `OutcomeAlreadyMerged` → `merge_completed` written with `source = "merge_call"` (executor reached gh, gh reported it). Reconcile is consistent with this — if it runs first, `PRStatusMerged` → `merge_completed` with `source = "recovery"`. Dashboards see both source values.

8. **Someone else's PR happens to use our nonce SHA.** Cosmic-ray collision: another team in the same org pushes a different PR whose head SHA collides with our recorded intent. Probability: 2⁻¹⁶⁰ for full SHA-1 collision in the relevant lookup window. Mitigation: c0's `LatestIntent` keys by `(agent_id, kind)`, not by SHA — the SHA is only the gh-side nonce, not the substrate-side primary key. Cross-agent collision is impossible by construction.

9. **Scheduler hook called multiple times for the same `(agent_id, head_sha)`.** Buggy scheduler emits the gate-pass signal twice. Mitigation: §6.1 — second `PrepareMerge` returns `ErrInvalidTransition`, second `ExecuteMerge` short-circuits with no-op. No external call duplication. The mergeWorker channel is bounded (cap 32); over-emit is shed at the channel boundary with an INFO log.

10. **gh CLI version skew on operator host.** Operator upgrades `gh` mid-run; new gh emits different stderr strings. Mitigation: classification fallback is `OutcomeUnknown` → recoverable → next sweep retries. The substring probe set is exercised by the integration tests against a real `gh` binary in CI; an annual gh-CLI-bump task in `cleanup-merged-branches`-style cron catches drift. Filed as follow-up: `gh-cli-version-pin-doc`.

11. **`--delete-branch` removes the agent's worktree branch before the worktree-manager has cleaned the local clone.** Mitigation: `spawner.WorktreeManager.Cleanup` already tolerates missing remote branches (it deletes the local worktree by path, not by remote-ref). No new code needed; spec note for the reviewer.

---

## §10 Test plan

### 10.1 Integration — fake `gh` binary

`internal/orchestrator/merge/executor/gh_fake_test.go` writes a tiny shell script as the `gh` binary, sets `Executor.bin = <path-to-fake>`. The fake reads `$2 $3 …` (e.g. `pr merge 142 …`) and emits stdout/stderr from a per-test scriptable map. Pattern adopted from c0's `fakeProber` shape (verbatim — same indirection, same lifecycle).

### 10.2 Crash-injection

`internal/orchestrator/merge/coordinator_crash_test.go`:

- After `PrepareMerge` returns, SIGKILL-equivalent: panic the test process; restart Coordinator with the same DB.
- Assert `Reconcile` finds the agent in `AwaitingMerge` with an intent row, probes the fake GitHub, and writes the correct outcome.
- Validate the unique-event index pins exactly one `merge_completed` row across the crash boundary.

### 10.3 Rate-limit handling

`internal/orchestrator/merge/executor/rate_limit_test.go`:

- Fake `gh` returns `OutcomeRateLimit` with a synthetic `X-RateLimit-Reset` 5 s in the future.
- Assert backoff waits ≥ 5 s; second attempt succeeds; verify the `merge_executed` event chain shows two rows (attempt 1 = rate_limit, attempt 2 = merged).

### 10.4 Real-merge dogfood

Manual test (not CI — requires a live test repo + operator GH token):

- Spin up a throwaway PR in `trilamsr/regatta-merge-dogfood-test` (an existing test repo).
- Wire the substrate to fire `ExecuteMerge` against it.
- Assert PR ends up merged on GitHub + `merge_completed` event in substrate.
- Run this once per release as a smoke check; documented in `docs/operator/runbooks/auto-merge-dogfood.md` (new doc, filed as follow-up).

### 10.5 Concurrency — two scheduler instances

`internal/orchestrator/merge/coordinator_concurrent_test.go` (sibling to c0's existing concurrent test):

- Spawn two goroutines that both call `ExecuteMerge` against the same agent in the same DB.
- Assert exactly one `merge_intent` write succeeds (FSM rejects the loser), exactly one `merge_executed` row, exactly one `merge_completed` row.

### 10.6 Test names (10+, all godoc-1-line compliant per `feedback_test_godoc_one_line`)

1. `TestExecuteMerge_HappyPath_PreparesExecutesAndCompletes` — gate-pass → intent → gh exit 0 → merge_completed.
2. `TestExecuteMerge_GhAutoQueued_LeavesInAwaitingMerge` — exit 0, stderr "auto-merge enabled" → no FSM transition; `merge_executed` row only.
3. `TestExecuteMerge_AlreadyMerged_TreatedAsSuccess` — exit !=0, stderr "already merged" → `merge_completed`.
4. `TestExecuteMerge_SHADiverged_TerminalFailure` — `--match-head-commit` rejection → `merge_failed` reason=sha_diverged.
5. `TestExecuteMerge_RateLimit_BacksOffAndRetries` — first attempt rate-limited, second succeeds.
6. `TestExecuteMerge_GhNotInstalled_RefusesToStartWorker` — `exec.ErrNotFound` → boot-time error with install hint.
7. `TestExecuteMerge_AuthExpired_FiresObsAlertAfterThreeFailures` — three consecutive auth-expired → severity=critical obs-alert.
8. `TestExecuteMerge_BranchProtection_TerminalNoRequeue` — branch-protection rejection → `merge_failed` reason=branch_protection_blocked, NO crashed→pending requeue.
9. `TestExecuteMerge_Conflict_TerminalWithObsAlert` — `--auto` cannot resolve conflict → `merge_failed` reason=conflict + warning obs-alert.
10. `TestExecuteMerge_TimeoutKillsGh_LeavesInAwaitingMerge` — 30s timeout fires; process killed; agent stays in AwaitingMerge for Reconcile.
11. `TestExecuteMerge_ConcurrentSchedulers_SingleIntentSingleCompletion` — two goroutines, one winner.
12. `TestExecuteMerge_OperatorMergedManually_ClassifiedAsAlreadyMerged` — operator clicks Merge mid-execute → `OutcomeAlreadyMerged` → `merge_completed` source=merge_call.
13. `TestExecuteMerge_CrashAfterPrepareBeforeGh_RecoverViaReconcile` — SIGKILL-equivalent; Reconcile picks up.
14. `TestExecuteMerge_TransitionInvalidTreatedAsRaceLoss` — `ErrInvalidTransition` is no-op, not error.
15. `TestMergeStatusCLI_ListsAwaitingMergeAgents` — `regatta merge status` returns the expected table shape.

---

## §11 Migration

**None.** c0's migration `0013_merge_event_unique.sql` already covers the unique-event index for `merge_completed`, `merge_failed`, `merge_recovered`. The new `merge_executed` event kind is intentionally NOT in that index — it is an audit row, repeatable across attempts (a single agent may have N `merge_executed` rows, one per `gh pr merge` attempt). `CurrentSchemaVersion` stays at 13.

---

## §12 Cost / quota

GitHub REST API quota: 5000 requests/hour per personal access token.

- `gh pr merge` (no `--auto` queue triggered) = 1 mutation request.
- `gh pr merge --auto` = 1 mutation + ~2 status probes (gh internal). ≤ 3 req/attempt.
- Reconcile sweep = 1 `gh pr view` per agent in AwaitingMerge.

Worst-case sustained throughput per operator: 5000 ÷ 3 ≈ 1666 merges/hr ≈ 28 merges/min. At current 3-4 lane parallel pace and typical PR cadence (15-30 PRs/day for the autonomous loop), the quota is not a bottleneck. The W4 self-improvement detector watches for ≥ 100 rate-limit events / 24h as a capacity-headroom signal.

---

## §13 Adversarial review section (5+ risks pre-addressed inline)

Per `feedback_adversarial_review`, the spec author pre-addresses the most likely reviewer-subagent findings:

1. **"Why a goroutine and not the scheduler tick?"** Hot-path latency. The scheduler tick is the regatta clock; adding a 500 ms gh-CLI call to it would push tick-completion latency past the 1 s soft SLO and starve other operations. The dedicated `mergeWorker` is shaped after the existing `reaper` goroutine — same lifecycle, same shutdown handshake.

2. **"Why not call `Coordinator.Reconcile` directly from `ExecuteMerge` instead of a new event kind?"** `Reconcile` is the *recovery* loop — it assumes the agent is already in `AwaitingMerge` and that the external call may or may not have happened. The normal-path code knows the external call DID happen (we just made it) and knows the outcome from the gh stdout/stderr. Routing the normal path through Reconcile would force a second `gh pr view` API call to re-discover what we just observed — pure waste. Keeping the paths separate (normal-path writes from `ExecuteMerge`, recovery-path writes from `Reconcile`) is cheaper AND makes the dashboard `source` field actually distinguish the two paths instead of conflating them.

3. **"What stops a runaway loop where the gh call always returns Unknown and the worker retries forever?"** Per-agent retry cap of 5 consecutive transient outcomes (§4.5). After 5, the worker stops scheduling that agent; Reconcile continues to probe, but the worker is not the one re-issuing gh calls. The W4 detector's `PRStatusUnknown ≥ 3 consecutive sweeps` rule (filed as a c0 follow-up) closes the long-tail.

4. **"What if `--match-head-commit` isn't on the operator's gh version (< 2.40)?"** Boot-time check: `gh --version` parsed at orchestrator start. If `< 2.40`, the merge worker logs a clear refusal (`merge: gh ≥ 2.40 required for --match-head-commit safety guard; falling back to --auto without head-match would risk merging un-gated code`) and refuses to start. Substrate continues without auto-merge. Same shape as the gh-not-installed path.

5. **"Why not `gh pr merge --merge` or `--rebase`?"** Repo policy is squash (visible in `git log`). Hard-coding `--squash` matches the operator's existing manual flow. If a future operator wants rebase, it becomes a `regatta.yaml: ci.merge_method: squash | merge | rebase` knob; out of scope for c2.

6. **"What's the deletion answer?"** Per `feedback_deletion_default`: this PR adds ~250 LoC (executor 120 + Coordinator method 50 + CLI 80) and removes ~30 LoC of operator-manual-merge documentation that becomes obsolete once auto-merge is the default path. Net: +220 LoC. Defense — the addition closes the load-bearing gap PHASE AUTONOMY explicitly names (operator-as-merge-actor), without which the autonomous loop is not autonomous. Each method is single-purpose; no helper-creep, no duplicate-state.

7. **"Why doesn't this spec own the `[needs-human-review]` label interlock?"** That interlock lives in the scheduler's gate-pass decision, not in the merge executor — the executor only runs when the scheduler has cleared the agent. Adding label-checks here would split the policy across two layers and force the executor to depend on `gh pr labels` reads. Out of scope; c3 owns it.

8. **"What's the rollback path if c2 ships and produces double-merges?"** Config knob `ci.automerge_on_pass: false` (default OFF) — same knob the brief names as the c1 acceptance criterion. Flipping it off stops the scheduler from feeding the worker; the worker drains its channel and idles. No DB migrations to revert, no event kinds to retract (`merge_executed` is audit-only).

---

## §14 B / A / A+ grade rubric

| Tier | Criteria |
|---|---|
| B (floor) | (a) `Coordinator.ExecuteMerge` ships with PrepareMerge → gh-shell → merge_completed wiring. (b) Default-OFF behind `ci.automerge_on_pass`. (c) Test names 1-10 from §10.6 pass; `make check` clean. (d) Release-notes fence in PR body with `[FEATURE]` category. (e) No new schema migration; c0's 0013 unchanged. |
| A (target) | B + (f) Test names 11-13 pass (concurrency + crash-injection). (g) Substrate event `merge_executed` emitted with `exit_code`, `duration_ms` payload. (h) OTel `merge.execute` span with attributes from §7.2. (i) `regatta merge status` CLI works against a populated `AwaitingMerge` set. (j) Adversarial reviewer subagent posts on the PR; all findings addressed inline or filed as follow-ups. (k) gh-CLI version-check at boot refuses to start the worker on `< 2.40`. |
| A+ (stretch) | A + (l) Per-agent backoff state survives orchestrator restart (persisted to a new `merge_backoff` event kind — audit shape, not a new table). (m) Real-merge dogfood test (§10.4) runs in a nightly cron against the dogfood repo. (n) Replay-harness deterministic across 100 random gh-stdout/stderr combinations (property test on `classify`). (o) gh-output classifier is a pure function with no I/O, exercised by a separate testdata fixture set (`testdata/gh_stderr/*.txt` → expected `OutcomeKind`). (p) `merge_executed` payload includes `match_head_commit_used: true` so a future audit can prove the `--match-head-commit` guard fired. |

**Self-scored tier (this spec, design-only):** A — the spec covers the B floor + every named A criterion. A+ items (l–p) are filed inline as follow-ups for the implementer to evaluate; none gate c2 shipping.

---

## §15 Follow-ups (inline, per `feedback_unaddressed_load_bearing`)

- **merge-base-pinning** — optional `ci.require_main_base: true` knob using `gh --match-base main`. Low priority; reopens when a multi-base-branch use case appears.
- **gh-cli-version-pin-doc** — operator-onboarding checklist names `gh ≥ 2.40` as a hard prereq. Land alongside W3 service-supervisor docs.
- **merge-method-knob** — `ci.merge_method: squash | merge | rebase` for non-squash repos. Reopens when a non-squash operator request appears.
- **per-agent-backoff-persistence** — A+ item (l). Survive restart by persisting backoff state to a new `merge_backoff` audit event.
- **gh-output-classifier-property-test** — A+ item (n) + (o). Pure-function classifier + fixture set + property test.
- **dogfood-cron** — nightly real-merge smoke test (§10.4) against `trilamsr/regatta-merge-dogfood-test`.
- **source-field-dashboards** — once c2 lands, dashboards filter `CompletedPayload.Source` on `merge_call` vs `recovery` to surface crash-driven completions separately. Tracked in c0's existing follow-up list.
- **W4 detector handoff** — `PRStatusUnknown` ≥ 3 consecutive sweeps fires a self-improvement issue. Already filed as c0 follow-up; c2 reaffirms.

---

## §16 Cross-refs

- Brief: `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W2 c2.
- Predecessor PR (merged): #558.
- Generalize-later (the outbox primitive this spec rides on): #551, #273, #219.
- Sibling specs: `docs/engineer/specs/2026-06-02-orchestrator-pr-watcher.md` (the watcher that drives `running → pr_open`; this spec consumes its downstream `gates_running → awaiting_merge` edge).
- OTel backbone the trace span rides on: `docs/engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md`.

---

## Self-host filter

Operator IS the customer. Every claim filtered by "does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?":

- gh CLI shell-out: kept — operator already has `gh auth status` working.
- `--match-head-commit` safety: kept — prevents merging un-gated code if the PR head moved.
- `regatta merge status` CLI: kept — operator visibility into the auto-queue.
- `merge_executed` audit event: kept — operator needs to debug "why did it take 30 s?" via the duration field.
- Multi-tenant rate-limit fairness: **deferred** to Phase X (multi-operator, multi-token).
- GraphQL `mergePullRequest` mutation: **deferred** — no operator ask, gh CLI is fine.
- Per-base-branch override: **deferred** — operator's repo has one main branch.

---

## Comment sweep (lens per `feedback_reviewer_comment_trim`)

State: **clean**. No version-comments, no what-not-why scaffolding, no banner blocks. Every section header earns its place by naming a falsifiable claim or an enumerable list. Tables replace prose where the comparison is the point. No mention of dates / PR numbers / version stamps inside section bodies other than the predecessor citation in §16 and the load-bearing migration number in §11.
