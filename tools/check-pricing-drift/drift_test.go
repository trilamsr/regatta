package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/cost/pricing"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// insertCounter monotonically uniqueifies the synthetic (id, nonce) across
// inserts so the UNIQUE(run_id, written_by, nonce) substrate constraint does
// not collide on co-located rows.
var insertCounter int64

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "drift.db")
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

func insertEvent(t *testing.T, db *sql.DB, kind, payload string, writtenAt time.Time, tenantID, key string) {
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

// reconciledPayload builds a budget_reconciled JSON payload for a single Cost
// API bucket (no per-row tokens — Cost API path). Tokens come from sibling
// token_spend rows in the same window.
func reconciledPayload(periodStart, periodEnd time.Time, model string, costUSD float64) string {
	return fmt.Sprintf(`{
		"period_start": %d,
		"period_end":   %d,
		"actual_usd":   %f,
		"recorded_usd": 0,
		"delta_usd":    %f,
		"drift_pct":    0,
		"model_breakdown": [
			{"model":"%s","usd":%f,"input_tokens":0,"output_tokens":0,"cache_read_tokens":0,"cache_creation_tokens":0}
		],
		"api_response_sig": "deadbeef"
	}`, periodStart.UnixMilli(), periodEnd.UnixMilli(), costUSD, costUSD, model, costUSD)
}

func tokenSpendPayload(model string, in, out, cr, cc int64, usd float64) string {
	return fmt.Sprintf(`{
		"usd":                  %f,
		"model":                "%s",
		"input_tokens":         %d,
		"output_tokens":        %d,
		"cache_read_tokens":    %d,
		"cache_creation_tokens": %d,
		"operator_id":          "op-1",
		"dag_id":               "dag-1",
		"work_item_id":         "wi-1",
		"pricing_rev":          "rev-1",
		"call_id":              "call-1"
	}`, usd, model, in, out, cr, cc)
}

// TestDetect_NoDrift_TableMatchesActualRate confirms a clean run when the
// implied rate from substrate matches pricing.Anthropic within threshold.
// Pricing for claude-sonnet-4-7: input=3.00, output=15.00 per Mtok.
// 1M input + 1M output tokens at table rates = $18.00. We emit actual=$18.00.
// Implied/expected ratio = 1.0; drift = 0%; below 5% threshold; exit 0.
func TestDetect_NoDrift_TableMatchesActualRate(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	periodStart := now.Add(-time.Hour)
	periodEnd := now

	insertEvent(t, db, "token_spend",
		tokenSpendPayload("claude-sonnet-4-7", 1_000_000, 1_000_000, 0, 0, 18.0),
		now.Add(-30*time.Minute), "default", "")
	insertEvent(t, db, "budget_reconciled",
		reconciledPayload(periodStart, periodEnd, "claude-sonnet-4-7", 18.0),
		now.Add(-29*time.Minute), "default", fmt.Sprintf("%d", periodStart.UnixMilli()))

	findings, err := detectDrift(context.Background(), db, detectOptions{
		Window:    24 * time.Hour,
		Threshold: 0.05,
		Now:       now,
		TenantID:  "default",
		Pricing:   pricing.Anthropic,
	})
	if err != nil {
		t.Fatalf("detectDrift: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings=%v; want none", findings)
	}
}

// TestDetect_Drift_TableUnderprices simulates Anthropic charging 20% more
// than our table predicts — operator overshoots cap silently. The script
// MUST flag this so the table refresh runbook fires.
func TestDetect_Drift_TableUnderprices(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	periodStart := now.Add(-time.Hour)
	periodEnd := now

	insertEvent(t, db, "token_spend",
		tokenSpendPayload("claude-sonnet-4-7", 1_000_000, 1_000_000, 0, 0, 18.0),
		now.Add(-30*time.Minute), "default", "")
	insertEvent(t, db, "budget_reconciled",
		reconciledPayload(periodStart, periodEnd, "claude-sonnet-4-7", 21.60),
		now.Add(-29*time.Minute), "default", fmt.Sprintf("%d", periodStart.UnixMilli()))

	findings, err := detectDrift(context.Background(), db, detectOptions{
		Window:    24 * time.Hour,
		Threshold: 0.05,
		Now:       now,
		TenantID:  "default",
		Pricing:   pricing.Anthropic,
	})
	if err != nil {
		t.Fatalf("detectDrift: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings=%d; want 1; got %+v", len(findings), findings)
	}
	if findings[0].Model != "claude-sonnet-4-7" {
		t.Fatalf("model=%q; want claude-sonnet-4-7", findings[0].Model)
	}
	if findings[0].DriftPct < 0.15 || findings[0].DriftPct > 0.25 {
		t.Fatalf("driftPct=%f; want ~0.20", findings[0].DriftPct)
	}
}

// TestDetect_BelowThreshold confirms a sub-threshold drift does NOT fire.
// 3% drift with 5% threshold = clean.
func TestDetect_BelowThreshold(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	periodStart := now.Add(-time.Hour)
	periodEnd := now

	insertEvent(t, db, "token_spend",
		tokenSpendPayload("claude-haiku-4-5", 1_000_000, 1_000_000, 0, 0, 4.80),
		now.Add(-30*time.Minute), "default", "")
	insertEvent(t, db, "budget_reconciled",
		reconciledPayload(periodStart, periodEnd, "claude-haiku-4-5", 4.944), // +3%
		now.Add(-29*time.Minute), "default", fmt.Sprintf("%d", periodStart.UnixMilli()))

	findings, err := detectDrift(context.Background(), db, detectOptions{
		Window:    24 * time.Hour,
		Threshold: 0.05,
		Now:       now,
		TenantID:  "default",
		Pricing:   pricing.Anthropic,
	})
	if err != nil {
		t.Fatalf("detectDrift: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings=%v; want none (3%% drift below 5%% threshold)", findings)
	}
}

// TestDetect_SkipUsageFallbackRows pins spec A+5: Usage API path is
// self-referential (both sides applied the same pricing table) so its
// reconciled rows MUST be skipped, leaving no findings even when the
// embedded model_breakdown looks off.
func TestDetect_SkipUsageFallbackRows(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	periodStart := now.Add(-time.Hour)
	periodEnd := now

	// Usage API fallback path: per-row tokens are populated.
	payload := fmt.Sprintf(`{
		"period_start": %d,
		"period_end":   %d,
		"actual_usd":   100.0,
		"recorded_usd": 50.0,
		"delta_usd":    50.0,
		"drift_pct":    0.5,
		"model_breakdown": [
			{"model":"claude-opus-4-7","usd":100.0,"input_tokens":1000000,"output_tokens":0,"cache_read_tokens":0,"cache_creation_tokens":0}
		],
		"api_response_sig": "cafebabe"
	}`, periodStart.UnixMilli(), periodEnd.UnixMilli())

	insertEvent(t, db, "budget_reconciled", payload,
		now.Add(-29*time.Minute), "default", fmt.Sprintf("%d", periodStart.UnixMilli()))

	findings, err := detectDrift(context.Background(), db, detectOptions{
		Window:    24 * time.Hour,
		Threshold: 0.05,
		Now:       now,
		TenantID:  "default",
		Pricing:   pricing.Anthropic,
	})
	if err != nil {
		t.Fatalf("detectDrift: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings=%v; want none (Usage API fallback rows are skipped per spec A+5)", findings)
	}
}

// TestDetect_WindowExcludesStale pins the configurable window — events
// outside it are ignored, so the script does not flag drift from
// pre-table-refresh history.
func TestDetect_WindowExcludesStale(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	stalePeriodStart := now.Add(-30 * 24 * time.Hour)
	stalePeriodEnd := stalePeriodStart.Add(time.Hour)

	insertEvent(t, db, "token_spend",
		tokenSpendPayload("claude-opus-4-7", 1_000_000, 0, 0, 0, 15.0),
		stalePeriodStart.Add(30*time.Minute), "default", "")
	// Severely drifted but stale.
	insertEvent(t, db, "budget_reconciled",
		reconciledPayload(stalePeriodStart, stalePeriodEnd, "claude-opus-4-7", 30.0),
		stalePeriodStart.Add(31*time.Minute), "default",
		fmt.Sprintf("%d", stalePeriodStart.UnixMilli()))

	findings, err := detectDrift(context.Background(), db, detectOptions{
		Window:    7 * 24 * time.Hour,
		Threshold: 0.05,
		Now:       now,
		TenantID:  "default",
		Pricing:   pricing.Anthropic,
	})
	if err != nil {
		t.Fatalf("detectDrift: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings=%v; want none (stale window excluded)", findings)
	}
}

// TestDetect_MissingPricingRow surfaces a finding when the substrate has
// reconciled rows for a model that pricing.go does not know about — that is
// itself a drift class: pricing table is missing a row Anthropic billed.
func TestDetect_MissingPricingRow(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	periodStart := now.Add(-time.Hour)
	periodEnd := now

	insertEvent(t, db, "token_spend",
		tokenSpendPayload("claude-mystery-9-9", 1_000_000, 0, 0, 0, 5.0),
		now.Add(-30*time.Minute), "default", "")
	insertEvent(t, db, "budget_reconciled",
		reconciledPayload(periodStart, periodEnd, "claude-mystery-9-9", 5.0),
		now.Add(-29*time.Minute), "default", fmt.Sprintf("%d", periodStart.UnixMilli()))

	findings, err := detectDrift(context.Background(), db, detectOptions{
		Window:    24 * time.Hour,
		Threshold: 0.05,
		Now:       now,
		TenantID:  "default",
		Pricing:   pricing.Anthropic,
	})
	if err != nil {
		t.Fatalf("detectDrift: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings=%d; want 1 (missing pricing row)", len(findings))
	}
	if !strings.Contains(findings[0].Reason, "pricing row missing") {
		t.Fatalf("reason=%q; want substring 'pricing row missing'", findings[0].Reason)
	}
}

// TestRender_TableHumanReadable verifies the operator-facing report format.
// One row per finding, model + expected + actual + drift% + reason.
func TestRender_TableHumanReadable(t *testing.T) {
	var sb strings.Builder
	renderFindings(&sb, []driftFinding{{
		Model:       "claude-sonnet-4-7",
		ExpectedUSD: 18.0,
		ActualUSD:   21.6,
		DriftPct:    0.20,
		Reason:      "actual exceeds expected by 20.00%",
	}})
	out := sb.String()
	for _, want := range []string{"claude-sonnet-4-7", "18.0", "21.6", "20.0", "actual exceeds"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}
