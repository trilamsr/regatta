// Command check-pricing-drift compares the hardcoded
// internal/cost/pricing.Anthropic table against per-model implied rates
// recovered from substrate `budget_reconciled` events. Closes the
// cost-governor design spec §7 A+5 rubric entry.
//
// Why a separate tool (not a runtime check): the reconciler already emits
// obs.EventCostDriftAlert when actual >> recorded inside one period, but
// that signal fires only when the operator's spend is large enough to
// cross the per-period drift threshold. Source-table drift can hide
// inside per-period noise — Anthropic raises Sonnet input by 15% on the
// same week the operator's input/output mix shifts and the per-period
// alert never trips. A nightly cross-window check on the same
// `cost_usd ÷ tokens` ratio surfaces that latent drift before the
// operator overshoots a budget.
//
// Mechanics:
//  1. Scan budget_reconciled rows whose written_at falls in the window
//     (default 7d, --window flag).
//  2. Per row, skip rows whose model_breakdown carries per-row tokens —
//     those are Usage API fallback writes and the comparison is
//     self-referential (both sides apply the same pricing table). Cost
//     API rows store USD-only per model (per cost/reconcile/tick.go).
//  3. Sum token_spend tokens-by-model inside [period_start, period_end]
//     from the substrate. That is our pricing.Anthropic-derived
//     expected USD denominator.
//  4. Compare expected_usd (apply pricing.Anthropic to summed tokens)
//     vs actual_usd from the reconciled row. drift_pct =
//     abs(actual - expected) / max(expected, 0.01).
//  5. Findings: drift_pct > threshold (default 0.05) OR model has no
//     pricing row (table is missing a SKU Anthropic billed).
//
// Exit codes: 0 clean, 1 findings, 2 usage/config error.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/internal/cost/pricing"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point — extracted so deferred db.Close fires
// before any return, satisfying gocritic's exitAfterDefer rule.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check-pricing-drift", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", "regatta.db", "Path to the regatta sqlite state DB")
	window := fs.Duration("window", 7*24*time.Hour, "Lookback window for budget_reconciled rows")
	threshold := fs.Float64("threshold", 0.05, "Per-model drift fraction that triggers a finding (0.05 = 5%)")
	tenantID := fs.String("tenant", "default", "Tenant scope; only this tenant's events are scanned")
	jsonOutput := fs.Bool("json", false, "Emit findings as JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	db, err := sql.Open("sqlite", state.DSN(*dbPath))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "check-pricing-drift: open db:", err)
		return 2
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	findings, err := detectDrift(context.Background(), db, detectOptions{
		Window:    *window,
		Threshold: *threshold,
		Now:       time.Now(),
		TenantID:  *tenantID,
		Pricing:   pricing.Anthropic,
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "check-pricing-drift:", err)
		return 2
	}
	if len(findings) == 0 {
		_, _ = fmt.Fprintln(stdout, "check-pricing-drift: clean (no drift > threshold in window)")
		return 0
	}
	if *jsonOutput {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(findings)
	} else {
		renderFindings(stdout, findings)
	}
	return 1
}

// detectOptions packs the seam between the CLI flags and the testable
// detectDrift entry point.
type detectOptions struct {
	Window    time.Duration
	Threshold float64
	Now       time.Time
	TenantID  string
	Pricing   map[string]pricing.Row
}

// driftFinding is one row in the report. Exposed to JSON so CI dashboards
// can grep --json output.
type driftFinding struct {
	Model       string  `json:"model"`
	PeriodStart int64   `json:"period_start"`
	PeriodEnd   int64   `json:"period_end"`
	ExpectedUSD float64 `json:"expected_usd"`
	ActualUSD   float64 `json:"actual_usd"`
	DriftPct    float64 `json:"drift_pct"`
	Reason      string  `json:"reason"`
}

// reconciledRow mirrors the relevant subset of spend.BudgetReconciledPayload
// we need to compute drift. Kept local so this tool stays import-clean of
// internal/cost/spend (which would force a tools→internal coupling growth).
type reconciledRow struct {
	PeriodStart    int64                 `json:"period_start"`
	PeriodEnd      int64                 `json:"period_end"`
	ModelBreakdown []reconciledBreakdown `json:"model_breakdown"`
}

type reconciledBreakdown struct {
	Model               string  `json:"model"`
	USD                 float64 `json:"usd"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
}

// tokenSpendRow is the subset of spend.TokenSpendPayload needed to compute
// per-model token totals for the implied-rate denominator.
type tokenSpendRow struct {
	Model               string `json:"model"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
}

// detectDrift is the tested entry point. Queries are scoped by
// (kind, tenant_id) per substrate spec §6.
func detectDrift(ctx context.Context, db *sql.DB, opt detectOptions) ([]driftFinding, error) {
	if opt.TenantID == "" {
		return nil, errors.New("tenant_id required")
	}
	cutoff := opt.Now.Add(-opt.Window).UnixMilli()

	// Materialise reconciled rows up-front so the sqlite single-connection
	// pool (MaxOpenConns=1) is free for tokensByModel's per-period query.
	// Iterating both queries concurrently would deadlock the conn.
	reconciledRows, err := loadReconciled(ctx, db, opt.TenantID, cutoff)
	if err != nil {
		return nil, err
	}

	var findings []driftFinding
	for _, r := range reconciledRows {
		// Sum tokens by model from token_spend rows in this period — the
		// denominator of the implied-rate calculation. Done once per
		// reconciled row so the queries scale with reconcile cadence (1/h
		// default) rather than per-call traffic.
		tokens, err := tokensByModel(ctx, db, opt.TenantID, r.PeriodStart, r.PeriodEnd)
		if err != nil {
			return nil, err
		}
		for _, b := range r.ModelBreakdown {
			// Usage API fallback path: per-row tokens populated. Skip —
			// the comparison would be self-referential (spec §7 A+5).
			if b.InputTokens != 0 || b.OutputTokens != 0 || b.CacheReadTokens != 0 || b.CacheCreationTokens != 0 {
				continue
			}
			row, ok := opt.Pricing[b.Model]
			if !ok {
				findings = append(findings, driftFinding{
					Model:       b.Model,
					PeriodStart: r.PeriodStart,
					PeriodEnd:   r.PeriodEnd,
					ActualUSD:   b.USD,
					Reason:      fmt.Sprintf("pricing row missing for SKU billed at $%.4f", b.USD),
				})
				continue
			}
			t, has := tokens[b.Model]
			if !has || (t.InputTokens+t.OutputTokens+t.CacheReadTokens+t.CacheCreationTokens) == 0 {
				// No token_spend rows in the period for this model. Cannot
				// compute implied rate; skip rather than emit a false
				// positive — the reconciler's per-period drift alert is
				// the right signal here, not this nightly check.
				continue
			}
			expected := expectedUSD(row, t)
			if expected <= 0 {
				continue
			}
			drift := absFloat(b.USD-expected) / expected
			if drift > opt.Threshold {
				findings = append(findings, driftFinding{
					Model:       b.Model,
					PeriodStart: r.PeriodStart,
					PeriodEnd:   r.PeriodEnd,
					ExpectedUSD: expected,
					ActualUSD:   b.USD,
					DriftPct:    drift,
					Reason:      driftReason(b.USD, expected, drift),
				})
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].PeriodStart != findings[j].PeriodStart {
			return findings[i].PeriodStart < findings[j].PeriodStart
		}
		return findings[i].Model < findings[j].Model
	})
	return findings, nil
}

func loadReconciled(ctx context.Context, db *sql.DB, tenantID string, cutoff int64) ([]reconciledRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT payload_json
		FROM substrate_events
		WHERE kind = 'budget_reconciled'
		  AND tenant_id = ?
		  AND written_at >= ?
		ORDER BY written_at ASC`, tenantID, cutoff)
	if err != nil {
		return nil, fmt.Errorf("query budget_reconciled: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []reconciledRow
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan reconciled: %w", err)
		}
		var r reconciledRow
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("decode reconciled payload: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reconciled: %w", err)
	}
	return out, nil
}

func tokensByModel(ctx context.Context, db *sql.DB, tenantID string, periodStart, periodEnd int64) (map[string]tokenSpendRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT payload_json
		FROM substrate_events
		WHERE kind = 'token_spend'
		  AND tenant_id = ?
		  AND written_at >= ?
		  AND written_at < ?`, tenantID, periodStart, periodEnd)
	if err != nil {
		return nil, fmt.Errorf("query token_spend: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]tokenSpendRow{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan token_spend: %w", err)
		}
		var t tokenSpendRow
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, fmt.Errorf("decode token_spend payload: %w", err)
		}
		acc := out[t.Model]
		acc.Model = t.Model
		acc.InputTokens += t.InputTokens
		acc.OutputTokens += t.OutputTokens
		acc.CacheReadTokens += t.CacheReadTokens
		acc.CacheCreationTokens += t.CacheCreationTokens
		out[t.Model] = acc
	}
	return out, rows.Err()
}

func expectedUSD(row pricing.Row, t tokenSpendRow) float64 {
	return perMillion(t.InputTokens, row.InputUSDPerMTok) +
		perMillion(t.OutputTokens, row.OutputUSDPerMTok) +
		perMillion(t.CacheReadTokens, row.CacheReadUSDPerMTok) +
		perMillion(t.CacheCreationTokens, row.CacheCreationUSDPerMTok)
}

func perMillion(tokens int64, ratePerMTok float64) float64 {
	return float64(tokens) * ratePerMTok / 1_000_000.0
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func driftReason(actual, expected, drift float64) string {
	if actual > expected {
		return fmt.Sprintf("actual exceeds expected by %.2f%%", drift*100)
	}
	return fmt.Sprintf("actual undershoots expected by %.2f%%", drift*100)
}

// renderFindings writes the human-readable table. Operator runbook in
// docs/operator/cost-governor.md links to this output shape.
func renderFindings(out io.Writer, findings []driftFinding) {
	_, _ = fmt.Fprintln(out, "check-pricing-drift: drift findings (Cost API path only):")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "MODEL\tPERIOD_START\tEXPECTED_USD\tACTUAL_USD\tDRIFT_PCT\tREASON")
	for _, f := range findings {
		periodLabel := "n/a"
		if f.PeriodStart > 0 {
			periodLabel = time.UnixMilli(f.PeriodStart).UTC().Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%.4f\t%.4f\t%.2f%%\t%s\n",
			f.Model, periodLabel, f.ExpectedUSD, f.ActualUSD, f.DriftPct*100, f.Reason)
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintln(out, strings.Repeat("-", 60))
	_, _ = fmt.Fprintf(out, "Refresh pricing table per docs/operator/cost-governor.md \"Pricing refresh\" runbook.\n")
}
