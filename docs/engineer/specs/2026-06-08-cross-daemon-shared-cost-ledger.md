---
title: "Cross-daemon shared cost ledger + rate-limit cooperation — Design Spec"
status: active
summary: "Aggregate cost cap + rate-limit cooperation across N daemons sharing one Anthropic API key + GitHub PAT. Default-simpler choice: shared SQLite ledger at a mounted filesystem path. Multi-machine ledger deferred to Phase X."
---

# Cross-daemon shared cost ledger + rate-limit cooperation — Design Spec

Status: ready for review
Date: 2026-06-08
Author: design subagent <tri@maydow.com>
Depends on:
- **Hard prereq (merged):** Cost governor (`internal/cost/cap/cap.go`, `internal/cost/spend/`) — global daily cap Enforcer + spend reader. Single-daemon today; this spec extends the seam.
- **Hard prereq (merged):** daemon-per-repo split (#933) — multiple daemons on one host become the common topology this spec addresses.

Memory rules in force: `feedback_default_simpler` (pick the simplest viable option, no premature abstractions), `feedback_decision_priority` (UX > ease > perf > best-prac > speed > velocity), `feedback_adversarial_review` (hostile-read mandate), `feedback_root_cause` (per-daemon cap doesn't sum — fix the aggregation surface, not the symptom), `feedback_deletion_default` (no new daemon if a shared file works), `feedback_unaddressed_load_bearing` (deferred items → tracking issues), `feedback_research_design_principles` (prefer proven OSS / kernel primitives).

---

## §0 Closing trigger

Done when: slices S1-S3 merge AND a 2-daemon acceptance test (`internal/cost/ledger/acceptance_test.go`) demonstrates aggregate cap enforcement under file:// `ledger_path=` on a shared SQLite file, with both daemons throttling at the shared ceiling rather than 2x the per-daemon ceiling.

## §1 Problem

When N regatta daemons run on one host (today: per-repo split per #933; tomorrow: any operator running ≥2 daemons against the same API key), three independent failures emerge.

**P1. Cost cap doesn't sum.** Each daemon constructs its own `cap.Enforcer` against its own SQLite DB (`internal/cost/cap/cap.go::evaluate`). The 24h spend SUM read at `cap.go:214` covers ONE daemon's substrate file. If daemon A and daemon B each carry `CapMicro = $20/day`, the operator-visible cap is "$20/day" but actual ceiling is `N × $20`. The cap loses its meaning the moment a second daemon launches.

**P2. Per-daemon rate-limit fights itself.** N daemons share one Anthropic API key + one GitHub PAT. Each daemon estimates its own token budget; bursts pile up at the upstream rate-limit gate. Headers (`anthropic-ratelimit-tokens-remaining`, `x-ratelimit-remaining`) are visible only to the daemon that just received a response — by the time daemon B reads near-zero remaining, daemon A already requeued and saturated the bucket. Result: all daemons throttle simultaneously, often into 429-storm + exponential backoff.

**P3. No durable aggregate spend view.** Per-daemon `spend.Reader` aggregates only its own DB; the operator's "what did I spend today across all daemons?" question requires N reads + summation, with no single durable artifact for `regatta cost status` to display.

Root cause: cost + rate-limit state is per-daemon-process; the **API-key boundary is the actual aggregation unit** but the code aggregates per-DB-file.

**Cross-spec relationship to #929 (closes #990):** #929 §4 Option A deferred cross-target cost aggregation under the self-host wedge (single-operator, single-target). This spec (#977) is exactly the cross-target/cross-daemon aggregation #929 named as the Option B graduation trigger. The two specs do NOT contradict: #929 picked Option A *for routing* (each target gets its own daemon + state DB); #977 adds shared-cost ONLY at the API-key boundary, leaving per-daemon state untouched. The graduation trigger from #929 (≥2 daemons sharing one API key) is the activation trigger for this spec's `cost.ledger_path` being non-empty. Operators running one daemon per API key continue with per-daemon mode unchanged.

## §2 Design options

Three options span the centralization spectrum. Each is evaluated against the decision priority (UX > ease > performance > best-practices > speed > velocity, long-term > short-term) and the self-host filter.

### Option A — Central HTTP service

A new long-lived `regatta cost-coordinator` daemon exposes gRPC/HTTP endpoints:
- `Reserve(api_key, est_micro) → (token, ok)` — pre-call hold.
- `Settle(token, actual_micro)` — post-call true-up.
- `Status(api_key) → spend_24h, rate_bucket_remaining`.

Each regatta daemon calls in before each LLM call.

**Pros.** Clean separation of concerns; multi-machine ready out of the box; allows non-SQLite stores (Redis, Postgres) trivially.

**Cons.**
- New always-on process: deploy, lifecycle, healthcheck, restart policy, port binding, auth.
- New failure mode: coordinator down → every daemon blocks (fail-closed) OR every daemon spends unbounded (fail-open). Both are worse than today.
- Network hop on every LLM call (today: 0 hops; gate is in-process).
- New crash-recovery surface: reservation tokens leak if daemon dies between Reserve + Settle → coordinator needs TTL sweeper.
- Phase-X for self-host: the sole operator runs one host, one apt of daemons; adding a coordinator daemon contradicts `feedback_default_simpler` ("don't pre-build for hypothetical drift").

**Verdict:** REJECT. Over-engineered for self-host. Reopen-trigger: ≥2 hosts running daemons against the same API key.

### Option B — Shared SQLite ledger via filesystem path

One SQLite file at `cost.ledger_path = /var/lib/regatta/ledger.db` (operator config). Every daemon on the host opens this file via `modernc.org/sqlite` with `_journal_mode=WAL&_busy_timeout=5000`. The ledger owns three tables:

```sql
CREATE TABLE shared_spend (
  api_key_hash TEXT NOT NULL,    -- sha256(API_KEY)[:16]; never the key itself
  day_anchor   TEXT NOT NULL,    -- YYYY-MM-DD in cap.TZ
  spend_micro  INTEGER NOT NULL, -- monotonic; UPSERT += delta
  updated_at   INTEGER NOT NULL, -- unix-ms
  PRIMARY KEY (api_key_hash, day_anchor)
);

CREATE TABLE shared_reservations (
  reservation_id TEXT PRIMARY KEY, -- ULID
  api_key_hash   TEXT NOT NULL,
  daemon_id      TEXT NOT NULL,    -- hostname + pid
  est_micro      INTEGER NOT NULL,
  expires_at     INTEGER NOT NULL  -- unix-ms; 60s TTL
);

CREATE TABLE shared_rate_buckets (
  api_key_hash    TEXT NOT NULL,
  bucket          TEXT NOT NULL,   -- 'anthropic_tokens' | 'anthropic_requests' | 'github_rest'
  tokens_remaining INTEGER NOT NULL,
  refill_at        INTEGER NOT NULL,
  PRIMARY KEY (api_key_hash, bucket)
);
```

Reservation flow:
1. Daemon estimates micro-USD for next LLM call (existing `cost/estimate`).
2. `BEGIN IMMEDIATE; SELECT SUM(spend_micro)+SUM(est_micro of unexpired reservations) FROM ...; ` → compare to cap.
3. If under cap: INSERT into `shared_reservations`, COMMIT, proceed.
4. Post-call: `BEGIN IMMEDIATE; UPSERT shared_spend += actual_micro; DELETE FROM shared_reservations WHERE reservation_id=?; COMMIT;`
5. Crash recovery: `expires_at < now()` reservations are swept on every ledger open + every 60s.

Rate-limit cooperation: response headers from each Anthropic call update `shared_rate_buckets` under the same transaction; pre-call, daemons check the bucket and either proceed, sleep, or yield to the daemon holding the longest-running reservation.

**Pros.**
- Zero new process. Operator deploys exactly what they deploy today.
- File:// path is the API-key boundary: any daemon mounting the same path is in the same cap.
- Crash-safe: WAL + IMMEDIATE transactions; SQLite handles N-writer arbitration; 5s busy_timeout absorbs bursts.
- In-process latency (no network); same `modernc.org/sqlite` dep already vendored.
- Operator-introspectable: `sqlite3 ledger.db 'SELECT * FROM shared_spend WHERE day_anchor = date()'` answers the cross-daemon question in one CLI.
- Forward-fits Option A: ledger schema is the wire schema; replacing the SQLite open with an HTTP client is a one-file swap inside `internal/cost/ledger/`.

**Cons.**
- Single-machine only (NFS-shared SQLite is documented unsafe — `https://www.sqlite.org/howtocorrupt.html` §2.1). Multi-machine is Phase X.
- Adds a config key (`cost.ledger_path`); operator must mount the same path across daemon services.
- Crash between Reserve and Settle leaks an `est_micro` reservation for up to TTL (60s). Tolerable: bounds aggregate over-spend at `N_concurrent_crashes × est_per_call ≈ ≤$0.50`.

**Verdict:** ADOPT.

### Option C — GitHub-resident state

Counter issue or project field updated via the GitHub API; each daemon `POST`s a comment + reads the latest counter.

**Pros.** No filesystem coupling; multi-machine ready.

**Cons.**
- Round-trip on every LLM call ($latency_max ≈ 300ms × N daemons = self-DDoS).
- Counter contention via comment append; no atomic increment in GitHub REST. Race-y.
- Burns GitHub PAT rate-limit budget to enforce GitHub PAT rate-limit budget. Snake-eats-tail.
- Public-by-default surface for spend data; redacting per-call USD requires private repo + per-call write — ceremony.

**Verdict:** REJECT.

### Recommendation

**Option B.** Shared SQLite ledger at `cost.ledger_path`, opened by every daemon on the host. Same-machine constraint is acceptable today (operator runs one host); multi-machine reopen-trigger documented in §6.

## §3 Architecture

```
+-----------------------------+    +-----------------------------+
| daemon A (repo-X)           |    | daemon B (repo-Y)           |
|  cost/cap.Enforcer          |    |  cost/cap.Enforcer          |
|  cost/spend.Reader (local)  |    |  cost/spend.Reader (local)  |
|  cost/ledger.Client  ---+   |    |  cost/ledger.Client  ---+   |
+---------------------------|-+    +---------------------------|-+
                            |                                   |
                            v                                   v
                   +----------------------------------------------+
                   | /var/lib/regatta/ledger.db (SQLite, WAL)     |
                   |  shared_spend(api_key_hash, day_anchor)      |
                   |  shared_reservations(reservation_id, …)      |
                   |  shared_rate_buckets(api_key_hash, bucket)   |
                   +----------------------------------------------+
```

New package: `internal/cost/ledger/`. Exposes:

```go
package ledger

type Client interface {
    Reserve(ctx context.Context, apiKey string, estMicro spend.USDMicro) (ReservationToken, error)
    Settle(ctx context.Context, tok ReservationToken, actualMicro spend.USDMicro) error
    AggregateSpend24h(ctx context.Context, apiKey string) (spend.USDMicro, error)

    // Rate-limit cooperation (S3).
    RateBucket(ctx context.Context, apiKey string, bucket string) (BucketState, error)
    UpdateRateBucket(ctx context.Context, apiKey string, bucket string, remaining int64, refillAt time.Time) error

    Close() error
}
```

`cap.Enforcer.cfg.Spend` swaps from local `spend.Reader` to a `LedgerSpendAdapter` that calls `ledger.Client.AggregateSpend24h` when `cost.ledger_path` is set; falls through to local Reader when unset (back-compat for single-daemon installs).

### Config surface (`regatta.yaml`)

```yaml
cost:
  ledger_path: /var/lib/regatta/ledger.db   # NEW. Unset → per-daemon mode (today).
  ledger_busy_timeout_ms: 5000               # default 5s; SQLite _busy_timeout.
  ledger_reservation_ttl_sec: 60             # crash-recovery sweep horizon.
  api_key_env: ANTHROPIC_API_KEY             # already exists; sha256(env) keys the ledger.
```

When `ledger_path` is unset → existing per-daemon behavior. **No silent migration.**

**Multi-daemon path resolution (#933 interaction, closes #990):** `ledger_path` is resolved as an ABSOLUTE host path by every daemon, NOT relative to the daemon's per-name working directory. Rationale: the ledger's point is cross-daemon coordination; resolving it under each daemon's `<workingDir>/<name>/` would defeat that. The operator MUST supply an absolute path (relative paths reject at config-load with `ErrLedgerPathNotAbsolute`). All daemons sharing an API key MUST point `cost.ledger_path` at the same host inode.

**Lockfile lifecycle (closes #1006):** The handshake anchor is the LEDGER FILE itself, not a sibling `.lock`. Each daemon `os.Stat(ledger_path)` at startup to record `(dev, inode, ctime)`. The first daemon to start creates `ledger_path`; the SQLite driver must NOT be the owner of the create (it opens with `O_CREAT` but not `O_EXCL`, which would re-introduce the race). Instead, the ledger client's constructor calls `os.OpenFile(ledger_path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)` BEFORE the first `modernc.org/sqlite` Open. On success, close the fd and let SQLite open the just-created empty file. On `EEXIST` (another daemon already created it), Stat the existing file to capture the inode — the late path. Either way, by the time SQLite opens the file the daemon already holds the verified `(dev, inode)` tuple. Late daemons see the existing file and Stat it.

Subsequent ENOENT (e.g. operator manually deletes `ledger_path` or it lives on tmpfs and the host reboots) triggers a fresh handshake on the next call: each surviving daemon re-Stats; if the dev/inode tuple differs from its cached value, it forces a re-init (close + reopen + re-verify), then resumes. The cached tuple is rechecked at MinPoll cadence (default 30s) so stale state cannot persist longer than one poll window.

**Sharing topology constraint (closes #1006):** `os.SameFile` returns `false` on NFS clients with stale client-side metadata and on overlayfs / bind-mount containers that report different device numbers for the same host inode. The ledger therefore supports LOCAL-FILESYSTEM sharing only in this Phase. When a daemon detects an apparent inode mismatch under the same `api_key_env`, it logs `WARN: ledger inode mismatch under shared API key — use a local filesystem; NFS / overlayfs / cross-container bind-mount sharing is unsupported in this Phase. Falling back to per-daemon mode.` Cross-host / cross-filesystem support is `decision_required` Phase-X; reopen trigger fires when an operator deploys ≥2 daemons across distinct hosts sharing one API key.

## §4 Acceptance

**A1 (aggregate cap).** Two daemons configured with `CapMicro = $1.00/day` + same `ledger_path` + same API key. Daemon A spends $0.60, daemon B spends $0.50 — total $1.10. The next call from EITHER daemon is throttled. (Today: both proceed because each sees only its own $0.60 or $0.50.)

**A2 (crash leaves ≤60s of orphan reservation).** Daemon A holds a $0.10 reservation, `kill -9`s. Within 60s of TTL elapsing, daemon B's reservation sweep removes the row; aggregate `spend_24h + reservations` drops by $0.10.

**A3 (rate-limit cooperation).** Daemon A receives `anthropic-ratelimit-tokens-remaining: 1000`. Daemon B's next pre-call check reads the shared bucket and either proceeds (if est ≤ 1000) or sleeps until `refill_at`. Single daemon without ledger configured: behavior unchanged.

**A4 (back-compat).** `ledger_path` unset → identical behavior to before this spec (per-daemon enforcement). No new dep, no schema migration on existing DBs.

**A5 (concurrency).** 50 goroutines × 2 daemons issuing concurrent Reserve+Settle for 60s against one ledger file: no SQLite `database is locked` escaping the 5s busy_timeout, aggregate spend reconciles within ±1 micro-USD of recorded actuals.

Test files:
- `internal/cost/ledger/ledger_test.go` — schema + Reserve/Settle unit.
- `internal/cost/ledger/concurrent_test.go` — A5 hammer test (parallel `t.Parallel()`).
- `internal/cost/ledger/acceptance_test.go` — A1-A4 via two `cap.Enforcer` instances against one ledger file.

## §5 Adversarial review

**R1 (ledger corruption race).** Two daemons issue `BEGIN IMMEDIATE` simultaneously. SQLite serializes via the file lock; loser blocks up to `busy_timeout_ms` then retries. Risk: a third writer arriving inside the busy window times out. Mitigation: 5s timeout > 99.9th-percentile of contended write under measured load (one row UPSERT in WAL mode ≈ 1-3ms). If timeout fires, daemon-side retry with jittered backoff (×3, then surface error to gate; gate fails-closed = throttled, matching `feedback_root_cause` + cap §1 fail-closed contract). Property test: `internal/cost/ledger/concurrent_test.go` asserts zero corrupted rows + bounded retry under N=50 concurrent writers.

**R2 (daemon crash leaves locked row).** `BEGIN IMMEDIATE` holds the SQLite write lock for the duration of the transaction. Daemon crash mid-transaction → SQLite rolls back via WAL on next open; lock released. The `shared_reservations` row is `expires_at`-keyed, not lock-keyed, so reservations expire on wall-time, not on daemon liveness. Sweep runs on every Client.Open + every 60s tick.

**R3 (ledger replication for multi-machine).** Explicitly OUT OF SCOPE. SQLite over NFS is documented-unsafe (`https://www.sqlite.org/howtocorrupt.html` §2.1). Multi-machine reopen-trigger: ≥2 hosts running daemons against the same API key. Migration path: Option A coordinator with the same wire schema = one-file swap in `internal/cost/ledger/`. Tracking: file under `[REVIEWER #PR] PHASE-X cross-daemon: multi-machine ledger` before this spec's slice-1 PR merges.

**R4 (cap-bypass via missing reservation).** Daemon calls LLM but never calls `ledger.Client.Reserve` (bug / network split / dev path). Aggregate cap silently underestimates. Mitigation: `cap.Enforcer.Allow` MUST be the only gate path; auditing the `cost/gate` callsite list confirms exactly one entry point. Reviewer comment-sweep enforces "no spend without prior reservation" invariant.

**R5 (API-key-hash collision).** `sha256(API_KEY)[:16]` = 64-bit truncation. Birthday-bound on collision: 2^32 keys. Operator self-host runs ≤ 10 keys lifetime → negligible. Document `api_key_hash` semantics + bump to `[:32]` if Phase X surfaces ≥1M keys.

**R6 (clock skew across daemons).** Two daemons on one machine share the kernel clock; skew = 0. Out of scope until multi-machine (R3).

**R7 (ledger schema migration).** First daemon to open creates schema; later daemons see it. Schema version pinned via `PRAGMA user_version = 1`; bumps gated by `internal/cost/ledger/migrations/`.

**R8 (Anthropic key rotation mid-window).** Spend pre-rotation lives under old `api_key_hash`; post-rotation under new hash. Aggregate cap effectively resets on rotation. Acceptable: key rotation is a deliberate operator action; documented in operator notes.

**R9 (rate-limit bucket staleness).** `shared_rate_buckets.tokens_remaining` is a snapshot of the last header any daemon saw; between snapshots, real budget drifts. Mitigation: store `refill_at` from headers (Anthropic provides), treat bucket as authoritative only until `refill_at`, fall back to optimistic-estimate after.

## §6 Out of scope

- **Multi-machine ledger.** Reopen-trigger: ≥2 hosts running daemons against the same API key. Migration: Option A coordinator with identical wire schema.
- **Tenant isolation.** Self-host is single-tenant; `api_key_hash` is already the practical isolation key. Phase X re-introduces `tenant_id` per the self-host filter.
- **Per-DAG / per-operator cross-daemon aggregation.** Cap is at the API-key boundary; sub-key scopes remain per-daemon.
- **Replacement of `internal/cost/spend` writer path.** Each daemon still writes per-DB substrate rows for audit; the ledger is an additive aggregation surface, not a replacement. Deletion of per-daemon spend writes is OUT OF SCOPE (would break per-daemon `regatta cost status` + audit).

## §7 Implementer brief (3 slices)

### S1 — Ledger schema + sqlite locking

Owner: implementer subagent 1.
Files: `internal/cost/ledger/{client.go,schema.go,client_test.go,concurrent_test.go,migrations/0001_initial.sql}` (new package + sibling migration dir).
Out of scope for S1: any wire-up to `cap.Enforcer` (S2) or `cost/gate` (S2).

**Migration namespace (pinned, closes #990):** the ledger schema lives in its OWN migration namespace at `internal/cost/ledger/migrations/`, not the existing `internal/orchestrator/state/migrations/`. Rationale: the ledger is an external-side-effect DB (shared across daemons via filesystem path), not part of the per-daemon state machine. Numbering restarts at `0001`; new ledger migrations land as `internal/cost/ledger/migrations/000N_<slug>.sql`. The orchestrator-state migration counter (per CLAUDE.md `feedback_migration_number_lock` + PR #971's `make next-migration`) is unaffected; subagents working on orchestrator-state migrations and ledger migrations in parallel cannot collide.

Deliverables:
1. `Client` interface + `sqliteClient` impl using `modernc.org/sqlite` with `_journal_mode=WAL&_busy_timeout=5000`.
2. Schema as in §3 with `PRAGMA user_version = 1`, embedded SQL in `internal/cost/ledger/migrations/0001_initial.sql`.
3. `Reserve` + `Settle` + `AggregateSpend24h` + reservation sweep (every 60s + on Open).
4. TDD-RED commit: `concurrent_test.go` failing against an empty package; THEN impl; THEN green.
5. `internal/cost/ledger/acceptance_test.go` shell (skipped) referencing the §4 acceptance criteria — populated in S2.

Acceptance for S1: A5 passes; A1-A4 skipped.

### S2 — Daemon-side reservation API + cap enforcement

Owner: implementer subagent 2 (after S1 merges).
Files: `internal/cost/cap/cap.go` (extend), `internal/cost/gate/gate.go` (wire Reserve/Settle), `cmd/regatta/serve.go` (config + Client construction), `internal/cost/ledger/acceptance_test.go` (populate A1-A4).

Deliverables:
1. `LedgerSpendAdapter` wrapping `ledger.Client.AggregateSpend24h` → satisfies `cap.SpendReader`.
2. `cost/gate` pre-call: `tok, err := ledger.Reserve(...)`; on LLM-call return: `ledger.Settle(tok, actual)`.
3. `regatta.yaml` config keys per §3.
4. A1-A4 green.

Acceptance for S2: A1-A4 + A5 all green.

### S3 — Rate-limit cooperation (per-key token bucket via same ledger)

Owner: implementer subagent 3 (after S2 merges).
Files: `internal/ratelimit/shared/` (new package), `internal/orchestrator/spawner/genai.go` (header capture), `internal/ghclient/ratelimit.go` (GitHub headers).

Deliverables:
1. `shared.Bucket` reads/writes `shared_rate_buckets` via `ledger.Client`.
2. Anthropic response hook updates `anthropic-ratelimit-tokens-remaining` + `anthropic-ratelimit-requests-remaining`.
3. GitHub response hook updates `x-ratelimit-remaining`.
4. Pre-call gate: if `tokens_remaining < est_tokens` and `now < refill_at`, sleep until `refill_at` or yield to caller.
5. `internal/ratelimit/shared/cooperation_test.go` — A3 acceptance.

Acceptance for S3: A1-A5 all green; A3 explicitly demonstrated under 2-daemon parallel load.

---

```release-notes
[DOCS] specs: cross-daemon shared cost ledger + rate-limit cooperation design (#NNN)
```
