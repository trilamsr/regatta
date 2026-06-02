package estimate_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/cost/estimate"
	"github.com/trilamsr/regatta/internal/cost/gate"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// openSpendDB opens a fresh migrated substrate DB. Mirrors the spend
// reader_test helper so the history estimator can be exercised against
// the same shape it'll see in production.
func openSpendDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", state.DSN(filepath.Join(t.TempDir(), "subs.db")))
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

var histInsertCounter int64

// insertSpend writes one synthetic token_spend row for the cohort
// (operator_id, model). The History estimator queries p95 USD over
// such rows.
func insertSpend(t *testing.T, db *sql.DB, usd float64, model, operator, tenant string, when time.Time) {
	t.Helper()
	histInsertCounter++
	payload, _ := json.Marshal(map[string]any{
		"usd":           usd,
		"model":         model,
		"operator_id":   operator,
		"dag_id":        "DAG-A",
		"work_item_id":  fmt.Sprintf("WI-%d", histInsertCounter),
		"input_tokens":  1000,
		"output_tokens": 500,
	})
	id := fmt.Sprintf("ev-%d-%d", when.UnixNano(), histInsertCounter)
	if len(id) > 26 {
		id = id[:26]
	}
	nonce := fmt.Sprintf("nonce-%d-%d", when.UnixNano(), histInsertCounter)
	_, err := db.Exec(`INSERT INTO substrate_events
		(id, run_id, work_item_id, tenant_id, trace_id, span_id, kind, key,
		 payload_json, blob_digest, supersedes, written_by, written_at,
		 schema_version, nonce, sig_alg, sig_key_id, sig_mac)
		VALUES (?, 'run-1', NULL, ?, '', '', 'token_spend', '', ?, '', NULL,
		        'tester', ?, 1, ?, 'hmac-sha256', 'test-1', 'mac')`,
		id, tenant, string(payload), when.UnixMilli(), nonce)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

// stubFallback satisfies gate.Estimator; the History estimator delegates
// to it on cold-start (< MinSamples). Captures invocation so tests can
// assert "history engaged" vs "fallback fired".
type stubFallback struct {
	usd    float64
	called int
}

func (s *stubFallback) Estimate(_ context.Context, _ gate.EstHint, _ string) (float64, error) {
	s.called++
	return s.usd, nil
}

// TestHistory_EngagedWhenSamplesMeetThreshold pins the opt-in path: when
// ≥ MinSamples token_spend rows exist for the cohort, History.Estimate
// returns the p95 of recorded USD — NOT the upper-bound fallback.
//
// This is the load-bearing assertion for spec §10 S1: "history-based
// estimator engaged when opt-in flag set + cohort warm".
func TestHistory_EngagedWhenSamplesMeetThreshold(t *testing.T) {
	db := openSpendDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// 20 token_spend rows for (operator=agent-7, model=claude-sonnet-4-7).
	// USD values 1.0..20.0 → p95 = 19.0 (linear interpolation between 18
	// and 19 yields 19.0 for a 20-element sorted set with R-7 method).
	for i := 1; i <= 20; i++ {
		insertSpend(t, db, float64(i), "claude-sonnet-4-7", "agent-7", "default",
			now.Add(-time.Duration(i)*time.Minute))
	}

	fb := &stubFallback{usd: 999.0}
	h := estimate.NewHistory(estimate.HistoryConfig{
		Reader:     spend.NewReader(db, func() time.Time { return now }),
		Fallback:   fb,
		MinSamples: 10,
		Period:     time.Hour,
		TenantID:   "default",
	})

	got, err := h.Estimate(context.Background(),
		gate.EstHint{InputTokens: 1000, MaxTokens: 4096},
		"claude-sonnet-4-7")
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	// Want history p95, NOT the fallback's 999.0. p95 of {1..20} is 19.0
	// (R-7 / nearest-rank=19 — both yield 19 here). Sanity: anything in
	// the [15, 20] band proves history is engaged.
	if got < 15.0 || got > 20.0 {
		t.Fatalf("Estimate=%v; want p95 in [15,20] band — history not engaged?", got)
	}
	if fb.called != 0 {
		t.Fatalf("fallback called %d times; history should have engaged with 20 samples", fb.called)
	}
}

// TestHistory_ColdStartFallback pins the < MinSamples path: when fewer
// than the threshold samples exist for the cohort, History delegates to
// the Fallback estimator (upper_bound in prod wiring).
func TestHistory_ColdStartFallback(t *testing.T) {
	db := openSpendDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// Only 3 rows — below MinSamples=10.
	for i := 1; i <= 3; i++ {
		insertSpend(t, db, float64(i)*10, "claude-sonnet-4-7", "agent-7", "default",
			now.Add(-time.Duration(i)*time.Minute))
	}

	fb := &stubFallback{usd: 0.42}
	h := estimate.NewHistory(estimate.HistoryConfig{
		Reader:     spend.NewReader(db, func() time.Time { return now }),
		Fallback:   fb,
		MinSamples: 10,
		Period:     time.Hour,
		TenantID:   "default",
	})

	got, err := h.Estimate(context.Background(),
		gate.EstHint{InputTokens: 1000, MaxTokens: 4096},
		"claude-sonnet-4-7")
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if got != 0.42 {
		t.Fatalf("Estimate=%v; want fallback 0.42 on cold-start (3 samples < min 10)", got)
	}
	if fb.called != 1 {
		t.Fatalf("fallback called %d times; want exactly 1 on cold-start", fb.called)
	}
}

// TestHistory_DeterministicAcrossCalls pins W9 replay-safety: same
// substrate snapshot ⇒ same estimate. The frozen clock + identical row
// set must produce byte-equal float64 results across invocations.
func TestHistory_DeterministicAcrossCalls(t *testing.T) {
	db := openSpendDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 15; i++ {
		insertSpend(t, db, float64(i)*0.5, "claude-haiku-4-5", "agent-1", "default",
			now.Add(-time.Duration(i)*time.Minute))
	}
	h := estimate.NewHistory(estimate.HistoryConfig{
		Reader:     spend.NewReader(db, func() time.Time { return now }),
		Fallback:   &stubFallback{},
		MinSamples: 10,
		Period:     time.Hour,
		TenantID:   "default",
	})

	first, err := h.Estimate(context.Background(),
		gate.EstHint{InputTokens: 1000, MaxTokens: 4096}, "claude-haiku-4-5")
	if err != nil {
		t.Fatalf("Estimate first: %v", err)
	}
	for i := 0; i < 50; i++ {
		got, err := h.Estimate(context.Background(),
			gate.EstHint{InputTokens: 1000, MaxTokens: 4096}, "claude-haiku-4-5")
		if err != nil {
			t.Fatalf("Estimate iter %d: %v", i, err)
		}
		if got != first {
			t.Fatalf("non-deterministic: iter %d got %v, first %v", i, got, first)
		}
	}
}

// TestHistory_CohortScopedByOperatorAndModel pins R9 forward-fit + spec
// requirement: history is per-(operator_id, model) cohort. Rows from a
// different operator or different model must NOT contaminate the
// estimate for (agent-7, claude-sonnet-4-7).
func TestHistory_CohortScopedByOperatorAndModel(t *testing.T) {
	db := openSpendDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// 15 in-cohort rows with low USD.
	for i := 1; i <= 15; i++ {
		insertSpend(t, db, 1.0, "claude-sonnet-4-7", "agent-7", "default",
			now.Add(-time.Duration(i)*time.Minute))
	}
	// 15 out-of-cohort rows (different operator) with very high USD.
	// If these leak into the p95, the estimate would jump near 999.
	for i := 1; i <= 15; i++ {
		insertSpend(t, db, 999.0, "claude-sonnet-4-7", "agent-OTHER", "default",
			now.Add(-time.Duration(i)*time.Minute))
	}
	// 15 different-model rows for the same operator — also must not leak.
	for i := 1; i <= 15; i++ {
		insertSpend(t, db, 555.0, "claude-haiku-4-5", "agent-7", "default",
			now.Add(-time.Duration(i)*time.Minute))
	}

	h := estimate.NewHistory(estimate.HistoryConfig{
		Reader:     spend.NewReader(db, func() time.Time { return now }),
		Fallback:   &stubFallback{},
		MinSamples: 10,
		Period:     time.Hour,
		TenantID:   "default",
	})
	got, err := h.Estimate(context.Background(),
		gate.EstHint{InputTokens: 1000, MaxTokens: 4096, OperatorID: "agent-7"},
		"claude-sonnet-4-7")
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	// p95 of 15 ones = 1.0; if cohort leak occurred, we'd see ≥ 555.
	if got > 5.0 {
		t.Fatalf("Estimate=%v; want ~1.0 (cohort leak from other operator/model rows)", got)
	}
}
