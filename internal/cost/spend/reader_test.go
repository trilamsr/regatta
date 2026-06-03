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

// TestReader_BudgetState_SumOverWindow asserts legacy $.usd rows convert per-row to micro before SUM over window.
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
	if got != spend.FromUSD(7.0) {
		t.Fatalf("BudgetState=%d; want %d (7 USD in micro)", got, spend.FromUSD(7.0))
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
	if got != spend.FromUSD(10.0) {
		t.Fatalf("BudgetState=%d; want %d (10 USD in micro, stale row excluded)", got, spend.FromUSD(10.0))
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
	if got != spend.FromUSD(10.0) {
		t.Fatalf("BudgetState=%d; want %d (10 USD in micro, cross-tenant row excluded)", got, spend.FromUSD(10.0))
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

// TestReader_RecordedUSDForWindow_SumsTokenSpendInWindow pins the reconciler-side seam — SUM(usd) over [start, end) with tenant isolation.
func TestReader_RecordedUSDForWindow_SumsTokenSpendInWindow(t *testing.T) {
	db := openReaderDB(t)
	start := time.Date(2026, 6, 1, 1, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 1, 2, 0, 0, 0, time.UTC)
	// In window (start, mid, end-1ms).
	insert(t, db, "token_spend", `{"usd":1.5}`, start, "default", "")
	insert(t, db, "token_spend", `{"usd":2.0}`, start.Add(30*time.Minute), "default", "")
	insert(t, db, "token_spend", `{"usd":0.5}`, end.Add(-time.Millisecond), "default", "")
	// Out of window (before start, at end, after end).
	insert(t, db, "token_spend", `{"usd":99}`, start.Add(-time.Millisecond), "default", "")
	insert(t, db, "token_spend", `{"usd":99}`, end, "default", "")
	insert(t, db, "token_spend", `{"usd":99}`, end.Add(time.Hour), "default", "")
	// Wrong tenant — pinned out.
	insert(t, db, "token_spend", `{"usd":42}`, start.Add(15*time.Minute), "other", "")

	r := spend.NewReader(db, time.Now)
	got, err := r.RecordedUSDForWindow(context.Background(), "default", start, end)
	if err != nil {
		t.Fatalf("RecordedUSDForWindow: %v", err)
	}
	want := spend.FromUSD(4.0)
	if got != want {
		t.Fatalf("RecordedUSDForWindow=%d; want %d ($4 in micro)", got, want)
	}
}

func TestReader_RecordedUSDForWindow_RequiresTenant(t *testing.T) {
	db := openReaderDB(t)
	r := spend.NewReader(db, time.Now)
	_, err := r.RecordedUSDForWindow(context.Background(), "", time.Now(), time.Now())
	if err == nil {
		t.Fatal("RecordedUSDForWindow with empty tenant returned nil; want error")
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestReader_BudgetState_LegacyFloatRowsConvertExactly asserts COALESCE prefers usd_micro and casts legacy $.usd to micro exactly.
func TestReader_BudgetState_LegacyFloatRowsConvertExactly(t *testing.T) {
	db := openReaderDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	// Three legacy rows: $1.00, $0.50, $0.25. JSON-float repr is exact for these.
	for i, usd := range []float64{1.0, 0.5, 0.25} {
		payload := fmt.Sprintf(`{"usd":%g,"dag_id":"DAG-A","operator_id":"o","work_item_id":"w"}`, usd)
		insert(t, db, "token_spend", payload, now.Add(-time.Duration(i+1)*time.Minute), "default", "")
	}
	// One new row carrying $.usd_micro (canonical) — proves COALESCE prefers it.
	insert(t, db, "token_spend",
		`{"usd_micro":250000,"usd":0.25,"dag_id":"DAG-A","operator_id":"o","work_item_id":"w"}`,
		now.Add(-10*time.Second), "default", "")

	r := spend.NewReader(db, func() time.Time { return now })
	got, err := r.BudgetState(context.Background(), spend.ScopeKey{
		Kind: spend.ScopeDAG, DAGID: "DAG-A", TenantID: "default",
	}, time.Hour)
	if err != nil {
		t.Fatalf("BudgetState: %v", err)
	}
	// $1 + $0.50 + $0.25 + $0.25 = $2.00 → 2_000_000 micro exactly.
	want := spend.FromUSD(2.0)
	if got != want {
		t.Fatalf("BudgetState=%d; want %d (legacy+micro mix sums exactly)", got, want)
	}
}

// TestBudget_FloatRoundingExceedsCap_IntegerDoesNot asserts integer-micro SUM hits cap exactly where float SUM undershoots by one ULP.
func TestBudget_FloatRoundingExceedsCap_IntegerDoesNot(t *testing.T) {
	db := openReaderDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// Witness: float accumulation undershoots the operator-typed cap
	// by one ULP. If a future runtime collapses the drift, the test
	// fixture is no longer demonstrating the bug — fail loud.
	var witness float64
	for i := 0; i < 11; i++ {
		witness += 0.10
	}
	const capUSD = 1.10
	if witness >= capUSD {
		t.Fatalf("witness fixture broken: 11 × 0.10 = %.20f ≥ cap %.20f", witness, capUSD)
	}

	// Write 11 legacy float-only rows of $0.10 each.
	for i := 0; i < 11; i++ {
		insert(t, db, "token_spend",
			`{"usd":0.1,"dag_id":"DAG-A","operator_id":"o","work_item_id":"w"}`,
			now.Add(-time.Duration(i+1)*time.Second), "default", "")
	}

	r := spend.NewReader(db, func() time.Time { return now })
	got, err := r.BudgetState(context.Background(), spend.ScopeKey{
		Kind: spend.ScopeDAG, DAGID: "DAG-A", TenantID: "default",
	}, time.Hour)
	if err != nil {
		t.Fatalf("BudgetState: %v", err)
	}
	// Integer micro sum = 11 × 100_000 = 1_100_000 micro = exactly the cap.
	wantMicro := spend.USDMicro(1_100_000)
	if got != wantMicro {
		t.Fatalf("integer-micro SUM=%d; want %d (exactly cap_micro, no ULP slop)", got, wantMicro)
	}
	// And the cap-comparison surface: a strict-> check denies only at
	// strictly-over; the integer math at exact-equal preserves the
	// "≤ cap is allowed" semantics. The bug surface (float allowed
	// because it undershot) is replaced by an integer-exact equality
	// the operator can reason about.
	capMicro := spend.FromUSD(capUSD)
	if got != capMicro {
		t.Fatalf("integer SUM=%d != capMicro=%d; rounding contract broken", got, capMicro)
	}
}
