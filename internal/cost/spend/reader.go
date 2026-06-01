// Package spend reads cumulative LLM spend out of the substrate event
// log. Wave 1 ships the reader only; the writer + payload structs land
// in Wave 2 (T3 — feedback_shared_primitive_owner).
//
// The Reader is consumed by internal/cost/gate.Gate for pre-call deny
// decisions. Every query is scoped by tenant_id for R9 W8-forward-fit;
// the substrate-spec §6 lint rejects any unscoped SELECT.
package spend

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Reader is the gate-side cumulative-spend reader. Spec §3.5 lines
// 300-303 verbatim. Construction is via NewReader; callers do not
// build zero-value Readers (clock injection is load-bearing for
// deterministic period-window tests).
type Reader struct {
	db    *sql.DB
	clock func() time.Time
}

// NewReader builds a Reader. clock is the wall-clock source used to
// anchor the period window; tests inject a fixed-time closure for
// deterministic excludes-stale assertions. nil clock falls back to
// time.Now so production callers omit the second arg in regular wiring.
func NewReader(db *sql.DB, clock func() time.Time) *Reader {
	if clock == nil {
		clock = time.Now
	}
	return &Reader{db: db, clock: clock}
}

// BudgetState returns cumulative recorded spend (USD) for a scope over
// a period ending at the Reader clock's now. Spec §3.5: single SELECT
// with SUM(json_extract(payload_json, '$.usd')) over kind='token_spend'
// rows whose written_at is in the window AND payload scope-field
// matches.
//
// scope.TenantID is required and pinned in every query — R9 W8-forward-
// fit per spec §9. The scope field placeholder is selected by
// scope.Kind; scope IDs are bound as parameters (NOT interpolated) so
// SQL injection is impossible.
func (r *Reader) BudgetState(ctx context.Context, scope ScopeKey, period time.Duration) (float64, error) {
	if scope.TenantID == "" {
		return 0, errors.New("spend.Reader.BudgetState: tenant_id required")
	}
	cutoff := r.clock().Add(-period).UnixMilli()

	// scopeField is selected from a Go-side switch — never user input.
	// All scope IDs flow through `?` placeholders so a hostile payload
	// cannot break out of the SQL literal.
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

// LastReconciliation returns the most recent budget_reconciled row for
// the tenant — raw payload bytes + written_at timestamp. Returns
// (nil, zero, nil) when no row exists.
//
// Wave 1 ships the raw-byte return so T1 stays file-disjoint with T3
// (which owns BudgetReconciledPayload). Callers that want the typed
// struct in Wave 2 wrap this with json.Unmarshal against T3's type.
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
