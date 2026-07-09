package reconcile_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/cost/reconcile"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

const backfillKeyID = "k1"

var backfillKey = []byte("0123456789abcdef0123456789abcdef")

// TestCostBackfill_EmitsTokenSpendRows asserts backfill writes one row per (bucket,model) (#272).
func TestCostBackfill_EmitsTokenSpendRows(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "subs.db")
	db, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := state.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	substrate.ResetClockForTesting()

	// Seed two heartbeat rows under run-X so MIN/MAX(written_at) frames
	// a 2h window (10:30 → 12:30 UTC).
	seedRunWindow(t, ctx, db, "run-X",
		time.Date(2026, 6, 1, 10, 30, 0, 0, time.UTC),
		time.Date(2026, 6, 1, 12, 30, 0, 0, time.UTC),
	)

	// Stub Anthropic Usage API: returns 2 hour-buckets × 2 models.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fail on Cost API path so backfill goes via Usage API.
		if strings.Contains(r.URL.Path, "/cost_report/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"bucket_start":                "2026-06-01T10:00:00Z",
					"bucket_end":                  "2026-06-01T11:00:00Z",
					"model":                       "claude-sonnet-4-7",
					"uncached_input_tokens":       1_000_000,
					"cache_read_input_tokens":     0,
					"cache_creation_input_tokens": 0,
					"output_tokens":               500_000,
				},
				{
					"bucket_start":                "2026-06-01T11:00:00Z",
					"bucket_end":                  "2026-06-01T12:00:00Z",
					"model":                       "claude-haiku-4-5",
					"uncached_input_tokens":       2_000_000,
					"cache_read_input_tokens":     0,
					"cache_creation_input_tokens": 0,
					"output_tokens":               1_000_000,
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	t.Setenv("ANTHROPIC_ADMIN_KEY", "sk-admin-test")

	result, err := reconcile.Backfill(ctx, reconcile.BackfillConfig{
		DB:       db,
		RunID:    "run-X",
		TenantID: substrate.DefaultTenantID,
		BaseURL:  srv.URL,
		Key:      backfillKey,
		KeyID:    backfillKeyID,
		Now:      func() time.Time { return time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if result.RowsEmitted != 2 {
		t.Fatalf("RowsEmitted=%d, want 2", result.RowsEmitted)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT payload_json, run_id, written_by FROM substrate_events
		   WHERE kind='token_spend' ORDER BY id`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	defer func() { _ = rows.Close() }()
	models := map[string]bool{}
	for rows.Next() {
		var raw, runID, writtenBy string
		if err := rows.Scan(&raw, &runID, &writtenBy); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if runID != "run-X" {
			t.Fatalf("run_id=%q, want run-X", runID)
		}
		if writtenBy != "cost-backfill" {
			t.Fatalf("written_by=%q, want cost-backfill", writtenBy)
		}
		var p spend.TokenSpendPayload
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !strings.HasPrefix(p.PricingRev, "backfill:") {
			t.Fatalf("pricing_rev=%q, want backfill:<sha>", p.PricingRev)
		}
		models[p.Model] = true
	}
	for _, want := range []string{"claude-sonnet-4-7", "claude-haiku-4-5"} {
		if !models[want] {
			t.Fatalf("missing token_spend row for model %q", want)
		}
	}

	// Idempotency: re-run is a no-op (deterministic nonce ⇒ replay).
	result2, err := reconcile.Backfill(ctx, reconcile.BackfillConfig{
		DB:       db,
		RunID:    "run-X",
		TenantID: substrate.DefaultTenantID,
		BaseURL:  srv.URL,
		Key:      backfillKey,
		KeyID:    backfillKeyID,
		Now:      func() time.Time { return time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Backfill second run: %v", err)
	}
	if result2.RowsEmitted != 0 || result2.RowsSkipped != 2 {
		t.Fatalf("idempotent re-run: emitted=%d skipped=%d, want emitted=0 skipped=2",
			result2.RowsEmitted, result2.RowsSkipped)
	}
}

// TestCostBackfill_TagOverridesPricingRev pins R1 disambiguator (#705).
func TestCostBackfill_TagOverridesPricingRev(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "subs.db")
	db, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := state.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	substrate.ResetClockForTesting()
	seedRunWindow(t, ctx, db, "run-R1",
		time.Date(2026, 6, 1, 10, 30, 0, 0, time.UTC),
		time.Date(2026, 6, 1, 12, 30, 0, 0, time.UTC),
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cost_report/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"bucket_start":                "2026-06-01T10:00:00Z",
				"bucket_end":                  "2026-06-01T11:00:00Z",
				"model":                       "claude-sonnet-4-7",
				"uncached_input_tokens":       1_000_000,
				"cache_read_input_tokens":     0,
				"cache_creation_input_tokens": 0,
				"output_tokens":               500_000,
			}},
		})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ANTHROPIC_ADMIN_KEY", "sk-admin-test")
	res, err := reconcile.Backfill(ctx, reconcile.BackfillConfig{
		DB:       db,
		RunID:    "run-R1",
		TenantID: substrate.DefaultTenantID,
		BaseURL:  srv.URL,
		Key:      backfillKey,
		KeyID:    backfillKeyID,
		Tag:      "ops-2026-06-03",
		Now:      func() time.Time { return time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if got, want := res.PricingRev, "backfill:ops-2026-06-03"; got != want {
		t.Fatalf("PricingRev=%q, want %q", got, want)
	}
}

// TestCostBackfill_WorkItemIDHasRandomSuffix pins R2 collision-proof prefix (#705).
func TestCostBackfill_WorkItemIDHasRandomSuffix(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "subs.db")
	db, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := state.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	substrate.ResetClockForTesting()
	seedRunWindow(t, ctx, db, "run-R2",
		time.Date(2026, 6, 1, 10, 30, 0, 0, time.UTC),
		time.Date(2026, 6, 1, 12, 30, 0, 0, time.UTC),
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cost_report/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"bucket_start":                "2026-06-01T10:00:00Z",
				"bucket_end":                  "2026-06-01T11:00:00Z",
				"model":                       "claude-sonnet-4-7",
				"uncached_input_tokens":       1_000_000,
				"cache_read_input_tokens":     0,
				"cache_creation_input_tokens": 0,
				"output_tokens":               500_000,
			}},
		})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ANTHROPIC_ADMIN_KEY", "sk-admin-test")
	_, err = reconcile.Backfill(ctx, reconcile.BackfillConfig{
		DB:       db,
		RunID:    "run-R2",
		TenantID: substrate.DefaultTenantID,
		BaseURL:  srv.URL,
		Key:      backfillKey,
		KeyID:    backfillKeyID,
		Now:      func() time.Time { return time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	var wid string
	if err := db.QueryRowContext(ctx,
		`SELECT work_item_id FROM substrate_events WHERE kind='token_spend' AND run_id='run-R2' LIMIT 1`,
	).Scan(&wid); err != nil {
		t.Fatalf("scan work_item_id: %v", err)
	}
	// Format: backfill:<run_id>:<8-hex>. The 8-hex suffix prevents
	// collision with operator-named work items prefixed `backfill:`.
	prefix := "backfill:run-R2:"
	if !strings.HasPrefix(wid, prefix) {
		t.Fatalf("work_item_id=%q, want prefix %q", wid, prefix)
	}
	suffix := strings.TrimPrefix(wid, prefix)
	if len(suffix) != 8 {
		t.Fatalf("work_item_id suffix=%q (len %d), want 8-hex", suffix, len(suffix))
	}
	for _, c := range suffix {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("work_item_id suffix=%q has non-hex char %q", suffix, c)
		}
	}
}

// TestCostBackfill_SingleEventWindow flags synthetic 1h window (#705 R4).
func TestCostBackfill_SingleEventWindow(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "subs.db")
	db, err := sql.Open("sqlite", state.DSN(dbPath))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := state.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	substrate.ResetClockForTesting()
	// Single event → MIN == MAX → start.Equal(end).
	at := time.Date(2026, 6, 1, 10, 30, 0, 0, time.UTC)
	seedRunWindow(t, ctx, db, "run-R4", at, at)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/cost_report/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ANTHROPIC_ADMIN_KEY", "sk-admin-test")
	res, err := reconcile.Backfill(ctx, reconcile.BackfillConfig{
		DB:       db,
		RunID:    "run-R4",
		TenantID: substrate.DefaultTenantID,
		BaseURL:  srv.URL,
		Key:      backfillKey,
		KeyID:    backfillKeyID,
		Now:      func() time.Time { return time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if !res.SingleEventWindow {
		t.Fatalf("SingleEventWindow=false, want true when MIN==MAX")
	}
	if res.RowsEmitted != 0 {
		t.Fatalf("RowsEmitted=%d, want 0 on empty Usage response", res.RowsEmitted)
	}
}

// seedRunWindow inserts two heartbeat rows defining the run window.
func seedRunWindow(t *testing.T, ctx context.Context, db *sql.DB, runID string, start, end time.Time) {
	t.Helper()
	for i, at := range []time.Time{start, end} {
		ev := substrate.SubstrateEvent{
			ID:            substrate.Mint(at),
			RunID:         runID,
			WorkItemID:    "WI-1",
			TenantID:      substrate.DefaultTenantID,
			Kind:          substrate.KindHeartbeat,
			PayloadJSON:   json.RawMessage(fmt.Sprintf(`{"work_item_id":"WI-1","timestamp":%d}`, at.UnixMilli())),
			WrittenBy:     "spawner-test",
			WrittenAt:     at.UnixMilli(),
			SchemaVersion: 1,
			Nonce:         fmt.Sprintf("hb-%d", i),
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if err := substrate.AppendEvent(ctx, tx, ev, backfillKey, backfillKeyID); err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed AppendEvent: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}
}
