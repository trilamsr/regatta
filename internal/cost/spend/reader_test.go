package spend_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func openReaderDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "subs.db")
	db, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := state.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// insertCounter monotonically uniqueifies the synthetic (id, nonce)
// across inserts in one test process so the UNIQUE(run_id, written_by,
// nonce) substrate constraint does not collide on co-located rows.
var insertCounter int64

// insert writes one raw substrate_events row of given kind. Bypasses
// substrate.AppendEvent so Wave-1 validator shape is irrelevant.
func insert(t *testing.T, db *sql.DB, kind, payload string, writtenAt time.Time, tenantID, key string) {
	t.Helper()
	insertCounter++
	id := fmt.Sprintf("ev-%d-%d", writtenAt.UnixNano(), insertCounter)
	if len(id) > 26 {
		id = id[:26]
	}
	nonce := fmt.Sprintf("nonce-%d-%d", writtenAt.UnixNano(), insertCounter)
	_, err := db.Exec(`INSERT INTO substrate_events
		(id, run_id, work_item_id, tenant_id, trace_id, span_id, kind, key,
		 payload_json, blob_digest, supersedes, written_by, written_at,
		 schema_version, nonce, sig_alg, sig_key_id, sig_mac)
		VALUES (?, 'run-1', NULL, ?, '', '', ?, ?, ?, '', NULL, 'tester', ?,
		        1, ?, 'hmac-sha256', 'test-1', 'mac')`,
		id, tenantID, kind, key, payload, writtenAt.UnixMilli(), nonce)
	if err != nil {
		t.Fatalf("insert %s: %v", kind, err)
	}
}

func TestReader_BudgetState_SumOverWindow(t *testing.T) {
	db := openReaderDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	for i, usd := range []float64{1.0, 2.5, 3.5} {
		payload := fmt.Sprintf(`{"usd":%f,"dag_id":"DAG-A","operator_id":"agent-7","work_item_id":"WI-X"}`, usd)
		insert(t, db, "token_spend", payload, now.Add(-time.Duration(i)*time.Minute), "default", "")
	}
	r := spend.NewReader(db, func() time.Time { return now })
	got, err := r.BudgetState(context.Background(), spend.ScopeKey{
		Kind:     spend.ScopeDAG,
		DAGID:    "DAG-A",
		TenantID: "default",
	}, time.Hour)
	if err != nil {
		t.Fatalf("BudgetState: %v", err)
	}
	if got != 7.0 {
		t.Fatalf("BudgetState=%f; want 7.0", got)
	}
}

func TestReader_BudgetState_PeriodWindow_ExcludesStale(t *testing.T) {
	db := openReaderDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	// In-window
	insert(t, db, "token_spend",
		`{"usd":10.0,"dag_id":"DAG-A","operator_id":"agent-7","work_item_id":"WI-X"}`,
		now.Add(-30*time.Minute), "default", "")
	// Stale (2h ago)
	insert(t, db, "token_spend",
		`{"usd":99.0,"dag_id":"DAG-A","operator_id":"agent-7","work_item_id":"WI-X"}`,
		now.Add(-2*time.Hour), "default", "")
	r := spend.NewReader(db, func() time.Time { return now })
	got, err := r.BudgetState(context.Background(), spend.ScopeKey{
		Kind:     spend.ScopeDAG,
		DAGID:    "DAG-A",
		TenantID: "default",
	}, time.Hour)
	if err != nil {
		t.Fatalf("BudgetState: %v", err)
	}
	if got != 10.0 {
		t.Fatalf("BudgetState=%f; want 10.0 (stale row excluded)", got)
	}
}

func TestReader_LastReconciliation_LWWPerPeriod(t *testing.T) {
	db := openReaderDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	periodStart := now.Truncate(time.Hour).UnixMilli()
	insert(t, db, "budget_reconciled",
		fmt.Sprintf(`{"period_start":%d,"actual_usd":50.0}`, periodStart),
		now.Add(-30*time.Minute), "default", fmt.Sprintf("%d", periodStart))
	insert(t, db, "budget_reconciled",
		fmt.Sprintf(`{"period_start":%d,"actual_usd":55.0}`, periodStart),
		now.Add(-10*time.Minute), "default", fmt.Sprintf("%d", periodStart))
	r := spend.NewReader(db, func() time.Time { return now })
	raw, writtenAt, err := r.LastReconciliation(context.Background(), "default")
	if err != nil {
		t.Fatalf("LastReconciliation: %v", err)
	}
	if writtenAt.IsZero() {
		t.Fatalf("writtenAt zero; want non-zero")
	}
	if string(raw) == "" || !contains(string(raw), `"actual_usd":55`) {
		t.Fatalf("payload=%s; want latest (actual_usd=55)", raw)
	}
}

func TestReader_FiltersOnTenantID(t *testing.T) {
	db := openReaderDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	insert(t, db, "token_spend",
		`{"usd":10.0,"dag_id":"DAG-A","operator_id":"agent-7","work_item_id":"WI-X"}`,
		now.Add(-30*time.Minute), "default", "")
	// Different tenant; must NOT count.
	insert(t, db, "token_spend",
		`{"usd":99.0,"dag_id":"DAG-A","operator_id":"agent-7","work_item_id":"WI-X"}`,
		now.Add(-30*time.Minute), "tenant-other", "")
	r := spend.NewReader(db, func() time.Time { return now })
	got, err := r.BudgetState(context.Background(), spend.ScopeKey{
		Kind:     spend.ScopeDAG,
		DAGID:    "DAG-A",
		TenantID: "default",
	}, time.Hour)
	if err != nil {
		t.Fatalf("BudgetState: %v", err)
	}
	if got != 10.0 {
		t.Fatalf("BudgetState=%f; want 10.0 (cross-tenant row excluded)", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
