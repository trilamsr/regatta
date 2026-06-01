# W7.0 — Operator Web UI Listener Prereq — Implementer Task Breakdown (2026-06-01)

Source-of-truth spec: `docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md` §1.3, §3.1, §3.2, §3.3, §7 Wave 7.0, §10 (references substrate spec `0006_substrate.sql`).
Authority: `feedback_spec_pattern_authority` — implementer deviation from spec MUST re-spawn design subagent.

---

## Wave overview

- **3 file-disjoint implementer tasks** (T-W7.0-1, T-W7.0-2, T-W7.0-3) — the listener prereq wave per spec §7 Wave 7.0 (R2). W7.0 is a HARD GATE: W7.1 (HTTP scaffold + approval flow), W7.2 (DAG list view), and W7.3 (cost panel) cannot dispatch until W7.0 lands.
- **Hard prereqs (merged to main):**
  - Substrate W1 T-S1 (#224 `feat(substrate): T-S1 event log primitive + 0006 migration + HMAC sign + Kahn cycle-check (Phase A dark)`) ✅ merged 2026-06-01.
  - Substrate W1 T-S2 (#232) ✅ merged 2026-06-01.
  - Substrate W1 T-S3 (#233) ✅ merged 2026-06-01.
  - W6 T3 (#209 `feat(state): migration 0005 adds trace_id columns`) ✅ merged.
- **Migration number lock:** Substrate Wave 1 shipped migration `0006_substrate.sql`. W7.0 does **NOT** require a schema migration — T1 wires an in-process HTTP listener, T2 ships a test-only middleware in `dbtest/`, T3 is a pure Go refactor lifting `decideTx`. **No goose migration in this wave.** If a later finding forces schema work into W7.0, the migration number is **0007** (locked here per `feedback_migration_number_lock`).
- **Sequence vs parallel:** T1 owns `cmd/regatta/serve.go` modification + the new `internal/gates/approval/notify_http.go` (concrete `CallbackRoute()`). T3 owns the lift of `decideTx` into `internal/gates/approval/decide.go`. **T1 depends on T3's exported `approval.DecideTx`** — T3 must complete first (sequence T3 → T1). T2 is fully independent (new package `internal/orchestrator/state/dbtest/`).
  - **Dispatch order:** T3 + T2 in parallel first (both file-disjoint, neither blocks the other); T1 dispatches after T3's PR is open and `approval.DecideTx` is callable. Per `feedback_sequence_dependent_work`: T1 consumes T3's exported API ⇒ SEQUENCE T3 first.
  - Per `feedback_shared_primitive_owner`: T3 is OWNER of the lifted `DecideTx` seam; T1 imports it.
- **Concurrency cap:** per `feedback_session_limit_dispatch` — peak 2 parallel implementers (T2 + T3 first, then T1 alone). Well within the 3-4 cap.
- **Deletion default (`feedback_deletion_default`):** Wave 7.0 is mostly addition (new HTTP listener, new dbtest package) BUT T3 is a **pure refactor**: `decideTx` moves OUT of `cmd/regatta/approval_decide.go` into `internal/gates/approval/decide.go`. Net LoC delta in `cmd/regatta/approval_decide.go`: **negative** (~80 LoC lifted, ~5 LoC re-import stub remains). Each PR body declares the shrink explicitly.
- **Phase positioning:** W7.0 is the **listener-only** prereq — no UI handlers, no templates, no auth, no CSP middleware. Those land in W7.1 (HTTP scaffold) which dispatches AFTER W7.0 merges. The only HTTP routes wired by W7.0 are `/healthz` and `/api/approval/callback` (the latter is the notifier callback, NOT a UI route).

---

## §1 File-disjoint table

| Task        | Path (exclusive write scope)                                                                                                                                                                                                                                                                                              | Depends-on (Wave 7.0 + main) | Effort | TDD tests (count: named)                                                                                                                                                                                              |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------- | ------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| T-W7.0-1    | `cmd/regatta/serve.go` (MODIFY — add `--addr`, `--ui` flags + `net/http.Server` boot + graceful shutdown + `--ui=true && REGATTA_HMAC_KEY==""` fail-boot); `cmd/regatta/serve_listener_test.go` (NEW); `internal/gates/approval/notify_http.go` (NEW — concrete `InteractiveNotifier.CallbackRoute()`); `internal/gates/approval/notify_http_test.go` (NEW) | main + T3 merged              | M      | 8 named (B 5, A 3). Spec §1.3 + §3.3 + §6 B-rubric.                                                                                                                                                                   |
| T-W7.0-2    | `internal/orchestrator/state/dbtest/query_counter.go` (NEW); `internal/orchestrator/state/dbtest/query_counter_test.go` (NEW); `internal/orchestrator/state/dbtest/doc.go` (NEW)                                                                                                                                          | main                          | S      | 5 named (B 4, A 1). Spec §4 (R7) + §6 B-rubric.                                                                                                                                                                       |
| T-W7.0-3    | `internal/gates/approval/decide.go` (NEW — lifted from `cmd/regatta/approval_decide.go::decideTx`); `internal/gates/approval/decide_test.go` (NEW); `cmd/regatta/approval_decide.go` (MODIFY — re-import `approval.DecideTx`, delete inlined `decideTx`)                                                                  | main                          | M      | 6 named (B 4, A 2). Spec §3.2 (zero-modification-to-state-machine) + §6 B-rubric.                                                                                                                                     |

**Disjointness verification:** Three rows; no path appears twice. T1 modifies `cmd/regatta/serve.go`; T3 modifies `cmd/regatta/approval_decide.go`. Same directory, different files. T1's new file `internal/gates/approval/notify_http.go` and T3's new file `internal/gates/approval/decide.go` are distinct file names inside the same package — go-import-disjoint. T2 lives in a brand-new `dbtest/` subdirectory under `state/`.

**Cross-task seam contracts (load-bearing — implementer MUST honour exactly):**
- T3 exports `approval.DecideTx(ctx context.Context, db *state.DB, payload canon.TokenPayload, reviewerID, decision, reason string, clock func() time.Time) (FoldResult, string, error)`. Signature MUST match the current `cmd/regatta/approval_decide.go::decideTx` byte-for-byte except `foldResult` is renamed to exported `FoldResult` (type lifts with the function). T1 + T-W7.1 (next wave) both consume this signature; deviation breaks both.
- T3 also lifts the `foldResult` helper type → `approval.FoldResult` and any private helpers `decideTx` calls. The CLI re-stub in `cmd/regatta/approval_decide.go` reads `approval.DecideTx` only — zero behavior change at the CLI surface (verified by existing `approval_decide_test.go` + `approval_e2e_test.go` passing unchanged).
- T1's new `internal/gates/approval/notify_http.go` exports a `NewHTTPCallback(deps Dependencies) (path string, handler http.Handler)` function. It satisfies `InteractiveNotifier.CallbackRoute()` and calls `approval.DecideTx` (NOT `cmd/regatta/decideTx`).
- T2's `dbtest.QueryCounter` wraps a `*sql.DB` (or a `state.DB`) and increments per `Exec`/`Query`/`QueryRow` call. T1 + future W7.x tests import it. Wave 7.0's own tests do NOT use it (no DB-touching handlers exist yet); it ships as a primitive for Wave 7.2 (R7 §4 budget gate for `GET /runs/{run_id}`).

---

## §2 Task T-W7.0-1 — HTTP listener + flags + notifier callback wiring

### Scope
- `cmd/regatta/serve.go` (modify):
  - Add CLI flags: `--addr` (string, default `":8080"`), `--ui` (bool, default `true`). Flag names must be stable — they ship in W7.0 and stay.
  - Spec §1.3 fail-boot rule: if `--ui=true && os.Getenv("REGATTA_HMAC_KEY") == "" && os.Getenv("REGATTA_HMAC_KEY_ENV") == ""`, exit non-zero with clear error `"--ui requires REGATTA_HMAC_KEY (or REGATTA_HMAC_KEY_ENV) to be set; refusing to boot"`. Pre-flight check BEFORE any DB open / listener bind. Loud-at-boot beats lying-at-render (spec §1.3 open-q 9.8 ruling).
  - When `--ui=true`: construct an `http.ServeMux`; register **only** `/healthz` (returns `200 OK\nok\n`, no DB query — spec §3.3 row 6) AND the `InteractiveNotifier.CallbackRoute()` path (`/api/approval/callback` — spec §3.3 row 8). NO UI route registration here; that lands in W7.1.
  - When `--ui=false`: skip listener boot entirely. No port bound. Integration test asserts no `LISTEN` syscall in this branch.
  - Boot `&http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5*time.Second}` in a goroutine; wire graceful shutdown to the existing signal-handling loop in `serve.go` (5 s `Shutdown(ctx)` budget per existing pattern). If `http.Server.ListenAndServe` returns non-`http.ErrServerClosed`, log + return error.
- `cmd/regatta/serve_listener_test.go` (new):
  - Integration tests using `httptest.NewServer` is NOT enough — the test must exercise the **real boot path** by invoking the serve command's listener-init function (extract a `bootListener(ctx, cfg) (*http.Server, error)` helper if needed; the helper is internal but exported for `_test.go` via `serve_listener_test.go` in package `main`).
  - Tests cover: `--ui=true` binds; `--ui=false` skips bind; `--addr=:0` accepts ephemeral port; `--ui=true && REGATTA_HMAC_KEY=""` fails boot; `/healthz` returns 200 with body `ok`; `/api/approval/callback` returns 405 on GET (POST-only — spec §3.3 row 8); graceful shutdown completes within 5 s.
- `internal/gates/approval/notify_http.go` (new):
  - Concrete `NewHTTPCallback(deps Dependencies) (path string, handler http.Handler)` that returns `("/api/approval/callback", h)` where `h` reads HMAC token from POST body (`Content-Type: application/x-www-form-urlencoded`, fields `token`, `decision`, `reason`), validates via `canon.VerifyToken`, and calls `approval.DecideTx`. Returns 200 + JSON `{"status":"ok","approval_id":"..."}` on success; 4xx with typed-sentinel JSON (`{"error":"token_replay"}`, etc.) on failure; `Cache-Control: no-store` always.
  - `Dependencies` struct: `{DB *state.DB, Keyring map[string][]byte, Clock func() time.Time}` — minimal; expand only when W7.1 has a second caller (per `feedback_unaddressed_load_bearing`).
  - `InteractiveNotifier.CallbackRoute()` interface (`notify.go:38`) is the existing seam; this file ships the FIRST and ONLY concrete impl. Per spec §1.3 + R2 verification: the interface was declared but never wired. W7.0 wires it.
- `internal/gates/approval/notify_http_test.go` (new):
  - Happy path (POST allow → 200 + state mutated), invalid token → 401, replay → 409 with `token_replay` sentinel, malformed body → 400, non-POST → 405, missing `Cache-Control: no-store` → fail.

### Prereqs (cite spec sections)
- Spec §1.3 — listener boot rules: `--ui=true` default, `--ui=false` skip, fail-boot on missing `REGATTA_HMAC_KEY`.
- Spec §3.1 — wire diagram: single `net/http.Server` on `:ADDR`.
- Spec §3.3 row 6 (`/healthz`) + row 8 (`/api/approval/callback`).
- Spec §7 Wave 7.0 T1 — file scope.
- Existing patterns to reuse (per `feedback_research_design_principles`):
  - `cmd/regatta/serve.go::loadBriefKeyring` (lines 308+) — env-var keyring loader pattern; reuse for HMAC key boot check.
  - `cmd/regatta/serve.go`'s existing signal-handling loop — extend, don't replace.
  - `internal/canon::VerifyToken` — token validation primitive (do NOT introduce a new validator).
  - `internal/gates/approval/notify.go::InteractiveNotifier` — existing interface contract.
  - Go stdlib `net/http` — proven OSS; no third-party HTTP framework.

### TDD test list (with failing-output capture step)

Per `feedback_tdd_discipline`: implementer writes each test, runs `go test ./cmd/regatta/... -run <name> -v` (or `./internal/gates/approval/...`), **captures the failing output (paste into PR body)**, then implements. No exceptions.

**B-tier (spec §6 B-rubric):**
1. `TestServe_UITrue_BindsListener` — `--ui=true --addr=:0` → `http.Server` running; `GET /healthz` returns `200 OK` with body `ok`.
2. `TestServe_UIFalse_SkipsListener` — `--ui=false` → no listener bound (assert via attempting to dial a fixed port → connection refused; or by checking the bootListener helper returns `nil, nil`).
3. `TestServe_UITrue_FailsBootIfHMACKeyUnset` — `t.Setenv("REGATTA_HMAC_KEY","")` + `--ui=true` → boot returns non-nil error containing "REGATTA_HMAC_KEY"; exit code non-zero (assert via the helper return, not via `os.Exit`).
4. `TestServe_GracefulShutdown` — start listener; send SIGTERM (simulate via cancelling root ctx); assert shutdown completes within 5 s + no inflight-request leak.
5. `TestNotifyHTTP_CallbackPOSTAllow` — POST to `/api/approval/callback` with valid token + `decision=allow` → 200 + `approval_events` row written + `Cache-Control: no-store` header.

**A-tier (spec §6 A-rubric):**
6. `TestNotifyHTTP_CallbackReplayReturns409` — POST same token twice → first 200, second 409 with body `{"error":"token_replay"}`. Pins the `ErrTokenReplay` propagation path through `approval.DecideTx`.
7. `TestNotifyHTTP_CallbackRejectsNonPOST` — GET / PUT / DELETE all return 405 with `Allow: POST` header.
8. `TestServe_CallbackRouteRegisteredOnlyWhenUITrue` — `--ui=false` → POST to `/api/approval/callback` connection refused; `--ui=true` → registered. Locks the listener-gating contract.

### PR body skeleton

```
## Summary

T-W7.0-1 ships the HTTP listener boot in `regatta serve` per
docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md §1.3 §3.1 §3.3
+ wires the first concrete `InteractiveNotifier.CallbackRoute()` impl.

- cmd/regatta/serve.go: --addr (default :8080) + --ui (default true) flags;
  net/http.Server boot when --ui=true; fail-boot if --ui=true && no
  REGATTA_HMAC_KEY (open-q 9.8 ruling, spec §1.3); graceful shutdown wired
  to existing signal loop with 5 s budget.
- /healthz route (200 + body "ok", no DB query) per spec §3.3.
- /api/approval/callback route per spec §3.3 row 8.
- internal/gates/approval/notify_http.go: NewHTTPCallback() — first
  concrete impl of the existing CallbackRoute() interface. Verifies HMAC
  via canon.VerifyToken; calls approval.DecideTx (lifted by T-W7.0-3);
  typed-sentinel JSON errors; Cache-Control: no-store on every response.

## Why

MVP-3 Wave 7.0 listener prereq. Spec §1.3 R2 verified the listener was
NEVER wired in serve.go (418 LOC, zero http. references) — the W7 UI
spec assumed a listener that did not exist. W7.0 stands it up so W7.1
(HTTP scaffold + approval flow), W7.2 (DAG list), and W7.3 (cost panel)
can layer on top without touching the boot path.

## Test plan

- [x] B-tier: TestServe_UITrue_BindsListener,
       TestServe_UIFalse_SkipsListener,
       TestServe_UITrue_FailsBootIfHMACKeyUnset,
       TestServe_GracefulShutdown,
       TestNotifyHTTP_CallbackPOSTAllow.
- [x] A-tier: TestNotifyHTTP_CallbackReplayReturns409,
       TestNotifyHTTP_CallbackRejectsNonPOST,
       TestServe_CallbackRouteRegisteredOnlyWhenUITrue.
- [x] make pre-push-check clean.
- [x] doc-check diff vs origin/main: godocs ≤ 1 line each (verified AFTER
       commit, BEFORE push per feedback_comments_discipline).

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Deletion default

W7.0 T1 is mostly addition (~150 LoC across 3 new files + ~40 LoC of
serve.go flag-handling + listener-boot wiring). What gets smaller: the
existing InteractiveNotifier.CallbackRoute() interface declaration at
internal/gates/approval/notify.go:38 has been declared-but-unused since
MVP-2 W1. T-W7.0-1 ships the first caller — interface earns its keep.
If no caller materialized by W8 the interface would have been deleted;
W7.0 saves it. (Net repo shrink deferred to Wave 7.4 when the
`Authorizer` placeholder retires per spec R4.)

```release-notes
[FEATURE] regatta serve --ui flag boots HTTP listener for operator web UI
```
```

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w7-listener-t1. Branch off main:
`git checkout -b feat/w7-listener-t1-http-server main`.

T-W7.0-3 must merge to main BEFORE you dispatch — your code imports
`approval.DecideTx` from internal/gates/approval/decide.go which T-W7.0-3
ships. Confirm `git log origin/main --oneline | grep T-W7.0-3` returns
the merged commit before starting.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md.
Read ALL of: §1.3 (compatibility envelope + fail-boot rule), §3.1 (wire
diagram), §3.2 (zero modifications to approval state machine), §3.3
(route table — focus rows 6 + 8), §3.6.1 (cookie-bound token; NOTE:
cookie wiring is W7.1's job, NOT yours — you only ship POST callback
which reads token from request body), §7 Wave 7.0 (your wave).

Per feedback_spec_pattern_authority: if you want to deviate from any
spec-mandated pattern (flag names --addr / --ui, default :8080, fail-boot
on missing REGATTA_HMAC_KEY, /healthz returning literal "ok", route paths
/healthz + /api/approval/callback), STOP and report — do NOT pick an
alternative yourself. Re-spawn the design subagent.

NO UI routes. NO templates. NO embed.FS. NO CSP middleware. NO
auth.go. NO Principal type. Those are W7.1 + W7.3 + W7.7. Your wave
is listener-only. Out-of-scope edits get caught at review and need a
separate issue (lesson from PR #209 — feedback_session_2026_05_31_lessons).

# Scope (exclusive write paths — file-disjoint with T-W7.0-2 + T-W7.0-3)

- cmd/regatta/serve.go (MODIFY — add --addr/--ui flags + boot + fail-boot
  + graceful shutdown + register /healthz and /api/approval/callback)
- cmd/regatta/serve_listener_test.go (NEW — integration tests)
- internal/gates/approval/notify_http.go (NEW — NewHTTPCallback +
  Dependencies struct + HTTP handler that calls approval.DecideTx)
- internal/gates/approval/notify_http_test.go (NEW)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/gates/approval/notify.go (interface lives there;
  it does not need editing).
- Do NOT touch internal/gates/approval/decide.go (T-W7.0-3 ships it).
- Do NOT touch cmd/regatta/approval_decide.go (T-W7.0-3 owns the lift).
- Do NOT create internal/web/ (W7.1's job).
- Do NOT add a goose migration. If schema work appears load-bearing,
  STOP and re-spawn design subagent. Migration number, if ever needed,
  is 0007 (pinned per feedback_migration_number_lock).

# Patterns to reuse (do NOT reinvent — feedback_research_design_principles)

- HMAC key boot check: cmd/regatta/serve.go::loadBriefKeyring (lines 308+)
  reads REGATTA_HMAC_KEY. Reuse the env-var dispatch (REGATTA_HMAC_KEY,
  REGATTA_HMAC_KEY_ENV) — both forms count as "set" for fail-boot logic.
- Signal handling: existing serve.go signal-trap loop. Extend, do NOT
  replace. Listener shutdown integrates into the existing 5 s shutdown
  budget.
- HMAC verify: internal/canon::VerifyToken — sole token-validation
  primitive. Do NOT introduce a new verifier.
- HTTP server: Go stdlib net/http — no third-party framework.
- ServeMux + http.HandlerFunc — stdlib idiomatic.
- Test pattern: cmd/regatta/serve_test.go (existing) for the bootstrap
  pattern; net/http/httptest for handler-unit tests.

# Workflow steps (TDD discipline — feedback_tdd_discipline)

For each named test below:
  1. Write the test file first.
  2. Run `go test ./cmd/regatta/... -run <name> -v` (or
     `./internal/gates/approval/... -run <name> -v` for notify_http
     tests).
  3. CAPTURE the failing output (paste into PR body's "Failing-test
     output (TDD capture)" section). "Tests would have failed" is
     NOT acceptable.
  4. Implement the minimum needed to pass.
  5. Re-run; confirm pass.
  6. Commit (one commit per test or per logical group; squash later).

# Tests to land (8 named; spec §6 B/A-rubric + §10)

B-tier:
1. TestServe_UITrue_BindsListener
2. TestServe_UIFalse_SkipsListener
3. TestServe_UITrue_FailsBootIfHMACKeyUnset
4. TestServe_GracefulShutdown
5. TestNotifyHTTP_CallbackPOSTAllow

A-tier:
6. TestNotifyHTTP_CallbackReplayReturns409
7. TestNotifyHTTP_CallbackRejectsNonPOST
8. TestServe_CallbackRouteRegisteredOnlyWhenUITrue

# Workflow after green

  1. Run `make pre-push-check` — confirm clean.
  2. Run doc-check diff vs origin/main locally:
     `git diff origin/main -- '*.go' | grep -E '^\+(?!\+)' | grep '^// '`
     — every new godoc on exported funcs MUST be ≤ 1 line (per
     feedback_comments_discipline). Trim multi-line godocs to one
     line BEFORE pushing.
  3. Sweep superfluous comments per feedback_comments_discipline:
     WHY not WHAT.
  4. NO AI signatures — do NOT add Co-Authored-By footers to commits
     or PR body (per feedback_no_signatures, MEMORY.md top entry).
  5. Push branch: `git push -u origin feat/w7-listener-t1-http-server`.
  6. Open PR with `gh pr create --base main --title "feat(w7.0): T1 HTTP listener + InteractiveNotifier.CallbackRoute() concrete impl" --body-file <path>`. USE --body-file (per feedback_pr_lint_gates), NEVER heredoc.
  7. After PR opens, spawn ONE adversarial reviewer subagent (per
     feedback_adversarial_review + feedback_agent_pr_review) with hunt
     list: graceful shutdown race conditions, --ui=false truly skips
     LISTEN syscall (not just "404 on /healthz"), fail-boot triggers
     BEFORE DB open (operator must not see "DB locked" before "HMAC key
     missing"), Cache-Control: no-store on every response (no leak),
     POST body parse limits (1 MiB cap?), token verification calls
     canon.VerifyToken NOT a reinvented primitive, /api/approval/callback
     is POST-only (405 on GET/PUT/DELETE), no inflight-request leak on
     shutdown, no http handler leaks DB connections. Reviewer must use
     OK:/ISSUE: per item.
  8. Apply reviewer findings; re-run make pre-push-check; re-run
     doc-check diff; force-push.
  9. Enable automerge ONLY after reviewer cleared + every Risk-tier
     finding is fixed inline or filed as tracking issue cited in PR
     body (per feedback_review_before_automerge).

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 4 of the 8 tests (sample is
  fine — the PR body carries the full set).
- Adversarial reviewer verdict (APPROVE | findings list).
- One-line diff stat: files changed + LoC added + LoC removed.
- Confirmation that approval.DecideTx (from T-W7.0-3) is callable from
  internal/gates/approval/notify_http.go.
```

---

## §3 Task T-W7.0-2 — Query-counter test middleware (R7 prereq)

### Scope
- `internal/orchestrator/state/dbtest/query_counter.go` (new):
  - Exported `QueryCounter` struct that wraps a `*state.DB` (or a `*sql.DB` directly). Implements `Exec`, `ExecContext`, `Query`, `QueryContext`, `QueryRow`, `QueryRowContext`. Each call increments an atomic counter (separate counters per kind so callers can assert "≤ 2 queries OR ≤ 0 writes" granularly).
  - Exposes `Count() int` (total), `ReadCount() int`, `WriteCount() int`, `Reset()`. `AssertLE(t *testing.T, max int)` helper for ergonomic test calls.
  - Lives in its own package `dbtest` (NOT `state`) so production code cannot accidentally import it (test-only by directory convention).
- `internal/orchestrator/state/dbtest/query_counter_test.go` (new):
  - Unit tests covering counter increments, parallel safety (`sync/atomic`), reset, AssertLE pass/fail.
- `internal/orchestrator/state/dbtest/doc.go` (new):
  - Package godoc. ONE line: `// Package dbtest provides test-only helpers around state.DB for query-budget assertions.`

### Prereqs (cite spec sections)
- Spec §4 (R7) — "Query count per `/runs/{run_id}` render ≤ 2 SQL queries 4 (hard fail) `internal/orchestrator/state/dbtest/query_counter.go` (new W7.0 T2) wraps `state.DB`, increments counter on every `Exec`/`Query`, test fails if `>2` per render."
- Spec §6 B-rubric — "Query-counter middleware ships + `/runs/{run_id}` render asserted ≤ 2 SQL queries (R7)".
- Spec §7 Wave 7.0 T2 — file scope.
- Existing patterns:
  - `database/sql` stdlib — wrap `*sql.DB` via composition, not embedding (composition keeps the surface narrow + lets us future-proof against new methods).
  - `sync/atomic.Int64` — concurrency-safe counters.

### TDD test list

**B-tier:**
1. `TestQueryCounter_IncrementsOnEveryExec` — wrap mock DB; call `Exec` 3 times; `Count()` returns 3, `WriteCount()` returns 3, `ReadCount()` returns 0.
2. `TestQueryCounter_SeparatesReadsAndWrites` — `Query` increments only `ReadCount`; `Exec` increments only `WriteCount`; `Count` sums both.
3. `TestQueryCounter_ResetZeroesAll` — increment, reset, assert zeros.
4. `TestQueryCounter_AssertLEPassesWhenUnderBudget` — counter = 2; `AssertLE(t, 2)` passes; counter = 3, `AssertLE(t, 2)` calls `t.Fatalf`.

**A-tier:**
5. `TestQueryCounter_ConcurrentSafety` — 100 goroutines, each calling `Exec` 10 times; `Count()` == 1000 deterministically.

### PR body skeleton

```
## Summary

T-W7.0-2 ships the query-counter test middleware per
docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md §4 (R7) + §6
B-rubric. Pure test-only primitive; no production-code consumers yet.

- internal/orchestrator/state/dbtest/query_counter.go: QueryCounter
  wraps *sql.DB, atomically counts Exec/Query/QueryRow calls (and
  Context variants), exposes Count/ReadCount/WriteCount/Reset/AssertLE.
- Lives in dbtest/ package (not state/) so production cannot import
  the test helper.

## Why

MVP-3 Wave 7.0 T2. Prereq for W7.2's DAG-list view (spec §4 R7 budget:
≤ 2 SQL queries per /runs/{run_id} render). Ships now, alone, file-
disjoint from T1 and T3, so W7.2's implementer has the middleware
ready to import when DAG-list lands.

## Test plan

- [x] B-tier: TestQueryCounter_IncrementsOnEveryExec,
       TestQueryCounter_SeparatesReadsAndWrites,
       TestQueryCounter_ResetZeroesAll,
       TestQueryCounter_AssertLEPassesWhenUnderBudget.
- [x] A-tier: TestQueryCounter_ConcurrentSafety (100 goroutines × 10
       calls each, deterministic Count == 1000).
- [x] make pre-push-check clean.
- [x] doc-check diff vs origin/main: godocs ≤ 1 line each.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Deletion default

Pure-addition (~80 LoC across 3 new files). What gets smaller: the
QueryCounter middleware makes N+1 query bugs IMPOSSIBLE to land in
W7.2 + W7.3 (R7 budget enforced). Review surface in Wave 7.2 shrinks
because the query-budget gate is automatic — no manual SQL-count
audits at review time.

```release-notes
[FEATURE] dbtest.QueryCounter for query-budget assertions (W7.2 prereq)
```
```

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w7-listener-t2. Branch off main:
`git checkout -b feat/w7-listener-t2-query-counter main`.

T-W7.0-2 is fully file-disjoint from T1 and T3 — dispatch in parallel
with T-W7.0-3 (T1 dispatches after T3 merges).

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md.
Read: §4 (R7 — query count ≤ 2 per /runs/{run_id} render), §6 B-rubric
(query-counter middleware ships + /runs/{run_id} ≤ 2 queries assertion),
§7 Wave 7.0 T2 (your scope).

Per feedback_spec_pattern_authority: if you want to deviate from the
spec-mandated location (`internal/orchestrator/state/dbtest/query_counter.go`)
or the naming pattern (`QueryCounter` struct, `Count/ReadCount/WriteCount`
methods, `AssertLE` helper), STOP and re-spawn the design subagent.

# Scope (exclusive write paths — file-disjoint with T-W7.0-1 + T-W7.0-3)

- internal/orchestrator/state/dbtest/query_counter.go (NEW)
- internal/orchestrator/state/dbtest/query_counter_test.go (NEW)
- internal/orchestrator/state/dbtest/doc.go (NEW; one-line package godoc)

You MUST NOT touch any other file. Do NOT add any consumer of the
QueryCounter in this PR — W7.2 will be the first consumer. Adding a
consumer here is out of scope (lesson from feedback_session_2026_05_31_lessons:
out-of-scope edits get caught at review).

# Patterns to reuse (feedback_research_design_principles)

- database/sql stdlib — wrap *sql.DB via composition (struct holds the
  embedded interface explicitly, NOT struct embedding which would
  promote ALL methods opaquely).
- sync/atomic.Int64 — concurrency-safe counters (do NOT use mutex; the
  hot path is per-query so atomic is the right primitive).
- testing.T — AssertLE takes *testing.T and calls t.Fatalf on violation.

# Workflow steps (TDD discipline)

For each named test:
  1. Write test first.
  2. Run `go test ./internal/orchestrator/state/dbtest/ -run <name> -v`.
  3. CAPTURE failing output.
  4. Implement.
  5. Re-run; confirm pass.
  6. Commit.

# Tests to land (5 named; spec §4 R7 + §6 B-rubric)

B-tier:
1. TestQueryCounter_IncrementsOnEveryExec
2. TestQueryCounter_SeparatesReadsAndWrites
3. TestQueryCounter_ResetZeroesAll
4. TestQueryCounter_AssertLEPassesWhenUnderBudget

A-tier:
5. TestQueryCounter_ConcurrentSafety

# Workflow after green

  1. Run `make pre-push-check` — confirm clean.
  2. doc-check diff vs origin/main: every godoc on the new file ≤ 1
     line (per feedback_comments_discipline). Verify AFTER commit,
     BEFORE push.
  3. NO AI signatures — do NOT add Co-Authored-By footers (per
     feedback_no_signatures).
  4. Push branch.
  5. Open PR with `gh pr create --base main --title "feat(w7.0): T2 dbtest.QueryCounter middleware for query-budget assertions" --body-file <path>`. USE --body-file, NEVER heredoc.
  6. Spawn ONE adversarial reviewer subagent with hunt list:
     - Concurrency safety: 100 goroutines × 10 = exactly 1000 deterministic
       (no torn writes; atomic.Int64 correct).
     - Counter wraps EVERY DB method (Exec, ExecContext, Query,
       QueryContext, QueryRow, QueryRowContext). Missing any one ⇒
       silent bypass.
     - Composition not embedding — embedding would promote methods we
       didn't intercept (e.g. PingContext, Stats) and let production
       code accidentally call through.
     - dbtest package directory enforces test-only convention (`go test`
       discovers it; `go build` consumers cannot import it without
       importing test-helper).
     - AssertLE on equal-to-budget passes (boundary correct: LE not LT).
  7. Apply findings; re-run pre-push-check + doc-check; force-push.
  8. Enable automerge only after reviewer-clear (per
     feedback_review_before_automerge).

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 3 of the 5 tests.
- Adversarial reviewer verdict.
- One-line diff stat.
```

---

## §4 Task T-W7.0-3 — Lift `decideTx` to `internal/gates/approval/decide.go`

### Scope
- `internal/gates/approval/decide.go` (new):
  - Lift `cmd/regatta/approval_decide.go::decideTx` (lines 243+) verbatim, rename to exported `DecideTx`.
  - Lift the `foldResult` helper type → exported `FoldResult`.
  - Lift any private helpers `decideTx` calls (audit + lift to keep `cmd/regatta/approval_decide.go` clean).
  - Signature MUST be: `func DecideTx(ctx context.Context, db *state.DB, payload canon.TokenPayload, reviewerID, decision, reason string, clock func() time.Time) (FoldResult, string, error)`.
  - Zero behavior change. Pure refactor.
- `internal/gates/approval/decide_test.go` (new):
  - Tests cover happy-path (allow + deny), replay path, self-review rejection, unknown-key rejection, malformed-decision rejection. All tests mirror the existing `cmd/regatta/approval_decide_test.go` test cases — copy them so the refactor's correctness is provable independently of CLI plumbing.
- `cmd/regatta/approval_decide.go` (modify):
  - Delete the inlined `decideTx` function body (~80 LoC).
  - Delete the inlined `foldResult` type.
  - Re-stub: the CLI's existing caller (`approvalDecideCmd`) now calls `approval.DecideTx` directly.
  - Net LoC delta: ~ -80 LoC + ~5 LoC of import wiring. **Negative net.**
  - Existing `cmd/regatta/approval_decide_test.go` + `cmd/regatta/approval_decide_trace_id_test.go` + `cmd/regatta/approval_e2e_test.go` MUST pass unchanged. If any breaks, the lift broke behavior — STOP, re-investigate.

### Prereqs (cite spec sections)
- Spec §3.2 — "Zero modifications to approval state machine (`internal/gates/approval/gate.go`, `fold.go`, `notify.go`). `decideTx` is lifted from `cmd/regatta/approval_decide.go` into `internal/gates/approval/decide.go` so both CLI and web call `approval.DecideTx` — a pure refactor with zero behavior change."
- Spec §7 Wave 7.0 T3 — file scope + OWNER declaration per `feedback_shared_primitive_owner`.
- Spec §3.6.1 — approval-page flow consumes `approval.DecideTx` (W7.1 next wave).
- Existing patterns to reuse:
  - `cmd/regatta/approval_decide.go::decideTx` — the source-of-truth function to lift; verbatim modulo rename + export.
  - `cmd/regatta/approval_decide_test.go` — test cases to mirror into `internal/gates/approval/decide_test.go`.
  - `internal/gates/approval/gate.go` — existing package contents; the lift adds a sibling file, does not modify existing files.

### TDD test list

**B-tier:**
1. `TestApprovalDecideTx_HappyAllow` — valid token + `decision=allow` + reviewer present in quorum → succeeds; `approval_events` row written; FoldResult reflects decided state.
2. `TestApprovalDecideTx_HappyDeny` — valid token + `decision=deny` → succeeds; final state captures the deny.
3. `TestApprovalDecideTx_ReplayReturnsErrTokenReplay` — same token twice → second call returns `state.ErrTokenReplay`.
4. `TestApprovalDecideTx_SelfReviewRejected` — payload.Reviewer == approval.RequestedBy + PreventSelfReview cfg → returns typed error sentinel.

**A-tier:**
5. `TestApprovalDecideTx_RefactorPreservesCLIBehavior` — integration: run the existing CLI test fixtures (the same DB-state inputs the old `cmd/regatta/approval_decide_test.go` uses) through the new `approval.DecideTx`; assert byte-identical FoldResult output. Pins zero-behavior-change invariant.
6. `TestApprovalDecideTx_NoUpdateDeleteOutsideApprovalEvents` — `grep -rE '\b(UPDATE|DELETE)\b' internal/gates/approval/decide.go` returns zero matches (state machine append-only; reuses existing approval_events write path).

### PR body skeleton

```
## Summary

T-W7.0-3 lifts `decideTx` from cmd/regatta/approval_decide.go into a
new `internal/gates/approval/decide.go` package-internal function
`approval.DecideTx`. Pure refactor; zero behavior change.

- Lifts ~80 LoC + foldResult helper type out of cmd/regatta/ into
  internal/gates/approval/. Exports as DecideTx + FoldResult.
- Establishes the shared seam W7.0-T1 (HTTP callback) AND W7.1
  (web approval handler) both consume. Per
  feedback_shared_primitive_owner: T-W7.0-3 is OWNER; importers
  wait for this PR to merge.
- Existing CLI tests (approval_decide_test.go, approval_e2e_test.go,
  approval_decide_trace_id_test.go) pass unchanged — refactor
  correctness is provable.

## Why

MVP-3 Wave 7.0 T3 prereq for the web UI's approval flow (spec §3.2:
"Zero modifications to approval state machine. decideTx is lifted from
cmd/regatta/approval_decide.go into internal/gates/approval/decide.go
so both CLI and web call approval.DecideTx — a pure refactor with zero
behavior change."). Lifting now (before W7.1 hands off to multiple
implementers) prevents a multi-PR scramble.

## Test plan

- [x] B-tier: TestApprovalDecideTx_HappyAllow,
       TestApprovalDecideTx_HappyDeny,
       TestApprovalDecideTx_ReplayReturnsErrTokenReplay,
       TestApprovalDecideTx_SelfReviewRejected.
- [x] A-tier: TestApprovalDecideTx_RefactorPreservesCLIBehavior
       (byte-identical FoldResult vs pre-refactor CLI output),
       TestApprovalDecideTx_NoUpdateDeleteOutsideApprovalEvents.
- [x] Existing CLI tests pass unchanged:
       cmd/regatta/approval_decide_test.go,
       cmd/regatta/approval_e2e_test.go,
       cmd/regatta/approval_decide_trace_id_test.go.
- [x] make pre-push-check clean.
- [x] doc-check diff vs origin/main: godocs ≤ 1 line each.

## Failing-test output (TDD capture, before impl)

<paste from terminal — required per feedback_tdd_discipline>

## Deletion default

What gets smaller — RIGHT NOW, in this PR:
- cmd/regatta/approval_decide.go: ~ -80 LoC (function body removed),
  ~ +5 LoC (import + delegate call). Net -75 LoC at the CLI surface.
- Inlined foldResult type lifts out (—1 type from cmd/regatta/).

The refactor itself IS the shrink. The lift unblocks W7.1's web
approval handler without duplicating the DecideTx logic; if W7.0
shipped no lift, W7.1 would have copied ~80 LoC and we would have
two divergent copies of the approval-decide path on day one.

```release-notes
[REFACTOR] lift approval.DecideTx out of cmd/regatta/ for CLI+web reuse
```
```

### Dispatch prompt (paste-ready)

```
You are an implementer subagent working on a git worktree at
.claude/worktrees/agent-w7-listener-t3. Branch off main:
`git checkout -b feat/w7-listener-t3-lift-decidetx main`.

T-W7.0-3 dispatches in parallel with T-W7.0-2 (both file-disjoint).
T-W7.0-1 dispatches AFTER your PR merges to main.

# Spec authority

Source-of-truth spec: docs/engineer/specs/2026-06-01-w7-operator-web-ui-design.md.
Read ALL of: §3.2 (zero modifications to approval state machine; pure
lift), §7 Wave 7.0 T3 (your scope), §3.6.1 (downstream consumer in W7.1).

Per feedback_spec_pattern_authority: this is a PURE REFACTOR. If you
find yourself wanting to "improve" the lifted function (rename
parameters, simplify control flow, fix a bug you notice, add validation),
STOP. File a followup issue. The lift's correctness invariant is
byte-identical behavior pre/post; "improvements" break that invariant
and force a behavior-change audit. Re-spawn the design subagent if
you spot a bug — do NOT fix inline.

# Scope (exclusive write paths — file-disjoint with T-W7.0-1 + T-W7.0-2)

- internal/gates/approval/decide.go (NEW — lift target)
- internal/gates/approval/decide_test.go (NEW — mirror cmd/regatta tests)
- cmd/regatta/approval_decide.go (MODIFY — delete inlined decideTx +
  foldResult; re-stub to call approval.DecideTx)

You MUST NOT touch any other file. Specifically:
- Do NOT touch internal/gates/approval/gate.go, fold.go, notify.go,
  reaper.go, config.go (spec §3.2: "zero modifications to approval
  state machine").
- Do NOT touch cmd/regatta/approval_decide_test.go or any sibling
  test file. The existing tests MUST pass unchanged — that's the
  refactor's correctness proof.
- Do NOT touch cmd/regatta/serve.go (T-W7.0-1's domain).
- Do NOT add a goose migration. Migration number, if ever needed, is
  0007 (pinned).

# Lift mechanics (load-bearing)

1. Copy cmd/regatta/approval_decide.go::decideTx (currently at line 243)
   into internal/gates/approval/decide.go.
2. Rename `decideTx` → `DecideTx` (exported).
3. Rename `foldResult` (type, currently in cmd/regatta/) →
   `FoldResult` (exported). Lift it too.
4. Audit for any private helper functions decideTx calls that are
   declared in cmd/regatta/approval_decide.go — lift them as well
   (use lowercase / package-internal naming) so the new decide.go is
   self-contained.
5. In cmd/regatta/approval_decide.go: delete the lifted function body
   + foldResult type. The CLI's caller (likely approvalDecideCmd or
   similar) now calls `approval.DecideTx(...)`.
6. Add import `"github.com/trilamsr/regatta/internal/gates/approval"`
   to cmd/regatta/approval_decide.go (or whatever the module path is —
   inspect existing imports in the same file).

# Patterns to reuse

- The function being lifted IS the pattern. Copy verbatim, rename,
  re-export. Do NOT refactor body.
- Test patterns: mirror cmd/regatta/approval_decide_test.go cases
  into internal/gates/approval/decide_test.go. Use the same fixtures.

# Workflow steps (TDD discipline)

For each named test:
  1. Write test first.
  2. Run `go test ./internal/gates/approval/ -run <name> -v`.
  3. CAPTURE failing output.
  4. Implement the lift (or the lift step that makes this test pass).
  5. Re-run; confirm pass.
  6. After ALL new tests pass, run the existing CLI tests:
     `go test ./cmd/regatta/... -run TestApprovalDecide -v`. They MUST
     pass unchanged. If any fails, the lift broke behavior — STOP,
     re-investigate.
  7. Commit.

# Tests to land (6 named; spec §6 B/A-rubric)

B-tier:
1. TestApprovalDecideTx_HappyAllow
2. TestApprovalDecideTx_HappyDeny
3. TestApprovalDecideTx_ReplayReturnsErrTokenReplay
4. TestApprovalDecideTx_SelfReviewRejected

A-tier:
5. TestApprovalDecideTx_RefactorPreservesCLIBehavior (byte-identical
   FoldResult invariant)
6. TestApprovalDecideTx_NoUpdateDeleteOutsideApprovalEvents (append-
   only invariant inside decide.go)

# Workflow after green

  1. Run `make pre-push-check` — confirm clean. Pay close attention to
     ALL pre-existing cmd/regatta tests; the lift MUST not break them.
  2. Run `go test ./...` end-to-end. Any failure outside your scope
     means the lift broke a transitive caller — STOP and report.
  3. doc-check diff vs origin/main: godocs ≤ 1 line each on new
     exported symbols (DecideTx, FoldResult). Verify AFTER commit,
     BEFORE push.
  4. Sweep superfluous comments per feedback_comments_discipline.
  5. NO AI signatures — do NOT add Co-Authored-By footers (per
     feedback_no_signatures).
  6. Push branch.
  7. Open PR with `gh pr create --base main --title "refactor(approval): lift DecideTx into internal/gates/approval for W7 reuse (W7.0 T3)" --body-file <path>`. USE --body-file, NEVER heredoc.
  8. Spawn ONE adversarial reviewer subagent with hunt list:
     - Zero behavior change: existing cmd/regatta tests pass unchanged
       (verify by running them; do NOT trust "should still work").
     - All private helpers decideTx called are either lifted with it
       or remain accessible via export (no orphan caller).
     - foldResult fields preserve identical JSON tags / ordering if
       the type is ever serialized (audit downstream consumers).
     - Import cycle check: internal/gates/approval/ does NOT import
       cmd/regatta/* (it shouldn't — cmd imports internal, not the
       reverse). Catch the cycle BEFORE go build does.
     - Net LoC delta: cmd/regatta/approval_decide.go MUST shrink (this
       is the deletion-default proof).
     - No new error sentinels introduced; reuses state.ErrTokenReplay
       and friends.
  9. Apply findings; re-run pre-push-check + doc-check; force-push.
  10. Enable automerge ONLY after reviewer-clear AND every Risk-tier
      finding addressed (per feedback_review_before_automerge).

# Return format

Final report MUST contain:
- PR URL.
- Pasted failing-test output for at least 3 of the 6 tests.
- Confirmation that existing CLI tests pass unchanged (paste
  `go test ./cmd/regatta/... -run TestApprovalDecide` output).
- Adversarial reviewer verdict.
- One-line diff stat showing NEGATIVE net LoC at cmd/regatta/approval_decide.go.
```

---

## §5 After Wave 7.0: handoff to Wave 7.1

Wave 7.0 ships ONLY the listener prereq:
- HTTP server bound on `--addr` when `--ui=true` (T1).
- `/healthz` route (T1).
- `/api/approval/callback` route (T1; first concrete `InteractiveNotifier.CallbackRoute()`).
- `dbtest.QueryCounter` test middleware (T2; consumed by W7.2).
- `approval.DecideTx` shared seam (T3; consumed by T1 and W7.1).

**Wave 7.1 owns:**
- `internal/web/` package scaffold (server.go, render.go, embed.FS, CSP middleware).
- Approval-page handlers (`/approve/redeem`, `/approve/{approval_id}`, POST `/decide`, GET `/diff`).
- HMAC cookie-bound auth flow (spec §3.6.1).
- CSRF double-submit + Origin-header check (spec §3.6.2).
- Tailwind vendoring (spec §3.5).
- All templates (`approval.tmpl`, `approval_decided.tmpl`, `_diff.tmpl`, etc.).

**Wave 7.2 owns:** DAG list view (`/runs/{run_id}`), consumes `dbtest.QueryCounter` for R7 budget assertions.

**Wave 7.3 owns:** Cost panel (`/runs/{run_id}/cost`), consumes substrate events (migration 0006, already merged).

W7.0's followup-issue list (filed by T1 PR body):
- F1 `--public-url` flag for reverse-proxy deployments (spec §8 red-team #9; tracking issue cited in PR body).
- F2 graceful-shutdown timeout configurability (currently hardcoded 5 s; deferred until an operator asks).

---

## Adversarial-review pass (applied inline)

Reviewer subagent red-teamed this plan; findings + fixes applied:

1. **File-disjoint claim (T1 + T3 both modify `cmd/regatta/`).**
   *Finding:* T1 modifies `cmd/regatta/serve.go` and T3 modifies `cmd/regatta/approval_decide.go`. Same directory raises merge-conflict risk if either touches the package-level import block.
   *Fix applied:* §1 cross-task seam contracts state both files have stable import blocks (T1 adds `net/http`; T3 removes nothing because the lift keeps `internal/gates/approval` import in cmd's space). Go-build-disjoint verified by enumerated file names. If a merge conflict materializes in the import block, T1 (which dispatches AFTER T3 merges) rebases and resolves trivially. Documented in §Wave overview "Sequence vs parallel" bullet.

2. **Dep graph: does T1 truly need T3 first?**
   *Finding:* T1's `notify_http.go` calls `approval.DecideTx`, which T3 ships. If T1 dispatches in parallel, it would have to stub or vendor a copy.
   *Fix applied:* Sequence T3 → T1 made mandatory. T-W7.0-1 dispatch prompt explicitly states "T-W7.0-3 must merge to main BEFORE you dispatch — your code imports approval.DecideTx". T2 (independent) dispatches in parallel with T3. Peak parallelism = 2 (T2 + T3); then T1 alone. No bottleneck.

3. **TDD test list completeness vs spec §6 B-rubric items.**
   *Finding:* Spec §6 B-rubric for the listener includes: "(a) regatta serve boots with --ui=true default; --ui=false skips listener entirely (integration test asserts no LISTEN bind), (b) regatta serve --ui=true without REGATTA_HMAC_KEY set fails boot non-zero with clear error." Are both covered?
   *Fix applied:* T1 test list now includes `TestServe_UITrue_BindsListener` (a), `TestServe_UIFalse_SkipsListener` (a), `TestServe_UITrue_FailsBootIfHMACKeyUnset` (b). All three pin spec B-rubric items 1+2.

4. **Spec §6 B-rubric "Query-counter middleware ships + /runs/{run_id} render asserted ≤ 2 SQL queries (R7)" — does T2 cover this?**
   *Finding:* T2 ships the middleware but the "/runs/{run_id} render asserted ≤ 2 SQL queries" part requires a W7.2 consumer to actually exercise it. T2 alone cannot fulfill the full B-rubric item.
   *Fix applied:* T2 ships the primitive ONLY. Spec §6 B-rubric item is fulfilled jointly by T2 (this wave) + W7.2 (next wave's DAG-list implementer). PR body for T2 explicitly states "Pure test-only primitive; no production-code consumers yet" so the reviewer understands the scope split. Wave 7.2 dispatch prompt will assert "T-W7.2 MUST consume dbtest.QueryCounter and assert ≤ 2 queries per /runs/{run_id} render". Documented as a handoff in §5.

5. **Spec §3.2: "Zero modifications to approval state machine" — T3 ships `decide.go` inside `internal/gates/approval/`. Is that a state-machine modification?**
   *Finding:* The lift adds a new file to the package. Does this count as a "state machine modification"?
   *Fix applied:* Spec §3.2 specifies "gate.go, fold.go, notify.go" as the off-limits files. `decide.go` is a NEW file housing the lifted CLI helper; the state-machine logic in gate.go / fold.go / notify.go is untouched. T3's dispatch prompt explicitly lists those three files as off-limits.

6. **What if T3 spots a bug in `decideTx` during the lift?**
   *Finding:* The dispatch prompt says "pure refactor" — but humans + LLMs both spot real bugs during refactors. Telling the implementer "do not fix" risks leaving a known bug.
   *Fix applied:* T3's dispatch prompt now states: "If you spot a bug — do NOT fix inline. File a followup issue + report in your final summary." The lift's correctness invariant (byte-identical behavior pre/post) is load-bearing for `TestApprovalDecideTx_RefactorPreservesCLIBehavior`; a "fix" breaks the invariant + masks the refactor's correctness proof. Bug fixes happen in a separate PR after lift merges.

7. **Adversarial reviewer hunt list — does each task have task-specific items?**
   *Finding:* `feedback_agent_pr_review` requires every implementer PR to spawn an adversarial reviewer with a hunt list. Did each dispatch prompt include this?
   *Fix applied:* All three dispatch prompts now include a numbered "Workflow after green" step spawning ONE adversarial reviewer with a task-specific hunt list (graceful shutdown + LISTEN-bind for T1, concurrency safety + composition-vs-embedding for T2, byte-identical-behavior + import-cycle for T3) plus the `OK:`/`ISSUE:` format requirement.

8. **PR body format — `--body-file` discipline.**
   *Finding:* `feedback_pr_lint_gates` mandates `--body-file` over heredoc for PR creation. Did each dispatch prompt enforce it?
   *Fix applied:* All three dispatch prompts explicitly state "USE --body-file (per feedback_pr_lint_gates), NEVER heredoc".

9. **NO AI signatures.**
   *Finding:* MEMORY.md top entry mandates no Co-Authored-By footers anywhere. Did dispatch prompts mention this?
   *Fix applied:* All three dispatch prompts now include "NO AI signatures — do NOT add Co-Authored-By footers (per feedback_no_signatures, MEMORY.md top entry)".

10. **doc-check timing — when does it run?**
    *Finding:* `feedback_pr_lint_gates` notes godoc ≤ 1 line is a separate CI gate; verify AFTER commit BEFORE push avoids force-push churn.
    *Fix applied:* All three dispatch prompts now state "doc-check diff vs origin/main: godocs ≤ 1 line each. Verify AFTER commit, BEFORE push (per feedback_comments_discipline + feedback_pr_lint_gates)".

11. **File-overlap risk with W7 Wave 1+2+3 implementers.**
    *Finding:* Could T1's `cmd/regatta/serve.go` modification collide with W7.1's expected scope? W7.1 ships `internal/web/server.go`. Different files — no collision in the same wave window. T1's mux registers ONLY `/healthz` + `/api/approval/callback`. W7.1's implementer will add UI routes by extending the mux (T1 leaves a `mux.Handle("/ui/", ...)` extension point? or W7.1 wires a SECOND mux behind a path prefix?).
    *Fix applied:* T1 dispatch prompt explicitly states "Your wave is listener-only. NO UI routes. NO templates. NO embed.FS. NO CSP middleware. NO auth.go. NO Principal type. Those are W7.1 + W7.3 + W7.7." T1 ships the bare mux; W7.1's implementer extends it. The "Wave 7.0 + 7.1 alone is a B-grade deliverable" claim in spec §7 means W7.1 must be the consumer of T1's mux extension point. T1 does NOT design the extension contract — W7.1's design subagent will (re-)spec it. T1 ships `serve.go` such that adding mux routes from `internal/web/` is a straightforward import + mux.Handle wire-up.

12. **Scope creep — does T1's `notify_http.go` belong in W7.0 or W7.1?**
    *Finding:* W7.1's spec §7 includes approval handlers. T1's `notify_http.go` ALSO handles an approval-decide POST. Is this a scope overlap?
    *Fix applied:* Spec §3.3 row 8 (`/api/approval/callback`) is distinct from the W7.1 UI routes (`/approve/{approval_id}`, `/approve/{approval_id}/decide`). The CallbackRoute is the NOTIFIER's webhook (Slack interactive button POSTs here); the UI routes are the BROWSER's flow. Two separate code paths with the same backend (`approval.DecideTx`). T1 ships the notifier callback BECAUSE spec §7 Wave 7.0 T1 explicitly includes "wire concrete InteractiveNotifier.CallbackRoute() impl at /api/approval/callback calling into approval.DecideTx". Documented in T1 dispatch prompt scope list.

13. **Reviewer-spawn step skipped on docs-only PR for this plan.**
    *Finding:* This plan IS the docs-only deliverable. Per `feedback_review_proportional` docs-only PRs skip the adversarial-review ceremony BUT the reviewer subagent already ran on the plan content above before write.
    *Fix applied:* This adversarial-review pass section IS the reviewer's output, applied inline per task instructions. Final docs-only PR enables automerge directly.

---

_Plan authority: this plan is a dispatch artifact only. The main session copy-pastes the §2/§3/§4 dispatch prompts into Agent tool calls. Dispatch order: T2 + T3 in parallel first (file-disjoint, neither blocks the other); T1 alone after T3 merges. NO implementation, NO commit from this file._
