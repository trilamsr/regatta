// Package spend reads cumulative LLM spend out of substrate. Wave 1
// ships reader only; writer + payload structs land Wave 2 (T3).
// Reader is consumed by internal/cost/gate.Gate for pre-call deny.
// Every query is tenant_id-scoped for R9 W8-forward-fit; the substrate
// §6 lint rejects any unscoped SELECT.
package spend

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Reader is the gate-side cumulative-spend reader (spec §3.5 lines
// 300-303). Construct via NewReader — clock injection is load-bearing
// for deterministic period-window tests.
type Reader struct {
	db    *sql.DB
	clock func() time.Time
}

// NewReader binds clock as the period-anchor wall source; nil falls
// back to time.Now so production wiring omits the arg.
func NewReader(db *sql.DB, clock func() time.Time) *Reader {
	if clock == nil {
		clock = time.Now
	}
	return &Reader{db: db, clock: clock}
}

// BudgetState returns cumulative recorded spend (USD) for a scope over
// a period ending at the Reader clock's now (spec §3.5: single SELECT
// with SUM(json_extract($.usd)) over token_spend rows in window AND
// payload scope-field matches). scope.TenantID required and pinned in
// every query (R9). Scope IDs flow through `?` placeholders so a
// hostile payload cannot break out of the SQL literal.
func (r *Reader) BudgetState(ctx context.Context, scope ScopeKey, period time.Duration) (float64, error) {
	if scope.TenantID == "" {
		return 0, errors.New("spend.Reader.BudgetState: tenant_id required")
	}
	cutoff := r.clock().Add(-period).UnixMilli()

	var (
		scopeFilter string
		scopeArg    any
	)
	switch scope.Kind {
	case ScopeDAG:
		scopeFilter = "AND json_extract(payload_json, '$.dag_id') = ?"
		scopeArg = scope.DAGID
	case ScopeOperator:
		scopeFilter = "AND json_extract(payload_json, '$.operator_id') = ?"
		scopeArg = scope.OperatorID
	case ScopeWorkItem:
		scopeFilter = "AND json_extract(payload_json, '$.work_item_id') = ?"
		scopeArg = scope.WorkItemID
	case ScopeGlobal:
		scopeFilter = ""
	default:
		return 0, fmt.Errorf("spend.Reader.BudgetState: unknown scope kind %q", scope.Kind)
	}

	q := `SELECT COALESCE(SUM(json_extract(payload_json, '$.usd')), 0)
	      FROM substrate_events
	      WHERE kind = 'token_spend'
	        AND tenant_id = ?
	        AND written_at >= ? ` + scopeFilter
	args := []any{scope.TenantID, cutoff}
	if scopeFilter != "" {
		args = append(args, scopeArg)
	}
	var total float64
	if err := r.db.QueryRowContext(ctx, q, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("spend.Reader.BudgetState: %w", err)
	}
	return total, nil
}

// CohortSpends returns the USD column of every token_spend row
// matching (tenant_id, model), optionally filtered by operator_id,
// within the period ending at the Reader clock's now. Consumed by the
// opt-in `history` estimator (#238) for p95-cohort spend; cold-start
// falls back to upper_bound under threshold rows. operatorID="" means
// "all operators in tenant".
func (r *Reader) CohortSpends(ctx context.Context, tenantID, operatorID, model string, period time.Duration) ([]float64, error) {
	if tenantID == "" {
		return nil, errors.New("spend.Reader.CohortSpends: tenant_id required")
	}
	if model == "" {
		return nil, errors.New("spend.Reader.CohortSpends: model required")
	}
	cutoff := r.clock().Add(-period).UnixMilli()
	q := `SELECT json_extract(payload_json, '$.usd')
	      FROM substrate_events
	      WHERE kind = 'token_spend'
	        AND tenant_id = ?
	        AND written_at >= ?
	        AND json_extract(payload_json, '$.model') = ?`
	args := []any{tenantID, cutoff, model}
	if operatorID != "" {
		q += ` AND json_extract(payload_json, '$.operator_id') = ?`
		args = append(args, operatorID)
	}
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("spend.Reader.CohortSpends: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("spend.Reader.CohortSpends scan: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spend.Reader.CohortSpends rows: %w", err)
	}
	return out, nil
}

// RecordedUSDForWindow returns the cumulative locally-recorded USD
// for `tenantID` over `[start, end)`. The reconciler diffs this
// against the Anthropic Cost/Usage API total to compute drift_pct.
// Window is operator-supplied (reconciler aligns to bucket boundaries
// via WindowForTick) — unlike BudgetState's now-period anchoring.
func (r *Reader) RecordedUSDForWindow(ctx context.Context, tenantID string, start, end time.Time) (float64, error) {
	if tenantID == "" {
		return 0, errors.New("spend.Reader.RecordedUSDForWindow: tenant_id required")
	}
	startMS := start.UnixMilli()
	endMS := end.UnixMilli()
	var total float64
	err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(json_extract(payload_json, '$.usd')), 0)
		 FROM substrate_events
		 WHERE kind = 'token_spend'
		   AND tenant_id = ?
		   AND written_at >= ?
		   AND written_at < ?`,
		tenantID, startMS, endMS,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("spend.Reader.RecordedUSDForWindow: %w", err)
	}
	return total, nil
}

// LastReconciliation returns the most recent budget_reconciled row
// for the tenant — raw payload bytes + written_at; (nil, zero, nil)
// when no row exists. Raw-byte return keeps T1 file-disjoint with T3
// which owns BudgetReconciledPayload.
func (r *Reader) LastReconciliation(ctx context.Context, tenantID string) (json.RawMessage, time.Time, error) {
	if tenantID == "" {
		return nil, time.Time{}, errors.New("spend.Reader.LastReconciliation: tenant_id required")
	}
	var payload []byte
	var writtenAt int64
	err := r.db.QueryRowContext(ctx,
		`SELECT payload_json, written_at
		 FROM substrate_events
		 WHERE kind = 'budget_reconciled' AND tenant_id = ?
		 ORDER BY written_at DESC, id DESC
		 LIMIT 1`,
		tenantID,
	).Scan(&payload, &writtenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("spend.Reader.LastReconciliation: %w", err)
	}
	return json.RawMessage(append([]byte(nil), payload...)), time.UnixMilli(writtenAt).UTC(), nil
}
