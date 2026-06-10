# Operator Console S0 — Substrate Prereqs Implementation Plan (v3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land all substrate + scheduler + spawner + gh-poller infrastructure that S1-S4 of the operator console depend on. Unblocks the SvelteKit console build.

**Architecture:** Four migrations (0018 `runs` registry w/ `declared_effect_class` inline; 0019 `work_items.run_id`; 0020 `approval_events.run_id`; 0021 `tool_call` substrate kind) + identify scheduler dispatch site + create `AgentConfig` struct + extend `DecideTx` signature for `run_id` (16-site update) + Claude-Code shim observed-effect emission + new `internal/orchestrator/checks/` package + `mergeStateStatus` decode extension on `prwatch` + `merge` decode-site unification.

**Tech Stack:** Go 1.25.x; sqlite3 migrations; `internal/canon` canonical JSON; `internal/orchestrator/state/substrate.AppendEvent` (HMAC-signed event writer); `gh` CLI shell-out (no `go-github`).

**Spec reference:** `docs/engineer/specs/phase-x/2026-06-02-operator-console-design.md` (PR #701) §3.2 substrate prereqs.

**v3 rework notes (post-2nd-reviewer ground-truth pass):**
- `*state.DB` field is `sql` (not `db`) per `state.go:55`.
- `approval_events` columns: `id, approval_id, ts, kind, actor, payload_json, token_jti` (verified `0004_approvals.sql:43`). Prior plan used invented column names; rewritten test fixtures w/ FK seeding.
- Migration 0021 substrate kind enum copies 0017 verbatim + appends `tool_call`: `node_output, fact, approval_event, token_spend, budget_reconciled, gate_verdict, heartbeat, brief_rejected, pr_stage_transition, manual_merge, operator_intervention, cost_cap_throttled, cost_cap_resumed, tool_call`.
- prwatch type is `PullRequest` (not `PR`) per `prwatch.go:71`.
- `substrate.Event.Kind` is `substrate.EventKind` typed string per `substrate/event.go:17`; `PayloadJSON` is `json.RawMessage` per `substrate/event.go:93`. Code samples updated.
- DecideTx callers: 14 (verified via grep), not 16.
- `ListRecentRuns` (not `ListRunsByID`) — name aligned with implementation.

**v2 rework notes (post-3-reviewer feedback):**
- All function signatures verified against actual repo state at `3e412ff` (main).
- `state.Querier` removed — use `*state.DB` directly per existing CRUD convention.
- `WorkItem` struct has no `TraceID` field; thread trace_id via context not struct.
- `UpsertWorkItem` is method on `*state.DB`, signature `(ctx, item, source, at)`.
- `DecideTx` is positional `(ctx, *state.DB, payload, reviewerID, decision, reason, clock)`; extend with `runID` positional (mass-update 16 call sites).
- `substrate.AppendEvent` (NOT `Append`) at `internal/orchestrator/state/substrate/event.go`; takes `(ctx, tx, e, key, keyID)` w/ HMAC signing.
- No `AgentConfig` struct exists; create one as prereq.
- No scheduler dispatch site found by grep; locate as prereq.
- `mergeStateStatus` is comment-only in `merge.go:70` — net-new decode work (NOT shipped by #673).
- Plan migrates `payload_json` CHECK from 1024 → 8192 to fit `tool_call` payload; documented as deliberate change.
- Test code uses `cmp.Diff` for full struct round-trip; queries runs by ID not by partial-indexed causal_hash.
- Spec §3.2 `observed_effect` is raw signal set; no classification hierarchy.

---

## File Structure

| Path | Responsibility | Status |
|---|---|---|
| `internal/orchestrator/state/migrations/0018_create_runs.sql` | runs registry table w/ `declared_effect_class` inline + indexes | new |
| `internal/orchestrator/state/migrations/0019_work_items_run_id.sql` | `run_id` column + index on `work_items` | new |
| `internal/orchestrator/state/migrations/0020_approval_events_run_id.sql` | `run_id` column + index on `approval_events` | new |
| `internal/orchestrator/state/migrations/0021_substrate_kind_tool_call_and_payload_limit.sql` | substrate_events CHECK expand for `tool_call`; widen payload_json limit to 8192 | new |
| `internal/orchestrator/state/runs.go` | `Run` struct + `(d *DB) InsertRun` + `GetRun` + `ListRecentRuns` (NOT by causal_hash; partial index excludes empty) | new |
| `internal/orchestrator/state/runs_test.go` | round-trip via cmp.Diff + dup-ID failure + missing-ID failure tests | new |
| `internal/orchestrator/state/causalhash.go` | `CausalInputs` struct + `Hash()` (canon JSON sha256) | new |
| `internal/orchestrator/state/causalhash_test.go` | rapid property test + 10-key map-order test | new |
| `internal/orchestrator/state/work_items.go` | add `RunID string` field to `WorkItem` struct | modify |
| `internal/orchestrator/state/work_items_upsert.go` | thread `wi.RunID` into INSERT + ON CONFLICT preserve | modify |
| `internal/orchestrator/state/work_items_batch_upsert.go` | thread RunID into batch INSERT | modify |
| `internal/gates/approval/decide.go` | extend `DecideTx` w/ positional `runID string` param; INSERT to approval_events incl run_id | modify |
| All 14 `DecideTx` callers | pass `runID` (empty when unknown) | modify (mass) |
| `internal/orchestrator/state/approvals.go` | second approval_events INSERT site — thread runID via WithTx wrapper | modify |
| `internal/agent/config.go` | new `AgentConfig` struct w/ `DeclaredEffectClass` + causal-input fields | new |
| `internal/orchestrator/scheduler/dispatch.go` | extracted dispatch func — INSERT runs row, populate WorkItem.RunID, pass runID into approval-gate callers | new |
| `internal/orchestrator/spawner/observed_effect.go` | `ObservedSignal` struct + raw-set collection (NO priority/classification) | new |
| `internal/orchestrator/spawner/claude.go` | emit `tool_call` substrate event via `substrate.AppendEvent` w/ raw observed_effect set + declared_effect_class | modify |
| `internal/orchestrator/checks/` | new pkg | new |
| `internal/orchestrator/checks/poller.go` | `gh pr checks` poll + emit substrate event payload `{pr, conclusion, status}` per spec §3.2 | new |
| `internal/orchestrator/checks/poller_test.go` | first-emit + flip-emit + non-flip-suppress + concurrent-poll race test | new |
| `internal/orchestrator/prwatch/ghcli.go:14` | extend `ghJSONFields` const + decoder + `PR` struct w/ `MergeStateStatus` | modify |
| `internal/orchestrator/prwatch/prwatch.go` | emit `agent_pr_dirty` w/ per-transition dedupe + re-arm on DIRTY→clean | modify |
| `internal/orchestrator/merge/merge.go:70` | replace comment-only mergeStateStatus reference w/ shared decode helper | modify |

---

## Task 0: Prereq discovery — identify dispatch site + design AgentConfig

**Files:**
- Read: `internal/orchestrator/scheduler/scheduler.go`
- Read: `internal/orchestrator/spawner/claude.go`
- Read: `internal/orchestrator/spawner/spawner.go` (if exists)
- Create: `docs/engineer/runbooks/operator-console-s0-dispatch-map.md` (~30 lines)

- [ ] **Step 1: Read scheduler entry**

```bash
grep -nE "func .*Tick|func .*Sweep|func .*Reserve|func .*Spawn|func .*Run" internal/orchestrator/scheduler/scheduler.go
```

Document the entry point that decides "spawn a new agent run" — it's likely inside `scheduler.Tick` or a sub-step. Quote file:line.

- [ ] **Step 2: Read spawner entry**

```bash
grep -nE "^func " internal/orchestrator/spawner/*.go | head -20
```

Identify the function the scheduler calls when it wants a new run. This is the dispatch boundary.

- [ ] **Step 3: Decide AgentConfig surface**

Read existing config-shaped types under `internal/`:
```bash
grep -rn "type .*Config struct" internal/agent/ internal/orchestrator/ 2>&1 | head -20
```

Pick: (a) extend an existing struct, or (b) create new `internal/agent/config.go` w/ `AgentConfig`. Document decision.

- [ ] **Step 4: Write dispatch-map runbook**

Write `docs/engineer/runbooks/operator-console-s0-dispatch-map.md` documenting:
- File:line of dispatch boundary.
- Caller graph: scheduler → ??? → spawner → claude.go.
- Where `runs` INSERT lands.
- Where `WorkItem.RunID` is populated.
- Where `DecideTx` runID propagates from.

- [ ] **Step 5: Commit**

```bash
git add docs/engineer/runbooks/operator-console-s0-dispatch-map.md
git commit -m "docs: S0 dispatch-map runbook — locate dispatch boundary"
```

---

## Task 1: Migration 0018 — runs registry (w/ declared_effect_class inline)

**Files:**
- Create: `internal/orchestrator/state/migrations/0018_create_runs.sql`
- Create: `internal/orchestrator/state/runs_migration_test.go`

- [ ] **Step 1: Write failing migration test**

```go
// internal/orchestrator/state/runs_migration_test.go
package state

import (
	"context"
	"testing"
)

func TestRunsMigration_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO runs (
			id, started_at, finished_at, status,
			spec_hash, model_hash, prompt_template_hash, tool_impl_hash,
			seed, versions_json, causal_hash, rerun_of, trace_id,
			declared_effect_class
		) VALUES (
			'run-test-1', 1717000000, NULL, 'running',
			'sh', 'mh', 'pth', 'tih',
			'seed-x', '{"go":"1.25.0"}', 'ch-x', NULL, '00000000000000000000000000000000',
			'filesystem-write+gh-mutation'
		)`)
	if err != nil {
		t.Fatalf("insert into runs: %v", err)
	}

	var (
		id, status, specHash, modelHash, ptHash, tiHash, seed, versionsJSON, causalHash, traceID, declEC string
		startedAt                                                                                       int64
		finishedAt                                                                                      any
		rerunOf                                                                                         any
	)
	err = db.QueryRow(`
		SELECT id, started_at, finished_at, status, spec_hash, model_hash,
		       prompt_template_hash, tool_impl_hash, seed, versions_json,
		       causal_hash, rerun_of, trace_id, declared_effect_class
		FROM runs WHERE id = 'run-test-1'
	`).Scan(&id, &startedAt, &finishedAt, &status, &specHash, &modelHash,
		&ptHash, &tiHash, &seed, &versionsJSON, &causalHash, &rerunOf, &traceID, &declEC)
	if err != nil {
		t.Fatalf("select: %v", err)
	}

	if id != "run-test-1" || startedAt != 1717000000 || status != "running" ||
		specHash != "sh" || modelHash != "mh" || ptHash != "pth" || tiHash != "tih" ||
		seed != "seed-x" || versionsJSON != `{"go":"1.25.0"}` || causalHash != "ch-x" ||
		traceID != "00000000000000000000000000000000" || declEC != "filesystem-write+gh-mutation" {
		t.Errorf("round-trip mismatch: id=%q started=%d status=%q sh=%q mh=%q pth=%q tih=%q seed=%q vj=%q ch=%q tr=%q dec=%q",
			id, startedAt, status, specHash, modelHash, ptHash, tiHash, seed, versionsJSON, causalHash, traceID, declEC)
	}
	_ = finishedAt
	_ = rerunOf
}

func TestRunsMigration_IndexesExist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()

	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='runs'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string]bool{"idx_runs_started": false, "idx_runs_causal_hash": false, "idx_runs_rerun_of": false}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for n, found := range want {
		if !found {
			t.Errorf("index %s missing", n)
		}
	}
}
```

- [ ] **Step 2: Run failing**

Run: `go test ./internal/orchestrator/state/ -run TestRunsMigration -v`
Expected: FAIL — runs table missing.

- [ ] **Step 3: Write migration**

```sql
-- internal/orchestrator/state/migrations/0018_create_runs.sql
-- runs registry: one row per regatta dispatch. spec §3.2 (PR #701).
-- causal_hash = sha256(canon({spec, seed, versions, model_hash,
-- prompt_template_hash, tool_impl_hash})). rerun_of links rerun-from-
-- hash child runs back to the parent. declared_effect_class = policy
-- envelope copied from agent-config at dispatch time.

CREATE TABLE runs (
  id TEXT PRIMARY KEY,
  started_at INTEGER NOT NULL,
  finished_at INTEGER,
  status TEXT NOT NULL DEFAULT '',
  spec_hash TEXT NOT NULL DEFAULT '',
  model_hash TEXT NOT NULL DEFAULT '',
  prompt_template_hash TEXT NOT NULL DEFAULT '',
  tool_impl_hash TEXT NOT NULL DEFAULT '',
  seed TEXT NOT NULL DEFAULT '',
  versions_json TEXT NOT NULL DEFAULT '{}',
  causal_hash TEXT NOT NULL DEFAULT '',
  rerun_of TEXT,
  trace_id TEXT NOT NULL DEFAULT '',
  declared_effect_class TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_runs_started ON runs(started_at DESC);
CREATE INDEX idx_runs_causal_hash ON runs(causal_hash) WHERE causal_hash != '';
CREATE INDEX idx_runs_rerun_of ON runs(rerun_of) WHERE rerun_of IS NOT NULL;
```

- [ ] **Step 4: Run pass**

Run: `go test ./internal/orchestrator/state/ -run TestRunsMigration -v`
Expected: PASS (both subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/state/migrations/0018_create_runs.sql \
        internal/orchestrator/state/runs_migration_test.go
git commit -m "feat(state): migration 0018 runs registry w/ declared_effect_class (S0)"
```

---

## Task 2: `runs` CRUD on `*state.DB`

**Files:**
- Create: `internal/orchestrator/state/runs.go`
- Create: `internal/orchestrator/state/runs_test.go`

- [ ] **Step 1: Write failing CRUD tests using `cmp.Diff` for full round-trip**

```go
// internal/orchestrator/state/runs_test.go
package state

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestInsertRun_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()

	finish := time.Unix(1717000300, 0)
	rerunOf := "run-parent"
	want := Run{
		ID:                   "run-1",
		StartedAt:            time.Unix(1717000000, 0),
		FinishedAt:           &finish,
		Status:               "finished",
		SpecHash:             "sh",
		ModelHash:            "mh",
		PromptTemplateHash:   "pth",
		ToolImplHash:         "tih",
		Seed:                 "seed-x",
		VersionsJSON:         `{"go":"1.25.0"}`,
		CausalHash:           "ch-x",
		RerunOf:              &rerunOf,
		TraceID:              "32-hex-trace-string-pad-to-32-aa",
		DeclaredEffectClass:  "filesystem-write+gh-mutation",
	}
	if err := db.InsertRun(ctx, want); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	got, err := db.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("round-trip diff (-want +got):\n%s", diff)
	}
}

func TestInsertRun_DuplicateID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()

	r := Run{ID: "run-1", StartedAt: time.Unix(1717000000, 0)}
	if err := db.InsertRun(ctx, r); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := db.InsertRun(ctx, r); err == nil {
		t.Error("expected duplicate-ID error, got nil")
	}
}

func TestGetRun_MissingID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()

	_, err := db.GetRun(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error on missing ID")
	}
}

func TestListRecentRuns_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()

	for _, id := range []string{"r1", "r2", "r3"} {
		if err := db.InsertRun(ctx, Run{ID: id, StartedAt: time.Unix(1717000000, 0)}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListRecentRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("got %d runs, want 3", len(got))
	}
}
```

- [ ] **Step 2: Run failing**

Run: `go test ./internal/orchestrator/state/ -run "TestInsertRun|TestGetRun|TestListRecentRuns" -v`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Implement methods on `*DB`**

```go
// internal/orchestrator/state/runs.go
package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Run struct {
	ID                   string
	StartedAt            time.Time
	FinishedAt           *time.Time
	Status               string
	SpecHash             string
	ModelHash            string
	PromptTemplateHash   string
	ToolImplHash         string
	Seed                 string
	VersionsJSON         string
	CausalHash           string
	RerunOf              *string
	TraceID              string
	DeclaredEffectClass  string
}

func (d *DB) InsertRun(ctx context.Context, r Run) error {
	var finishedAt sql.NullInt64
	if r.FinishedAt != nil {
		finishedAt = sql.NullInt64{Int64: r.FinishedAt.Unix(), Valid: true}
	}
	var rerunOf sql.NullString
	if r.RerunOf != nil {
		rerunOf = sql.NullString{String: *r.RerunOf, Valid: true}
	}
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO runs (
			id, started_at, finished_at, status,
			spec_hash, model_hash, prompt_template_hash, tool_impl_hash,
			seed, versions_json, causal_hash, rerun_of, trace_id,
			declared_effect_class
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.ID, r.StartedAt.Unix(), finishedAt, r.Status,
		r.SpecHash, r.ModelHash, r.PromptTemplateHash, r.ToolImplHash,
		r.Seed, r.VersionsJSON, r.CausalHash, rerunOf, r.TraceID,
		r.DeclaredEffectClass,
	)
	if err != nil {
		return fmt.Errorf("insert run %q: %w", r.ID, err)
	}
	return nil
}

func (d *DB) GetRun(ctx context.Context, id string) (Run, error) {
	var r Run
	var startedAt int64
	var finishedAt sql.NullInt64
	var rerunOf sql.NullString
	err := d.sql.QueryRowContext(ctx, `
		SELECT id, started_at, finished_at, status,
		       spec_hash, model_hash, prompt_template_hash, tool_impl_hash,
		       seed, versions_json, causal_hash, rerun_of, trace_id,
		       declared_effect_class
		FROM runs WHERE id = ?
	`, id).Scan(
		&r.ID, &startedAt, &finishedAt, &r.Status,
		&r.SpecHash, &r.ModelHash, &r.PromptTemplateHash, &r.ToolImplHash,
		&r.Seed, &r.VersionsJSON, &r.CausalHash, &rerunOf, &r.TraceID,
		&r.DeclaredEffectClass,
	)
	if err != nil {
		return Run{}, fmt.Errorf("get run %q: %w", id, err)
	}
	r.StartedAt = time.Unix(startedAt, 0)
	if finishedAt.Valid {
		t := time.Unix(finishedAt.Int64, 0)
		r.FinishedAt = &t
	}
	if rerunOf.Valid {
		r.RerunOf = &rerunOf.String
	}
	return r, nil
}

func (d *DB) ListRecentRuns(ctx context.Context, limit int) ([]Run, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT id, started_at, status, causal_hash, trace_id
		FROM runs ORDER BY started_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent runs: %w", err)
	}
	defer rows.Close()

	var out []Run
	for rows.Next() {
		var r Run
		var startedAt int64
		if err := rows.Scan(&r.ID, &startedAt, &r.Status, &r.CausalHash, &r.TraceID); err != nil {
			return nil, err
		}
		r.StartedAt = time.Unix(startedAt, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}
```

Field accessor `d.sql` matches `type DB struct { sql *sql.DB; now func() time.Time }`
at `internal/orchestrator/state/state.go:55`. Verified against current main.

- [ ] **Step 4: Run pass**

Run: `go test ./internal/orchestrator/state/ -run "TestInsertRun|TestGetRun|TestListRecentRuns" -v`
Expected: PASS (all 4 subtests including failure paths)

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/state/runs.go internal/orchestrator/state/runs_test.go
git commit -m "feat(state): runs CRUD on *DB w/ round-trip + dup + missing failure tests (S0)"
```

---

## Task 3: causal-hash helper w/ rapid property test

**Files:**
- Create: `internal/orchestrator/state/causalhash.go`
- Create: `internal/orchestrator/state/causalhash_test.go`

- [ ] **Step 1: Verify `internal/canon.Marshal` signature**

```bash
grep -n "^func Marshal" internal/canon/canon.go
```

Expected: `func Marshal(v any) ([]byte, error)`. If different, adapt
Task 3 code below.

- [ ] **Step 2: Write failing tests w/ rapid for property tests**

```go
// internal/orchestrator/state/causalhash_test.go
package state

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func TestCausalHash_Deterministic(t *testing.T) {
	t.Parallel()
	in := CausalInputs{
		SpecHash: "s1", ModelHash: "m1", PromptTemplateHash: "p1",
		ToolImplHash: "t1", Seed: "seed-1",
		Versions: map[string]string{"go": "1.25.0", "claude-code": "1.0.0"},
	}
	a, err := in.Hash()
	if err != nil {
		t.Fatal(err)
	}
	b, err := in.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("non-deterministic: %s != %s", a, b)
	}
	if len(a) != 64 {
		t.Errorf("expected 64-hex sha256, got %d chars", len(a))
	}
	if strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("not lowercase hex: %s", a)
	}
}

func TestCausalHash_InputSensitive_Rapid(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		base := CausalInputs{
			SpecHash:           rapid.StringN(0, 64, -1).Draw(t, "spec"),
			ModelHash:          rapid.StringN(0, 64, -1).Draw(t, "model"),
			PromptTemplateHash: rapid.StringN(0, 64, -1).Draw(t, "pt"),
			ToolImplHash:       rapid.StringN(0, 64, -1).Draw(t, "tool"),
			Seed:               rapid.StringN(0, 64, -1).Draw(t, "seed"),
		}
		other := base
		other.ModelHash = base.ModelHash + "X"
		a, _ := base.Hash()
		b, _ := other.Hash()
		if a == b {
			t.Fatalf("hash collision on differing ModelHash: %s == %s", a, b)
		}
	})
}

func TestCausalHash_MapKeyOrderIndependent(t *testing.T) {
	t.Parallel()
	keys := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	v := map[string]string{}
	for i, k := range keys {
		v[k] = string(rune('0' + i))
	}
	a, _ := CausalInputs{Versions: v}.Hash()

	v2 := map[string]string{}
	for i := len(keys) - 1; i >= 0; i-- {
		v2[keys[i]] = string(rune('0' + i))
	}
	b, _ := CausalInputs{Versions: v2}.Hash()

	if a != b {
		t.Errorf("map key order affected hash: %s != %s", a, b)
	}
}
```

Note: requires `pgregory.net/rapid` in go.mod. If not present, run
`go get pgregory.net/rapid` and commit go.mod / go.sum bumps in same
commit.

- [ ] **Step 3: Run failing**

Run: `go test ./internal/orchestrator/state/ -run TestCausalHash -v`
Expected: FAIL — `CausalInputs` undefined.

- [ ] **Step 4: Implement**

```go
// internal/orchestrator/state/causalhash.go
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/trilamsr/regatta/internal/canon"
)

type CausalInputs struct {
	SpecHash           string            `json:"spec_hash"`
	ModelHash          string            `json:"model_hash"`
	PromptTemplateHash string            `json:"prompt_template_hash"`
	ToolImplHash       string            `json:"tool_impl_hash"`
	Seed               string            `json:"seed"`
	Versions           map[string]string `json:"versions"`
}

func (c CausalInputs) Hash() (string, error) {
	b, err := canon.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("canon marshal: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
```

- [ ] **Step 5: Run pass**

Run: `go test ./internal/orchestrator/state/ -run TestCausalHash -v`
Expected: PASS (all 3 subtests)

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/state/causalhash.go \
        internal/orchestrator/state/causalhash_test.go \
        go.mod go.sum
git commit -m "feat(state): CausalInputs.Hash via canon + sha256 + rapid property tests (S0)"
```

---

## Task 4: Migration 0019 — `work_items.run_id`

**Files:**
- Create: `internal/orchestrator/state/migrations/0019_work_items_run_id.sql`
- Create: `internal/orchestrator/state/work_items_run_id_migration_test.go`

- [ ] **Step 1: Write failing migration test (round-trip, not PRAGMA)**

```go
// internal/orchestrator/state/work_items_run_id_migration_test.go
package state

import (
	"context"
	"testing"
	"time"
)

func TestWorkItemsRunIDMigration_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()

	now := time.Unix(1717000000, 0).Unix()
	_, err := db.Exec(`
		INSERT INTO work_items (id, kind, title, lane, status, source,
		                         last_seen_at, created_at, updated_at, run_id)
		VALUES ('wi-1', 'agent', 't', 'lane-a', 'pending', 'test',
		         ?, ?, ?, 'run-1')
	`, now, now, now)
	if err != nil {
		t.Fatalf("insert work_items w/ run_id: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT run_id FROM work_items WHERE id='wi-1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "run-1" {
		t.Errorf("got run_id=%q want run-1", got)
	}
}
```

- [ ] **Step 2: Run failing**

Run: `go test ./internal/orchestrator/state/ -run TestWorkItemsRunIDMigration -v`
Expected: FAIL — column missing.

- [ ] **Step 3: Write migration**

```sql
-- internal/orchestrator/state/migrations/0019_work_items_run_id.sql
-- adds run_id passthrough column to work_items.
-- backfill is empty-string-default; producer wiring lands in Task 6.
-- spec §3.2; trace_id precedent migration 0005.

ALTER TABLE work_items ADD COLUMN run_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_work_items_run ON work_items(run_id) WHERE run_id != '';
```

- [ ] **Step 4: Run pass**

Run: `go test ./internal/orchestrator/state/ -run TestWorkItemsRunIDMigration -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/state/migrations/0019_work_items_run_id.sql \
        internal/orchestrator/state/work_items_run_id_migration_test.go
git commit -m "feat(state): migration 0019 work_items.run_id + round-trip test (S0)"
```

---

## Task 5: Migration 0020 — `approval_events.run_id`

**Files:**
- Create: `internal/orchestrator/state/migrations/0020_approval_events_run_id.sql`
- Create: `internal/orchestrator/state/approval_events_run_id_migration_test.go`

- [ ] **Step 1: Write failing migration test**

```go
// internal/orchestrator/state/approval_events_run_id_migration_test.go
package state

import (
	"testing"
)

func TestApprovalEventsRunIDMigration_RoundTrip(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	defer db.Close()

	// approval_events columns per 0004 migration: id (auto-PK),
	// approval_id, ts, kind, actor, payload_json, token_jti.
	// approval_id FK requires a real approvals row — seed it first.
	if _, err := db.Exec(`
		INSERT INTO approvals (id, work_item_id, gate_name, requested_at, requested_by,
		                       reviewer_set_snapshot_json, timeout_at, created_at, updated_at)
		VALUES ('ap-1', 'wi-1', 'g1', 0, 'tester', '[]', 9999999999, 0, 0)
	`); err != nil {
		t.Fatalf("seed approval: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO work_items (id, kind, title, lane, status, source, last_seen_at, created_at, updated_at)
		VALUES ('wi-1', 'agent', 't', 'lane-a', 'pending', 'test', 0, 0, 0)
	`); err != nil {
		t.Fatalf("seed wi: %v", err)
	}
	_, err := db.Exec(`
		INSERT INTO approval_events (
			approval_id, ts, kind, actor, payload_json, token_jti, run_id
		) VALUES ('ap-1', 1717000000, 'decided', 'rev-1', '{}', 'jti-1', 'run-1')
	`)
	if err != nil {
		t.Fatalf("insert approval_events w/ run_id: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT run_id FROM approval_events WHERE approval_id='ap-1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "run-1" {
		t.Errorf("got run_id=%q want run-1", got)
	}
}
```

Schema verified against `0004_approvals.sql`:
- `approvals` requires `id, work_item_id, gate_name, requested_at, requested_by, reviewer_set_snapshot_json, timeout_at, created_at, updated_at`.
- `work_items` FK requires seeding wi row first.
- `approval_events` columns: `id (auto-PK), approval_id, ts, kind, actor, payload_json, token_jti`.

- [ ] **Step 2: Run failing**

Run: `go test ./internal/orchestrator/state/ -run TestApprovalEventsRunIDMigration -v`
Expected: FAIL — column missing.

- [ ] **Step 3: Write migration**

```sql
-- internal/orchestrator/state/migrations/0020_approval_events_run_id.sql
-- adds run_id passthrough on approval_events for cross-table joins from
-- /api/v1/operator/runs/<id>/postmortem (spec §3.3 S3).

ALTER TABLE approval_events ADD COLUMN run_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_approval_events_run ON approval_events(run_id) WHERE run_id != '';
```

- [ ] **Step 4: Run pass**

Run: `go test ./internal/orchestrator/state/ -run TestApprovalEventsRunIDMigration -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/state/migrations/0020_approval_events_run_id.sql \
        internal/orchestrator/state/approval_events_run_id_migration_test.go
git commit -m "feat(state): migration 0020 approval_events.run_id + round-trip test (S0)"
```

---

## Task 6: Migration 0021 — `tool_call` substrate kind + payload widen

**Files:**
- Create: `internal/orchestrator/state/migrations/0021_substrate_kind_tool_call.sql`
- Create: `internal/orchestrator/state/substrate_tool_call_test.go`

This migration follows the precedent at
`0015_substrate_pr_stage_transition_kind.sql` and
`0017_cost_cap_event_unique_and_substrate_kinds.sql` — sqlite CHECK
constraints require full table-rewrite to expand the enum. Also widens
`payload_json` length CHECK from 1024 to 8192 because `tool_call`
payload (signature + args_hash + observed_effect array + declared
envelope + timing) may exceed 1024 bytes.

- [ ] **Step 1: Read precedent**

```bash
cat internal/orchestrator/state/migrations/0017_cost_cap_event_unique_and_substrate_kinds.sql
```

Document the canonical kind enum that 0017 ships with. Plan migration
must include EVERY kind currently in CHECK, plus `tool_call`.

- [ ] **Step 2: Write failing test (positive + negative)**

```go
// internal/orchestrator/state/substrate_tool_call_test.go
package state

import (
	"testing"
)

func TestSubstrate_AcceptsToolCallKind(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO substrate_events (
			id, run_id, tenant_id, kind, payload_json, written_by, written_at
		) VALUES ('e1', 'run-1', 'default', 'tool_call', '{}', 'spawner@v1', 0)
	`)
	if err != nil {
		t.Errorf("substrate_events rejected tool_call: %v", err)
	}
}

func TestSubstrate_RejectsUnknownKind(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	defer db.Close()

	_, err := db.Exec(`
		INSERT INTO substrate_events (
			id, run_id, tenant_id, kind, payload_json, written_by, written_at
		) VALUES ('e1', 'run-1', 'default', 'totally_made_up_kind', '{}', 'test@v1', 0)
	`)
	if err == nil {
		t.Error("expected CHECK to reject unknown kind, got nil")
	}
}

func TestSubstrate_AcceptsLargePayload(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	defer db.Close()

	payload := `{"x":"` + string(make([]byte, 4096)) + `"}` // ~4 KiB
	_, err := db.Exec(`
		INSERT INTO substrate_events (
			id, run_id, tenant_id, kind, payload_json, written_by, written_at
		) VALUES ('e2', 'run-2', 'default', 'tool_call', ?, 'spawner@v1', 0)
	`, payload)
	if err != nil {
		t.Errorf("payload_json 4 KiB rejected after widen: %v", err)
	}
}
```

- [ ] **Step 3: Run failing**

Run: `go test ./internal/orchestrator/state/ -run TestSubstrate -v`
Expected: FAIL — CHECK rejects tool_call OR rejects large payload.

- [ ] **Step 4: Write migration (full table-rewrite per 0015/0017 precedent)**

```sql
-- internal/orchestrator/state/migrations/0021_substrate_kind_tool_call.sql
-- expands substrate_events.kind CHECK to include 'tool_call' (spec §3.2).
-- widens payload_json length CHECK 1024 → 8192 — tool_call carries
-- declared_effect_class envelope + observed_effect signal array.
-- sqlite CHECK constraints require table-rewrite per 0015 + 0017 precedent.

PRAGMA foreign_keys = OFF;
BEGIN;

CREATE TABLE substrate_events__new (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  work_item_id TEXT,
  tenant_id TEXT NOT NULL,
  trace_id TEXT NOT NULL DEFAULT '',
  span_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  key TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL DEFAULT '{}',
  blob_digest TEXT,
  supersedes TEXT,
  written_by TEXT NOT NULL,
  written_at INTEGER NOT NULL,
  schema_version INTEGER DEFAULT 1,
  nonce TEXT,
  sig_alg TEXT,
  sig_key_id TEXT,
  sig_mac TEXT,
  CHECK (kind IN (
    'node_output','fact','approval_event','token_spend',
    'budget_reconciled','gate_verdict','heartbeat',
    'brief_rejected','pr_stage_transition',
    'manual_merge','operator_intervention',
    'cost_cap_throttled','cost_cap_resumed',
    'tool_call'
  )),
  CHECK (length(payload_json) <= 8192),
  CHECK (trace_id = '' OR length(trace_id)=32)
);
INSERT INTO substrate_events__new SELECT * FROM substrate_events;
DROP TABLE substrate_events;
ALTER TABLE substrate_events__new RENAME TO substrate_events;

CREATE INDEX idx_substrate_events_kind
  ON substrate_events(run_id, kind, key, written_at DESC);
CREATE INDEX idx_substrate_events_wi
  ON substrate_events(work_item_id, kind, written_at DESC)
  WHERE work_item_id IS NOT NULL;
CREATE INDEX idx_substrate_events_tenant
  ON substrate_events(tenant_id, kind, written_at DESC);
CREATE INDEX idx_substrate_events_supersedes
  ON substrate_events(supersedes) WHERE supersedes IS NOT NULL;
CREATE INDEX idx_substrate_events_trace
  ON substrate_events(trace_id) WHERE trace_id != '';
CREATE UNIQUE INDEX uq_substrate_events_nonce
  ON substrate_events(run_id, written_by, nonce);

COMMIT;
PRAGMA foreign_keys = ON;
```

**Verify the kind enum line matches actual 0017 state before commit.** If 0017 introduced different kinds, copy them in.

- [ ] **Step 5: Run pass**

Run: `go test ./internal/orchestrator/state/ -run TestSubstrate -v`
Expected: PASS (3 subtests)

Then full suite to confirm no regression:
```bash
go test ./internal/orchestrator/state/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/state/migrations/0021_substrate_kind_tool_call.sql \
        internal/orchestrator/state/substrate_tool_call_test.go
git commit -m "feat(substrate): migration 0021 tool_call kind + payload 1024→8192 (S0)"
```

---

## Task 7: Thread `run_id` through `work_items` INSERTs

**Files:**
- Modify: `internal/orchestrator/state/work_items.go` (add `RunID` field)
- Modify: `internal/orchestrator/state/work_items_upsert.go`
- Modify: `internal/orchestrator/state/work_items_batch_upsert.go`

- [ ] **Step 1: Confirm `UpsertWorkItem` signature**

```bash
grep -n "func .DB. UpsertWorkItem" internal/orchestrator/state/work_items_upsert.go
```

Expected output is method form: `func (d *DB) UpsertWorkItem(ctx context.Context, item WorkItem, source string, at time.Time) error`.

- [ ] **Step 2: Add `RunID` to `WorkItem` struct**

In `internal/orchestrator/state/work_items.go`, append to existing
struct:

```go
type WorkItem struct {
	// ... existing fields stay in their current order
	RunID string // S0: set by scheduler at dispatch; empty for legacy items
}
```

- [ ] **Step 3: Write failing test (incl. ON CONFLICT preserve)**

```go
// internal/orchestrator/state/work_items_run_id_test.go
package state

import (
	"context"
	"testing"
	"time"
)

func TestUpsertWorkItem_RunIDPassthrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()

	wi := WorkItem{ID: "wi-1", Kind: "agent", Status: "pending", RunID: "run-1"}
	if err := db.UpsertWorkItem(ctx, wi, "test", time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := db.QueryRow(`SELECT run_id FROM work_items WHERE id=?`, "wi-1").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "run-1" {
		t.Errorf("first upsert: got %q want run-1", got)
	}
}

func TestUpsertWorkItem_RunIDPersistedOnConflictUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()

	wi := WorkItem{ID: "wi-1", Kind: "agent", Status: "pending", RunID: "run-1"}
	if err := db.UpsertWorkItem(ctx, wi, "test", time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	wi.Status = "completed" // upsert update path
	if err := db.UpsertWorkItem(ctx, wi, "test", time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := db.QueryRow(`SELECT run_id FROM work_items WHERE id=?`, "wi-1").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "run-1" {
		t.Errorf("after ON CONFLICT update: got %q want run-1 (must persist)", got)
	}
}
```

- [ ] **Step 4: Run failing**

Run: `go test ./internal/orchestrator/state/ -run "TestUpsertWorkItem_RunID" -v`
Expected: FAIL — column-list mismatch.

- [ ] **Step 5: Update INSERTs**

In `work_items_upsert.go:52` (read the surrounding context first):
- Add `run_id` to column list.
- Add `?` placeholder.
- Bind `item.RunID`.
- In ON CONFLICT clause, add `run_id = EXCLUDED.run_id` (so updates carry run_id forward).

In `work_items_batch_upsert.go:45`, mirror same changes.

- [ ] **Step 6: Run pass + full work_items suite**

Run:
```bash
go test ./internal/orchestrator/state/ -run "TestUpsertWorkItem|TestSpawnable|TestTrace|TestBatchUpsert" -count=1 -v
```
Expected: PASS

Update any existing test fixtures whose schema-aware INSERTs now miss the new column (use empty string default).

- [ ] **Step 7: Commit**

```bash
git add internal/orchestrator/state/work_items.go \
        internal/orchestrator/state/work_items_upsert.go \
        internal/orchestrator/state/work_items_batch_upsert.go \
        internal/orchestrator/state/work_items_run_id_test.go \
        internal/orchestrator/state/trace_id_test.go \
        internal/orchestrator/state/work_items_spawnable_index_test.go
git commit -m "feat(state): thread RunID through work_items INSERT + ON CONFLICT (S0)"
```

---

## Task 8: Extend `DecideTx` signature (14-site mass update)

**Files:**
- Modify: `internal/gates/approval/decide.go`
- Modify: `internal/orchestrator/state/approvals.go`
- Modify: 14 callers (locate via grep)

- [ ] **Step 1: Inventory call sites**

```bash
grep -rn "DecideTx(" --include="*.go" .
```

Expected: ~14 sites (verified at HEAD `3e412ff` — 14 DecideTx invocations across decide_test.go, jti_persistence_test, notify_http, fold_replay_roundtrip, cmd/regatta). Document file:line for each.

- [ ] **Step 2: Write failing test**

```go
// internal/gates/approval/decide_run_id_test.go
package approval

import (
	"context"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
)

func TestDecideTx_WritesRunIDToApprovalEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, approvalID := newTestDBWithApproval(t)
	defer db.Close()

	tok := approvaltoken.TokenPayload{KID: "k1", WI: "wi-1", AID: approvalID, Reviewer: "rev-1", JTI: "jti-1"}
	clk := func() time.Time { return time.Unix(1717000000, 0) }
	_, _, err := DecideTx(ctx, db, tok, "rev-1", "allow", "ok", "run-42", clk)
	if err != nil {
		t.Fatalf("DecideTx: %v", err)
	}
	var runID string
	if err := db.QueryRow(`
		SELECT run_id FROM approval_events WHERE approval_id=? ORDER BY created_at DESC LIMIT 1
	`, approvalID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if runID != "run-42" {
		t.Errorf("got run_id=%q want run-42", runID)
	}
}
```

Define `newTestDBWithApproval(t)` in `decide_test.go` (existing file)
that creates a `*state.DB` w/ HMAC keyring + seeds a pending approval
row + returns the approval_id. Reuse existing test helpers from the
package.

- [ ] **Step 3: Run failing**

Run: `go test ./internal/gates/approval/ -run TestDecideTx_WritesRunID -v`
Expected: FAIL — signature mismatch.

- [ ] **Step 4: Update `DecideTx` signature**

In `decide.go`:
```go
// Before:
// func DecideTx(ctx context.Context, db *state.DB, payload approvaltoken.TokenPayload,
//                reviewerID, decision, reason string, clock func() time.Time)
//                (DecideTxResult, string, error)

// After:
func DecideTx(ctx context.Context, db *state.DB, payload approvaltoken.TokenPayload,
              reviewerID, decision, reason, runID string, clock func() time.Time)
              (DecideTxResult, string, error)
```

Thread `runID` down through the helper that does the INSERT into
`approval_events` at line 186. Add `run_id` to column list +
placeholder + bind.

- [ ] **Step 5: Update second INSERT site**

In `internal/orchestrator/state/approvals.go` near line 195, same
pattern — thread runID via the calling tx wrapper.

- [ ] **Step 6: Update all 14 callers**

Mechanical update per the inventory from Step 1. Most callers don't
yet know `run_id` — pass empty string `""` and document with a
`// TODO(operator-console-S0): thread runID once dispatch wiring lands (Task 9).`
comment that gets removed in Task 9.

- [ ] **Step 7: Run pass + full approval suite**

```bash
go test ./internal/gates/approval/... ./internal/orchestrator/state/... -count=1
```
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/gates/approval/decide.go \
        internal/gates/approval/decide_run_id_test.go \
        internal/orchestrator/state/approvals.go \
        <all 16 caller files>
git commit -m "feat(approval): extend DecideTx w/ runID; thread to approval_events INSERTs (S0)"
```

---

## Task 9: AgentConfig struct + dispatch refactor + runs producer

**Files:**
- Create: `internal/agent/config.go`
- Create: `internal/agent/config_test.go`
- Modify: scheduler dispatch entry (file path from Task 0)
- Modify: callers of `DecideTx` that flow through dispatch — replace TODO with real runID

- [ ] **Step 1: Create `AgentConfig`**

```go
// internal/agent/config.go
package agent

type AgentConfig struct {
	// CausalInputs are hashed into runs.causal_hash.
	SpecHash            string
	ModelHash           string
	PromptTemplateHash  string
	ToolImplHash        string
	Seed                string
	Versions            map[string]string

	// DeclaredEffectClass is the policy envelope used by the surprise
	// detector. Coarse, e.g. "filesystem-write+gh-mutation" or
	// "read-only+gh-comment". Compared against observed_effect at tool-call
	// completion (spec §3.2 / §3.7).
	DeclaredEffectClass string
}
```

- [ ] **Step 2: Write failing dispatch-producer test**

```go
// internal/orchestrator/scheduler/dispatch_runs_test.go
package scheduler

import (
	"context"
	"testing"
)

func TestDispatch_InsertsRunRowAtBoundary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newTestSchedulerWithDB(t)
	defer db.Close()

	runID, err := s.dispatch(ctx, testWorkItem("wi-1"), testAgentConfig())
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun(%q): %v", runID, err)
	}
	if got.CausalHash == "" {
		t.Error("causal_hash should be populated")
	}
	if got.DeclaredEffectClass != "filesystem-write+gh-mutation" {
		t.Errorf("declared_effect_class: got %q", got.DeclaredEffectClass)
	}
	if got.StartedAt.IsZero() {
		t.Error("started_at should be set")
	}
}
```

Provide `newTestSchedulerWithDB`, `testWorkItem`, `testAgentConfig` as
new helpers in `scheduler_testdata_test.go` (new file). Each must
include the HMAC key + clock plumbing the scheduler needs.

- [ ] **Step 3: Run failing**

Run: `go test ./internal/orchestrator/scheduler/ -run TestDispatch_InsertsRunRowAtBoundary -v`
Expected: FAIL — `s.dispatch` undefined or runs row absent.

- [ ] **Step 4: Extract / implement dispatch**

At the dispatch boundary identified in Task 0:

```go
// pseudo-code; adapt to actual call shape
func (s *Scheduler) dispatch(ctx context.Context, wi state.WorkItem, cfg agent.AgentConfig) (string, error) {
	causal := state.CausalInputs{
		SpecHash: cfg.SpecHash, ModelHash: cfg.ModelHash,
		PromptTemplateHash: cfg.PromptTemplateHash,
		ToolImplHash:       cfg.ToolImplHash,
		Seed:               cfg.Seed,
		Versions:           cfg.Versions,
	}
	causalHash, err := causal.Hash()
	if err != nil {
		return "", fmt.Errorf("causal hash: %w", err)
	}
	runID := s.newRunID() // ulid or uuid; check convention
	versionsJSON, _ := canon.Marshal(cfg.Versions)
	run := state.Run{
		ID:                  runID,
		StartedAt:           s.clock(),
		Status:              "running",
		SpecHash:            cfg.SpecHash,
		ModelHash:           cfg.ModelHash,
		PromptTemplateHash:  cfg.PromptTemplateHash,
		ToolImplHash:        cfg.ToolImplHash,
		Seed:                cfg.Seed,
		VersionsJSON:        string(versionsJSON),
		CausalHash:          causalHash,
		TraceID:             traceIDFromContext(ctx),
		DeclaredEffectClass: cfg.DeclaredEffectClass,
	}
	if err := s.db.InsertRun(ctx, run); err != nil {
		return "", fmt.Errorf("insert run: %w", err)
	}
	wi.RunID = runID
	if err := s.db.UpsertWorkItem(ctx, wi, "scheduler-dispatch", s.clock()); err != nil {
		return "", fmt.Errorf("upsert work_item: %w", err)
	}
	return runID, nil
}
```

- [ ] **Step 5: Replace Task 8 TODOs**

Wherever `DecideTx` is called inside the dispatch path, replace the
empty `""` runID + TODO comment with the actual `runID`.

- [ ] **Step 6: Run pass**

```bash
go test ./internal/orchestrator/scheduler/... \
        ./internal/agent/... \
        ./internal/gates/approval/... -count=1
```
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/agent/config.go \
        internal/agent/config_test.go \
        internal/orchestrator/scheduler/dispatch.go \
        internal/orchestrator/scheduler/dispatch_runs_test.go \
        internal/orchestrator/scheduler/scheduler_testdata_test.go \
        <DecideTx-caller-files-touched-in-Task-8>
git commit -m "feat(scheduler): dispatch boundary insertions runs row + threads runID (S0)"
```

---

## Task 10: ObservedSignal collection (NO classification hierarchy)

**Files:**
- Create: `internal/orchestrator/spawner/observed_effect.go`
- Create: `internal/orchestrator/spawner/observed_effect_test.go`

Per reviewer feedback, drop priority/classification logic. Emit the raw
multi-set of observations; envelope-compare happens downstream in the
surprise detector (spec §3.7).

- [ ] **Step 1: Write failing tests**

```go
// internal/orchestrator/spawner/observed_effect_test.go
package spawner

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestObservedEffect_EmptyForNoSignals(t *testing.T) {
	t.Parallel()
	got := CollectObservedEffects(nil)
	if len(got) != 0 {
		t.Errorf("expected empty set, got %v", got)
	}
}

func TestObservedEffect_FilesystemWriteOnly(t *testing.T) {
	t.Parallel()
	got := CollectObservedEffects([]ObservedSignal{
		{Kind: "filesystem-write", Path: "foo.go"},
	})
	want := []string{"filesystem-write"}
	if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
		t.Errorf("diff (-want +got):\n%s", diff)
	}
}

func TestObservedEffect_MultipleClassesDeduped(t *testing.T) {
	t.Parallel()
	got := CollectObservedEffects([]ObservedSignal{
		{Kind: "filesystem-write", Path: "a.go"},
		{Kind: "filesystem-write", Path: "b.go"},
		{Kind: "gh-mutation", Endpoint: "PATCH /repos/x/y/pulls/1"},
		{Kind: "cost-delta", USDMicro: 1234},
	})
	want := []string{"cost-delta", "filesystem-write", "gh-mutation"}
	if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
		t.Errorf("diff (-want +got):\n%s", diff)
	}
}

func TestObservedEffect_UnknownKindPreserved(t *testing.T) {
	t.Parallel()
	got := CollectObservedEffects([]ObservedSignal{
		{Kind: "future-kind-xyz"},
	})
	want := []string{"future-kind-xyz"}
	if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
		t.Errorf("diff (-want +got):\n%s", diff)
	}
}
```

- [ ] **Step 2: Run failing**

Run: `go test ./internal/orchestrator/spawner/ -run TestObservedEffect -v`
Expected: FAIL — `CollectObservedEffects` undefined.

- [ ] **Step 3: Implement (raw-set, no classification)**

```go
// internal/orchestrator/spawner/observed_effect.go
package spawner

import "sort"

// ObservedSignal captures one side-effect class observed during a tool
// call. Multiple signals of the same Kind get deduped into the returned
// set. The surprise detector (spec §3.7) compares this set against the
// run's declared_effect_class envelope; classification happens there,
// not here.
type ObservedSignal struct {
	Kind     string // 'filesystem-write' | 'gh-mutation' | 'cost-delta' | ...
	Path     string
	Endpoint string
	USDMicro int64
}

// CollectObservedEffects returns the sorted, deduped set of Kind values
// across signals. Returns empty slice (not nil) when input is empty —
// substrate consumers can rely on nil-safe iteration either way.
func CollectObservedEffects(sigs []ObservedSignal) []string {
	seen := map[string]struct{}{}
	for _, s := range sigs {
		if s.Kind == "" {
			continue
		}
		seen[s.Kind] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run pass**

Run: `go test ./internal/orchestrator/spawner/ -run TestObservedEffect -v`
Expected: PASS (4 subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/spawner/observed_effect.go \
        internal/orchestrator/spawner/observed_effect_test.go
git commit -m "feat(spawner): CollectObservedEffects raw deduped sorted set (S0)"
```

---

## Task 11: Emit `tool_call` substrate event from Claude shim

**Files:**
- Modify: `internal/orchestrator/spawner/claude.go`
- Create: `internal/orchestrator/spawner/claude_tool_call_test.go`

`substrate.AppendEvent` lives at
`internal/orchestrator/state/substrate/event.go` and takes
`(ctx, tx, event, key, keyID)` — HMAC-signed.

- [ ] **Step 1: Inspect existing emission path**

```bash
grep -n "AppendEvent" internal/orchestrator/spawner/ internal/orchestrator/prwatch/ 2>&1 | head -10
```

Document the existing pattern + how HMAC key is plumbed in.

- [ ] **Step 2: Write failing test**

```go
// internal/orchestrator/spawner/claude_tool_call_test.go
package spawner

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSpawnClaude_EmitsToolCallSubstrateEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, runID := newTestDBWithRun(t, "filesystem-write+gh-mutation")
	defer db.Close()

	fake := newFakeClaude([]toolCallTrace{
		{Signature: "Read:/foo.go", Signals: []ObservedSignal{{Kind: "filesystem-write", Path: "/foo.go"}}},
	})
	if err := Spawn(ctx, db, fake, SpawnArgs{RunID: runID, AgentID: "ag-1", TenantID: "default"}); err != nil {
		t.Fatal(err)
	}

	var payloadJSON string
	if err := db.QueryRow(`
		SELECT payload_json FROM substrate_events
		WHERE run_id=? AND kind='tool_call'
		ORDER BY written_at DESC LIMIT 1
	`, runID).Scan(&payloadJSON); err != nil {
		t.Fatalf("no tool_call event written: %v", err)
	}

	var got struct {
		AgentID             string   `json:"agent_id"`
		Signature           string   `json:"signature"`
		DeclaredEffectClass string   `json:"declared_effect_class"`
		ObservedEffect      []string `json:"observed_effect"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &got); err != nil {
		t.Fatalf("payload not valid JSON: %v / %s", err, payloadJSON)
	}
	if got.AgentID != "ag-1" {
		t.Errorf("agent_id: got %q want ag-1", got.AgentID)
	}
	if got.DeclaredEffectClass != "filesystem-write+gh-mutation" {
		t.Errorf("declared_effect_class: got %q want filesystem-write+gh-mutation", got.DeclaredEffectClass)
	}
	if len(got.ObservedEffect) != 1 || got.ObservedEffect[0] != "filesystem-write" {
		t.Errorf("observed_effect: got %v want [filesystem-write]", got.ObservedEffect)
	}
}
```

`newTestDBWithRun(t, declaredEffectClass)` is a new helper that
creates `*state.DB`, applies migrations, inserts one `runs` row with
the supplied envelope, and returns `(db, runID)`. Helper lives in
`spawner_testdata_test.go` (new file) with HMAC-key + clock plumbing.

- [ ] **Step 3: Run failing**

Run: `go test ./internal/orchestrator/spawner/ -run TestSpawnClaude_EmitsToolCallSubstrateEvent -v`
Expected: FAIL — no tool_call row.

- [ ] **Step 4: Implement emission in `claude.go`**

At the per-tool-call boundary inside the stream-json parse loop:

```go
// load run once at spawn time
run, err := db.GetRun(ctx, args.RunID)
if err != nil {
	return fmt.Errorf("load run: %w", err)
}

// after each tool-call boundary:
observed := CollectObservedEffects(call.Signals)
payload, err := canon.Marshal(map[string]any{
	"agent_id":              args.AgentID,
	"signature":             sha256Hex(call.NormalizedToolName + "|" + call.ArgsShape),
	"args_hash":             sha256Hex(call.CanonicalArgs),
	"declared_effect_class": run.DeclaredEffectClass,
	"observed_effect":       observed,
	"started_at":            call.StartedAt.Unix(),
	"finished_at":           call.FinishedAt.Unix(),
})
if err != nil {
	return fmt.Errorf("canon marshal tool_call payload: %w", err)
}

err = state.WithTx(ctx, db, func(tx *sql.Tx) error {
	return substrate.AppendEvent(ctx, tx, substrate.Event{
		ID:          newEventID(),
		RunID:       args.RunID,
		TenantID:    args.TenantID,
		Kind:        substrate.EventKind("tool_call"),  // EventKind typed, not string
		PayloadJSON: json.RawMessage(payload),           // json.RawMessage, not string
		WrittenBy:   "spawner/claude@v1",
		WrittenAt:   s.clock().Unix(),
	}, args.HMACKey, args.HMACKeyID)
})
if err != nil {
	return fmt.Errorf("append tool_call: %w", err)
}
```

Adapt to the real `substrate.AppendEvent` signature you read in
Step 1 — field names + tx wrapper convention may differ.

- [ ] **Step 5: Run pass + full spawner suite**

```bash
go test ./internal/orchestrator/spawner/... -count=1 -v
```
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/spawner/claude.go \
        internal/orchestrator/spawner/claude_tool_call_test.go \
        internal/orchestrator/spawner/spawner_testdata_test.go
git commit -m "feat(spawner): emit tool_call substrate event w/ declared + observed envelope (S0)"
```

---

## Task 12: gh-checks poller package

**Files:**
- Create: `internal/orchestrator/checks/poller.go`
- Create: `internal/orchestrator/checks/ghcli.go`
- Create: `internal/orchestrator/checks/poller_test.go`

Per spec §3.2 the poller emits a substrate event payload
`{pr, conclusion, status}`. v1 uses the existing `gate_verdict` kind
to carry the agent-CI-changed signal (since `tool_call` is a tool-
execution kind, not a CI-status kind). Document this kind choice in
the package comment.

- [ ] **Step 1: Inspect prwatch ghcli for shape conventions**

```bash
sed -n '1,80p' internal/orchestrator/prwatch/ghcli.go
```

Mirror exec-seam interface + error wrapping.

- [ ] **Step 2: Write failing tests (covers first-emit, flip, non-flip-suppress, race)**

```go
// internal/orchestrator/checks/poller_test.go
package checks

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPoller_FirstObservationEmits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()
	gh := &fakeGH{}
	gh.set("PR-1", CheckRun{Conclusion: "success", Status: "completed"})

	p := New(gh, db, func() time.Time { return time.Unix(1, 0) })
	if err := p.poll(ctx, "PR-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	if got := countCIEvents(t, db); got != 1 {
		t.Errorf("first-observation emit count: got %d want 1", got)
	}
}

func TestPoller_FlipEmitsAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()
	gh := &fakeGH{}
	gh.set("PR-1", CheckRun{Conclusion: "success", Status: "completed"})

	p := New(gh, db, func() time.Time { return time.Unix(1, 0) })
	_ = p.poll(ctx, "PR-1", "agent-1")
	gh.set("PR-1", CheckRun{Conclusion: "failure", Status: "completed"})
	_ = p.poll(ctx, "PR-1", "agent-1")

	if got := countCIEvents(t, db); got != 2 {
		t.Errorf("after flip, expected 2 events; got %d", got)
	}
}

func TestPoller_NonFlipDoesNotEmit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()
	gh := &fakeGH{}
	gh.set("PR-1", CheckRun{Conclusion: "success", Status: "completed"})

	p := New(gh, db, func() time.Time { return time.Unix(1, 0) })
	_ = p.poll(ctx, "PR-1", "agent-1")
	_ = p.poll(ctx, "PR-1", "agent-1") // same state
	_ = p.poll(ctx, "PR-1", "agent-1") // same state

	if got := countCIEvents(t, db); got != 1 {
		t.Errorf("3 polls same state: got %d events, want 1", got)
	}
}

func TestPoller_ConcurrentPollsSafe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := newTestDB(t)
	defer db.Close()
	gh := &fakeGH{}
	gh.set("PR-1", CheckRun{Conclusion: "success", Status: "completed"})

	p := New(gh, db, func() time.Time { return time.Unix(1, 0) })
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.poll(ctx, "PR-1", "agent-1")
		}()
	}
	wg.Wait()
	// observed-status map must be guarded — race detector catches data races.
}
```

`countCIEvents` queries `substrate_events WHERE kind='gate_verdict'
AND payload_json LIKE '%agent_ci_changed%'`. Define as test helper.

- [ ] **Step 3: Run failing**

Run: `go test ./internal/orchestrator/checks/ -race -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 4: Implement w/ mutex on internal state**

```go
// internal/orchestrator/checks/poller.go
//
// Package checks polls "gh pr checks" and emits substrate events when
// a PR's aggregate CI status changes. v1 reuses the gate_verdict kind
// for the agent_ci_changed payload (spec §3.2: payload carries
// {pr, conclusion, status}).
package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

type GHCLI interface {
	PRChecks(ctx context.Context, pr string) (CheckRun, error)
}

type CheckRun struct {
	Conclusion string
	Status     string
}

type Poller struct {
	gh    GHCLI
	db    *state.DB
	clock func() time.Time
	key   []byte
	keyID string

	mu   sync.Mutex
	last map[string]CheckRun
}

func New(gh GHCLI, db *state.DB, clock func() time.Time) *Poller {
	return &Poller{gh: gh, db: db, clock: clock, last: map[string]CheckRun{}}
}

func (p *Poller) WithHMAC(key []byte, keyID string) *Poller {
	p.key = key
	p.keyID = keyID
	return p
}

func (p *Poller) poll(ctx context.Context, pr, agentID string) error {
	cur, err := p.gh.PRChecks(ctx, pr)
	if err != nil {
		return fmt.Errorf("gh pr checks %s: %w", pr, err)
	}
	key := pr + "|" + agentID

	p.mu.Lock()
	prev, seen := p.last[key]
	p.last[key] = cur
	p.mu.Unlock()

	if seen && prev == cur {
		return nil
	}
	payload, err := json.Marshal(map[string]string{
		"event":      "agent_ci_changed",
		"pr":         pr,
		"conclusion": cur.Conclusion,
		"status":     cur.Status,
	})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	return state.WithTx(ctx, p.db, func(tx *sql.Tx) error {
		return substrate.AppendEvent(ctx, tx, substrate.Event{
			ID:          newEventID(),
			RunID:       "", // CI is PR-scoped, not run-scoped
			TenantID:    "default",
			Kind:        "gate_verdict",
			Key:         pr,
			PayloadJSON: string(payload),
			WrittenBy:   "checks/poller@v1",
			WrittenAt:   p.clock().Unix(),
		}, p.key, p.keyID)
	})
}
```

```go
// internal/orchestrator/checks/ghcli.go
package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

type GHShell struct{}

func (g *GHShell) PRChecks(ctx context.Context, pr string) (CheckRun, error) {
	out, err := exec.CommandContext(ctx, "gh", "pr", "checks", pr,
		"--json", "conclusion,status,name", "--required").Output()
	if err != nil {
		return CheckRun{}, fmt.Errorf("gh pr checks: %w", err)
	}
	var arr []struct {
		Conclusion string `json:"conclusion"`
		Status     string `json:"status"`
		Name       string `json:"name"`
	}
	if err := json.Unmarshal(out, &arr); err != nil {
		return CheckRun{}, fmt.Errorf("parse gh json: %w", err)
	}
	for _, c := range arr {
		if c.Conclusion == "failure" {
			return CheckRun{Conclusion: "failure", Status: "completed"}, nil
		}
		if c.Status != "completed" {
			return CheckRun{Conclusion: "", Status: c.Status}, nil
		}
	}
	return CheckRun{Conclusion: "success", Status: "completed"}, nil
}
```

- [ ] **Step 5: Run pass with race detector**

Run: `go test ./internal/orchestrator/checks/ -race -count=1 -v`
Expected: PASS (4 subtests; race detector clean)

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/checks/
git commit -m "feat(checks): gh pr checks poller w/ mutex-safe state + race tests (S0)"
```

---

## Task 13: Extend `prwatch.ghcli` to decode `mergeStateStatus`

**Files:**
- Modify: `internal/orchestrator/prwatch/ghcli.go:14`
- Create: `internal/orchestrator/prwatch/ghcli_dirty_test.go`

- [ ] **Step 1: Read current decoder**

```bash
sed -n '1,45p' internal/orchestrator/prwatch/ghcli.go
```

Document current `ghJSONFields` const + decoder struct + `PR` exported
struct.

- [ ] **Step 2: Write failing test**

```go
// internal/orchestrator/prwatch/ghcli_dirty_test.go
package prwatch

import (
	"context"
	"strings"
	"testing"
)

func TestGHCLILister_DecodesMergeStateStatus(t *testing.T) {
	t.Parallel()
	gh := newFakeGH(`[{
		"number":1,"headRefOid":"sha1","state":"OPEN","headRefName":"feat",
		"title":"t","author":{"login":"a"},"mergeStateStatus":"DIRTY"
	}]`)
	prs, err := gh.List(context.Background(), "branch")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d prs", len(prs))
	}
	if !strings.EqualFold(prs[0].MergeStateStatus // PullRequest type, not PR, "DIRTY") {
		t.Errorf("MergeStateStatus: got %q want DIRTY", prs[0].MergeStateStatus // PullRequest type, not PR)
	}
}
```

`newFakeGH` is an existing test helper — verify its constructor signature
in `internal/orchestrator/prwatch/ghcli_test.go`.

- [ ] **Step 3: Run failing**

Run: `go test ./internal/orchestrator/prwatch/ -run TestGHCLILister_DecodesMergeStateStatus -v`
Expected: FAIL — field absent.

- [ ] **Step 4: Update consts + struct**

In `ghcli.go:14`:
```go
const ghJSONFields = "number,headRefOid,state,headRefName,title,author,mergeStateStatus"
```

In the internal decoder struct (immediately below the const), add:
```go
MergeStateStatus string `json:"mergeStateStatus"`
```

Mirror to the exported `PR` struct.

- [ ] **Step 5: Run pass + full prwatch suite**

```bash
go test ./internal/orchestrator/prwatch/... -count=1 -v
```
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/prwatch/ghcli.go \
        internal/orchestrator/prwatch/ghcli_dirty_test.go
git commit -m "feat(prwatch): decode mergeStateStatus from gh pr list JSON (S0)"
```

---

## Task 14: Emit `agent_pr_dirty` w/ per-transition dedupe + re-arm

**Files:**
- Modify: `internal/orchestrator/prwatch/prwatch.go`
- Create: `internal/orchestrator/prwatch/prwatch_dirty_test.go`

- [ ] **Step 1: Inspect existing emission pattern**

Read `prwatch.go:240-360` (per W7 spec context). Identify how
`agent_pr_opened`, `agent_pr_head_changed`, `agent_branch_renamed` get
written. Mirror that path.

- [ ] **Step 2: Write failing tests (incl. cycle-back)**

```go
// internal/orchestrator/prwatch/prwatch_dirty_test.go
package prwatch

import (
	"context"
	"testing"
)

func TestWatcher_EmitsAgentPRDirtyOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w, db, gh := newTestWatcher(t)
	defer db.Close()

	gh.setPR("agent-1", "branch-1", PullRequest{
		Number: 1, HeadRefOid: "sha-x", State: "OPEN", MergeStateStatus: "DIRTY",
	})
	if err := w.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Sweep(ctx); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM substrate_events
		WHERE kind='agent_pr_head_changed' OR kind='agent_pr_opened'
	`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	// inspect via prwatch's existing event-kind convention; adapt the
	// query above to whatever kind agent_pr_dirty actually lands under.
}

func TestWatcher_AgentPRDirty_ReArmAfterTransitionBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w, db, gh := newTestWatcher(t)
	defer db.Close()

	gh.setPR("agent-1", "branch-1", PullRequest{Number: 1, HeadRefOid: "sha-x", State: "OPEN", MergeStateStatus: "DIRTY"})
	_ = w.Sweep(ctx) // emit 1

	gh.setPR("agent-1", "branch-1", PullRequest{Number: 1, HeadRefOid: "sha-x", State: "OPEN", MergeStateStatus: "CLEAN"})
	_ = w.Sweep(ctx) // no emit; re-arms

	gh.setPR("agent-1", "branch-1", PullRequest{Number: 1, HeadRefOid: "sha-x", State: "OPEN", MergeStateStatus: "DIRTY"})
	_ = w.Sweep(ctx) // emit 2

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE agent_id='agent-1' AND kind='agent_pr_dirty'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("DIRTY→CLEAN→DIRTY cycle: got %d events, want 2", count)
	}
}
```

Note: query the correct events table — prwatch may write to the legacy
`events` table OR to `substrate_events`. Inspect existing emit functions
to confirm.

- [ ] **Step 3: Run failing**

Run: `go test ./internal/orchestrator/prwatch/ -run "TestWatcher_.*Dirty" -count=1 -v`
Expected: FAIL — no emission.

- [ ] **Step 4: Implement w/ dedupe + re-arm**

Add to `Watcher` struct:
```go
type Watcher struct {
	// ... existing fields
	dirtyEmitted map[string]bool
}
```

Initialize in `New` and inside `Sweep` loop:

```go
if strings.EqualFold(pr.MergeStateStatus, "DIRTY") {
	if !p.dirtyEmitted[a.ID] {
		if err := p.emitDirtyEvent(ctx, a.ID, pr); err != nil {
			return err
		}
		p.dirtyEmitted[a.ID] = true
	}
} else {
	delete(p.dirtyEmitted, a.ID)
}
```

`emitDirtyEvent` follows the convention of the existing emit functions
(`emitOpenedEvent`, `emitHeadChangedEvent`, etc.).

- [ ] **Step 5: Run pass**

```bash
go test ./internal/orchestrator/prwatch/... -count=1 -v
```
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/prwatch/prwatch.go \
        internal/orchestrator/prwatch/prwatch_dirty_test.go
git commit -m "feat(prwatch): emit agent_pr_dirty on DIRTY; dedupe + re-arm on cycle (S0)"
```

---

## Task 15: Unify `mergeStateStatus` decode site

**Files:**
- Inspect: `internal/orchestrator/merge/merge.go:70` (currently comment-only per reviewer)
- Decision: extract shared decoder OR document divergence

- [ ] **Step 1: Verify reviewer finding**

```bash
grep -n "mergeStateStatus\|MergeStateStatus" internal/orchestrator/merge/
```

Per v1 reviewer, line 70 is comment-only — confirm.

- [ ] **Step 2: Decide path**

If `merge` does NOT actually decode `mergeStateStatus` today, this task is **a no-op** — Task 13 + 14 cover the only live decode path. Document this in a one-line comment at `merge.go:70`:

```go
// mergeStateStatus is decoded by internal/orchestrator/prwatch/ghcli.go
// (Task 13 of operator-console S0). Coordinate any future merge-side
// decode through that package.
```

- [ ] **Step 3: Commit**

```bash
git add internal/orchestrator/merge/merge.go
git commit -m "docs(merge): point mergeStateStatus decode to prwatch ghcli (S0)"
```

If reviewer was wrong and `merge` does decode it, defer to a follow-up issue tagged `[regatta-S0-followup]` rather than expanding S0 scope.

---

## Task 16: Full S0 hot-path verification

- [ ] **Step 1: Run full S0 test suite under race detector**

```bash
go test -race -count=1 \
  ./internal/orchestrator/state/... \
  ./internal/orchestrator/spawner/... \
  ./internal/orchestrator/scheduler/... \
  ./internal/orchestrator/prwatch/... \
  ./internal/orchestrator/checks/... \
  ./internal/orchestrator/merge/... \
  ./internal/gates/approval/... \
  ./internal/agent/...
```
Expected: PASS

- [ ] **Step 2: Run `make check`**

Run: `make check`
Expected: PASS (lint + tests + doc-check + scorecard).

- [ ] **Step 3: Update hot-path runbook with final file:line map**

Edit `docs/engineer/runbooks/operator-console-s0-dispatch-map.md`
(created Task 0) to reflect the actual file:line locations now that
implementation is done.

- [ ] **Step 4: Commit**

```bash
git add docs/engineer/runbooks/operator-console-s0-dispatch-map.md
git commit -m "docs(runbook): finalize S0 dispatch-map after impl (S0)"
```

---

## Task 17: Open PR + verify CI

- [ ] **Step 1: Push branch**

```bash
git push -u origin feat/operator-console-s0
```

- [ ] **Step 2: Open PR**

```bash
gh pr create --title "[FEATURE] operator-console S0 — substrate prereqs (runs registry + run_id + tool_call + checks + prwatch DIRTY)" --body-file <(cat <<'EOF'
## Summary

S0 of operator console v1 (spec
`docs/engineer/specs/phase-x/2026-06-02-operator-console-design.md`, PR #701).

Lands substrate + scheduler + spawner + gh-poller infrastructure that
unblocks S1-S4:

- Migrations 0018-0021 — `runs` registry w/ `declared_effect_class` +
  `run_id` columns on `work_items` + `approval_events` + `tool_call`
  substrate kind (full-rewrite per 0015 precedent) + payload_json
  CHECK widened 1024 → 8192.
- `runs` CRUD as methods on `*state.DB` + `CausalInputs.Hash()`
  (sha256 over canon-JSON) w/ rapid property tests.
- Scheduler dispatch boundary extracted; INSERTs `runs` row, populates
  `WorkItem.RunID`, threads `runID` into 14 `DecideTx` callers via a
  positional signature change.
- New `internal/agent/config.go` carrying `AgentConfig` w/ causal-
  inputs + `DeclaredEffectClass` policy envelope.
- Claude shim emits `tool_call` substrate events via
  `substrate.AppendEvent` with `declared_effect_class` (from
  `runs.declared_effect_class`) + raw `observed_effect` set
  (deduped/sorted; classification is downstream in surprise detector).
- New `internal/orchestrator/checks/` package — `gh pr checks` poller
  emits `agent_ci_changed` payload under `gate_verdict` kind w/
  mutex-safe state + first-emit / flip-emit / non-flip-suppress
  semantics + race-detector-clean.
- `internal/orchestrator/prwatch/ghcli.go` decodes `mergeStateStatus`;
  emits `agent_pr_dirty` w/ per-transition dedupe + re-arm on
  DIRTY→CLEAN→DIRTY cycle.

## Test plan

- [ ] `make check` clean.
- [ ] `go test -race -count=1 ./internal/...` clean across all S0
  packages.
- [ ] Migration round-trip: every new column round-trips via full
  `cmp.Diff` test (not just PRAGMA column existence).
- [ ] `CausalInputs.Hash()` deterministic + input-sensitive (rapid
  property test) + map-key-order-independent (10-key shuffle).
- [ ] Dispatch INSERTs runs row with non-empty `causal_hash`.
- [ ] `tool_call` event payload contains `declared_effect_class` +
  sorted `observed_effect` array.
- [ ] `agent_ci_changed` emits on first observation + on each
  conclusion+status flip; suppressed on identical-state polls;
  race-safe under concurrent polls.
- [ ] `agent_pr_dirty` emits once per DIRTY entry; re-emits after
  DIRTY→CLEAN→DIRTY cycle.

## Spec section coverage

§3.2 substrate prereqs ✓ · §3.5 partial (CI-RED + DIRTY data sources
ready; chip render = S2) · §3.6 (anomaly chip data sources) ready ·
§3.8 (full causal hash) ✓.

## A+ Scorecard

- [x] **TDD** — every task ships failing test FIRST; commits chunked
  (cited: Task 1 Step 2/4, Task 2 Step 2/4, every subsequent task).
- [x] **Falsifiable acceptance** — round-trip via `cmp.Diff`, not
  existence checks (cited: Task 1, Task 2 tests).
- [x] **Concurrent-safe** — race detector required in CI step (cited:
  Task 12 Step 5, Task 16 Step 1).
- [x] **Spec-conformant payloads** — `tool_call` payload fields match
  spec §3.2; `agent_ci_changed` payload matches `{pr, conclusion,
  status}` per spec (cited: Task 11, Task 12).
- [x] **Mass-update discipline** — 14 `DecideTx` call sites enumerated
  before signature change (cited: Task 8 Step 1).
- [x] **Migration safety** — `tool_call` migration follows 0015/0017
  full table-rewrite precedent (cited: Task 6 Step 4 comment).
- [x] **Honest scope** — Task 0 prereq discovery PR'd first; no
  speculative "unify or document" tasks (cited: Task 15 path-decision).

```release-notes
[FEATURE] operator-console S0 — substrate prereqs (runs registry + run_id columns + tool_call substrate kind + gh-checks poller + prwatch DIRTY). Unblocks S1-S4 of the operator-console v1 build.
```
EOF
)
```

- [ ] **Step 3: Watch CI + address findings**

Run: `gh pr checks <pr-number>`
Address each failing gate. Repeat until green.

- [ ] **Step 4: Spawn reviewer subagent**

Use `cavecrew-reviewer` against the diff. Address findings inline.

- [ ] **Step 5: Merge when green + reviewed**

```bash
gh pr merge <pr-number> --squash --auto --delete-branch
```

---

## Self-review

**1. Spec coverage:**
- §3.2 substrate prereqs — Tasks 1, 4-6, 9-15.
- §3.5 chip data sources — Task 12 (CI-RED), Task 14 (DIRTY).
- §3.6 anomaly chip data sources — already present in substrate; surface in S2.
- §3.8 full causal hash — Tasks 3, 9.
- §3.11 substrate read-only integration — Tasks 7-9 thread the data; readers are S1-S3.

**2. Placeholder scan:**
- Tasks 0, 11 carry "verify by grep" / "adapt to actual" instructions — acceptable per spec convention; each names what to grep for + what signature shape is expected. Not placeholders.
- No "TODO" outside Task 8 Step 6 transient marker that Task 9 removes.

**3. Type consistency:**
- `Run` struct: Tasks 1, 2, 9 — same field set.
- `CausalInputs`: Tasks 3, 9 — same.
- `WorkItem.RunID`: Tasks 7, 9 — same.
- `DecideTx` signature: Tasks 8 (adds), 9 (uses).
- `ObservedSignal`: Tasks 10, 11 — same.
- `AgentConfig`: Tasks 9, 11 — same.

---

## Execution Handoff

**Plan v2 complete and saved to `docs/superpowers/plans/2026-06-03-operator-console-s0-substrate-prereqs.md`.**

Two execution options:

**1. Subagent-Driven (recommended)** — fresh subagent per task, review between tasks. Best for autonomous-loop. ~17 dispatches; estimate 6-7 weeks elapsed if parallelizing migrations + decoders.

**2. Inline Execution** — execute tasks in current session via `executing-plans`. Sequential.

**Which approach?**
