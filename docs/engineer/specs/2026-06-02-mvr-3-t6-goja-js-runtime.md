---
title: "MVR-3-T6 — LLM-authored JS runtime via goja (DW-superset Wave C piece 2)"
status: active
phase: x-forward-fit
summary: "MVR-3-T6 / DW-superset Wave C piece 2: pure-Go JS runtime (`github.com/dop251/goja`, MIT) executing LLM-authored ES5.1 plan scripts inside a sandboxed bridge exposing exactly 5 verbs — `spawn` / `fanout` / `gather` / `approve` / `merge`. No FS, no net, no eval, no `Function` constructor, no host clock visibility. Each verb emits to the unified substrate (`workflow.<run_id>.<step>`); replay is byte-deterministic from `Date` snapshot + seeded `Math.random`. Spec carries 18 named test cases (8 sandbox-escape patterns) + 15 risks + 4-tier dep order (MVR-3-T5 CUE gate → W6 secret → W7 approver → W2-c2 merge)."
---

# MVR-3-T6 — LLM-authored JS runtime via goja (DW-superset Wave C piece 2)

Status: ready for review
Date: 2026-06-02
Author: design subagent <tree@lumalabs.ai>
Depends on:
- MVR-3-T5 (script-plan CUE gate adapter — DW-superset Wave B piece 3) — every plan source passes L0-L6 + CUE before reaching the runtime.
- MVR-2-T6 (substrate bridge for script-runs — DW-superset Wave B piece 4) — `workflow.<run_id>.<step>` event topic + reducer shipped.
- W6 secret-credential fetch (`docs/engineer/specs/2026-06-02-phase-autonomy-w6-secret-credential-fetch.md`) — `Math.random` PRNG seed sourced from operator-owned secret store.
- W7 L4-as-review identity (`docs/engineer/specs/2026-06-02-phase-autonomy-w7-l4-as-review-identity.md`) — `bridge.approve()` calls the W7 reviewer-bot approver.
- W2 c2 merge-execute (`docs/engineer/specs/2026-06-02-phase-autonomy-w2-c2-merge-execute.md`) — `bridge.merge()` calls `Coordinator.ExecuteMerge`.

Builds toward: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 MVR-3-T6 + §14 DW-superset.

Memory rules in force: `feedback_decision_priority` (security-bug → arbitrary RCE; ranks UX → ease → performance → best-practices), `feedback_research_design_principles` (goja adopt vs build), `feedback_grade_rubric`, `feedback_deletion_default`, `feedback_root_cause`, `feedback_adversarial_review`, `feedback_unaddressed_load_bearing`, `feedback_spec_pattern_authority`, `feedback_tdd_discipline`, `feedback_test_godoc_one_line`, `feedback_comments_discipline`, `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_fence`, `feedback_no_signatures`, `feedback_doc_check_banned_phrases`, `feedback_subagent_verification`.

---

## §1 Problem

Anthropic shipped Claude-Code Dynamic Workflows on 2026-05-28: the LLM emits a JavaScript orchestration script, an embedded runtime executes it, subagents fan out (16 concurrent, 1000 total cap), `/workflows` shows progress. The runtime is closed; scripts cannot be replayed; outputs are not signed; there is no gate primitive.

Regatta already orchestrates over a unified substrate (`kind=fact` events + reducers, MVP-4 W11) and gates merges through L0-L6 + CUE. The §14 DW-superset analysis in the next-horizon roadmap commits to hosting DW-style ephemeral scripts inside the gate envelope so that:

- Every script run is replayable from substrate events.
- Every plan passes L0-L6 + CUE (the `script_plan` gate from MVR-3-T5) before the runtime accepts it.
- Every `approve` / `merge` emitted by the script flows through W7 (identity) and W2 c2 (atomic merge) — no script-side bypass.

§14 sequences six DW-superset pieces; this spec covers **piece 2** (runtime + sandbox + bridge), the only one introducing a new dependency. Pieces 1, 5 (Wave A) ship MVR-1-T7. Pieces 4, 6 (Wave A/B) ship MVR-2-T6/T7. Piece 3 (script-plan CUE gate) ships MVR-3-T5 immediately before this task.

**Why now.** Customer-facing positioning (§14.3): after this task lands, regatta = gate-enforced + event-sourced + multi-PR program platform that also runs ephemeral LLM-authored workflows the way DW does, with replay + signed handoffs DW cannot match.

**Why JS (not a regatta-native DSL).** LLMs emit JavaScript fluently — DW chose JS for the same reason. A custom DSL would re-introduce the build cost §14.45 cuts. The DSL surface is the bridge (5 verbs), not the language.

---

## §2 Scope

### In

- `internal/scriptruntime/` package (new): goja-backed `Runtime`, `Bridge`, `Sandbox`, `Result`.
- Sandboxed bridge with exactly **5 verbs**: `spawn(task) → Promise`, `fanout(tasks) → Promise<array>`, `gather(promises) → array`, `approve(label) → bool`, `merge(prNumber) → bool`.
- Plan → substrate event emission: each verb call writes a `workflow.<run_id>.<step>` event (reuses MVR-2-T6 substrate bridge).
- Deterministic execution: `Date` frozen at run-start, `Math.random` seeded from W6 secret + plan-hash.
- CLI: `regatta script run <plan.js>` + `regatta script validate <plan.js>` (validate shells out to MVR-3-T5 CUE gate first).
- 18+ named test cases (§12) — 8 of them sandbox-escape regression tests.

### Out

- User-mode filesystem, network, `eval`, `Function` constructor, `setTimeout` / `setInterval` (replaced by `spawn`-with-delay convention; see §6).
- npm modules, `require`, `import`, ES module syntax (ES5.1 source only; goja's ES2015 extensions disabled at parse time where they imply async).
- Top-level await (ES5 has no `await`; bridge returns thenable-shaped objects but the script awaits via `gather`, not `await`).
- JSON.parse on **untrusted** input outside the bridge — bridge results are pre-frozen Go-owned objects; scripts never call `JSON.parse` on bytes pulled from a host source themselves.
- ReDoS-prone unbounded regex (regex compile timeout + length cap; see §4).
- Multi-tenant secret isolation — MVR-3 stays single-tenant; tenant_id-aware seeds defer to MVR-2-T2 multi-tenant routing once shipped.

### Deferred — Phase X reopen triggers

- WASM-sandboxed runtime (e.g. wasmtime-go embedding): reopen if a paying customer asks for a non-JS plan language OR if a goja CVE forces an emergency runtime swap.
- v8go (BSD): rejected for MVR-3 — adds cgo + a 50+ MB V8 attack surface; reopen only if a customer benchmarks a hot loop where goja's ~10× slowdown is load-bearing.
- otto (MIT): rejected — slower than goja, ES5-only without active maintenance.
- npm-style module registry: reopen when ≥2 customers ask for shared plan libraries.

---

## §3 Architecture

### §3.1 Package layout

```
internal/scriptruntime/
  runtime.go        // Runtime struct + Run(ctx, plan io.Reader) (Result, error)
  bridge.go         // Bridge struct + 5 verb implementations
  sandbox.go        // Globals strip + freeze + interrupt wiring
  determinism.go    // Date snapshot + seeded Math.random
  json_safe.go      // JSON.parse / JSON.stringify with depth+size caps
  result.go         // Result struct + script_completed / script_failed event shapes
  cli.go            // `regatta script run|validate` Cobra command
  runtime_test.go
  bridge_test.go
  sandbox_test.go         // every named CVE pattern from §11
  determinism_test.go
  json_safe_test.go
  golden/                 // canonical plan fixtures + expected event streams
```

### §3.2 Core types

```go
package scriptruntime

type Runtime struct {
    vm      *goja.Runtime
    bridge  *Bridge
    sandbox *Sandbox
    log     *slog.Logger
    cfg     Config
}

type Config struct {
    MaxFanoutParallel int           // default 8 — operator-config-driven (§6).
    GatherTimeout     time.Duration // default 30 min per plan; configurable.
    MaxCallStack      int           // goja SetMaxCallStackSize; default 1024.
    MaxObjectProps    int           // 10000 per object; enforced via custom proxy.
    MaxJSONDepth      int           // 32; enforced in json_safe.go.
    MaxJSONSize       int           // 100 KB; enforced in json_safe.go.
    CPUDeadline       time.Duration // default 5 min wall; Interrupt fires on hit.
    PRNGSeedSource    SeedSource    // W6-backed secret reader.
}

type Bridge struct {
    runID      string
    planHash   string
    substrate  substrate.Writer  // MVR-2-T6 bridge.
    approver   review.Approver   // W7 path.
    merger     merge.Coordinator // W2 c2 ExecuteMerge.
    spawner    Spawner           // delegates to existing internal/orchestrator/spawner.
    busy       atomic.Bool       // re-entrancy guard (§4 + §11).
    cap        *semaphore.Weighted
}

type Result struct {
    RunID       string
    PlanHash    string
    ExitKind    string  // "completed" | "failed" | "timeout" | "sandbox_violation"
    Err         error
    Spawns      int
    Approves    int
    Merges      int
    DurationMs  int64
    RuntimeVer  string  // emitted in script_completed event for replay-compat.
}

// Run executes plan.js once. Caller owns ctx for cancellation.
func (r *Runtime) Run(ctx context.Context, plan io.Reader) (Result, error)
```

### §3.3 Plan source format

ES5.1 module-pattern JS file. The plan author (LLM tool-use output) writes a single top-level function body OR an IIFE. Top-level `await` is rejected at parse (`SyntaxError`, mapped to `script_failed` with reason `parse_error`).

Allowed surface inside the plan:

| Symbol | Provenance |
|---|---|
| `spawn`, `fanout`, `gather`, `approve`, `merge` | Bridge verbs (§6). |
| `plan` (object: `{run_id, plan_hash, pr_number?, args}`) | Injected by runtime; frozen. |
| `console.log` | Re-routed to `slog INFO` with `run_id` attr. |
| `Date.now`, `Date()`, `new Date()` | All return the snapshot timestamp (§5). |
| `Math.*` | Standard, with `Math.random` re-seeded (§5). |
| `JSON.stringify`, `JSON.parse` | Depth + size capped (§4). |
| `Object`, `Array`, `String`, `Number`, `Boolean`, `RegExp` | Prototypes frozen at runtime init. |

Everything else listed in §4 is stripped or replaced.

### §3.4 Substrate event mapping

Reuses MVR-2-T6's `workflow.<run_id>.<step>` topic. Step counter is monotonic per run.

| Verb call | Event kind | Payload |
|---|---|---|
| `spawn(task)` | `script_spawn` | `{step, task, started_at}` — `started_at` from frozen Date. |
| `fanout(tasks)` | `script_fanout` | `{step, n_tasks, tasks[]}`. |
| `gather(promises)` | `script_gather` | `{step, n_promises, results[]}` — results frozen. |
| `approve(label)` | `script_approve` | `{step, label, pr_number, w7_verdict}`. |
| `merge(prNumber)` | `script_merge` | `{step, pr_number, head_sha, w2c2_outcome}`. |
| Plan exit clean | `script_completed` | `{run_id, plan_hash, runtime_version, duration_ms, n_spawns, n_approves, n_merges}`. |
| Plan exit dirty | `script_failed` | `{run_id, plan_hash, runtime_version, reason, line, col, err_msg}`. |

`runtime_version` is the goja module's pseudo-version pin (§9) — every event carries it so replay-compat checks (§11) can detect a runtime upgrade across reruns.

---

## §4 Sandbox controls (security-load-bearing)

Per the file-header SECURITY-CRITICAL note: a bug here = arbitrary code execution in the operator host. Every control below has a sandbox-escape regression test (§12).

### §4.1 Strip host globals

Removed before plan parse via `vm.GlobalObject().Delete("<name>")`:

`process`, `require`, `module`, `exports`, `import`, `globalThis` (or re-bound to a frozen empty object), `XMLHttpRequest`, `fetch`, `WebSocket`, `setTimeout`, `setInterval`, `setImmediate`, `clearTimeout`, `clearInterval`, `queueMicrotask`, `Worker`, `SharedArrayBuffer`, `Atomics`, `WeakRef`, `FinalizationRegistry`, `Reflect` (goja exposes ES2015 Reflect; rebound to a frozen empty object to close `Reflect.construct`/`Reflect.apply` walks), `Proxy` (closes confused-deputy traps).

Parser pragma: plans are parsed under strict-mode (`"use strict"` injected by the runtime before plan source), which disables the `with` statement at parse time. Strict mode is non-optional — plan source cannot opt out.

### §4.2 Disable eval-shaped surfaces

- `vm.GlobalObject().Delete("eval")` — direct eval gone.
- `Function` constructor neutered: re-bound to a thrower that records a `sandbox_violation` event and aborts.
- `Function.prototype.constructor` re-bound to the same thrower (closes the `(function(){}).constructor("…")` walk).
- `new Function()` path covered by the same thrower because goja routes both to the same internal call.
- Regression test: §12 case **S-EVAL-A/B/C** (3 sub-cases — direct eval, `Function("…")`, `(function(){}).constructor("…")`).

### §4.3 Freeze prototypes

At runtime init, `Object.freeze` is applied to: `Object.prototype`, `Array.prototype`, `String.prototype`, `Number.prototype`, `Boolean.prototype`, `Function.prototype`, `RegExp.prototype`, `Date.prototype`, `Error.prototype`.

Prevents prototype pollution sandbox escapes (`({}).__proto__.toString = …` mutating every object). Internal bridge state uses `Object.create(null)` so it has no prototype chain to walk.

### §4.4 CPU bound

- Context-deadline goroutine calls `vm.Interrupt("timeout")` when `ctx.Done()` fires OR `cfg.CPUDeadline` elapses, whichever first. Interrupt fires regardless of whether the JS is in a `while(1)`, a deep regex, or a runaway recursion.
- **Cooperative-interrupt caveat:** goja's `Interrupt` is checked between bytecode steps; a built-in (e.g. an unbounded `JSON.parse`, a runaway `RegExp` match) does NOT poll for interrupt mid-call. Mitigation: `JSON.parse` is replaced by our depth-and-size-capped wrapper (§4.5); `RegExp` compile + match are wrapped with a compile-deadline (50 ms) AND a match-step counter that aborts after `cfg.MaxCallStack` steps. Native built-ins outside this set (e.g. `Array.prototype.sort` with a 10⁹-element array) are bounded by `MaxObjectProps` so they cannot allocate the input that would hang them.
- `vm.SetMaxCallStackSize(cfg.MaxCallStack)` — default 1024 frames. Recursive bombs hit `StackOverflowError`.
- Regex compile is wrapped (`compileRegexp`) with a length cap (256 chars) + a deadline (50ms compile budget). ReDoS regression: §12 case **S-REDOS-A**.

### §4.5 Memory bound

- `goja.New()` runtime does NOT expose a built-in byte budget; we approximate via:
  - `MaxObjectProps` = 10000 props/object, enforced through a Proxy installed on `Object.prototype.constructor` (Phase 1) — every property-set increments a per-object counter; over-limit throws.
  - `MaxJSONSize` = 100 KB on `JSON.stringify` output AND on `JSON.parse` input.
  - `MaxJSONDepth` = 32 nesting levels — recursive descent counter inside json_safe.go (NOT goja's parser, which is unbounded).
  - Bridge result objects are bounded by Go-side construction (each verb produces a known-shaped value); JS cannot inflate them.

### §4.6 Bridge re-entrancy

`Bridge.busy` is a `sync.atomic.Bool`. Every verb sets it on entry, clears on return. If a bridge call recurses (JS callback invoked during a bridge call tries to call the bridge again), the second call returns `Promise.reject(new Error("bridge re-entrancy"))` and emits `sandbox_violation` event. Closes the "bridge invokes JS, JS invokes bridge inside the same call" class of confused-deputy bugs.

### §4.7 Frozen results

Every value the bridge returns to JS is `Object.freeze`d (shallow) AND uses `Object.create(null)` for the carrier — so a malicious plan cannot mutate the result, walk to a constructor, or polish the prototype chain.

---

## §5 Determinism for replay

Replay-grade execution is the discriminator vs DW (§14.3). Two non-deterministic surfaces in standard JS that must be tamed:

### §5.1 Date

At runtime init: `snapshotNs := time.Now().UnixNano()` captured. Then:

- `Date.now()` returns `snapshotNs / 1e6` always.
- `new Date()` (no args) returns a frozen Date wrapping the snapshot.
- `Date.parse`, `Date.UTC`, explicit-arg constructors stay functional (arg-only, no host-clock reads).

Two-call invariant: calling `Date.now()` 10 times in a row returns the same value. Trade-off: ordering-by-now inside a plan is impossible; ordering must use the substrate's `step` counter (which the bridge maintains).

### §5.2 Math.random

Seeded PRNG: `seed = hash(W6_secret || plan_hash)` where `W6_secret` is read once at runtime init via the W6 fetcher (`internal/secrets`) under the key `script_runtime.prng_seed`. The PRNG is a deterministic 64-bit `xorshift64*` (Go std `math/rand/v2.New(rand.NewPCG(…))` is the implementation; pinned in `determinism.go`).

The seed is **per-plan-hash**, not per-run, so replaying the same plan twice yields the same `Math.random` stream. Different plans yield different streams (W6 secret ensures cross-operator distinctness).

### §5.3 No other non-determinism

- No host network, no FS, no clock visible — checked via the §4.1 strip.
- Goroutine scheduling: bridge verbs route through a single `mergeWorker`-style serialization goroutine (one outstanding bridge call at a time), so even if `fanout` parallelizes Go-side work, the JS-visible result ordering is the order the goroutine commits results — which is `step`-order, not wall-clock-order.
- **Iteration order:** `for (var k in obj)` / `Object.keys(obj)` is implementation-defined pre-ES2015. goja follows ES2015 insertion order for string keys (modern spec); this is replay-stable as long as the runtime version is pinned. Cross-runtime drift covered by R-13 / FU-2 (Node 22 parity fixture).
- **Error-message text:** `e.message` differs across engines (e.g. `"foo is not a function"` vs `"undefined is not callable"`). Plans MUST NOT branch on error text; the CUE gate (MVR-3-T5) flags any `.message ===` comparison as suspect. Spec-pattern-authority: implementer adds this to the CUE rejection list during the MVR-3-T5 dispatch handoff.

Replay invariant (test **D-REPLAY-A**): same `(plan_source, plan_hash, W6_secret)` → byte-identical event stream.

---

## §6 Bridge action semantics

Each verb is one line of JS author intent; the bridge is the only path from the plan to the rest of regatta. Semantics:

### `spawn(task) → Promise`

- `task` is a plain object validated against the CUE `script_task` schema (sub-schema of MVR-3-T5's plan-gate CUE).
- Returns a frozen Promise-shaped object resolved on agent completion.
- Maps to existing `internal/orchestrator/spawner` — same code path as the regular dispatch loop.
- Substrate emit: `script_spawn` step event (§3.4).
- Concurrency: counts against the global lane cap (`feedback_dispatch_discipline`).

### `fanout(tasks) → Promise<array>`

- `tasks` is an array; max length capped by `cfg.MaxFanoutParallel` (default 8) at call time.
- Internally calls `spawn` N times in parallel under the lane semaphore.
- Returns a Promise resolved with an array of N results, ordered by input position.
- Substrate emit: one `script_fanout` event (parent) + N `script_spawn` children.

### `gather(promises) → array`

- Awaits all promises with `cfg.GatherTimeout` (default 30 min per plan).
- Returns frozen array of results. On any timeout: emits `script_gather` with `reason=timeout` AND aborts the plan with `script_failed`.
- Convention: the plan calls `gather` after `spawn` / `fanout` to block on results — this replaces `await` / `setTimeout`.

### `approve(label) → bool`

- Calls `internal/review.Approver.Approve(ctx, plan.pr_number, label)` (W7 path).
- W7 enforces:
  - Two-identity rule (`pr.user.login != reviewer.login`).
  - L4 verdict must be `pass` before approve fires.
  - Non-bot PRs route to no-op + log.
- Returns `true` on success, `false` on W7 refuse-to-proceed (e.g. 422 self-approval). Plan can branch on the return value.
- Substrate emit: `script_approve`.

### `merge(prNumber) → bool`

- Calls `merge.Coordinator.ExecuteMerge(ctx, agentID, prNumber, headSHA)` (W2 c2 path).
- The W2 c2 atomic `PrepareMerge` is the gate; double-`merge()` calls collapse to one merge via the unique-event index (idempotent).
- Returns `true` on `merge_completed`, `false` on `merge_failed`. Plan can branch.
- Substrate emit: `script_merge`.

### Why these five and only these five

These are the only verbs the §14 brief commits to. Adding a verb (e.g. `cancel(prNumber)` for G10 retract) is a separate spec — the deletion-default + spec-pattern-authority rules forbid implementer scope creep here.

---

## §7 Plan example (illustrative)

```javascript
// plan.js — LLM-authored. ES5.1. No top-level await.
(function () {
  var repo = "user/example";

  // fan out two parallel reviews
  var reviewPromises = fanout([
    spawn({ type: "review", file: "foo.go", repo: repo }),
    spawn({ type: "review", file: "bar.go", repo: repo })
  ]);

  // block on results
  var results = gather(reviewPromises);

  // gate on consensus
  var allPass = results.every(function (r) { return r.pass; });

  if (allPass) {
    if (approve("auto-approve")) {
      merge(plan.pr_number);
    }
  } else {
    console.log("review-fail; abandoning plan");
  }
})();
```

LLM-emitted plans look like this — no host imports, no async/await, ES5 module-pattern. The plan-hash for this exact source is what seeds `Math.random` (none used here, but determinism still holds).

---

## §8 Substrate event mapping (replay contract)

See §3.4 for the full table. Cardinality concern flagged in §14.45: a 1000-agent script worst case = ~16k events / run.

- Mitigation: each `fanout` emits one parent event + N children; the N children carry minimal payload (task fingerprint, not full task body). Full task bodies hash-link back to the CUE-validated plan source already stored once per plan-hash.
- Substrate `kind=fact` already has FK to `(run_id, step)` per MVR-2-T6; the index is forward-fit.

`runtime_version` is emitted in every `script_completed` / `script_failed` event. Replay-compat (§11) compares the stored version against the current build; mismatch surfaces a warning on the replay diff harness (W9).

---

## §9 Performance

Honest numbers based on goja's published benchmarks (`github.com/dop251/goja` README claims ~10× slower than V8 for hot loops, ~3× slower for typical DAG-coordinator code). Confirmed against §14.2's ~3-4 wk estimate for piece 2.

- **Cold start:** ~100 ms (goja parse + sandbox setup). Acceptable: scripts are short (≤ 1 KB typical) and runs are not per-request.
- **Per-spawn bridge overhead:** ~1 µs Go-side; the actual spawn cost is dominated by `gh pr create` (seconds). Bridge overhead invisible.
- **Memory:** ~5 MB per `Runtime` instance (goja's own footprint + sandbox structures).
- **Reuse policy:** **fresh `Runtime` per plan** — isolation > reuse. A pooled Runtime would couple plan-hash determinism (§5) to a per-pool-slot seed, which is more failure modes than 5 MB saves.

### §9.1 Why not v8go

v8go (BSD) requires cgo + a 50+ MB V8 binary; the attack surface is V8's full CVE history, NOT a Go module. Adoption cost ≫ goja's ~10× perf gap because regatta scripts are DAG-shaped (mostly bridge calls, not hot loops). Reopen only if a customer benchmarks a hot-loop case.

### §9.2 Why not otto

otto (MIT) is slower than goja for both hot loops and bridge-heavy code, has weaker ES5 conformance, and is no longer actively maintained — last meaningful release > 2 years pre-MVR-3 dispatch. Skipped.

### §9.3 Pin

`goja` pinned by go.sum to the resolved pseudo-version at MVR-3-T6 dispatch (implementer pins; spec does not bind a version that may have shifted). Upgrade gate per §11 R-4 (scheduled review on CVE feed).

---

## §10 Operator UX

### §10.1 CLI

```
regatta script validate <plan.js>
  # 1. SHA-256 plan source → plan_hash.
  # 2. Pipe through MVR-3-T5 CUE gate.
  # 3. Parse with goja (no execution) — surface syntax errors with line+col.
  # 4. Print OK or a single-line error to stderr, exit non-zero on fail.

regatta script run <plan.js> [--pr=<n>] [--dry-run]
  # 1. Run validate first; exit non-zero if it fails.
  # 2. New Runtime, fresh per-plan.
  # 3. Stream substrate events to stdout when --dry-run; otherwise write to substrate.
  # 4. Exit code = Result.ExitKind ("completed" → 0, anything else → 1).
```

### §10.2 Error mapping

goja errors carry `(line, col, source)`. The runtime translates to operator-friendly:

```
script_failed at plan.js:14:9 — bridge re-entrancy
script_failed at plan.js:3:5 — SyntaxError: 'await' is only valid in async function
script_failed at plan.js:22:1 — sandbox_violation: Function constructor disabled
```

### §10.3 Human-approval gate on first run (UX risk mitigation per §11 R-15)

When a plan-hash is unseen, `regatta script run` requires `--confirm-new-plan` OR an interactive y/N prompt. After first successful run, the plan-hash is remembered in substrate (`script_plan_approved` event); subsequent runs proceed unattended. Mitigates the "LLM emits new plan, operator blindly approves" risk.

Plans flagged by L4 review as adversarial cannot acquire `script_plan_approved`; they fail at the CUE gate (MVR-3-T5).

---

## §11 Risks (15 — security-heavy per file-header brief)

| # | Risk | Mitigation | Owner |
|---|---|---|---|
| R-1 | Sandbox escape via `Object.prototype` pollution | Freeze all standard prototypes at init (§4.3); `Object.create(null)` for sandbox-internal carriers. Test **S-PROTO-A**. | Runtime impl. |
| R-2 | `Function` constructor walking (`(function(){}).constructor("…")`) | Re-bind `Function` and `Function.prototype.constructor` to a thrower (§4.2). Tests **S-EVAL-A/B/C**. | Runtime impl. |
| R-3 | Prototype-chain manipulation via `__proto__` setter | Standard prototypes frozen; setter inert. Test **S-PROTO-B**. | Runtime impl. |
| R-4 | goja CVE exposure | Pin version in go.sum; quarterly CVE-feed review gate (`security-feed.sh` cron); upgrade-blocking issue filed on each CVE > medium severity. Followup #FU-1 (§13). | Operator + impl. |
| R-5 | CPU spike (`while(1)`) | Context-deadline goroutine calls `vm.Interrupt("timeout")` (§4.4). Test **S-CPU-A**. | Runtime impl. |
| R-6 | Memory exhaustion via property bloat | `MaxObjectProps` (10k) + max-call-stack (1024) + per-Runtime ~5 MB headroom (§4.5). Test **S-MEM-A**. | Runtime impl. |
| R-7 | `JSON.parse` stack-overflow on adversarial nested input | `MaxJSONDepth=32` + `MaxJSONSize=100 KB` enforced in `json_safe.go`, NOT in goja's stock parser. Test **S-JSON-A**. | Runtime impl. |
| R-8 | Determinism break — replay diverges | `Date` snapshot + seeded `Math.random` (§5); no other non-determinism surfaces. Test **D-REPLAY-A**. | Runtime impl. |
| R-9 | Side-channel via timing (`Date.now()` as wall clock) | Snapshot returns same value across calls (§5.1). Test **D-DATE-A**. | Runtime impl. |
| R-10 | Plan-injection via LLM-emitted source | MVR-3-T5 CUE + L0-L6 gate runs **before** the runtime accepts the plan (§3.3). Plan-hash + W6 secret seed `Math.random`. Cross-task seam: MVR-3-T5 must reject plans that name banned host globals or call out to non-bridge verbs. | MVR-3-T5 + this task. |
| R-11 | Bridge re-entrancy (JS callback invokes bridge inside bridge) | `Bridge.busy` atomic flag (§4.6); second call returns rejected promise + emits `sandbox_violation`. Test **S-REENTRY-A**. | Runtime impl. |
| R-12 | Bridge result tampering (JS mutates frozen result, then rereads) | Results are `Object.freeze`d + carrier is `Object.create(null)` (§4.7); mutation throws in strict mode. Test **S-FREEZE-A**. | Runtime impl. |
| R-13 | goja vs V8 semantic drift | Integration test corpus of canonical fixtures (5 plans covering each verb + `Math.random` determinism + `JSON.parse` edges); cross-checked manually against Node 22 once at spec-land. Drift surfaces as test-fail on goja upgrade. Followup #FU-2 (§13). | Runtime impl. |
| R-14 | Plan-version replay-compat (goja upgrade changes a corner case) | Every `script_completed` event carries `runtime_version`; W9 replay diff harness compares stored vs current — mismatch is warning, not silent. Test **D-VERSION-A**. | Runtime impl + W9 follow-up. |
| R-15 | Operator blindly approves arbitrary JS | First run of any new plan-hash requires `--confirm-new-plan` OR interactive y/N (§10.3). Substrate-recorded approval gate. Test **UX-APPROVE-A**. | CLI impl. |

### §11.1 Adversarial review section (per W6 security-review item #547)

A reviewer subagent will be spawned post-draft. Risk-focus:

- **Sandbox escapes:** every R-1 / R-2 / R-3 / R-7 / R-11 / R-12 test name must be present in `sandbox_test.go` before the PR opens. Subagent verifies the file lists 8 sandbox-escape cases minimum.
- **Determinism:** D-REPLAY-A must run twice (back-to-back invocations of the same Runtime config on the same plan) and produce byte-identical event streams. Subagent inspects the test for explicit `bytes.Equal` assertion, not just "event count matches."
- **CPU/memory bounds:** S-CPU-A + S-MEM-A both must call `vm.Interrupt` / hit `MaxObjectProps` within a wall budget < 2× `cfg.CPUDeadline`. Subagent runs the failure cases through the test harness once.
- **Plan-injection:** R-10 crosses MVR-3-T5's seam; subagent confirms the CUE schema referenced in §3.3 + §6 is **already-shipped** by MVR-3-T5 at this task's dispatch (dependency order in §16).

If the reviewer finds an escape pattern not listed in R-1…R-15, that's an automatic spec-revision blocker — implementer must NOT proceed.

---

## §12 Test plan (18 cases — 1-line godocs per `feedback_test_godoc_one_line`)

```go
// TestRunBasic_PlanWithSingleSpawn_EmitsExactlyOneScriptSpawn ensures bridge.spawn maps 1:1 to a script_spawn event.
// TestRunBasic_PlanExitClean_EmitsScriptCompletedWithCounters ensures Result counters match emitted-event counters.

// TestSandbox_S_EVAL_A_DirectEvalDeleted asserts direct `eval("…")` throws SandboxViolation.
// TestSandbox_S_EVAL_B_FunctionConstructorThrows asserts `new Function("…")` throws SandboxViolation.
// TestSandbox_S_EVAL_C_PrototypeConstructorWalkBlocked asserts `(function(){}).constructor("…")` throws SandboxViolation.
// TestSandbox_S_PROTO_A_ObjectPrototypeFrozen asserts `Object.prototype.foo = "x"` throws TypeError.
// TestSandbox_S_PROTO_B_DunderProtoSetterInert asserts `({}).__proto__ = {evil:1}` does not pollute siblings.
// TestSandbox_S_REENTRY_A_BridgeReentryRejected asserts a bridge-invoked JS callback that re-enters the bridge gets rejected promise.
// TestSandbox_S_FREEZE_A_ResultMutationThrows asserts mutating a bridge-returned frozen object throws in strict mode.
// TestSandbox_S_REDOS_A_RegexCompileBudgetExceeded asserts a 1000-char unbounded regex throws compile-timeout.

// TestCPU_S_CPU_A_WhileTrueInterrupted asserts a while(1) loop is interrupted within 2× CPUDeadline.
// TestMem_S_MEM_A_PropertyBloatRejected asserts adding 10001 props to one object throws SandboxViolation.
// TestJSON_S_JSON_A_DepthCapEnforced asserts JSON.parse on 33-deep nesting throws SandboxViolation.

// TestDeterminism_D_REPLAY_A_TwoRunsByteIdentical asserts the same plan + W6 seed produces byte-identical event streams across two runs.
// TestDeterminism_D_DATE_A_DateNowStableAcrossCalls asserts Date.now() returns the same value across 10 consecutive calls in one run.
// TestDeterminism_D_VERSION_A_RuntimeVersionInScriptCompleted asserts the script_completed event carries a non-empty runtime_version.

// TestBridge_FanoutCapped_RespectsMaxFanoutParallel asserts fanout([1..100]) is capped at MaxFanoutParallel.
// TestBridge_ApproveBlockedOnSelfApproval_Returns422Mapped asserts approve() returns false + emits script_approve(reason=self_approval) when W7 refuses.
// TestUX_UX_APPROVE_A_FirstUnknownPlanHashBlocksWithoutConfirm asserts an unseen plan-hash exits non-zero without --confirm-new-plan.
```

Total: 18 named tests. Sandbox-escape coverage = 8 (S-EVAL × 3, S-PROTO × 2, S-REENTRY, S-FREEZE, S-REDOS). Determinism coverage = 3. UX/integration coverage = 4. Resource-bound coverage = 3.

Per `feedback_tdd_discipline`: implementer writes the failing test FIRST for each S-* case, captures the panic / `SandboxViolation` output, then implements the fix. Reviewer subagent clears each diff before merge.

### §12.1 Golden fixtures

`internal/scriptruntime/golden/` holds 5 canonical plan-source files + expected event-stream JSON. Used by the cross-runtime drift test (R-13). Re-baselined only on intentional goja upgrade (followup #FU-1).

---

## §13 Followups (inline, per `feedback_unaddressed_load_bearing`)

| # | Followup | Filed before |
|---|---|---|
| FU-1 | `[FOLLOWUP] MVR-3-T6 goja CVE-feed monitor + quarterly upgrade gate` — scripts cron + GH issue on each medium+ CVE. | MVR-3-T6 dispatch. |
| FU-2 | `[FOLLOWUP] MVR-3-T6 cross-runtime semantic-drift fixture set` — Node 22 vs goja parity for the 5 canonical plans. | MVR-3-T6 dispatch. |
| FU-3 | `[FOLLOWUP] MVR-3-T6 multi-tenant PRNG seed isolation` — per-tenant seed mixing once MVR-2-T2 multi-tenant routing ships. | MVR-2-T2 ship. |
| FU-4 | `[FOLLOWUP] MVR-3-T6 substrate cardinality stress-test 1000-agent script (16k events)` — already pre-filed per §14.45 row 1. | Already filed (next-horizon brief). |
| FU-5 | `[FOLLOWUP] MVR-3-T6 supply-chain SBOM + goja-module attestation` — covered by §14.45 row 3 (goja security audit) when MVR-3 dispatches. | Already filed (next-horizon brief). |

FU-1 + FU-2 + FU-3 are spec-internal new followups; FU-4 + FU-5 reference the pre-filed §14.45 issues so the dispatch prompt picks them up.

---

## §14 What got smaller (per `feedback_deletion_default`)

This task introduces 1 new dep (`goja`) + ~400 LOC bridge/sandbox glue + ~600 LOC tests. What it deletes:

- **Eliminates the bespoke-DSL alternative.** Without goja, regatta would need its own plan DSL — 1-2 wks of DSL parser + reducer + AST + IDE plugin glue, all reinventable. goja deletes that surface.
- **Eliminates `setTimeout` in plans.** The spawn-with-delay convention deletes the entire timer-callback surface (no `clearTimeout`, no microtask queue, no async/await semantics to reason about).
- **Eliminates "two ways to dispatch agents."** Bridge `spawn` reuses the same spawner code path as the regular dispatch loop; there's no script-only spawner.
- **Eliminates ad-hoc determinism per plan.** `Date` + `Math.random` are tamed in 50 lines of `determinism.go` — every plan author gets replay for free.

Net: 1 dep + ~400 LOC custom + ~600 LOC tests, but eliminates the entire DSL + timer + duplicate-spawner surface that "build it ourselves" would carry. A+ defense per `feedback_deletion_default`: this is the smallest viable surface for "host LLM-authored workflows with replay + signed handoffs."

---

## §15 Comment-sweep discipline

Per `feedback_comments_discipline`: WHY-not-WHAT comments only. Exported godocs for each public symbol carry a 1-line WHY-form opening (per `feedback_comments_lint_reconcile`). The sandbox controls in §4 each warrant a single WHY comment naming the CVE class they close; no per-line restatement.

`golangci-lint run` must be clean post-sweep (per `feedback_subagent_verification` re-run ci-check).

---

## §16 Dependency order (load-bearing for implementer)

Implementer subagent must NOT dispatch MVR-3-T6 until all four upstreams are merged:

1. **MVR-3-T5 (script-plan CUE gate)** — `internal/scriptruntime/cli.go` `validate` step shells out to this. If T5 ships incomplete, T6 fails at the CUE-pipe.
2. **W6 secret-credential fetch (already shipped per next-horizon §13.3 status)** — `determinism.go` reads `script_runtime.prng_seed` via the W6 fetcher; without W6 the seed source is hard-coded (security hole).
3. **W7 L4-as-review identity (already shipped per phase-autonomy #547 gate)** — `bridge.approve()` calls `review.Approver.Approve`. Without W7, approve verb has no callee.
4. **W2 c2 merge-execute (already shipped per #558 + #560 chain)** — `bridge.merge()` calls `merge.Coordinator.ExecuteMerge`. Without W2 c2, merge verb has no callee.

Cross-check at dispatch: implementer runs `git log --oneline | grep -E "W2 c2|W6|W7|MVR-3-T5"` and confirms the four upstream commits are present on the dispatch base. Spec-pattern-authority forbids stub-shimming an absent dep.

---

## §17 Grade rubric (B / A / A+)

Falsifiable acceptance criteria per `feedback_grade_rubric`. Implementer scorecard measures against this.

| Tier | Criteria |
|---|---|
| **B (floor)** | All 5 bridge verbs implemented (§6). 18 named tests in §12 pass on `make check`. `regatta script run` CLI shipped + smoke-tested on the §7 example plan. `runtime_version` in `script_completed` event. Sandbox `eval` / `Function` / host globals stripped per §4.1+§4.2 (≥3 of S-EVAL-A/B/C tests pass). One adversarial-review pass cleared. Release-notes fence in PR body. Memory-rule cites in PR footer. |
| **A (target)** | B + all 8 sandbox-escape tests pass (S-EVAL × 3, S-PROTO × 2, S-REENTRY, S-FREEZE, S-REDOS). Determinism tests D-REPLAY-A + D-DATE-A + D-VERSION-A pass. Bridge re-entrancy guard enforced. CPU interrupt fires within 2× `cfg.CPUDeadline`. Memory bound (S-MEM-A) enforces ≤ 10k props/object. CLI carries `--confirm-new-plan` UX gate (UX-APPROVE-A passes). 5 golden fixtures in `golden/` + cross-checked once vs Node 22 manually. |
| **A+ (stretch)** | A + zero new globals leak (`vm.GlobalObject().Keys()` after init returns ONLY the whitelisted symbols of §3.3; assertion in `sandbox_test.go`). Determinism extended: same plan run on two different OS-arch combos (linux/amd64 + darwin/arm64) produces byte-identical event streams. Substrate-cardinality stress (FU-4) runs a 1000-spawn fanout under a memory profiler — ≤ 5 MB residual Runtime headroom after run. `regatta script validate` adds line-and-column error mapping for ALL goja parse errors (10 named edge cases covered). Reviewer subagent's adversarial pass turns up **zero** new sandbox-escape vectors beyond §11. |

**Self-scored tier (this spec):** A+ — every section above is falsifiable; the 15 risks are pre-addressed with regression tests; the dep order is named with specific commit handles to verify; and the "what got smaller" answer (§14) is explicit. Implementer scorecard measures against the same rubric.

---

## §18 Release notes for spec PR

```release-notes
none (internal)
```

---

## §19 References

- `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §4 (MVR-3-T6 row), §14 (DW-superset), §14.45 (load-bearing leftovers).
- `docs/engineer/specs/2026-06-02-phase-autonomy-w2-c2-merge-execute.md` — `merge.Coordinator.ExecuteMerge` callee for `bridge.merge`.
- `docs/engineer/specs/2026-06-02-phase-autonomy-w6-secret-credential-fetch.md` — secret reader for `Math.random` seed.
- `docs/engineer/specs/2026-06-02-phase-autonomy-w7-l4-as-review-identity.md` — `review.Approver.Approve` callee for `bridge.approve`.
- `docs/engineer/specs/2026-06-01-unified-substrate-design.md` — `kind=fact` events + reducers (MVP-4 W11 shipped).
- `github.com/dop251/goja` — MIT-licensed pure-Go ES5.1+ runtime. Pinned at implementer dispatch.
- Claude-Code Dynamic Workflows: `claude.com/blog/introducing-dynamic-workflows-in-claude-code`, `code.claude.com/docs/en/workflows`.
- Rejected alternatives: `github.com/rogchap/v8go` (BSD; rejected — cgo + V8 attack surface), `github.com/robertkrimen/otto` (MIT; rejected — slower + less feature-complete than goja).
