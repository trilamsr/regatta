package spend_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

// spendSnapshot is the reducer-folded view: per-call_id → USD as written to substrate.
type spendSnapshot struct {
	USD map[string]float64
}

func snapshotSpend(t *testing.T, db *sql.DB) spendSnapshot {
	t.Helper()
	rows, err := db.Query(`SELECT payload_json FROM substrate_events WHERE kind='token_spend' ORDER BY id`)
	if err != nil {
		t.Fatalf("snapshot query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	snap := spendSnapshot{USD: map[string]float64{}}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("snapshot scan: %v", err)
		}
		var p spend.TokenSpendPayload
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatalf("snapshot decode: %v", err)
		}
		snap.USD[p.CallID] = p.USD
	}
	return snap
}

func diffSpendSnapshots(want, got spendSnapshot) string {
	var diffs []string
	for id, w := range want.USD {
		g, ok := got.USD[id]
		if !ok {
			diffs = append(diffs, "call["+id+"] missing post-recover")
			continue
		}
		if w != g {
			diffs = append(diffs, fmt.Sprintf("call[%s] usd want=%g got=%g", id, w, g))
		}
	}
	for id := range got.USD {
		if _, ok := want.USD[id]; !ok {
			diffs = append(diffs, "call["+id+"] phantom post-recover")
		}
	}
	sort.Strings(diffs)
	if len(diffs) == 0 {
		return ""
	}
	return strings.Join(diffs, "; ")
}

// spendCrashHarness records N CallRecords in N sequential txes. Crash = rollback at tx k.
type spendCrashHarness struct {
	callIDs []string
	model   string
	tenant  string
	now     time.Time
}

func (h *spendCrashHarness) record(t *testing.T, db *sql.DB) spendSnapshot {
	t.Helper()
	return h.runUpTo(t, db, len(h.callIDs), -1)
}

// runUpTo records the first n calls; if crashAt ∈ [0,n), that tx
// rolls back instead of committing. Returns the resulting snapshot.
func (h *spendCrashHarness) runUpTo(t *testing.T, db *sql.DB, n, crashAt int) spendSnapshot {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx[%d]: %v", i, err)
		}
		err = spend.RecordCall(ctx, tx, spend.CallRecord{
			CallID:       h.callIDs[i],
			RetrySeq:     0,
			Model:        h.model,
			InputTokens:  1_000_000,
			OutputTokens: 500_000,
			OperatorID:   "agent-7",
			DAGID:        "DAG-A",
			WorkItemID:   "WI-1",
			TenantID:     h.tenant,
			WrittenBy:    "spawner-1",
			RunID:        "run-1",
		}, spend.WriteOptions{
			Now: func() time.Time { return h.now },
			// Key + KeyID — borrowed from writer_test's harness.
			Key:   spendCrashKey,
			KeyID: spendCrashKeyID,
		})
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("RecordCall[%d]: %v", i, err)
		}
		if i == crashAt {
			// Simulate crash: rollback instead of commit. The substrate
			// row was never persisted, so the recovery process can
			// retry with the same (CallID, RetrySeq=0) nonce without
			// hitting ErrReplay.
			_ = tx.Rollback()
			continue
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit tx[%d]: %v", i, err)
		}
	}
	return snapshotSpend(t, db)
}

// runBaseline records all N calls cleanly.
func (h *spendCrashHarness) runBaseline(t *testing.T) spendSnapshot {
	t.Helper()
	db := spendCrashDB(t)
	return h.record(t, db)
}

// runCrashAndRecover records 0..crashAt cleanly, rolls back crashAt's
// tx, then "recovers" by replaying crashAt..N. The recovery process is
// a fresh tx-driven loop; sqlite persistence is the only carry-over.
func (h *spendCrashHarness) runCrashAndRecover(t *testing.T, crashAt int) spendSnapshot {
	t.Helper()
	db := spendCrashDB(t)
	_ = h.runUpTo(t, db, crashAt+1, crashAt) // crash at index crashAt; rows [0,crashAt) commit, crashAt rolls back.
	// Recovery: re-issue the crashed row + the remainder of the queue.
	h.replay(t, db, crashAt)
	return snapshotSpend(t, db)
}

// replay re-runs the calls from index start onwards in a fresh tx-loop.
// Simulates a recovered process that resumes the spend queue.
func (h *spendCrashHarness) replay(t *testing.T, db *sql.DB, start int) {
	t.Helper()
	ctx := context.Background()
	for i := start; i < len(h.callIDs); i++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("replay begin[%d]: %v", i, err)
		}
		err = spend.RecordCall(ctx, tx, spend.CallRecord{
			CallID:       h.callIDs[i],
			RetrySeq:     0,
			Model:        h.model,
			InputTokens:  1_000_000,
			OutputTokens: 500_000,
			OperatorID:   "agent-7",
			DAGID:        "DAG-A",
			WorkItemID:   "WI-1",
			TenantID:     h.tenant,
			WrittenBy:    "spawner-1",
			RunID:        "run-1",
		}, spend.WriteOptions{
			Now:   func() time.Time { return h.now },
			Key:   spendCrashKey,
			KeyID: spendCrashKeyID,
		})
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("replay RecordCall[%d]: %v", i, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("replay commit[%d]: %v", i, err)
		}
	}
}

var (
	spendCrashKey   = []byte("0123456789abcdef0123456789abcdef")
	spendCrashKeyID = "spend-crash-key"
)

// spendCrashDB clones the golden DB and returns the underlying *sql.DB.
func spendCrashDB(t *testing.T) *sql.DB {
	t.Helper()
	db := statetest.GoldenClone(t, nil)
	substrate.ResetClockForTesting()
	return db.SQL()
}

// callIDs builds n unique call IDs prefixed with the test name slug.
func callIDs(prefix string, n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = fmt.Sprintf("%s-msg-%02d", prefix, i)
	}
	return out
}

// TestSpendCrashRecovery_DetectsForcedDivergence pins the diff harness — if recovery is skipped, diff must fire.
func TestSpendCrashRecovery_DetectsForcedDivergence(t *testing.T) {
	h := &spendCrashHarness{
		callIDs: callIDs("forced", 2),
		model:   "claude-sonnet-4-7",
		tenant:  substrate.DefaultTenantID,
		now:     time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}
	baseline := h.runBaseline(t)

	// Forge a "recovered" snapshot where no calls were ever recorded.
	forced := spendSnapshot{USD: map[string]float64{}}
	if d := diffSpendSnapshots(baseline, forced); d == "" {
		t.Fatalf("diffSpendSnapshots did not flag forced divergence — runner is blind to recovery skips")
	} else if !strings.Contains(d, h.callIDs[0]) {
		t.Fatalf("forced-divergence diff missing expected call labels: %s", d)
	}
}

// TestSpendCrashRecovery_BaselineMatchesRecover pins happy path — crash at first write then replay.
func TestSpendCrashRecovery_BaselineMatchesRecover(t *testing.T) {
	h := &spendCrashHarness{
		callIDs: callIDs("happy", 2),
		model:   "claude-sonnet-4-7",
		tenant:  substrate.DefaultTenantID,
		now:     time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}
	baseline := h.runBaseline(t)
	recovered := h.runCrashAndRecover(t, 0)
	if d := diffSpendSnapshots(baseline, recovered); d != "" {
		t.Fatalf("baseline ≠ recovered for crash-at-first-write: %s", d)
	}
}

// TestSpendCrashRecovery_CatchesMissingReplay pins TDD invariant — diff catches no-op recovery.
func TestSpendCrashRecovery_CatchesMissingReplay(t *testing.T) {
	h := &spendCrashHarness{
		callIDs: callIDs("missing", 2),
		model:   "claude-sonnet-4-7",
		tenant:  substrate.DefaultTenantID,
		now:     time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}
	baseline := h.runBaseline(t)

	// Bug: crash the first write + DO NOT replay. Recovered DB has 0 rows.
	db := spendCrashDB(t)
	_ = h.runUpTo(t, db, 1, 0)
	buggy := snapshotSpend(t, db)
	if d := diffSpendSnapshots(baseline, buggy); d == "" {
		t.Fatalf("diff did not catch missing replay — runner blind to the canonical bug")
	}

	// ErrReplay sanity: re-recording an already-committed (CallID, RetrySeq=0)
	// row MUST surface substrate.ErrReplay. Without this guarantee the
	// replay step could double-charge if it re-tried a committed row.
	ctx := context.Background()
	db2 := spendCrashDB(t)
	_ = h.runUpTo(t, db2, 1, -1) // commit one row.
	tx, err := db2.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("dup begin: %v", err)
	}
	dupErr := spend.RecordCall(ctx, tx, spend.CallRecord{
		CallID: h.callIDs[0], RetrySeq: 0,
		Model: h.model, InputTokens: 1_000_000, OutputTokens: 500_000,
		OperatorID: "agent-7", DAGID: "DAG-A", WorkItemID: "WI-1",
		TenantID: h.tenant, WrittenBy: "spawner-1", RunID: "run-1",
	}, spend.WriteOptions{
		Now: func() time.Time { return h.now }, Key: spendCrashKey, KeyID: spendCrashKeyID,
	})
	_ = tx.Rollback()
	if !isReplay(dupErr) {
		t.Fatalf("duplicate RecordCall(same callID,retry=0) err=%v; want substrate.ErrReplay", dupErr)
	}
}

func isReplay(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), substrate.ErrReplay.Error())
}
