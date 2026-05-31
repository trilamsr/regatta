# MVP-1 Planner-as-DAG Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the MVP-1 universal-queue planner-as-DAG pipeline so a single `markdown_catalog` program with ≥3 acceptance criteria produces exactly 3 reserved + running stub agents via `regatta program plan --write` + `regatta serve --tick-once`.

**Architecture:** New `state.work_items` sqlite table is single source of truth. Two writers (`AdapterSync`, `BriefLoader`) populate it each tick. Scheduler reads via `work_items LEFT JOIN agents` and reserves directly into `agents` in one transaction — eliminating the orphan-row class. Cascade-soft archival preserves in-flight agents. Brief signing is verified before persistence; rejections emit `slog.LevelWarn` events. A process-level flock prevents concurrent `PollOnce`.

**Tech Stack:** Go 1.22+, modernc.org/sqlite, `pressly/goose` v3 (forward-only migrations, in-process embed), `gofrs/flock` v0.12 (advisory lockfile), `log/slog` (stdlib), `pgregory.net/rapid` v1 (property testing). No new HTTP, no new RPC.

**Decision priority (apply at every choice point):** UX > ease-of-use > best-practices > execution-speed > velocity. Long-term over short-term.

**Spec:** `docs/superpowers/specs/2026-05-30-mvp-1-planner-as-dag-design.md`. Binding RFC: `docs/rfcs/0001-mvp-1-program-publish.md`. The 13 locked decisions in the spec are non-negotiable.

**TDD discipline:** Every step labeled "Write the failing test" comes BEFORE the implementation step. Run the test. See it fail. Then write code. See it pass. Then commit.

---

## Task 0 (A0): Foundation — typed errors + clock interface

Blocks every other task. Lands first.

**Files:**
- Create: `internal/orchestrator/errors.go`
- Create: `internal/orchestrator/errors_test.go`
- Create: `internal/orchestrator/clock/clock.go`
- Create: `internal/orchestrator/clock/clock_test.go`

- [ ] **Step 0.1: Write failing test for typed error sentinels**

Create `internal/orchestrator/errors_test.go`:

```go
package orchestrator

import (
	"errors"
	"testing"
)

func TestSentinelsDistinct(t *testing.T) {
	all := []error{
		ErrBriefSHAMismatch,
		ErrHMACInvalid,
		ErrTargetExists,
		ErrFlockHeld,
		ErrSchemaTooNew,
		ErrCycleDetected,
	}
	seen := map[string]bool{}
	for _, e := range all {
		if e == nil {
			t.Fatalf("sentinel must be non-nil")
		}
		msg := e.Error()
		if seen[msg] {
			t.Fatalf("duplicate sentinel message: %q", msg)
		}
		seen[msg] = true
	}
}

func TestSentinelWrapping(t *testing.T) {
	wrapped := errors.Join(ErrBriefSHAMismatch, errors.New("extra context"))
	if !errors.Is(wrapped, ErrBriefSHAMismatch) {
		t.Fatalf("errors.Is must match through Join")
	}
}
```

- [ ] **Step 0.2: Run test to verify it fails**

Run: `go test ./internal/orchestrator/...`
Expected: FAIL — `undefined: ErrBriefSHAMismatch` (and other undefined sentinels).

- [ ] **Step 0.3: Implement typed sentinels**

Create `internal/orchestrator/errors.go`:

```go
// Package orchestrator hosts the typed error sentinels shared across
// the MVP-1 universal-queue pipeline. Downstream packages
// (adaptersync, brief loader, lockfile, scheduler, state migrations)
// MUST import sentinels from here rather than calling errors.New
// at boundary points — verified by `make ci-check` grep gate.
//
// per RFC-0001 §3: cascade-soft, fail-fast PollOnce, single-source-of-truth
// work_items table.
package orchestrator

import "errors"

var (
	// ErrBriefSHAMismatch fires when the operator-pinned planner
	// prompt SHA in regatta.yaml does not match the on-disk prompt.
	ErrBriefSHAMismatch = errors.New("orchestrator: planner prompt SHA does not match pinned value")

	// ErrHMACInvalid fires when a program_brief.json fails HMAC
	// verification against the configured keyring.
	ErrHMACInvalid = errors.New("orchestrator: brief HMAC signature invalid")

	// ErrTargetExists fires when `regatta program plan --write` would
	// overwrite an existing brief with different content (use --force
	// to override).
	ErrTargetExists = errors.New("orchestrator: target file exists with different content")

	// ErrFlockHeld fires when the process-level lockfile is held by
	// another live regatta instance. Distinct from state.ErrLockHeld
	// which guards hotspot-locks within a single process.
	ErrFlockHeld = errors.New("orchestrator: process flock held by another instance")

	// ErrSchemaTooNew fires when a v2 database is opened by a binary
	// that only knows v1 migrations — downgrade-resistance.
	ErrSchemaTooNew = errors.New("orchestrator: database schema is newer than this binary supports")

	// ErrCycleDetected fires when work_items.depends_on_features would
	// introduce a cycle, blocking the upsert.
	ErrCycleDetected = errors.New("orchestrator: dependency cycle detected in work_items")
)
```

- [ ] **Step 0.4: Run test to verify it passes**

Run: `go test ./internal/orchestrator/...`
Expected: PASS — `TestSentinelsDistinct` and `TestSentinelWrapping` both green.

- [ ] **Step 0.5: Write failing test for Clock interface**

Create `internal/orchestrator/clock/clock_test.go`:

```go
package clock

import (
	"testing"
	"time"
)

func TestSystemClockReturnsRecentNow(t *testing.T) {
	c := System()
	before := time.Now()
	got := c.Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatalf("SystemClock.Now()=%v outside [%v, %v]", got, before, after)
	}
}

func TestFakeClockReturnsFixedTime(t *testing.T) {
	want := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	c := Fake(want)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("FakeClock.Now()=%v want %v", got, want)
	}
}

func TestFakeClockAdvance(t *testing.T) {
	start := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	c := Fake(start)
	c.Advance(5 * time.Second)
	want := start.Add(5 * time.Second)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("after Advance(5s) Now()=%v want %v", got, want)
	}
}
```

- [ ] **Step 0.6: Run test to verify it fails**

Run: `go test ./internal/orchestrator/clock/...`
Expected: FAIL — `undefined: Clock`, `undefined: System`, `undefined: Fake`.

- [ ] **Step 0.7: Implement Clock**

Create `internal/orchestrator/clock/clock.go`:

```go
// Package clock provides a Clock interface so time-sensitive
// orchestrator paths (PollOnce timestamps, tombstone cutoffs,
// stale-PID reclaim) are deterministic under test.
//
// Production code uses System(). Tests use Fake(t0) + Advance(d).
package clock

import (
	"sync"
	"time"
)

// Clock is the abstraction over time.Now. Implementations must be
// safe for concurrent use.
type Clock interface {
	Now() time.Time
}

// System returns a Clock backed by time.Now.
func System() Clock { return systemClock{} }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// FakeClock is a Clock with a settable now value, safe for concurrent
// reads via internal mutex.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// Fake returns a FakeClock initialized at t.
func Fake(t time.Time) *FakeClock {
	return &FakeClock{now: t}
}

// Now reports the current fake time.
func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the fake time forward by d. d may be zero or
// negative; callers control monotonicity.
func (f *FakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
```

- [ ] **Step 0.8: Run test to verify it passes**

Run: `go test ./internal/orchestrator/clock/...`
Expected: PASS — all three tests green.

- [ ] **Step 0.9: Commit Wave 0**

```bash
git add internal/orchestrator/errors.go internal/orchestrator/errors_test.go \
        internal/orchestrator/clock/clock.go internal/orchestrator/clock/clock_test.go
git commit -m "feat(orchestrator): typed error sentinels + Clock interface

Wave 0 of MVP-1 Planner-as-DAG. Downstream waves import sentinels
from internal/orchestrator/errors.go and inject Clock into
PollOnce, AdapterSync, BriefLoader, and lockfile reclaim."
```

---

## Task 1 (A1): Migrations runner (goose) + go.mod deps

Wave 1, parallel with A2. Owns the migration numbering authority for the entire PR series; other waves needing schema changes coordinate via PR review on A1's stub.

**Files:**
- Create: `internal/orchestrator/state/migrations/0001_initial.sql`
- Create: `internal/orchestrator/state/migrations/0002_work_items.sql`
- Create: `internal/orchestrator/state/migrate.go`
- Create: `internal/orchestrator/state/migrate_test.go`
- Modify: `internal/orchestrator/state/state.go` (replace `migrate(ctx)` with goose call)
- Delete: `internal/orchestrator/state/schema.sql` (after extraction into 0001_initial.sql)
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1.1: Front-load every PR-series dependency**

Task 1 owns sole authority on `go.mod` / `go.sum` for the entire PR series. Add every dep any downstream task will need so no later task touches the module file:

```bash
go get github.com/pressly/goose/v3@v3.22.1     # Task 1: migrations
go get github.com/gofrs/flock@v0.12.1          # Task 2: lockfile
go get pgregory.net/rapid@v1.1.0               # Task 4: DAG property test
go mod tidy
```

Expected: `go.sum` updated; `go.mod` shows all three new deps. **Subsequent tasks MUST NOT run `go get`** — flag any plan step that does as a bug and back-port the dep here.

In Task 4 Step 4.5, the `go get rapid` line is now a no-op; skip it.

- [ ] **Step 1.2: Extract schema.sql into 0001_initial.sql**

Run:
```bash
mkdir -p internal/orchestrator/state/migrations
```

Copy the contents of `internal/orchestrator/state/schema.sql` into `internal/orchestrator/state/migrations/0001_initial.sql` verbatim, prepending goose headers:

```sql
-- +goose Up
-- +goose StatementBegin
-- Regatta orchestrator state schema, version 1.
--
-- Tables follow the agent state machine in docs/design.md §State,
-- persistence, recovery. Migrations are forward-only: bump
-- schema_version, append a new section, never edit a shipped block.

CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS agents (
    id              INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    work_item_id    TEXT    NOT NULL UNIQUE,
    lane            TEXT    NOT NULL,
    state           TEXT    NOT NULL,
    pid             INTEGER NOT NULL DEFAULT 0,
    session_id      TEXT    NOT NULL DEFAULT '',
    pr_sha          TEXT    NOT NULL DEFAULT '',
    rejection_count INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agents_state ON agents(state);
CREATE INDEX IF NOT EXISTS idx_agents_lane  ON agents(lane);

CREATE TABLE IF NOT EXISTS locks (
    name         TEXT    NOT NULL PRIMARY KEY,
    agent_id     INTEGER NOT NULL REFERENCES agents(id),
    acquired_at  INTEGER NOT NULL,
    heartbeat_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_locks_agent ON locks(agent_id);

CREATE TABLE IF NOT EXISTS events (
    id           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    agent_id     INTEGER REFERENCES agents(id),
    kind         TEXT    NOT NULL,
    payload_json TEXT    NOT NULL DEFAULT '{}',
    created_at   INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_agent ON events(agent_id);
CREATE INDEX IF NOT EXISTS idx_events_kind  ON events(kind);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only; down migrations are intentionally empty. Operators
-- recover by restoring from snapshot.
SELECT 1;
-- +goose StatementEnd
```

- [ ] **Step 1.3: Write 0002_work_items.sql**

Create `internal/orchestrator/state/migrations/0002_work_items.sql`:

```sql
-- +goose Up
-- +goose StatementBegin
-- Universal queue: state.work_items is single source of truth for
-- spawnable work. AdapterSync (source=adapter) + BriefLoader
-- (source=brief) both upsert here. Scheduler.ListSpawnable joins
-- against agents to materialize pending rows on demand.
-- per RFC-0001 §3.

CREATE TABLE IF NOT EXISTS work_items (
    id                   TEXT    NOT NULL PRIMARY KEY,
    kind                 TEXT    NOT NULL,           -- feature | program
    title                TEXT    NOT NULL,
    lane                 TEXT    NOT NULL,
    status               TEXT    NOT NULL,           -- planned | running | pr_open | merged | archived | blocked
    parent_program_id    TEXT,                       -- NULL for top-level
    depends_on_features  TEXT    NOT NULL DEFAULT '[]',
    acceptance_json      TEXT    NOT NULL DEFAULT '[]',
    source               TEXT    NOT NULL,           -- adapter | brief
    last_seen_at         INTEGER NOT NULL,           -- unix seconds
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_work_items_status ON work_items(status);
CREATE INDEX IF NOT EXISTS idx_work_items_parent ON work_items(parent_program_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
```

- [ ] **Step 1.4: Write failing test for goose runner**

Create `internal/orchestrator/state/migrate_test.go`:

```go
package state

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

func TestMigrate_EmptyDBAppliesV1AndV2(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	raw, err := sql.Open("sqlite", DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)

	if err := Migrate(context.Background(), raw); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var version int
	if err := raw.QueryRow("SELECT MAX(version_id) FROM goose_db_version").Scan(&version); err != nil {
		t.Fatalf("read goose version: %v", err)
	}
	if version != 2 {
		t.Fatalf("version=%d want 2", version)
	}

	var work int
	if err := raw.QueryRow("SELECT COUNT(*) FROM work_items").Scan(&work); err != nil {
		t.Fatalf("work_items table missing: %v", err)
	}
}

func TestMigrate_IdempotentOnSecondCall(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	raw, _ := sql.Open("sqlite", DSN(dbPath))
	defer raw.Close()
	raw.SetMaxOpenConns(1)

	if err := Migrate(context.Background(), raw); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := Migrate(context.Background(), raw); err != nil {
		t.Fatalf("second Migrate (should be no-op): %v", err)
	}
}

func TestMigrate_DowngradeResistance(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	raw, _ := sql.Open("sqlite", DSN(dbPath))
	defer raw.Close()
	raw.SetMaxOpenConns(1)

	// Forcibly insert a future version row simulating a v3 DB.
	if err := Migrate(context.Background(), raw); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := raw.Exec("INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES (99, 1, CURRENT_TIMESTAMP)"); err != nil {
		t.Fatalf("inject future version: %v", err)
	}

	err := Migrate(context.Background(), raw)
	if !errors.Is(err, orchestrator.ErrSchemaTooNew) {
		t.Fatalf("err=%v want ErrSchemaTooNew", err)
	}
}
```

- [ ] **Step 1.5: Run test to verify it fails**

Run: `go test ./internal/orchestrator/state/ -run TestMigrate`
Expected: FAIL — `undefined: Migrate`.

- [ ] **Step 1.6: Implement goose runner**

Create `internal/orchestrator/state/migrate.go`:

```go
// Package state migration runner. Wraps pressly/goose to apply
// versioned forward-only migrations from the embedded migrations/
// directory. Open() calls Migrate(); callers should not invoke this
// directly outside tests.
//
// per RFC-0001 §3: schema bumps require goose-managed migrations,
// hand-rolled DDL inside Open() is replaced.
package state

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/pressly/goose/v3"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// highestKnownVersion is the version this binary's embedded
// migrations top out at. Migrate() rejects DBs whose
// goose_db_version exceeds this — see ErrSchemaTooNew.
const highestKnownVersion int64 = 2

// Migrate applies every pending forward migration to db. Returns
// ErrSchemaTooNew (wrapped) when the database has been touched by
// a newer binary; the operator must upgrade rather than downgrade.
func Migrate(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("state: goose dialect: %w", err)
	}

	current, err := goose.GetDBVersionContext(ctx, db)
	if err != nil && !errors.Is(err, goose.ErrNoNextVersion) {
		// First boot: goose_db_version doesn't exist yet, treated as 0.
		if !isMissingVersionTable(err) {
			return fmt.Errorf("state: read goose version: %w", err)
		}
		current = 0
	}
	if current > highestKnownVersion {
		return fmt.Errorf("%w: db=%d binary=%d", orchestrator.ErrSchemaTooNew, current, highestKnownVersion)
	}

	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("state: goose up: %w", err)
	}

	// Re-check after Up — handles case where an injected future row
	// was below the highest version pre-Up but goose advanced past it.
	current, err = goose.GetDBVersionContext(ctx, db)
	if err == nil && current > highestKnownVersion {
		return fmt.Errorf("%w: db=%d binary=%d", orchestrator.ErrSchemaTooNew, current, highestKnownVersion)
	}
	return nil
}

func isMissingVersionTable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "no such table") || contains(msg, "goose_db_version")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 1.7: Rewire Open() to call Migrate (BEFORE deleting schema.sql)**

In `internal/orchestrator/state/state.go`, in the same edit:

1. Replace the `Open` function body's `db.migrate(ctx)` call with `Migrate(ctx, raw)`:
   ```go
   	if err := Migrate(ctx, raw); err != nil {
   		_ = raw.Close()
   		return nil, err
   	}
   ```
2. Remove the `//go:embed schema.sql` directive and the `var schemaSQL string` declaration.
3. Delete the old `func (d *DB) migrate(ctx context.Context) error { ... }` body.
4. Drop `_ "embed"` import if no longer needed.

Save and run `go build ./...` to confirm the tree compiles **before** deleting schema.sql. If build fails because `schemaSQL` is referenced elsewhere (e.g. tests), fix those references first.

- [ ] **Step 1.8: Run state tests with migrations active**

Run: `go test ./internal/orchestrator/state/...`
Expected: PASS — `TestMigrate_*` plus all existing state tests green. If existing tests fail because they hand-roll the schema, fix by calling `Open()` (which now applies migrations) instead of executing `schemaSQL` directly.

- [ ] **Step 1.9: Delete schema.sql (only after tests pass)**

Once the migrate path is proven, the old file is dead:
```bash
git rm internal/orchestrator/state/schema.sql
```
Re-run `go build ./...` and `go test ./internal/orchestrator/state/...` to confirm nothing depended on the file being present.

Run: `go test ./internal/orchestrator/state/...`
Expected: PASS — `TestMigrate_*` plus all existing state tests green. If existing tests fail because they hand-roll the schema, fix by calling `Open()` (which now applies migrations) instead of executing `schemaSQL` directly.

- [ ] **Step 1.10: Wave 1.5 smoke gate**

Run: `go test -race ./internal/orchestrator/state/...`
Expected: PASS, no race warnings. **Wave 2 is BLOCKED until this gate is green.**

- [ ] **Step 1.11: Commit Task 1**

```bash
git add internal/orchestrator/state/ go.mod go.sum
git commit -m "feat(state): goose-managed migrations + work_items schema

Replaces hand-rolled migrate() with pressly/goose runner reading
from embedded migrations/. Adds work_items table (migration 0002)
as universal-queue source of truth. Adds gofrs/flock dependency
for Task 2.

Closes the 'no migrations infrastructure' gap noted in spec §2.1.
ErrSchemaTooNew wraps downgrade attempts; verified by
TestMigrate_DowngradeResistance."
```

---

## Task 2 (A2): Lockfile package (gofrs/flock + stale-PID reclaim)

Wave 1, parallel with A1.

**Files:**
- Create: `internal/orchestrator/lockfile/lockfile.go`
- Create: `internal/orchestrator/lockfile/lockfile_test.go`

- [ ] **Step 2.1: Write failing test**

Create `internal/orchestrator/lockfile/lockfile_test.go`:

```go
package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

func TestAcquire_Release_Roundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lockfile not removed: %v", err)
	}
}

func TestAcquire_HeldByLivePID_ReturnsErrFlockHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer first.Release()

	_, err = Acquire(path)
	if !errors.Is(err, orchestrator.ErrFlockHeld) {
		t.Fatalf("second Acquire err=%v want ErrFlockHeld", err)
	}
}

func TestAcquire_StalePID_Reclaims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	// Plant a lockfile containing a definitely-dead PID. PID 0 is
	// reserved on POSIX and kill(0, 0) returns ESRCH or EPERM —
	// either is enough for the reclaim path to consider it dead.
	// We use a very high PID instead to guarantee ESRCH.
	staleContent := []byte(strconv.Itoa(0x7FFFFFFE))
	if err := os.WriteFile(path, staleContent, 0o600); err != nil {
		t.Fatalf("plant stale lockfile: %v", err)
	}

	lock, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire after stale: %v", err)
	}
	defer lock.Release()
}
```

- [ ] **Step 2.2: Run test to verify it fails**

Run: `go test ./internal/orchestrator/lockfile/...`
Expected: FAIL — `undefined: Acquire`.

- [ ] **Step 2.3: Implement lockfile**

Create `internal/orchestrator/lockfile/lockfile.go`:

```go
// Package lockfile provides a process-level advisory lock keyed on a
// file path. Used by orchestrator.PollOnce to prevent concurrent
// ticks (e.g. `regatta serve` + `regatta serve --tick-once` in
// parallel) from racing the work_items + agents tables.
//
// Convention: pass `<dbPath> + ".lock"`. PollOnce derives the path
// from o.dbPath so the lockfile sits beside the sqlite file the
// operator chose.
//
// Sentinel is orchestrator.ErrFlockHeld — distinct from
// state.ErrLockHeld which is for in-process hotspot locks. Same word
// "lock", two semantic surfaces.
//
// Stale-PID reclaim: if the lockfile contains a PID that no longer
// exists (kill(pid, 0) returns ESRCH), Acquire removes the stale
// file and proceeds. Same-host assumption — pid-based liveness has
// no meaning across machines, but regatta is operator-local so this
// holds.
package lockfile

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/gofrs/flock"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

// Lock represents a held advisory lock. Call Release when done.
type Lock struct {
	path string
	fl   *flock.Flock
}

// Acquire takes an exclusive lock on path. Returns ErrFlockHeld
// (wrapped) if another live process holds the lock. Reclaims if the
// lockfile contains a stale PID.
func Acquire(path string) (*Lock, error) {
	if err := maybeReclaimStale(path); err != nil {
		return nil, err
	}

	fl := flock.New(path)
	locked, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("lockfile: trylock: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("%w: %s", orchestrator.ErrFlockHeld, path)
	}

	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(path, []byte(pid), 0o600); err != nil {
		_ = fl.Unlock()
		return nil, fmt.Errorf("lockfile: write pid: %w", err)
	}
	return &Lock{path: path, fl: fl}, nil
}

// Release removes the lockfile and releases the advisory lock.
func (l *Lock) Release() error {
	if l == nil || l.fl == nil {
		return nil
	}
	if err := l.fl.Unlock(); err != nil {
		return fmt.Errorf("lockfile: unlock: %w", err)
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("lockfile: remove: %w", err)
	}
	return nil
}

func maybeReclaimStale(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("lockfile: read: %w", err)
	}
	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		return os.Remove(path)
	}
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		// Garbage content — treat as stale, reclaim.
		return os.Remove(path)
	}
	if processAlive(pid) {
		return nil
	}
	return os.Remove(path)
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// EPERM means the process exists but we can't signal it —
	// still alive from our perspective.
	if errno, ok := err.(syscall.Errno); ok && errno == syscall.EPERM {
		return true
	}
	return false
}
```

- [ ] **Step 2.4: Run test to verify it passes**

Run: `go test ./internal/orchestrator/lockfile/...`
Expected: PASS — all three tests green.

- [ ] **Step 2.5: Commit Task 2**

```bash
git add internal/orchestrator/lockfile/
git commit -m "feat(lockfile): gofrs/flock wrapper with stale-PID reclaim

PollOnce derives lockfile path as <dbPath>.lock per spec §2.7.
ErrFlockHeld distinguishes process-level contention from
state.ErrLockHeld (hotspot locks). Stale reclaim uses kill(pid, 0)
on POSIX rather than mtime to tolerate clock skew."
```

---

## Task 3 (A3): work_items API — interface stub + upsert / tombstone / cascade

Wave 2. **BLOCKED on Task 1's Wave 1.5 smoke gate.** Task 4 (A4) imports the stub.

**Files:**
- Create: `internal/orchestrator/state/work_items.go` (types + stubs)
- Create: `internal/orchestrator/state/work_items_upsert.go` (impls)
- Create: `internal/orchestrator/state/work_items_upsert_test.go`

- [ ] **Step 3.1: Write the stub file first (lands before A4 starts)**

Create `internal/orchestrator/state/work_items.go`:

```go
// Package state — work_items types and method signatures.
//
// Universal-queue spec §2.2. AdapterSync (source=adapter) and
// BriefLoader (source=brief) both upsert here. Scheduler reads via
// the join-based ListSpawnable and reserves directly into agents in
// one transaction. Cascade-soft: archived parents do not kill
// in-flight child agents; the child row's acceptance_json snapshot
// keeps validation self-contained.
//
// Methods split across:
//   - work_items_upsert.go: UpsertWorkItem, TombstoneBySource,
//                            CascadeArchiveChildren
//   - work_items_query.go: ListSpawnable, CycleCheck, ListByParent,
//                           GetWorkItem
package state

import (
	"context"
	"time"
)

// WorkItemKind enumerates work_items.kind values.
type WorkItemKind string

const (
	KindFeature WorkItemKind = "feature"
	KindProgram WorkItemKind = "program"
)

// WorkItemStatus enumerates work_items.status values.
type WorkItemStatus string

const (
	WorkStatusPlanned  WorkItemStatus = "planned"
	WorkStatusRunning  WorkItemStatus = "running"
	WorkStatusPROpen   WorkItemStatus = "pr_open"
	WorkStatusMerged   WorkItemStatus = "merged"
	WorkStatusArchived WorkItemStatus = "archived"
	WorkStatusBlocked  WorkItemStatus = "blocked"
)

// WorkItemSource enumerates work_items.source values.
type WorkItemSource string

const (
	SourceAdapter WorkItemSource = "adapter"
	SourceBrief   WorkItemSource = "brief"
)

// WorkItem mirrors a row in work_items. depends_on_features and
// acceptance_json are stored as JSON text in sqlite; the Go fields
// here are the decoded slices.
type WorkItem struct {
	ID                 string
	Kind               WorkItemKind
	Title              string
	Lane               string
	Status             WorkItemStatus
	ParentProgramID    string // empty for top-level
	DependsOnFeatures  []string
	AcceptanceJSON     string // raw JSON, opaque to state package
	Source             WorkItemSource
	LastSeenAt         time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Owned by work_items_upsert.go (A3):
//
//   UpsertWorkItem(ctx, item, source, seenAt) error
//   TombstoneBySource(ctx, source, before) (archivedIDs []string, err error)
//   CascadeArchiveChildren(ctx, parentID) error
//
// Owned by work_items_query.go (A4):
//
//   ListSpawnable(ctx) ([]WorkItem, error)
//   CycleCheck(ctx, item) error
//   ListByParent(ctx, parentID) ([]WorkItem, error)
//   GetWorkItem(ctx, id) (WorkItem, error)

// ErrWorkItemNotFound is the sentinel GetWorkItem returns when id is
// absent. Stub lives here so A3 tests compile against the real type
// before A4 replaces the body.
var ErrWorkItemNotFound = errors.New("state: work_item not found")

// GetWorkItem stub — A4 replaces with the real query body. Allows
// A3 tests in work_items_upsert_test.go to compile and run round-
// trip assertions before A4 lands.
func (d *DB) GetWorkItem(ctx context.Context, id string) (WorkItem, error) {
	row := d.sql.QueryRowContext(ctx, `
		SELECT id, kind, title, lane, status,
		       COALESCE(parent_program_id, ''), depends_on_features,
		       acceptance_json, source, last_seen_at, created_at, updated_at
		FROM work_items WHERE id = ?`, id)
	var w WorkItem
	var depsJSON string
	var lastSeen, created, updated int64
	if err := row.Scan(&w.ID, &w.Kind, &w.Title, &w.Lane, &w.Status,
		&w.ParentProgramID, &depsJSON, &w.AcceptanceJSON, &w.Source,
		&lastSeen, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WorkItem{}, ErrWorkItemNotFound
		}
		return WorkItem{}, err
	}
	if depsJSON != "" {
		_ = json.Unmarshal([]byte(depsJSON), &w.DependsOnFeatures)
	}
	w.LastSeenAt = time.Unix(lastSeen, 0).UTC()
	w.CreatedAt = time.Unix(created, 0).UTC()
	w.UpdatedAt = time.Unix(updated, 0).UTC()
	return w, nil
}
```

Add imports: `"database/sql"`, `"encoding/json"`, `"errors"`.

Commit this stub immediately so A4 can start in parallel:

```bash
git add internal/orchestrator/state/work_items.go
git commit -m "feat(state): work_items types + API stub (Wave 2 seam)

Empty stub freezes WorkItem type + method signature surface so
work_items_upsert.go (A3) and work_items_query.go (A4) can land
in parallel without import-block races."
```

- [ ] **Step 3.2: Write failing tests for upsert + tombstone + cascade**

Create `internal/orchestrator/state/work_items_upsert_test.go`:

```go
package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newWorkItemsTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), DSN(filepath.Join(t.TempDir(), "wi.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestUpsertWorkItem_RoundTrip(t *testing.T) {
	db := newWorkItemsTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

	item := WorkItem{
		ID: "PROG-1", Kind: KindProgram, Title: "test prog",
		Lane: "server", Status: WorkStatusPlanned,
	}
	if err := db.UpsertWorkItem(ctx, item, SourceAdapter, now); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}

	got, err := db.GetWorkItem(ctx, "PROG-1")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if got.Title != "test prog" || got.Source != SourceAdapter {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestTombstoneBySource_SourceScoped(t *testing.T) {
	db := newWorkItemsTestDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(1 * time.Minute)

	adapter := WorkItem{ID: "ADAPT-1", Kind: KindFeature, Title: "a", Lane: "server", Status: WorkStatusPlanned}
	brief := WorkItem{ID: "BRIEF-1", Kind: KindFeature, Title: "b", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, adapter, SourceAdapter, t0); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkItem(ctx, brief, SourceBrief, t0); err != nil {
		t.Fatal(err)
	}

	// BriefLoader tombstones brief sources before t1 — but no brief
	// rows have been re-upserted at t1, so BRIEF-1 should archive.
	archived, err := db.TombstoneBySource(ctx, string(SourceBrief), t1)
	if err != nil {
		t.Fatalf("TombstoneBySource: %v", err)
	}
	if len(archived) != 1 || archived[0] != "BRIEF-1" {
		t.Fatalf("archived=%v want [BRIEF-1]", archived)
	}

	// ADAPT-1 still planned because TombstoneBySource is per-source.
	got, _ := db.GetWorkItem(ctx, "ADAPT-1")
	if got.Status != WorkStatusPlanned {
		t.Fatalf("ADAPT-1.status=%s want planned", got.Status)
	}
}

func TestCascadeArchiveChildren_DoesNotKillRunningAgent(t *testing.T) {
	db := newWorkItemsTestDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

	parent := WorkItem{ID: "PROG-1", Kind: KindProgram, Title: "p", Lane: "server", Status: WorkStatusPlanned}
	child := WorkItem{ID: "F-1", Kind: KindFeature, Title: "c", Lane: "server", Status: WorkStatusPlanned, ParentProgramID: "PROG-1"}
	if err := db.UpsertWorkItem(ctx, parent, SourceAdapter, t0); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkItem(ctx, child, SourceBrief, t0); err != nil {
		t.Fatal(err)
	}

	// Simulate child being mid-run by setting status=running.
	if _, err := db.sql.ExecContext(ctx,
		`UPDATE work_items SET status='running' WHERE id=?`, "F-1"); err != nil {
		t.Fatal(err)
	}

	if err := db.CascadeArchiveChildren(ctx, "PROG-1"); err != nil {
		t.Fatalf("CascadeArchiveChildren: %v", err)
	}

	// Per spec §2.4 cascade-soft: the row status flips to archived,
	// but no agent rows are touched. Spec line 24: "running children
	// finish naturally." Here we verify only the row.
	got, _ := db.GetWorkItem(ctx, "F-1")
	if got.Status != WorkStatusArchived {
		t.Fatalf("F-1.status=%s want archived", got.Status)
	}
}
```

- [ ] **Step 3.3: Run test to verify it fails**

Run: `go test ./internal/orchestrator/state/ -run "TestUpsertWorkItem|TestTombstone|TestCascade"`
Expected: FAIL — `undefined: UpsertWorkItem`, etc.

- [ ] **Step 3.4: Implement upsert / tombstone / cascade**

Create `internal/orchestrator/state/work_items_upsert.go`:

```go
package state

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// UpsertWorkItem inserts a new work_items row or updates an existing
// one (matched by id). last_seen_at and updated_at are set to seenAt;
// created_at is preserved on update.
//
// per spec §2.2 — depends_on_features and acceptance_json are stored
// as JSON text. Empty slice -> "[]". AcceptanceJSON must be valid
// JSON; an empty string is normalized to "[]".
func (d *DB) UpsertWorkItem(ctx context.Context, item WorkItem, source WorkItemSource, seenAt time.Time) error {
	depsJSON, err := encodeDeps(item.DependsOnFeatures)
	if err != nil {
		return fmt.Errorf("state: encode deps: %w", err)
	}
	accept := item.AcceptanceJSON
	if accept == "" {
		accept = "[]"
	}
	now := seenAt.UTC().Unix()

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("state: begin upsert tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var existingCreated int64
	row := tx.QueryRowContext(ctx, `SELECT created_at FROM work_items WHERE id = ?`, item.ID)
	switch err := row.Scan(&existingCreated); err {
	case nil:
		// Update path.
		if _, err := tx.ExecContext(ctx, `
			UPDATE work_items SET
				kind = ?, title = ?, lane = ?, status = ?,
				parent_program_id = ?, depends_on_features = ?,
				acceptance_json = ?, source = ?, last_seen_at = ?,
				updated_at = ?
			WHERE id = ?`,
			string(item.Kind), item.Title, item.Lane, string(item.Status),
			nullable(item.ParentProgramID), depsJSON, accept,
			string(source), now, now, item.ID,
		); err != nil {
			return fmt.Errorf("state: update work_item: %w", err)
		}
	default:
		// Insert path. Treat sql.ErrNoRows AND missing-table errors
		// the same — assume new row.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO work_items (
				id, kind, title, lane, status,
				parent_program_id, depends_on_features, acceptance_json,
				source, last_seen_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.ID, string(item.Kind), item.Title, item.Lane, string(item.Status),
			nullable(item.ParentProgramID), depsJSON, accept,
			string(source), now, now, now,
		); err != nil {
			return fmt.Errorf("state: insert work_item: %w", err)
		}
	}
	return tx.Commit()
}

// TombstoneBySource archives every row whose source matches and
// last_seen_at < before. Returns the list of archived IDs in the
// order sqlite returns them. Per-source so AdapterSync and
// BriefLoader cannot tombstone each other's rows.
func (d *DB) TombstoneBySource(ctx context.Context, source string, before time.Time) ([]string, error) {
	cutoff := before.UTC().Unix()
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("state: begin tombstone tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM work_items
		WHERE source = ? AND last_seen_at < ? AND status != ?`,
		source, cutoff, string(WorkStatusArchived))
	if err != nil {
		return nil, fmt.Errorf("state: select tombstone candidates: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("state: scan tombstone id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			UPDATE work_items SET status = ?, updated_at = ?
			WHERE id = ?`,
			string(WorkStatusArchived), cutoff, id,
		); err != nil {
			return nil, fmt.Errorf("state: tombstone %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("state: commit tombstone: %w", err)
	}
	return ids, nil
}

// CascadeArchiveChildren marks every work_items row whose
// parent_program_id matches as archived. Cascade-SOFT (spec §2.4):
// the agents table is not touched, so any in-flight agent continues
// to its natural terminal state.
func (d *DB) CascadeArchiveChildren(ctx context.Context, parentID string) error {
	now := time.Now().UTC().Unix()
	_, err := d.sql.ExecContext(ctx, `
		UPDATE work_items SET status = ?, updated_at = ?
		WHERE parent_program_id = ? AND status != ?`,
		string(WorkStatusArchived), now, parentID, string(WorkStatusArchived))
	if err != nil {
		return fmt.Errorf("state: cascade archive %s: %w", parentID, err)
	}
	return nil
}

func encodeDeps(deps []string) (string, error) {
	if len(deps) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(deps)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
```

- [ ] **Step 3.5: Run test to verify it passes**

Run: `go test ./internal/orchestrator/state/ -run "TestUpsertWorkItem|TestTombstone|TestCascade"`
Expected: PASS — all three tests green. **Note**: `GetWorkItem` is implemented by Task 4 (A4); the test uses it, so Task 3 cannot run its tests until Task 4's `GetWorkItem` is in place. **Hand-off contract**: A4 commits `GetWorkItem` stub first (returns `WorkItem{}, nil` for any ID) so A3 tests pass before A4 lands the real query body. Alternative ordering: A4 commits `GetWorkItem` first, then A3 lands. Either way, Wave 2 ends with all three round-trip tests green.

- [ ] **Step 3.6: Commit Task 3**

```bash
git add internal/orchestrator/state/work_items_upsert.go \
        internal/orchestrator/state/work_items_upsert_test.go
git commit -m "feat(state): work_items UpsertWorkItem, TombstoneBySource, CascadeArchiveChildren

Universal-queue writers (AdapterSync, BriefLoader) call UpsertWorkItem
with their source label and pollStartedAt. TombstoneBySource is the
per-source archive sweep that runs at the end of each sync, scoped so
the two writers cannot stomp on each other. CascadeArchiveChildren is
cascade-SOFT: only flips work_items.status, leaving agents alone."
```

---

## Task 4 (A4): work_items API — query side (ListSpawnable, CycleCheck, ListByParent, GetWorkItem)

Wave 2, parallel with A3 after the stub commits.

**Files:**
- Create: `internal/orchestrator/state/work_items_query.go`
- Create: `internal/orchestrator/state/work_items_query_test.go`

- [ ] **Step 4.1: Write failing tests**

Create `internal/orchestrator/state/work_items_query_test.go`:

```go
package state

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

func newQueryTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), DSN(filepath.Join(t.TempDir(), "q.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestGetWorkItem_NotFound(t *testing.T) {
	db := newQueryTestDB(t)
	_, err := db.GetWorkItem(context.Background(), "missing")
	if !errors.Is(err, ErrWorkItemNotFound) {
		t.Fatalf("err=%v want ErrWorkItemNotFound", err)
	}
}

func TestListSpawnable_NoDeps_ReturnsAllPlanned(t *testing.T) {
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Now()
	for _, id := range []string{"F-1", "F-2", "F-3"} {
		w := WorkItem{ID: id, Kind: KindFeature, Title: id, Lane: "server", Status: WorkStatusPlanned}
		if err := db.UpsertWorkItem(ctx, w, SourceBrief, now); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListSpawnable(ctx)
	if err != nil {
		t.Fatalf("ListSpawnable: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3 (got IDs %v)", len(got), idsOf(got))
	}
}

func TestListSpawnable_DepBlockedUntilMerged(t *testing.T) {
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Now()
	c1 := WorkItem{ID: "F-1", Kind: KindFeature, Title: "c1", Lane: "server", Status: WorkStatusPlanned}
	c2 := WorkItem{ID: "F-2", Kind: KindFeature, Title: "c2", Lane: "server", Status: WorkStatusPlanned,
		DependsOnFeatures: []string{"F-1"}}
	if err := db.UpsertWorkItem(ctx, c1, SourceBrief, now); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkItem(ctx, c2, SourceBrief, now); err != nil {
		t.Fatal(err)
	}

	got, _ := db.ListSpawnable(ctx)
	if len(got) != 1 || got[0].ID != "F-1" {
		t.Fatalf("first round: got %v want [F-1]", idsOf(got))
	}

	// Flip F-1 to merged.
	if _, err := db.sql.ExecContext(ctx,
		`UPDATE work_items SET status='merged' WHERE id=?`, "F-1"); err != nil {
		t.Fatal(err)
	}

	got, _ = db.ListSpawnable(ctx)
	if len(got) != 1 || got[0].ID != "F-2" {
		t.Fatalf("after merge: got %v want [F-2]", idsOf(got))
	}
}

func TestListSpawnable_ExcludesAlreadyReserved(t *testing.T) {
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Now()
	w := WorkItem{ID: "F-1", Kind: KindFeature, Title: "x", Lane: "server", Status: WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, w, SourceBrief, now); err != nil {
		t.Fatal(err)
	}
	// Simulate a reservation already in the agents table.
	if _, err := db.UpsertPending(ctx, "F-1", "server"); err != nil {
		t.Fatal(err)
	}
	got, _ := db.ListSpawnable(ctx)
	if len(got) != 0 {
		t.Fatalf("got %v want []", idsOf(got))
	}
}

func TestCycleCheck_RejectsCycle(t *testing.T) {
	db := newQueryTestDB(t)
	ctx := context.Background()
	now := time.Now()

	a := WorkItem{ID: "F-A", Kind: KindFeature, Title: "a", Lane: "server",
		Status: WorkStatusPlanned, DependsOnFeatures: []string{"F-B"}}
	if err := db.UpsertWorkItem(ctx, a, SourceBrief, now); err != nil {
		t.Fatal(err)
	}

	b := WorkItem{ID: "F-B", Kind: KindFeature, Title: "b", Lane: "server",
		Status: WorkStatusPlanned, DependsOnFeatures: []string{"F-A"}}
	err := db.CycleCheck(ctx, b)
	if !errors.Is(err, orchestrator.ErrCycleDetected) {
		t.Fatalf("err=%v want ErrCycleDetected", err)
	}
}

func idsOf(items []WorkItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}
```

- [ ] **Step 4.2: Run test to verify it fails**

Run: `go test ./internal/orchestrator/state/ -run "TestGetWorkItem|TestListSpawnable|TestCycleCheck"`
Expected: FAIL — `undefined: GetWorkItem`, `undefined: ListSpawnable`, `undefined: CycleCheck`, `undefined: ErrWorkItemNotFound`.

- [ ] **Step 4.3: Implement query side**

Create `internal/orchestrator/state/work_items_query.go` — note that `GetWorkItem` + `ErrWorkItemNotFound` are already defined in `work_items.go` from Task 3 (the stub-first handoff). This file adds **only** `ListByParent`, `ListSpawnable`, and `CycleCheck`:

```go
package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

// ListByParent returns every work_items row whose parent_program_id
// equals parentID, in id order.
func (d *DB) ListByParent(ctx context.Context, parentID string) ([]WorkItem, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT id, kind, title, lane, status,
		       COALESCE(parent_program_id, ''), depends_on_features,
		       acceptance_json, source, last_seen_at, created_at, updated_at
		FROM work_items WHERE parent_program_id = ? ORDER BY id`, parentID)
	if err != nil {
		return nil, fmt.Errorf("state: list by parent: %w", err)
	}
	defer rows.Close()
	return scanWorkItems(rows)
}

// ListSpawnable returns every work_items row whose status is
// 'planned', that has no entry in agents yet, and whose
// depends_on_features are either empty or all already 'merged'.
// per spec §2.8 — the SELECT here is the materialization-eliminator:
// scheduler.Tick consumes the rows directly into the reservation tx.
func (d *DB) ListSpawnable(ctx context.Context) ([]WorkItem, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT w.id, w.kind, w.title, w.lane, w.status,
		       COALESCE(w.parent_program_id, ''), w.depends_on_features,
		       w.acceptance_json, w.source, w.last_seen_at,
		       w.created_at, w.updated_at
		FROM work_items w
		LEFT JOIN agents a ON w.id = a.work_item_id
		WHERE w.status = 'planned'
		  AND a.id IS NULL
		  AND (
		    w.depends_on_features = '[]'
		    OR NOT EXISTS (
		      SELECT 1 FROM json_each(w.depends_on_features)
		      WHERE value NOT IN (SELECT id FROM work_items WHERE status = 'merged')
		    )
		  )
		ORDER BY w.id`)
	if err != nil {
		return nil, fmt.Errorf("state: list spawnable: %w", err)
	}
	defer rows.Close()
	return scanWorkItems(rows)
}

// CycleCheck verifies that inserting (or updating) candidate would
// not introduce a dependency cycle. Walks the existing graph + the
// candidate's depends_on_features and looks for a back-edge to
// candidate.ID. Returns ErrCycleDetected wrapped if a cycle is
// found.
func (d *DB) CycleCheck(ctx context.Context, candidate WorkItem) error {
	// Build adjacency: id -> []depends_on_features. Start with
	// existing rows, then overlay candidate.
	rows, err := d.sql.QueryContext(ctx, `SELECT id, depends_on_features FROM work_items`)
	if err != nil {
		return fmt.Errorf("state: cycle scan: %w", err)
	}
	adj := map[string][]string{}
	for rows.Next() {
		var id, depsJSON string
		if err := rows.Scan(&id, &depsJSON); err != nil {
			rows.Close()
			return fmt.Errorf("state: cycle scan row: %w", err)
		}
		var deps []string
		if err := json.Unmarshal([]byte(depsJSON), &deps); err != nil {
			rows.Close()
			return fmt.Errorf("state: cycle decode deps for %s: %w", id, err)
		}
		adj[id] = deps
	}
	rows.Close()
	adj[candidate.ID] = candidate.DependsOnFeatures

	if reachable(adj, candidate.ID, candidate.ID) {
		return fmt.Errorf("%w: %s", orchestrator.ErrCycleDetected, candidate.ID)
	}
	return nil
}

func reachable(adj map[string][]string, start, target string) bool {
	visited := map[string]bool{}
	var dfs func(node string, depth int) bool
	dfs = func(node string, depth int) bool {
		if depth > 0 && node == target {
			return true
		}
		if visited[node] {
			return false
		}
		visited[node] = true
		for _, next := range adj[node] {
			if dfs(next, depth+1) {
				return true
			}
		}
		return false
	}
	return dfs(start, 0)
}

func scanWorkItems(rows *sql.Rows) ([]WorkItem, error) {
	var out []WorkItem
	for rows.Next() {
		var w WorkItem
		var depsJSON string
		var lastSeen, created, updated int64
		if err := rows.Scan(&w.ID, &w.Kind, &w.Title, &w.Lane, &w.Status,
			&w.ParentProgramID, &depsJSON, &w.AcceptanceJSON, &w.Source,
			&lastSeen, &created, &updated); err != nil {
			return nil, fmt.Errorf("state: scan work_items: %w", err)
		}
		if err := json.Unmarshal([]byte(depsJSON), &w.DependsOnFeatures); err != nil {
			return nil, fmt.Errorf("state: decode deps for %s: %w", w.ID, err)
		}
		w.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		w.CreatedAt = time.Unix(created, 0).UTC()
		w.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, w)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4.4: Run test to verify it passes**

Run: `go test ./internal/orchestrator/state/...`
Expected: PASS — all work_items tests (A3 + A4) green; existing state tests still green.

- [ ] **Step 4.5: Add DAG property test (rubric Grade-A)**

`pgregory.net/rapid` was pre-staged by Task 1; no `go get` here.

Create `internal/orchestrator/state/work_items_property_test.go`:

```go
package state

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"pgregory.net/rapid"
)

// TestListSpawnable_PropertyTopologicalReady (Grade-A DoD per spec §6):
// for any DAG of n≤8 nodes, ListSpawnable returns exactly the set of
// nodes whose dependencies are all in status='merged'. We generate
// random DAGs, mark a random subset as merged, then assert the
// returned set equals the topological-ready subset of planned nodes.
func TestListSpawnable_PropertyTopologicalReady(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 8).Draw(rt, "n")
		nodes := make([]string, n)
		for i := range nodes {
			nodes[i] = "F-" + string(rune('A'+i))
		}
		// Generate a DAG by allowing edges only from higher index to
		// lower (guaranteed acyclic).
		deps := make(map[string][]string, n)
		for i := 0; i < n; i++ {
			possible := nodes[:i]
			pick := rapid.SliceOfN(rapid.SampledFrom(possible), 0, len(possible)).Draw(rt, "deps_"+nodes[i])
			deps[nodes[i]] = dedupe(pick)
		}
		// Mark a random subset of nodes as 'merged'.
		merged := map[string]bool{}
		for _, n := range nodes {
			if rapid.Bool().Draw(rt, "merged_"+n) {
				merged[n] = true
			}
		}

		dbPath := filepath.Join(rt.TempDir(), "p.db")
		db, err := Open(context.Background(), DSN(dbPath))
		if err != nil {
			rt.Fatalf("Open: %v", err)
		}
		defer db.Close()

		now := time.Now()
		for _, id := range nodes {
			status := WorkStatusPlanned
			if merged[id] {
				status = WorkStatusMerged
			}
			w := WorkItem{ID: id, Kind: KindFeature, Title: id,
				Lane: "server", Status: status, DependsOnFeatures: deps[id]}
			if err := db.UpsertWorkItem(context.Background(), w, SourceBrief, now); err != nil {
				rt.Fatalf("upsert %s: %v", id, err)
			}
		}

		// Compute expected: planned nodes whose every dep is merged.
		want := map[string]bool{}
		for _, id := range nodes {
			if merged[id] {
				continue
			}
			ok := true
			for _, d := range deps[id] {
				if !merged[d] {
					ok = false
					break
				}
			}
			if ok {
				want[id] = true
			}
		}

		got, err := db.ListSpawnable(context.Background())
		if err != nil {
			rt.Fatalf("ListSpawnable: %v", err)
		}
		gotSet := map[string]bool{}
		for _, w := range got {
			gotSet[w.ID] = true
		}

		if !setsEqual(want, gotSet) {
			rt.Fatalf("deps=%v merged=%v want=%v got=%v",
				deps, mergedKeys(merged), keys(want), keys(gotSet))
		}
	})
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func setsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func mergedKeys(m map[string]bool) []string { return keys(m) }
```

Run: `go test ./internal/orchestrator/state/ -run TestListSpawnable_PropertyTopologicalReady -rapid.checks=200`
Expected: PASS — `rapid` exercises 200 random DAGs.

- [ ] **Step 4.6: Commit Task 4**

```bash
git add internal/orchestrator/state/work_items_query.go \
        internal/orchestrator/state/work_items_query_test.go \
        internal/orchestrator/state/work_items_property_test.go
git commit -m "feat(state): work_items query side — ListSpawnable join, CycleCheck, GetWorkItem + DAG property test

ListSpawnable is the materialization-eliminator query (spec §2.8):
work_items LEFT JOIN agents filters planned-no-agent-deps-satisfied
rows in one SQL pass. scheduler.Tick consumes the rows directly
into reservation tx (Task 8) — no orphan-row class.

CycleCheck runs a candidate-aware DFS to fail upserts that would
introduce A→B→A patterns at the work_items level.

Property test (spec §6 Grade-A): 200 random DAGs n≤8 via rapid;
ListSpawnable returns exactly the topological-ready set."
```

---

## Task 5 (A5): AdapterSync — mirror SpecAdapter into work_items

Wave 3, parallel with Tasks 6 and 7.

**Files:**
- Create: `internal/orchestrator/adaptersync/adaptersync.go`
- Create: `internal/orchestrator/adaptersync/adaptersync_test.go`

- [ ] **Step 5.1: Write failing test**

Create `internal/orchestrator/adaptersync/adaptersync_test.go`:

```go
package adaptersync

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/clock"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

type stubAdapter struct {
	items []schemas.WorkItem
}

func (s *stubAdapter) List(context.Context) ([]schemas.WorkItem, error) { return s.items, nil }
func (s *stubAdapter) Get(context.Context, schemas.WorkItemID) (schemas.WorkItem, error) {
	return schemas.WorkItem{}, schemas.ErrNotFound
}
func (s *stubAdapter) UpdateStatus(context.Context, schemas.WorkItemID, schemas.Status, string) error {
	return nil
}
func (s *stubAdapter) Capabilities() schemas.AdapterCapabilities { return schemas.AdapterCapabilities{} }

func newSyncTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "s.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSync_UpsertsAdapterItems(t *testing.T) {
	db := newSyncTestDB(t)
	adapter := &stubAdapter{items: []schemas.WorkItem{
		{ID: "ITEM-1", Kind: schemas.WorkItemKindFeature, Title: "a", Lane: "server", Status: schemas.StatusPlanned},
		{ID: "ITEM-2", Kind: schemas.WorkItemKindFeature, Title: "b", Lane: "server", Status: schemas.StatusPlanned},
	}}
	clk := clock.Fake(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	syncer := New(adapter, db, clk)

	if err := syncer.Sync(context.Background(), clk.Now()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	got, _ := db.GetWorkItem(context.Background(), "ITEM-1")
	if got.Source != state.SourceAdapter {
		t.Fatalf("ITEM-1.source=%s want adapter", got.Source)
	}
}

func TestSync_TombstonesMissingOnSecondTick(t *testing.T) {
	db := newSyncTestDB(t)
	adapter := &stubAdapter{items: []schemas.WorkItem{
		{ID: "ITEM-1", Kind: schemas.WorkItemKindFeature, Title: "a", Lane: "server", Status: schemas.StatusPlanned},
		{ID: "ITEM-2", Kind: schemas.WorkItemKindFeature, Title: "b", Lane: "server", Status: schemas.StatusPlanned},
	}}
	clk := clock.Fake(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	syncer := New(adapter, db, clk)

	if err := syncer.Sync(context.Background(), clk.Now()); err != nil {
		t.Fatal(err)
	}

	// Second tick: ITEM-2 removed.
	clk.Advance(1 * time.Second)
	adapter.items = adapter.items[:1]
	if err := syncer.Sync(context.Background(), clk.Now()); err != nil {
		t.Fatal(err)
	}

	got, _ := db.GetWorkItem(context.Background(), "ITEM-2")
	if got.Status != state.WorkStatusArchived {
		t.Fatalf("ITEM-2.status=%s want archived", got.Status)
	}
}
```

- [ ] **Step 5.2: Run test to verify it fails**

Run: `go test ./internal/orchestrator/adaptersync/...`
Expected: FAIL — `undefined: New`.

- [ ] **Step 5.3: Implement AdapterSync**

Create `internal/orchestrator/adaptersync/adaptersync.go`:

```go
// Package adaptersync mirrors the read-only SpecAdapter into the
// state.work_items universal-queue table. Runs as step 3 of
// orchestrator.PollOnce (per spec §2.9). Tombstones rows the
// adapter no longer returns (source='adapter', last_seen_at <
// pollStartedAt) — see CascadeArchiveChildren for the program ->
// child fan-out.
package adaptersync

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/clock"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Syncer ties an adapter, the state DB, and a Clock together.
type Syncer struct {
	adapter SpecAdapter
	db      *state.DB
	clk     clock.Clock
}

// SpecAdapter mirrors the orchestrator's existing read surface.
// Keep the interface local so adaptersync doesn't import the
// orchestrator package (would create a cycle once Wave 5 wires
// PollOnce -> adaptersync.Sync).
type SpecAdapter interface {
	List(ctx context.Context) ([]schemas.WorkItem, error)
}

// New constructs a Syncer. The clock argument is retained for
// future use (the public Sync method accepts pollStartedAt
// directly today).
func New(adapter SpecAdapter, db *state.DB, clk clock.Clock) *Syncer {
	return &Syncer{adapter: adapter, db: db, clk: clk}
}

// Sync calls adapter.List, upserts every returned item with
// source=adapter and last_seen_at=pollStartedAt, then tombstones
// adapter-source rows whose last_seen_at is older than
// pollStartedAt. Cascade-archives children of archived programs.
//
// per spec §3 fail-fast: any error returns immediately, leaving
// next tick to retry.
func (s *Syncer) Sync(ctx context.Context, pollStartedAt time.Time) error {
	items, err := s.adapter.List(ctx)
	if err != nil {
		return fmt.Errorf("adaptersync: adapter list: %w", err)
	}

	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		// schemas.WorkItem.Kind, .Status, .Lane are defined string
		// types in contracts/schemas; state.WorkItemKind, etc. are
		// defined identically (string-underlying enums). Direct cast
		// is safe because the values "feature"/"program",
		// "planned"/"running"/..., and the lane names are
		// byte-equal across both packages. If a future contract
		// renames a value, fix the cast site by adding a switch.
		wi := state.WorkItem{
			ID:     string(it.ID),
			Kind:   state.WorkItemKind(string(it.Kind)),
			Title:  it.Title,
			Lane:   string(it.Lane),
			Status: state.WorkItemStatus(string(it.Status)),
		}
		if err := s.db.UpsertWorkItem(ctx, wi, state.SourceAdapter, pollStartedAt); err != nil {
			return fmt.Errorf("adaptersync: upsert %s: %w", it.ID, err)
		}
	}

	archived, err := s.db.TombstoneBySource(ctx, string(state.SourceAdapter), pollStartedAt)
	if err != nil {
		return fmt.Errorf("adaptersync: tombstone: %w", err)
	}
	for _, id := range archived {
		slog.Warn("adapter.tombstoned", "id", id, "at", pollStartedAt)
		// Cascade-archive children of an archived program. Children
		// of a feature have parent_program_id IS NULL so this is a
		// no-op for non-programs.
		if err := s.db.CascadeArchiveChildren(ctx, id); err != nil {
			return fmt.Errorf("adaptersync: cascade %s: %w", id, err)
		}
	}
	return nil
}
```

- [ ] **Step 5.4: Run test to verify it passes**

Run: `go test ./internal/orchestrator/adaptersync/...`
Expected: PASS.

- [ ] **Step 5.5: Commit Task 5**

```bash
git add internal/orchestrator/adaptersync/
git commit -m "feat(adaptersync): mirror SpecAdapter into state.work_items

Syncer upserts every adapter-returned item with source=adapter +
last_seen_at=pollStartedAt; rows whose last_seen_at is older than
the current poll are tombstoned and any child programs cascade-archived.
Emits slog.Warn 'adapter.tombstoned' per Wave 6 DoD checklist."
```

---

## Task 6 (A6): BriefLoader + LoadAndVerifyBrief

Wave 3, parallel with Tasks 5 and 7.

**Files:**
- Create: `internal/program/brief_loader.go`
- Create: `internal/program/brief_loader_test.go`

- [ ] **Step 6.1: Write failing tests**

Create `internal/program/brief_loader_test.go`:

```go
package program

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/clock"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func newBriefTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "bl.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustSignedBrief(t *testing.T, key []byte) (*ProgramBrief, []byte) {
	t.Helper()
	b := &ProgramBrief{
		SchemaVersion:    1,
		ProgramID:        "m-1234567890ab",
		ParentWorkItemID: "PROG-1",
		ParentCriteria: []PlanCriterion{
			{ID: "c1", Text: "add foo"},
			{ID: "c2", Text: "add bar"},
			{ID: "c3", Text: "add baz"},
		},
		PlannerModelID: "claude-test",
		Features: []PlannedFeature{
			{ID: "F-1", Title: "foo", Fulfills: []string{"c1"}},
			{ID: "F-2", Title: "bar", Fulfills: []string{"c2"}},
			{ID: "F-3", Title: "baz", Fulfills: []string{"c3"}},
		},
		ProducedAt: time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
	}
	signed, err := b.Sign(key, "key-1")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return signed, raw
}

func TestLoadAndVerifyBrief_Valid(t *testing.T) {
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	got, err := LoadAndVerifyBrief(fsys, "PROG-1.json", map[string][]byte{"key-1": key})
	if err != nil {
		t.Fatalf("LoadAndVerifyBrief: %v", err)
	}
	if got.ParentWorkItemID != "PROG-1" {
		t.Fatalf("ParentWorkItemID=%q want PROG-1", got.ParentWorkItemID)
	}
}

func TestLoadAndVerifyBrief_TamperedHMAC(t *testing.T) {
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	// Flip a single byte in the body to break the HMAC.
	tampered := append([]byte{}, raw...)
	for i, b := range tampered {
		if b == 'f' { // first 'f' in "foo" feature title — guaranteed present.
			tampered[i] = 'x'
			break
		}
	}
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: tampered}}

	_, err := LoadAndVerifyBrief(fsys, "PROG-1.json", map[string][]byte{"key-1": key})
	if !errors.Is(err, orchestrator.ErrHMACInvalid) {
		t.Fatalf("err=%v want ErrHMACInvalid", err)
	}
}

func TestBriefLoaderSync_UpsertsThreeChildren(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	fsys := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	// Seed the parent program row so cascade lookups would work.
	parent := state.WorkItem{ID: "PROG-1", Kind: state.KindProgram, Title: "p",
		Lane: "server", Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItem(context.Background(), parent, state.SourceAdapter, time.Now()); err != nil {
		t.Fatal(err)
	}

	clk := clock.Fake(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	loader := NewBriefLoader(fsys, db, clk, map[string][]byte{"key-1": key})

	if err := loader.Sync(context.Background(), clk.Now()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	children, err := db.ListByParent(context.Background(), "PROG-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 3 {
		t.Fatalf("children=%d want 3", len(children))
	}
	for _, c := range children {
		if c.Source != state.SourceBrief {
			t.Fatalf("child %s source=%s want brief", c.ID, c.Source)
		}
	}
}

func TestBriefLoaderSync_SkipsTmpFiles(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	fsys := fstest.MapFS{"PROG-1.json.tmp": &fstest.MapFile{Data: raw}}

	clk := clock.Fake(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	loader := NewBriefLoader(fsys, db, clk, map[string][]byte{"key-1": key})
	if err := loader.Sync(context.Background(), clk.Now()); err != nil {
		t.Fatal(err)
	}
	children, _ := db.ListByParent(context.Background(), "PROG-1")
	if len(children) != 0 {
		t.Fatalf("children=%d want 0 (.tmp must be skipped)", len(children))
	}
}

func TestBriefLoaderSync_TombstonesMissingBrief(t *testing.T) {
	db := newBriefTestDB(t)
	key := []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")
	_, raw := mustSignedBrief(t, key)
	files := fstest.MapFS{"PROG-1.json": &fstest.MapFile{Data: raw}}

	clk := clock.Fake(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	loader := NewBriefLoader(fs.FS(files), db, clk, map[string][]byte{"key-1": key})
	if err := loader.Sync(context.Background(), clk.Now()); err != nil {
		t.Fatal(err)
	}

	// Next tick — brief gone.
	clk.Advance(1 * time.Second)
	delete(files, "PROG-1.json")
	if err := loader.Sync(context.Background(), clk.Now()); err != nil {
		t.Fatal(err)
	}

	children, _ := db.ListByParent(context.Background(), "PROG-1")
	if len(children) != 3 {
		t.Fatalf("children=%d want 3 rows (tombstoned but preserved)", len(children))
	}
	for _, c := range children {
		if c.Status != state.WorkStatusArchived {
			t.Fatalf("child %s status=%s want archived", c.ID, c.Status)
		}
	}
}

// Avoid unused import in some test combinations.
var _ schemas.WorkItem
```

- [ ] **Step 6.2: Run test to verify it fails**

Run: `go test ./internal/program/ -run "TestLoadAndVerifyBrief|TestBriefLoader"`
Expected: FAIL — `undefined: LoadAndVerifyBrief`, `undefined: NewBriefLoader`.

- [ ] **Step 6.3: Implement BriefLoader + LoadAndVerifyBrief**

Create `internal/program/brief_loader.go`:

```go
// Package program — BriefLoader scans .regatta/programs/*.json,
// verifies each brief, and upserts child work_items rows.
//
// per spec §2.4 + §3 sign-then-persist: a brief that fails Validate
// or VerifySignature emits slog.Warn("brief.rejected") and no
// child rows touch state. Rejections never enter sqlite; audit
// trail lives in logs (RFC-0001 §audit deferral to MVP-3+).
package program

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator"
	"github.com/trilamsr/regatta/internal/orchestrator/clock"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// LoadAndVerifyBrief reads path from fsys, unmarshals into
// ProgramBrief, runs ProgramBrief.Validate, then VerifySignature
// under keyring. Returns ErrHMACInvalid (wrapped) when the
// signature does not check out under any key.
func LoadAndVerifyBrief(fsys fs.FS, path string, keyring map[string][]byte) (*ProgramBrief, error) {
	if len(keyring) == 0 {
		return nil, fmt.Errorf("program: keyring required to verify briefs")
	}
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("program: read brief: %w", err)
	}
	var brief ProgramBrief
	if err := json.Unmarshal(raw, &brief); err != nil {
		return nil, fmt.Errorf("program: parse brief: %w", err)
	}
	if err := brief.Validate(); err != nil {
		return nil, fmt.Errorf("program: validate brief: %w", err)
	}
	if err := brief.VerifySignature(keyring); err != nil {
		return nil, fmt.Errorf("%w: %v", orchestrator.ErrHMACInvalid, err)
	}
	return &brief, nil
}

// BriefLoader is the recurring sync. Construct once at orchestrator
// boot; Sync once per PollOnce tick.
type BriefLoader struct {
	fsys    fs.FS
	db      *state.DB
	clk     clock.Clock
	keyring map[string][]byte
}

// NewBriefLoader constructs a BriefLoader. fsys is typically
// os.DirFS(filepath.Join(repoRoot, ".regatta", "programs")) in
// production and fstest.MapFS in tests.
func NewBriefLoader(fsys fs.FS, db *state.DB, clk clock.Clock, keyring map[string][]byte) *BriefLoader {
	return &BriefLoader{fsys: fsys, db: db, clk: clk, keyring: keyring}
}

// Sync globs *.json in fsys (skipping *.tmp), verifies each, and
// upserts child WorkItems for the brief's features. Rows whose
// last_seen_at predates pollStartedAt and source=brief are
// tombstoned at the end of the loop.
func (b *BriefLoader) Sync(ctx context.Context, pollStartedAt time.Time) error {
	entries, err := fs.Glob(b.fsys, "*.json")
	if err != nil {
		return fmt.Errorf("brief_loader: glob: %w", err)
	}

	for _, path := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.HasSuffix(path, ".tmp") {
			continue
		}
		brief, err := LoadAndVerifyBrief(b.fsys, path, b.keyring)
		if err != nil {
			slog.Warn("brief.rejected", "path", path, "reason", err.Error())
			continue
		}
		acceptanceByFulfilled := map[string]string{}
		for _, c := range brief.ParentCriteria {
			acceptanceByFulfilled[c.ID] = c.Text
		}
		for _, feat := range brief.Features {
			snapshot, mErr := json.Marshal(featureAcceptanceSnapshot(feat, acceptanceByFulfilled))
			if mErr != nil {
				return fmt.Errorf("brief_loader: marshal snapshot for %s: %w", feat.ID, mErr)
			}
			child := state.WorkItem{
				ID:                feat.ID,
				Kind:              state.KindFeature,
				Title:             feat.Title,
				Lane:              "server", // MVP-1: single-lane; spec §out-of-scope notes multi-lane is MVP-2
				Status:            state.WorkStatusPlanned,
				ParentProgramID:   brief.ParentWorkItemID,
				DependsOnFeatures: feat.DependsOnFeatures,
				AcceptanceJSON:    string(snapshot),
			}
			if cycErr := b.db.CycleCheck(ctx, child); cycErr != nil {
				slog.Warn("brief.rejected", "path", path, "reason", cycErr.Error())
				continue
			}
			if upErr := b.db.UpsertWorkItem(ctx, child, state.SourceBrief, pollStartedAt); upErr != nil {
				return fmt.Errorf("brief_loader: upsert %s: %w", feat.ID, upErr)
			}
		}
	}

	archived, err := b.db.TombstoneBySource(ctx, string(state.SourceBrief), pollStartedAt)
	if err != nil {
		return fmt.Errorf("brief_loader: tombstone: %w", err)
	}
	for _, id := range archived {
		slog.Warn("brief.tombstoned", "id", id, "at", pollStartedAt)
	}

	if err := b.flagDependencyArchived(ctx); err != nil {
		return err
	}
	return nil
}

// featureAcceptanceSnapshot returns the subset of parent criteria
// this feature fulfills, in stable order.
func featureAcceptanceSnapshot(f PlannedFeature, byFulfilled map[string]string) []PlanCriterion {
	out := make([]PlanCriterion, 0, len(f.Fulfills))
	for _, fid := range f.Fulfills {
		out = append(out, PlanCriterion{ID: fid, Text: byFulfilled[fid]})
	}
	return out
}

// flagDependencyArchived walks children whose depends_on_features
// references a tombstoned (archived) sibling. Each such child gets
// cascade-archived + a WARN log per spec §6 rubric A.
func (b *BriefLoader) flagDependencyArchived(ctx context.Context) error {
	// Scan all brief-source rows; check each dep against the
	// archived set.
	rows, err := b.db.SQL().QueryContext(ctx, `
		SELECT id, depends_on_features FROM work_items
		WHERE source = ? AND status != ?`,
		string(state.SourceBrief), string(state.WorkStatusArchived))
	if err != nil {
		return fmt.Errorf("brief_loader: scan deps: %w", err)
	}
	type pending struct {
		id   string
		deps []string
	}
	var rowsList []pending
	for rows.Next() {
		var id, depsJSON string
		if err := rows.Scan(&id, &depsJSON); err != nil {
			rows.Close()
			return err
		}
		var deps []string
		if err := json.Unmarshal([]byte(depsJSON), &deps); err != nil {
			rows.Close()
			return err
		}
		rowsList = append(rowsList, pending{id, deps})
	}
	rows.Close()

	for _, r := range rowsList {
		for _, dep := range r.deps {
			depItem, err := b.db.GetWorkItem(ctx, dep)
			if err != nil {
				if errors.Is(err, state.ErrWorkItemNotFound) {
					continue
				}
				return err
			}
			if depItem.Status == state.WorkStatusArchived {
				slog.Warn("child.dependency_archived", "child", r.id, "dep", dep)
				if _, err := b.db.SQL().ExecContext(ctx,
					`UPDATE work_items SET status = ?, updated_at = ? WHERE id = ?`,
					string(state.WorkStatusArchived), time.Now().UTC().Unix(), r.id); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}
```

- [ ] **Step 6.4: Run test to verify it passes**

Run: `go test ./internal/program/ -run "TestLoadAndVerifyBrief|TestBriefLoader"`
Expected: PASS.

- [ ] **Step 6.5: Commit Task 6**

```bash
git add internal/program/brief_loader.go internal/program/brief_loader_test.go
git commit -m "feat(program): BriefLoader + LoadAndVerifyBrief

Verifies signed program_brief.json files and upserts one child
work_items row per feature with acceptance_json snapshot
(cascade-soft self-containment per spec §2.4). Rejected briefs
emit slog.Warn 'brief.rejected'; tombstoned-dep children get
'child.dependency_archived' + cascade-archive.

Uses fs.FS for test injection (fstest.MapFS in unit tests,
os.DirFS in production)."
```

---

## Task 7 (A7): planner.LoadPlannerPrompt

Wave 3, parallel with Tasks 5 and 6.

**Files:**
- Modify: `internal/program/planner.go` (add `LoadPlannerPrompt`)
- Modify: `internal/program/planner_test.go` (add 4 tests)

- [ ] **Step 7.1: Write failing tests**

Append to `internal/program/planner_test.go`:

```go
func TestLoadPlannerPrompt_FromDiskNoSHA(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	want := "custom prompt"
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPlannerPrompt(path, "")
	if err != nil {
		t.Fatalf("LoadPlannerPrompt: %v", err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLoadPlannerPrompt_FromDiskCorrectSHA(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	content := "pinned prompt"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(content))
	got, err := LoadPlannerPrompt(path, hex.EncodeToString(h[:]))
	if err != nil {
		t.Fatalf("LoadPlannerPrompt: %v", err)
	}
	if got != content {
		t.Fatalf("got %q want %q", got, content)
	}
}

func TestLoadPlannerPrompt_SHAMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	if err := os.WriteFile(path, []byte("actual"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPlannerPrompt(path, "0000000000000000000000000000000000000000000000000000000000000000")
	if !errors.Is(err, orchestrator.ErrBriefSHAMismatch) {
		t.Fatalf("err=%v want ErrBriefSHAMismatch", err)
	}
}

func TestLoadPlannerPrompt_FallbackOnMissingPath(t *testing.T) {
	got, err := LoadPlannerPrompt(filepath.Join(t.TempDir(), "nope.md"), "")
	if err != nil {
		t.Fatalf("LoadPlannerPrompt: %v", err)
	}
	if got != defaultPlannerPrompt {
		t.Fatalf("did not fall back to defaultPlannerPrompt")
	}
}
```

Add imports to the top of `planner_test.go` if not already present: `"crypto/sha256"`, `"encoding/hex"`, `"errors"`, `"os"`, `"path/filepath"`, `"github.com/trilamsr/regatta/internal/orchestrator"`.

- [ ] **Step 7.2: Run test to verify it fails**

Run: `go test ./internal/program/ -run TestLoadPlannerPrompt`
Expected: FAIL — `undefined: LoadPlannerPrompt`.

- [ ] **Step 7.3: Implement LoadPlannerPrompt**

Append to `internal/program/planner.go`:

```go
// LoadPlannerPrompt returns the planner system prompt to use. If
// path exists, its contents are returned (after a SHA check when
// expectedSHA is non-empty). If path is missing, the embedded
// defaultPlannerPrompt fallback is returned.
//
// expectedSHA is hex-encoded sha256 of the prompt bytes. Pinned in
// regatta.yaml at prompts.planner_sha; surfaced by A10's config
// loader accessor. Empty string disables the check.
//
// per spec §2.5 — UX wins by failing closed when the operator
// pinned a hash and disk drifted.
func LoadPlannerPrompt(path string, expectedSHA string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultPlannerPrompt, nil
		}
		return "", fmt.Errorf("program: read planner prompt: %w", err)
	}
	if expectedSHA != "" {
		h := sha256.Sum256(data)
		got := hex.EncodeToString(h[:])
		if got != expectedSHA {
			return "", fmt.Errorf("%w: path=%s got=%s want=%s",
				orchestrator.ErrBriefSHAMismatch, path, got, expectedSHA)
		}
	}
	return string(data), nil
}
```

Add imports to the top of `planner.go` if not present: `"crypto/sha256"`, `"encoding/hex"`, `"os"`, `"github.com/trilamsr/regatta/internal/orchestrator"`.

- [ ] **Step 7.4: Run test to verify it passes**

Run: `go test ./internal/program/ -run TestLoadPlannerPrompt`
Expected: PASS — all 4 tests green.

- [ ] **Step 7.5: Commit Task 7**

```bash
git add internal/program/planner.go internal/program/planner_test.go
git commit -m "feat(program): LoadPlannerPrompt with SHA-pin verification

Reads contracts/prompts/planner.md when present, verifying against
operator-pinned prompts.planner_sha from regatta.yaml. Falls back
to embedded defaultPlannerPrompt on missing file (operator hasn't
extracted yet). Returns ErrBriefSHAMismatch when on-disk drifts
from pinned value — fail-closed."
```

---

## Task 8 (A8): Scheduler rewire — join-driven reservation

Wave 4. **BLOCKED on Tasks 3, 4 (state.work_items API).**

**Files:**
- Modify: `internal/orchestrator/scheduler/scheduler.go` (replace adapter.List path with state.ListSpawnable + reservation tx)
- Modify: `internal/orchestrator/scheduler/scheduler_test.go` (rewrite around DB-driven tick)

- [ ] **Step 8.1: Read existing scheduler.go to anchor the rewrite**

Run:
```bash
grep -n "func\|adapter\|Tick\|UpsertPending\|TransitionAgent\|TryAcquireLocks" internal/orchestrator/scheduler/scheduler.go
```

Expected output: locate `Tick` method (~L97-135 per spec), `resolveLocks`, and any direct adapter calls. This anchors what to replace.

- [ ] **Step 8.2: Write failing test**

Replace the contents of `internal/orchestrator/scheduler/scheduler_test.go` with:

```go
package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func newSchedTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "s.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestTick_ReservesAllPlannedNoDeps(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	now := time.Now()
	for _, id := range []string{"F-1", "F-2", "F-3"} {
		w := state.WorkItem{ID: id, Kind: state.KindFeature, Title: id,
			Lane: "server", Status: state.WorkStatusPlanned}
		if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, now); err != nil {
			t.Fatal(err)
		}
	}

	s := New(db, Config{})
	ids, err := s.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("reserved=%d want 3 (ids=%v)", len(ids), ids)
	}
}

func TestTick_DepBlocksUntilMerged(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	now := time.Now()
	c1 := state.WorkItem{ID: "F-1", Kind: state.KindFeature, Title: "c1",
		Lane: "server", Status: state.WorkStatusPlanned}
	c2 := state.WorkItem{ID: "F-2", Kind: state.KindFeature, Title: "c2",
		Lane: "server", Status: state.WorkStatusPlanned,
		DependsOnFeatures: []string{"F-1"}}
	for _, w := range []state.WorkItem{c1, c2} {
		if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, now); err != nil {
			t.Fatal(err)
		}
	}

	s := New(db, Config{})
	ids, err := s.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("first tick reserved=%d want 1", len(ids))
	}
}

func TestTick_IdempotentSecondCall(t *testing.T) {
	db := newSchedTestDB(t)
	ctx := context.Background()
	now := time.Now()
	w := state.WorkItem{ID: "F-1", Kind: state.KindFeature, Title: "x",
		Lane: "server", Status: state.WorkStatusPlanned}
	if err := db.UpsertWorkItem(ctx, w, state.SourceBrief, now); err != nil {
		t.Fatal(err)
	}

	s := New(db)
	first, _ := s.Tick(ctx)
	second, _ := s.Tick(ctx)
	if len(first) != 1 || len(second) != 0 {
		t.Fatalf("first=%d second=%d want 1, 0", len(first), len(second))
	}
}
```

- [ ] **Step 8.3: Run test to verify it fails**

Run: `go test ./internal/orchestrator/scheduler/...`
Expected: FAIL — depends on the new `Tick` shape that reads from `state` instead of `adapter`.

- [ ] **Step 8.4: Modify scheduler.go to materialize from work_items**

Surgical change, **not** a rewrite. Preserve the existing `Scheduler` struct, `New(db, Config)` constructor, lane-cap logic, stale-lock expiry, hotspot resolver, and per-tick lock-acquire-then-transition ordering. Only **the source of candidate agents** changes: today `Tick` calls `db.ListAgentsByState(AgentPending)`; after this task it calls a new helper that:

1. Reads `db.ListSpawnable(ctx)` — the join over `work_items` + `agents`.
2. For each returned work_item that has no `agents` row yet, calls `db.UpsertPending(ctx, item.ID, item.Lane)` to materialize one.
3. Returns the existing list of pending `*state.Agent` rows so the rest of `Tick` (lane cap, lock acquire, transition to spawning) is unchanged.

Replace the body of `Tick` in `internal/orchestrator/scheduler/scheduler.go` with:

```go
// Tick performs one scheduling pass. Reads work_items via
// ListSpawnable (universal-queue source of truth per spec §2.8),
// materializes a pending agents row for any work_item missing one,
// then reserves lanes + hotspot locks as before.
//
// Lock acquire is non-blocking; an agent whose hotspot is held by
// another agent is left in pending and retried next tick.
func (s *Scheduler) Tick(ctx context.Context) ([]int64, error) {
	if _, err := s.db.ExpireStaleLocks(ctx, s.cfg.LockTTL); err != nil {
		return nil, fmt.Errorf("scheduler: expire stale locks: %w", err)
	}

	pending, err := s.materializePending(ctx)
	if err != nil {
		return nil, err
	}

	occupancy, err := s.db.CountAgentsByLane(ctx, activeStates...)
	if err != nil {
		return nil, err
	}

	var reserved []int64
	for _, a := range pending {
		if !s.laneHasCapacity(a.Lane, occupancy) {
			continue
		}
		locks := s.resolveLocks(a.WorkItemID)
		if err := s.db.TryAcquireLocks(ctx, locks, a.ID, s.cfg.LockTTL); err != nil {
			if errors.Is(err, state.ErrLockHeld) {
				s.logf("scheduler: agent %d (%s) skipped: hotspot locked", a.ID, a.WorkItemID)
				continue
			}
			return reserved, fmt.Errorf("scheduler: acquire locks for agent %d: %w", a.ID, err)
		}
		if _, err := s.db.TransitionAgent(ctx, a.ID, state.AgentSpawning, state.AgentMutation{}); err != nil {
			_, _ = s.db.ReleaseAgentLocks(ctx, a.ID)
			return reserved, fmt.Errorf("scheduler: mark agent %d spawning: %w", a.ID, err)
		}
		occupancy[a.Lane]++
		reserved = append(reserved, a.ID)
	}
	return reserved, nil
}

// materializePending walks ListSpawnable (work_items LEFT JOIN
// agents WHERE agents.id IS NULL ...) and UpsertPending-s one
// agents row per missing work_item. Returns the union: every
// existing pending agent + every newly-materialized one, in
// db-natural order. Per spec §2.8 — Scheduler is the
// materialization point so the orphan-row class is impossible by
// construction.
func (s *Scheduler) materializePending(ctx context.Context) ([]*state.Agent, error) {
	spawnable, err := s.db.ListSpawnable(ctx)
	if err != nil {
		return nil, fmt.Errorf("scheduler: list spawnable: %w", err)
	}
	for _, w := range spawnable {
		if _, err := s.db.UpsertPending(ctx, w.ID, w.Lane); err != nil {
			return nil, fmt.Errorf("scheduler: materialize pending for %s: %w", w.ID, err)
		}
	}
	return s.db.ListAgentsByState(ctx, state.AgentPending)
}
```

Leave the rest of `scheduler.go` untouched: `New(db, cfg)`, `Config`, `HotspotResolver`, `SetLogger`, `activeStates`, `laneHasCapacity`, `resolveLocks` all stay. The signature `scheduler.New(db, scheduler.Config{...})` is preserved so Task 9's orchestrator wire does not need to change the construction call site.

Note on transactional atomicity: spec §2.8 idealized a single `BEGIN IMMEDIATE` covering INSERT-into-agents + transition + lock-acquire. The existing scheduler splits these across three calls today (UpsertPending tx → TryAcquireLocks tx → TransitionAgent tx). Preserving that split keeps this Task surgical (no concurrent-correctness regression vs. existing behavior), and any failure between the steps lands the row in `pending` — the same retry-eligible state the spec's idealized rollback would produce. The orphan-row class is still eliminated by `ListSpawnable`'s LEFT JOIN: the next tick re-discovers the work_item, sees an agent already exists (or doesn't), and proceeds. Tighter per-row atomicity is an MVP-2 follow-up (file `internal/orchestrator/state` issue if not in tracker).

- [ ] **Step 8.5: Run test to verify it passes**

Run: `go test ./internal/orchestrator/scheduler/...`
Expected: PASS.

If `TryAcquireLocks` has a different signature than shown, run `grep -n "TryAcquireLocks" internal/orchestrator/state/` to find the real signature and adapt the call in `reserveOne`. The contract is "atomic acquire of all named locks; return false if any unavailable." If the existing API requires `[]string` plus a heartbeat time, this matches; if not, adapt.

- [ ] **Step 8.6: Commit Task 8**

```bash
git add internal/orchestrator/scheduler/
git commit -m "feat(scheduler): join-driven Tick reads work_items + reserves agents

ListSpawnable replaces the adapter.List walk; each returned row
becomes an agent row via UpsertPending + TransitionAgent(spawning)
+ TryAcquireLocks in one logical reservation. Lock-held rolls back
to pending so the next tick retries (no zombie spawning rows).

UpsertPending stays in state package but no caller in PollOnce
invokes it anymore — Wave 5 deletes that path."
```

---

## Task 9 (A9): Orchestrator wire — PollOnce gets flock + AdapterSync + BriefLoader

Wave 5. **BLOCKED on Tasks 2, 5, 6, 8.** Parallel with Task 10.

**Files:**
- Modify: `internal/orchestrator/orchestrator.go` (PollOnce rewrite)
- Modify: `internal/orchestrator/orchestrator_test.go` (adversarial cases)

- [ ] **Step 9.1: Read existing orchestrator.go to anchor changes**

Run:
```bash
grep -n "PollOnce\|UpsertPending\|adapter\.List\|Orchestrator struct\|New" internal/orchestrator/orchestrator.go
```

This locates the constructor, the existing PollOnce, and current adapter coupling.

- [ ] **Step 9.2: Write failing test (adversarial)**

Append to `internal/orchestrator/orchestrator_test.go`:

```go
func TestPollOnce_FlockHeldReturnsError(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	lock1, err := lockfile.Acquire(dbPath + ".lock")
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer lock1.Release()

	o := newTestOrchestrator(t, db, dbPath)
	err = o.PollOnce(context.Background())
	if !errors.Is(err, orchestrator.ErrFlockHeld) {
		t.Fatalf("err=%v want ErrFlockHeld", err)
	}
}

func TestPollOnce_AdapterSyncFailReturnsEarly(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := state.Open(context.Background(), state.DSN(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	o := newTestOrchestratorWithFailingAdapter(t, db, dbPath)
	if err := o.PollOnce(context.Background()); err == nil {
		t.Fatal("expected adapter-fail error, got nil")
	}
}
```

The helpers `newTestOrchestrator` and `newTestOrchestratorWithFailingAdapter` need to be added to the test file. Define them at file scope:

```go
func newTestOrchestrator(t *testing.T, db *state.DB, dbPath string) *Orchestrator {
	t.Helper()
	adapter := &okAdapter{}
	syncer := adaptersync.New(adapter, db, clock.System())
	loader := program.NewBriefLoader(fstest.MapFS{}, db, clock.System(), map[string][]byte{"k": []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")})
	sched := scheduler.New(db, scheduler.Config{})
	spawn := spawner.NewStub()
	return New(Config{
		Adapter:     adapter,
		AdapterSync: syncer,
		BriefLoader: loader,
		DB:          db,
		Scheduler:   sched,
		Spawner:     spawn,
		DBPath:      dbPath,
		Clock:       clock.System(),
	})
}

func newTestOrchestratorWithFailingAdapter(t *testing.T, db *state.DB, dbPath string) *Orchestrator {
	t.Helper()
	adapter := &failingAdapter{}
	syncer := adaptersync.New(adapter, db, clock.System())
	loader := program.NewBriefLoader(fstest.MapFS{}, db, clock.System(), map[string][]byte{"k": []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")})
	sched := scheduler.New(db, scheduler.Config{})
	spawn := spawner.NewStub()
	return New(Config{
		Adapter:     adapter,
		AdapterSync: syncer,
		BriefLoader: loader,
		DB:          db,
		Scheduler:   sched,
		Spawner:     spawn,
		DBPath:      dbPath,
		Clock:       clock.System(),
	})
}

type okAdapter struct{}

func (okAdapter) List(context.Context) ([]schemas.WorkItem, error) { return nil, nil }
```

Add imports: `"path/filepath"`, `"testing/fstest"`, `"errors"`, `"github.com/trilamsr/regatta/internal/orchestrator"`, `"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"`, `"github.com/trilamsr/regatta/internal/orchestrator/clock"`, `"github.com/trilamsr/regatta/internal/orchestrator/lockfile"`, `"github.com/trilamsr/regatta/internal/orchestrator/scheduler"`, `"github.com/trilamsr/regatta/internal/orchestrator/spawner"`, `"github.com/trilamsr/regatta/internal/orchestrator/state"`, `"github.com/trilamsr/regatta/internal/program"`.

- [ ] **Step 9.3: Run test to verify it fails**

Run: `go test ./internal/orchestrator/ -run "TestPollOnce_Flock|TestPollOnce_AdapterSync"`
Expected: FAIL — `Orchestrator` constructor takes the old shape, not the new `Config`.

- [ ] **Step 9.4: Rewrite Orchestrator constructor + PollOnce**

In `internal/orchestrator/orchestrator.go`, replace the `Orchestrator` struct, `New` constructor, and `PollOnce` method:

```go
// Orchestrator is the top-level glue: per-tick it acquires a
// process-level lock, syncs the adapter, syncs briefs, then
// reserves and spawns.
//
// per spec §2.9: tick sequence is flock -> AdapterSync -> BriefLoader
// -> Scheduler.Tick, fail-fast on first error.
type Orchestrator struct {
	adapter     SpecAdapter
	adapterSync *adaptersync.Syncer
	briefLoader *program.BriefLoader
	db          *state.DB
	sched       *scheduler.Scheduler
	spawner     spawner.Spawner
	dbPath      string
	clock       clock.Clock
}

// Config holds the dependencies Orchestrator needs at construction.
type Config struct {
	Adapter     SpecAdapter
	AdapterSync *adaptersync.Syncer
	BriefLoader *program.BriefLoader
	DB          *state.DB
	Scheduler   *scheduler.Scheduler
	Spawner     spawner.Spawner
	DBPath      string
	Clock       clock.Clock
}

// New constructs an Orchestrator. dbPath is required to derive the
// process-level lockfile path (<dbPath>.lock).
func New(cfg Config) *Orchestrator {
	if cfg.Clock == nil {
		cfg.Clock = clock.System()
	}
	return &Orchestrator{
		adapter:     cfg.Adapter,
		adapterSync: cfg.AdapterSync,
		briefLoader: cfg.BriefLoader,
		db:          cfg.DB,
		sched:       cfg.Scheduler,
		spawner:     cfg.Spawner,
		dbPath:      cfg.DBPath,
		clock:       cfg.Clock,
	}
}

// PollOnce acquires the process flock, mirrors the adapter into
// work_items, loads + verifies briefs into work_items, and reserves
// agents from the join. Fail-fast: any error returns immediately.
func (o *Orchestrator) PollOnce(ctx context.Context) error {
	lock, err := lockfile.Acquire(o.dbPath + ".lock")
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	pollStartedAt := o.clock.Now().UTC()

	if err := o.adapterSync.Sync(ctx, pollStartedAt); err != nil {
		return fmt.Errorf("orchestrator: adapter sync: %w", err)
	}
	if err := o.briefLoader.Sync(ctx, pollStartedAt); err != nil {
		return fmt.Errorf("orchestrator: brief sync: %w", err)
	}
	if _, err := o.sched.Tick(ctx); err != nil {
		return fmt.Errorf("orchestrator: scheduler tick: %w", err)
	}
	return nil
}

// SpecAdapter narrows the existing adapter interface to the read
// surface the orchestrator needs.
type SpecAdapter interface {
	List(ctx context.Context) ([]schemas.WorkItem, error)
}
```

Update imports at top of file: add `"fmt"`, `"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"`, `"github.com/trilamsr/regatta/internal/orchestrator/clock"`, `"github.com/trilamsr/regatta/internal/orchestrator/lockfile"`, `"github.com/trilamsr/regatta/internal/program"`, `"github.com/trilamsr/regatta/contracts/schemas"`.

Delete the old `PollOnce` (with `adapter.List` + `UpsertPending` loop). Keep `ScheduleOnce` and `ReapTerminal` — Tasks 8 and 9 do not change those.

- [ ] **Step 9.5: Update cmd/regatta/main.go construction call**

`cmd/regatta/main.go` will fail to compile because `New(...)` signature changed. Update the call site in `runServe` (or wherever the orchestrator is constructed) to use the `Config` struct:

```go
// In cmd/regatta/main.go, in the function that constructs the orchestrator:
clk := clock.System()
syncer := adaptersync.New(adapter, db, clk)
briefFS := os.DirFS(filepath.Join(repoRoot, ".regatta", "programs"))
keyring := loadBriefKeyring() // see helper below
loader := program.NewBriefLoader(briefFS, db, clk, keyring)
sched := scheduler.New(db, scheduler.Config{
	LaneCaps: laneCaps,         // preserve existing lane-cap behavior; pull from cfg if accessor exists
	LockTTL:  15 * time.Minute, // existing scheduler default
	Hotspots: hotspotsResolver, // existing resolver wiring
})
orch := orchestrator.New(orchestrator.Config{
	Adapter:     adapter,
	AdapterSync: syncer,
	BriefLoader: loader,
	DB:          db,
	Scheduler:   sched,
	Spawner:     spawn,
	DBPath:      dbPath,
	Clock:       clk,
})
```

(Imports: `"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"`, `"github.com/trilamsr/regatta/internal/orchestrator/clock"`, `"github.com/trilamsr/regatta/internal/program"`.)

Helper `loadBriefKeyring` added in same file. Reuses the existing `-hmac-key-env` convention from `runProgramPlan` to avoid double-config:

```go
// loadBriefKeyring reads the HMAC key from REGATTA_HMAC_KEY (or
// the env var named by REGATTA_HMAC_KEY_ENV when set) and returns
// it as a one-entry keyring keyed "default". Empty keyring when
// unset — BriefLoader.Sync fails loud on first verify attempt,
// surfacing the misconfig to the operator.
func loadBriefKeyring() map[string][]byte {
	envName := os.Getenv("REGATTA_HMAC_KEY_ENV")
	if envName == "" {
		envName = "REGATTA_HMAC_KEY"
	}
	v := os.Getenv(envName)
	if v == "" {
		return map[string][]byte{}
	}
	return map[string][]byte{"default": []byte(v)}
}
```

`laneCaps` and `hotspotsResolver` come from the existing `cmd/regatta/main.go` `runServe` body — if they aren't named exactly that today, grep for what is and reuse. If accessor methods (`cfg.LaneCaps()`, etc.) need to be added to `internal/config/validate/load.go`, do it here as 3-line wrappers next to Task 10's `PlannerPromptSHA`.

- [ ] **Step 9.6: Run test to verify it passes**

Run: `go test ./internal/orchestrator/... ./cmd/regatta/...`
Expected: PASS — orchestrator tests green, cmd tests still compile.

- [ ] **Step 9.7: Commit Task 9**

```bash
git add internal/orchestrator/orchestrator.go internal/orchestrator/orchestrator_test.go cmd/regatta/main.go
git commit -m "feat(orchestrator): PollOnce flock -> AdapterSync -> BriefLoader -> Scheduler.Tick

Universal-queue PollOnce (spec §2.9). Process-level lockfile
prevents concurrent ticks. UpsertPending direct call removed —
scheduler.Tick is now the materialization point per Task 8.
Fail-fast on any sync error; next tick retries.

Constructor takes Config struct so test wiring can swap any
dependency."
```

---

## Task 10 (A10): runProgramPlan --write + config accessor

Wave 5, parallel with Task 9. **No dependency on Task 7** — uses existing `defaultPlannerPrompt`.

**Files:**
- Modify: `cmd/regatta/main.go` (`runProgramPlan` adds `--write` flag, atomic temp+rename)
- Modify: `internal/config/validate/load.go` (3-line `prompts.planner_sha` accessor)
- Create or modify: `cmd/regatta/program_plan_test.go` (target-exists test)

- [ ] **Step 10.1: Read current runProgramPlan**

Run:
```bash
grep -n "runProgramPlan\|--hmac-key\|--write\|program_brief\|json.Marshal" cmd/regatta/main.go | head -30
```

This locates the existing `runProgramPlan` and shows the current output-to-stdout flow.

- [ ] **Step 10.2: Write failing test**

Append to or create `cmd/regatta/program_plan_test.go`:

```go
package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/internal/orchestrator"
)

func TestRunProgramPlan_WriteCreatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HMAC_KEY", "test-key-32-bytes-aaaaaaaaaaaaaaa")

	// Seed a minimal program markdown.
	itemPath := filepath.Join(dir, "PROG-1.md")
	if err := os.WriteFile(itemPath, []byte(`---
id: PROG-1
kind: program
title: smoke
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: do a
- [planned] c2: do b
- [planned] c3: do c
`), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, ".regatta", "programs")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Use the stub planner so the test is deterministic and offline.
	args := []string{"-hmac-key-env=HMAC_KEY", "-planner=stub", "-write",
		"-write-dir=" + outDir, itemPath}
	if rc := runProgramPlan(args); rc != 0 {
		t.Fatalf("runProgramPlan rc=%d want 0", rc)
	}

	briefPath := filepath.Join(outDir, "PROG-1.json")
	if _, err := os.Stat(briefPath); err != nil {
		t.Fatalf("brief not written: %v", err)
	}
}

func TestRunProgramPlan_WriteTargetExistsErrors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HMAC_KEY", "test-key-32-bytes-aaaaaaaaaaaaaaa")
	itemPath := filepath.Join(dir, "PROG-1.md")
	if err := os.WriteFile(itemPath, []byte(`---
id: PROG-1
kind: program
title: smoke
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: a
- [planned] c2: b
- [planned] c3: c
`), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, ".regatta", "programs")
	_ = os.MkdirAll(outDir, 0o755)

	args := []string{"-hmac-key-env=HMAC_KEY", "-planner=stub", "-write",
		"-write-dir=" + outDir, itemPath}
	if rc := runProgramPlan(args); rc != 0 {
		t.Fatalf("first run rc=%d want 0", rc)
	}
	// Second call without --force on existing target with different
	// content (stub planner stamps fresh produced_at) returns
	// non-zero. runProgramPlan currently returns int; the error
	// surfaces via stderr. Assert rc != 0 and (if exposed) check
	// the typed error via a side-channel — when runProgramPlan is
	// refactored to return error, switch to errors.Is.
	if rc := runProgramPlan(args); rc == 0 {
		t.Fatalf("second run rc=0 want non-zero (target exists)")
	}
	// `orchestrator.ErrTargetExists` is the typed sentinel
	// returned from atomicWriteBrief; non-zero rc here proves the
	// failure path fires. To assert sentinel identity directly,
	// the cmd-layer must expose runProgramPlanE returning error —
	// optional refactor noted in plan.
	_ = errors.Is(nil, orchestrator.ErrTargetExists) // keep import used
}
```

Note: tests call `runProgramPlan([]string{...})` directly (returns `int` exit code), matching the existing convention in `cmd/regatta/program_plan_test.go`. CLI flags use single-dash form (`-write`, `-planner=stub`) because `runProgramPlan` constructs its own `flag.FlagSet`. There is no `runMain` wrapper.

- [ ] **Step 10.3: Run test to verify it fails**

Run: `go test ./cmd/regatta/ -run TestRunProgramPlan_Write`
Expected: FAIL — `--write` and `--write-dir` flags unknown.

- [ ] **Step 10.4: Implement --write + --planner + atomic temp+rename**

In `cmd/regatta/main.go`, modify `runProgramPlan` to register the new flags and implement the write path. The `-planner=stub` flag is required so the e2e test in Task 11 runs offline without an Anthropic key:

```go
// In runProgramPlan, add after existing flag registrations:
writeFlag := fs.Bool("write", false,
	"write signed brief to <write-dir>/<program_id>.json atomically; default stdout")
writeDir := fs.String("write-dir", "",
	"directory to write brief into when --write is set; defaults to <repo>/.regatta/programs")
force := fs.Bool("force", false,
	"overwrite existing brief at the target path")
plannerName := fs.String("planner", "anthropic",
	"planner implementation: 'anthropic' (default, calls Anthropic API) or 'stub' (deterministic offline planner for tests)")
```

After flag parsing, branch on `*plannerName`:

```go
var planner program.Planner
switch *plannerName {
case "anthropic":
	planner = program.NewAnthropicPlanner(program.AnthropicConfig{ /* existing wiring */ })
case "stub":
	planner = program.NewStubPlanner()
default:
	fmt.Fprintf(os.Stderr, "regatta program plan: unknown planner %q (want anthropic|stub)\n", *plannerName)
	return 2
}
```

If `program.NewStubPlanner` doesn't exist, add it in `internal/program/planner_stub.go` (this Task owns it — it's needed for any deterministic test):

```go
// internal/program/planner_stub.go
package program

import (
	"context"
	"fmt"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// StubPlanner emits a deterministic ProgramBrief from the parent's
// acceptance criteria, one feature per criterion. Tests use this so
// the planner pipeline runs offline.
type StubPlanner struct{}

// NewStubPlanner returns a StubPlanner.
func NewStubPlanner() *StubPlanner { return &StubPlanner{} }

// Plan implements the Planner interface. One feature per parent
// criterion, no inter-feature deps.
func (s *StubPlanner) Plan(ctx context.Context, parent schemas.WorkItem) (*ProgramBrief, error) {
	criteria := parentCriteriaFromWorkItem(parent) // existing helper used by AnthropicPlanner
	if len(criteria) < 1 {
		return nil, fmt.Errorf("stub planner: parent has no acceptance criteria")
	}
	feats := make([]PlannedFeature, 0, len(criteria))
	for i, c := range criteria {
		feats = append(feats, PlannedFeature{
			ID:       fmt.Sprintf("F-%02d", i+1),
			Title:    fmt.Sprintf("stub feature for %s", c.ID),
			Fulfills: []string{c.ID},
		})
	}
	return &ProgramBrief{
		SchemaVersion:    1,
		ProgramID:        fmt.Sprintf("m-%012x", time.Now().UnixNano()),
		ParentWorkItemID: string(parent.ID),
		ParentCriteria:   criteria,
		PlannerModelID:   "stub",
		Features:         feats,
		ProducedAt:       time.Now().UTC(),
	}, nil
}
```

If `parentCriteriaFromWorkItem` doesn't exist yet, inline the equivalent — read the parent markdown's acceptance criteria from `parent.AcceptanceCriteria` (schemas.WorkItem field; grep to confirm name) and convert to `[]PlanCriterion`.

// ... existing planner setup ...

// After brief is signed and ready to emit:
if *writeFlag {
	target := *writeDir
	if target == "" {
		target = filepath.Join(repoRoot, ".regatta", "programs")
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("regatta program plan: mkdir %s: %w", target, err)
	}
	briefPath := filepath.Join(target, signed.ProgramID+".json")
	raw, err := json.MarshalIndent(signed, "", "  ")
	if err != nil {
		return fmt.Errorf("regatta program plan: marshal: %w", err)
	}
	if err := atomicWriteBrief(briefPath, raw, *force); err != nil {
		return err
	}
	return nil
}
// fall through to existing stdout path
```

Add the helper near the bottom of the file:

```go
// atomicWriteBrief writes data to path via temp + os.Rename. If
// path exists and force is false, returns ErrTargetExists when the
// existing content differs from data.
func atomicWriteBrief(path string, data []byte, force bool) error {
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		if !force {
			return fmt.Errorf("%w: %s", orchestrator.ErrTargetExists, path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("regatta program plan: stat target: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".brief-*.tmp")
	if err != nil {
		return fmt.Errorf("regatta program plan: create temp: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("regatta program plan: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("regatta program plan: close temp: %w", err)
	}
	return os.Rename(tmp.Name(), path)
}
```

Imports to add: `"bytes"`, `"github.com/trilamsr/regatta/internal/orchestrator"`.

- [ ] **Step 10.5: Add the prompts.planner_sha accessor**

In `internal/config/validate/load.go` (or whichever config-loader file holds the parsed `regatta.yaml` struct), add a method:

```go
// PlannerPromptSHA returns the operator-pinned hex-encoded sha256
// of contracts/prompts/planner.md. Empty string when unpinned.
// Used by cmd/regatta/main.go runProgramPlan to feed
// program.LoadPlannerPrompt.
func (c *Config) PlannerPromptSHA() string {
	if c == nil || c.Prompts == nil {
		return ""
	}
	return c.Prompts.PlannerSHA
}
```

If the Config struct doesn't currently expose `Prompts.PlannerSHA`, add the YAML tag wiring in the same file (3 lines: struct field + tag). If the cue schema already validates `prompts.planner_sha` (per spec line 141), the Go side just needs the field; the validator will already accept it.

- [ ] **Step 10.6: Run test to verify it passes**

Run: `go test ./cmd/regatta/ -run TestRunProgramPlan_Write`
Expected: PASS.

- [ ] **Step 10.7: Commit Task 10**

```bash
git add cmd/regatta/main.go cmd/regatta/program_plan_test.go internal/config/validate/load.go
git commit -m "feat(cli): regatta program plan --write + config.PlannerPromptSHA accessor

--write emits the signed brief to .regatta/programs/<program_id>.json
via atomic temp+rename. Re-runs with different content return
ErrTargetExists unless --force is passed. Stdout remains the
default for stdout-piping workflows.

internal/config/validate: 3-line accessor surfaces
prompts.planner_sha to runProgramPlan (and indirectly to
program.LoadPlannerPrompt when Wave 5+ wires it)."
```

---

## Task 11 (A11): E2E acceptance test + fixture

Wave 6, parallel with Task 12.

**Files:**
- Create: `testdata/program/PROG-1.md`
- Create: `internal/program/end_to_end_test.go`

- [ ] **Step 11.1: Write the fixture**

Create `testdata/program/PROG-1.md`:

```markdown
---
id: PROG-1
kind: program
title: MVP-1 acceptance fixture
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: add a foo function
- [planned] c2: add a bar function
- [planned] c3: add a baz function
```

(No golden brief file checked in: signatures embed `produced_at` timestamps, so a checked-in golden would rot. The e2e test asserts the *shape* — 3 child rows, 3 running agents — not byte equality.)

- [ ] **Step 11.2: Write failing test**

Create `internal/program/end_to_end_test.go`:

```go
package program_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestEndToEnd_PlanAndServe exercises the MVP-1 acceptance flow:
// regatta program plan --write -> regatta serve --tick-once ->
// 3 work_items rows + 3 running agents.
//
// Per spec §6 series-complete DoD #5.
func TestEndToEnd_PlanAndServe(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test; skip with -short")
	}
	repoRoot := mustRepoRoot(t)
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "regatta")
	build := exec.Command("go", "build", "-o", bin, "./cmd/regatta")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, ".regatta", "programs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, ".regatta", "items"), 0o755); err != nil {
		t.Fatal(err)
	}

	fixture, err := os.ReadFile(filepath.Join(repoRoot, "testdata", "program", "PROG-1.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	itemPath := filepath.Join(target, ".regatta", "items", "PROG-1.md")
	if err := os.WriteFile(itemPath, fixture, 0o600); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"HMAC_KEY=test-key-32-bytes-aaaaaaaaaaaaaaa",
		"REGATTA_HMAC_KEY=test-key-32-bytes-aaaaaaaaaaaaaaa",
	)

	plan := exec.Command(bin, "program", "plan",
		"-hmac-key-env=HMAC_KEY",
		"-planner=stub",
		"-write",
		itemPath)
	plan.Dir = target
	plan.Env = env
	if out, err := plan.CombinedOutput(); err != nil {
		t.Fatalf("plan: %v\n%s", err, out)
	}

	serve := exec.Command(bin, "serve",
		"-spawner=stub",
		"-tick-once",
		"-repo", target,
		"-db", filepath.Join(target, "state.db"))
	serve.Dir = target
	serve.Env = env
	if out, err := serve.CombinedOutput(); err != nil {
		t.Fatalf("serve: %v\n%s", err, out)
	}

	// Verify state.
	dbPath := filepath.Join(target, "state.db")
	assertCounts(t, dbPath)
}

func assertCounts(t *testing.T, dbPath string) {
	t.Helper()
	out, err := exec.Command("sqlite3", dbPath,
		"SELECT COUNT(*) FROM work_items WHERE parent_program_id='PROG-1'").Output()
	if err != nil {
		t.Fatalf("query work_items: %v", err)
	}
	if got := trimNL(string(out)); got != "3" {
		t.Fatalf("work_items count=%s want 3", got)
	}
	out, err = exec.Command("sqlite3", dbPath,
		"SELECT COUNT(*) FROM agents WHERE state='running'").Output()
	if err != nil {
		t.Fatalf("query agents: %v", err)
	}
	if got := trimNL(string(out)); got != "3" {
		t.Fatalf("running agents count=%s want 3", got)
	}
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatalf("no go.mod above %s", wd)
		}
		dir = next
	}
}
```

- [ ] **Step 11.3: Run test to verify it passes**

Run: `go test ./internal/program/ -run TestEndToEnd_PlanAndServe -v`
Expected: PASS — 3 work_items rows + 3 running agents.

If the test depends on a flag (`--planner=stub`, `--spawner=stub`) that doesn't exist in `cmd/regatta/main.go`, the failure points to a real omission: those toggles must exist so the e2e is offline + deterministic. Add the flag handling in main.go before the e2e test will pass.

- [ ] **Step 11.4: Commit Task 11**

```bash
git add testdata/program/PROG-1.md internal/program/end_to_end_test.go
git commit -m "test(program): MVP-1 end-to-end acceptance — plan + serve --tick-once

Builds regatta, runs program plan --write + serve --tick-once
against a 3-criterion PROG-1 fixture, asserts 3 work_items rows
+ 3 running agents. Validates the spec §6 series-complete DoD."
```

---

## Task 12 (A12): Documentation + DoD checklist

Wave 6, parallel with Task 11.

**Files:**
- Modify: `docs/operator/configure.md` (prompts.planner_sha)
- Modify: `docs/operator/quickstart.md` (program plan walkthrough + flock + slog reference)
- Create: `docs/engineer/mvp-1-dod-checklist.md`
- Delete: `docs/engineer/specs/mvp-1-planner.md` (superseded by new spec)
- Modify: `CHANGELOG.md` (MVP-1 entry)

- [ ] **Step 12.1: Write the DoD checklist**

Create `docs/engineer/mvp-1-dod-checklist.md`:

```markdown
# MVP-1 Definition-of-Done Checklist

Per spec §6. Tick each item as the PR series progresses. Anything
left unchecked blocks the v0.1.0 tag.

## Merge-time (per PR)
- [ ] `go test -race ./...` green
- [ ] `make ci-check` exit 0
- [ ] Paired `_test.go` for every new file

## Series-complete
- [ ] `regatta program plan --write` then `regatta serve --tick-once`
      → exactly 3 work_items WHERE parent_program_id='PROG-1'
- [ ] Exactly 3 agents WHERE state='running' for those work_item_ids
- [ ] Zero events at `slog.LevelWarn` or higher during happy-path run.
      Happy path means: adapter returns a stable item set across both
      ticks (nothing disappears), brief verifies on first read, no
      tombstones fire. Tombstone WARN events (`adapter.tombstoned`,
      `brief.tombstoned`, `child.cascade_archived`,
      `child.dependency_archived`) are *expected* in adversarial
      scenarios — they are only "noise" in the steady-state baseline.
- [ ] CHANGELOG.md updated with MVP-1 entry
- [ ] `docs/engineer/specs/mvp-1-planner.md` deleted (superseded)

## Grade-A (production-trustable) checks
- [ ] `grep -rn 'errors.New(' internal/orchestrator/ internal/program/`
      returns only `internal/orchestrator/errors.go`
- [ ] Every slog WARN path enumerated below is emitted by at least one test:
  - [ ] `brief.rejected` (HMAC fail, sha mismatch, parse error)
  - [ ] `brief.tombstoned` (file disappeared)
  - [ ] `adapter.tombstoned` (item disappeared)
  - [ ] `child.cascade_archived` (parent archived) — emitted by AdapterSync.CascadeArchiveChildren caller
  - [ ] `child.dependency_archived` (depends_on target tombstoned)
- [ ] Adversarial tests green:
  - [ ] Brief disappears mid-poll
  - [ ] AdapterSync fails — fail-fast
  - [ ] Stale flock — reclaim succeeds
  - [ ] HMAC rotation — old + new keys coexist in keyring
  - [ ] Tombstoned dep — auto-cascade child
- [ ] DAG property test: 200 random DAGs n≤8, reserved set == topological-ready set
- [ ] Operator docs include: program plan walkthrough, flock troubleshooting, slog event reference
- [ ] Per-package coverage thresholds (run `go test -cover`):
  - [ ] `internal/orchestrator/state` ≥ 90%
  - [ ] `internal/program` ≥ 85%
  - [ ] `cmd/regatta` ≥ 70%

## Grade-A+ (stretch)
- [ ] Mutation test: comment out version-check in `state/migrate.go`,
      `TestMigrate_DowngradeResistance` must fail
- [ ] MTTD a rejected brief ≤ 60 seconds using only `journalctl | grep`
      — operator runbook in `docs/operator/quickstart.md` timed externally
- [ ] Operator recovery procedure for corrupted `.regatta/state.db`
      documented + tested in `docs/operator/quickstart.md`

## Decision → file mapping
| Decision (per spec §"Locked decisions") | Implementation site |
|---|---|
| 1. Universal queue | `state.work_items` table; `migrations/0002_work_items.sql` |
| 2. Two writers, one reader | `adaptersync.Sync` + `program.BriefLoader.Sync` write; `scheduler.Tick` reads |
| 3. Scheduler join | `state.ListSpawnable` SQL in `work_items_query.go` |
| 4. Cascade-soft | `state.CascadeArchiveChildren` — only touches work_items |
| 5. Cascade snapshot | `program.BriefLoader.Sync` → `acceptance_json` per child |
| 6. Sign-then-persist | `program.LoadAndVerifyBrief` — Validate + VerifySignature before upsert |
| 7. slog WARN rejections | `brief_loader.go` `slog.Warn("brief.rejected", ...)` |
| 8. DAG enforce + cycle check | `state.ListSpawnable` deps clause + `state.CycleCheck` |
| 9. pollStartedAt cutoff | `orchestrator.PollOnce` captures, passes to both syncs |
| 10. Fail-fast PollOnce | `orchestrator.PollOnce` returns on first error |
| 11. Flock | `internal/orchestrator/lockfile/lockfile.go` |
| 12. sqlite stays | `state.go` `_ "modernc.org/sqlite"` driver |
| 13. TDD + library-first | every step in this plan; `goose` + `gofrs/flock` deps |
```

- [ ] **Step 12.2: Update operator docs**

In `docs/operator/quickstart.md`, append a section after the existing init walkthrough:

```markdown
## Program plan + serve walkthrough (MVP-1)

Once `regatta init` is done, the program-plan loop looks like this:

```sh
# 1. Author a program markdown item with ≥3 acceptance criteria.
cat > .regatta/items/PROG-1.md <<'EOF'
---
id: PROG-1
kind: program
title: first program
lane: server
status: planned
---

## Acceptance criteria

- [planned] c1: add foo
- [planned] c2: add bar
- [planned] c3: add baz
EOF

# 2. Plan + sign + persist.
export REGATTA_HMAC_KEY=$(openssl rand -hex 32)
regatta program plan --hmac-key-env=REGATTA_HMAC_KEY --write \
  .regatta/items/PROG-1.md

# 3. Spin up one tick to spawn child agents.
regatta serve --spawner=stub --tick-once --repo .

# 4. Verify.
sqlite3 .regatta/state.db \
  "SELECT id, status FROM work_items WHERE parent_program_id='PROG-1'"
# Expected: 3 rows, all status=planned then running after the tick.
```

### Troubleshooting

- **`ErrFlockHeld`**: another `regatta serve` process holds the
  database lock. Check with `lsof .regatta/state.db.lock` or read the
  PID from the lockfile directly. If you know the process is dead,
  delete the lockfile and retry — Regatta also auto-reclaims on the
  next call.

- **Rejected briefs**: search the operator log:
  ```sh
  journalctl -u regatta | grep brief.rejected
  ```
  Each rejection logs `path` and `reason`. Most common: stale HMAC
  key after rotation — re-run `regatta program plan --write` with the
  new key.
```

In `docs/operator/configure.md`, document `prompts.planner_sha`:

```markdown
### `prompts.planner_sha` (optional)

Hex-encoded sha256 of `contracts/prompts/planner.md`. When set, the
binary refuses to start with a prompt file that doesn't match —
fail-closed against on-disk drift. Compute with:

```sh
sha256sum contracts/prompts/planner.md
```
```

- [ ] **Step 12.3: Delete the superseded spec**

```bash
git rm docs/engineer/specs/mvp-1-planner.md
```

- [ ] **Step 12.4: Update CHANGELOG.md**

Open `CHANGELOG.md`. Append (after the most recent `## [Unreleased]` section or under a new `## [v0.1.0] - 2026-05-30` heading) the MVP-1 entry:

```markdown
## v0.1.0 — MVP-1 Planner-as-DAG (2026-05-30)

### Added
- `state.work_items` table — universal queue, single source of truth
- `internal/orchestrator/adaptersync` — mirrors SpecAdapter into work_items each tick
- `internal/program.BriefLoader` + `LoadAndVerifyBrief` — verifies signed program briefs and upserts child work_items
- `internal/program.LoadPlannerPrompt` — operator-pinned SHA verification with embedded-prompt fallback
- `internal/orchestrator/lockfile` — `gofrs/flock` wrapper with stale-PID reclaim
- `internal/orchestrator/clock` — `Clock` interface for deterministic time injection
- `internal/orchestrator/errors` — typed sentinels (`ErrBriefSHAMismatch`, `ErrHMACInvalid`, `ErrTargetExists`, `ErrFlockHeld`, `ErrSchemaTooNew`, `ErrCycleDetected`)
- `regatta program plan --write` — atomic temp+rename of signed brief into `.regatta/programs/<program_id>.json`
- `pressly/goose` migration runner (`internal/orchestrator/state/migrations/`)
- DAG enforcement: blocked children wait until upstream `status=merged`; cycle detection at upsert
- Operator docs: program plan walkthrough, flock troubleshooting, slog event reference

### Changed
- `orchestrator.PollOnce` rewired: flock acquire → AdapterSync → BriefLoader → Scheduler.Tick (fail-fast)
- `scheduler.Tick` now reads `state.ListSpawnable` and reserves agents in single tx (replaces direct `adapter.List` + `UpsertPending` path)
- `state.Open` uses `Migrate(ctx, db)` (goose) instead of inline `schema.sql` apply

### Removed
- `internal/orchestrator/state/schema.sql` — extracted into `migrations/0001_initial.sql`
- `docs/engineer/specs/mvp-1-planner.md` — superseded by `docs/superpowers/specs/2026-05-30-mvp-1-planner-as-dag-design.md`
```

- [ ] **Step 12.5: Run full test suite + ci-check**

Run:
```bash
go test -race ./...
make ci-check
```

Both must exit 0. Any failure here is a series-complete DoD blocker; loop back to whichever Task introduced the regression.

- [ ] **Step 12.6: Commit Task 12**

```bash
git add docs/ CHANGELOG.md
git commit -m "docs(mvp-1): operator walkthrough, DoD checklist, changelog

Documents the program-plan + serve --tick-once flow operators use
to ship one program through three child agents. Adds DoD checklist
with grade-A and A+ criteria mapped to verification commands.
Deletes superseded spec. Flips CHANGELOG to v0.1.0 entry."
```

---

## Final validation gate (after Task 12 lands)

Before tagging v0.1.0:

- [ ] **G.1: Series-complete DoD #5** — run e2e once more from a clean tmpdir:
  ```bash
  go test ./internal/program/ -run TestEndToEnd_PlanAndServe -v
  ```
  Expected: PASS. 3 work_items + 3 running agents. **Per spec §6 risk #5**: if this test fails once, re-run 2 more times before declaring flake. Three consecutive failures = real bug, block tag, root-cause in `--tick-once` synchrony.

- [ ] **G.2: Grade-A grep gate**:
  ```bash
  grep -rn 'errors.New(' internal/orchestrator/ internal/program/ | grep -v '^internal/orchestrator/errors.go:'
  ```
  Expected: zero matches.

- [ ] **G.3: Coverage check**:
  ```bash
  go test -cover ./internal/orchestrator/state/...
  go test -cover ./internal/program/...
  go test -cover ./cmd/regatta/...
  ```
  Expected: ≥ 90% / 85% / 70%.

- [ ] **G.4: Race**:
  ```bash
  go test -race ./...
  ```
  Expected: no race warnings.

- [ ] **G.5: Lint + vet**:
  ```bash
  make ci-check
  ```
  Expected: exit 0.

- [ ] **G.6: A+ stretch** (optional but record outcome): mutation test on migrate.go, MTTD walkthrough timing, corrupted-DB recovery procedure smoke.

---

## Cross-cutting reminders

- **No backwards-compat shims**: `UpsertPending` survives in `state/` only because `scheduler.Tick` still calls it. If a follow-up wave proves no caller wants it, delete it cleanly — do not keep deprecated wrappers.
- **Caveman → normal in code/commits/docs**: per repo memory rule, every commit message and doc paragraph in this plan is written in full English, not caveman. Code comments follow the WHY-not-WHAT rule.
- **No AI signatures**: per repo memory rule, commits never include `Co-Authored-By: Claude` or similar footers.
- **Self-validate**: per repo memory rule, before opening any PR for review, the executing engineer runs every command shown in the relevant Task. No "should work" claims without log evidence.
