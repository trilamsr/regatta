// Package edgeagg holds the pure data types and reducers behind
// state.CountNonDefaultEdgeStates. The *DB method shell stays in
// internal/orchestrator/state; only the no-receiver, no-state-import
// logic lives here. See docs/engineer/specs/2026-06-04-state-package-
// split-design.md §4.2 + §5.T2 — do NOT import state from this package
// (circular import; enforced mechanically by T6's check-state-tier-order.sh).
package edgeagg

import (
	"database/sql"
	"fmt"
	"time"
)

// EdgeRow mirrors a row in work_item_edges. Fired uses a string sum
// rather than sql.NullBool so the "not yet evaluated" sentinel
// ("pending") is queryable via an indexed equality predicate; see
// idx_work_item_edges_from(from_id, fired) in migration 0003.
type EdgeRow struct {
	ID           int64
	ProgramID    string
	FromID       string
	ToID         string
	PredicateCEL string
	IsDefault    bool
	OnSkip       string
	Fired        string
	FiredAgainst string
	EvaluatedAt  time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// EdgeFromAggregate carries the five facts the scheduler's
// default-fallback decision needs about a from_id's sibling set
// (evalPendingEdges, scheduler.go default-fallback branch). It exists
// so the post-write check can avoid materialising the sibling slice
// per pending-edge group — see #187 for the +19% allocs/op regression
// the slice path was paying at N=1000.
type EdgeFromAggregate struct {
	// NonDefaultCount is the number of is_default=0 rows. The fallback
	// guard `nonDefaultFired == 0` reads this.
	NonDefaultCount int
	// AnyNonDefaultTrue is true iff at least one is_default=0 row has
	// fired='true'. Blocks the fallback (a real branch already won).
	AnyNonDefaultTrue bool
	// AnyNonDefaultPending is true iff at least one is_default=0 row
	// still has fired='pending'. Blocks the fallback (decision not
	// yet ripe).
	AnyNonDefaultPending bool
	// DefaultCount is the number of is_default=1 rows. >1 is a brief
	// misconfiguration the scheduler warns about (deduped per
	// (program_id, from_id)).
	DefaultCount int
	// DefaultEdgeID identifies the default row to mark fired when the
	// fallback wins. Selects the lowest id when multiple defaults
	// exist so the choice is deterministic; the multi-default warn
	// still fires regardless.
	DefaultEdgeID int64
	// DefaultFired is the fired status of the row identified by
	// DefaultEdgeID. The fallback runs only when this is 'pending'.
	DefaultFired string
	// DefaultProgramID labels the multi-default warn so a misconfigured
	// brief is identifiable in logs.
	DefaultProgramID string
}

// BuildAggregate folds the raw sql.Null* scan outputs into an
// EdgeFromAggregate. Extracted from CountNonDefaultEdgeStates so the
// reducer can be table-tested without a real *sql.DB round-trip.
func BuildAggregate(
	nonDefaultCount int,
	anyTrue, anyPending sql.NullInt64,
	defaultCount int,
	defID sql.NullInt64,
	defFired, defProgram sql.NullString,
) EdgeFromAggregate {
	agg := EdgeFromAggregate{
		NonDefaultCount:      nonDefaultCount,
		AnyNonDefaultTrue:    anyTrue.Int64 != 0,
		AnyNonDefaultPending: anyPending.Int64 != 0,
		DefaultCount:         defaultCount,
	}
	if defID.Valid {
		agg.DefaultEdgeID = defID.Int64
	}
	if defFired.Valid {
		agg.DefaultFired = defFired.String
	}
	if defProgram.Valid {
		agg.DefaultProgramID = defProgram.String
	}
	return agg
}

// ScanEdges materialises EdgeRows from a query against the
// selectEdgeCols projection in state/work_item_edges.go. Lives here so
// the row shape is co-located with the EdgeRow definition; callers in
// state pass the *sql.Rows back so error/Close semantics stay on the
// *DB side.
func ScanEdges(rows *sql.Rows) ([]EdgeRow, error) {
	var out []EdgeRow
	for rows.Next() {
		var e EdgeRow
		var isDefault int
		var evaluated, created, updated int64
		if err := rows.Scan(&e.ID, &e.ProgramID, &e.FromID, &e.ToID,
			&e.PredicateCEL, &isDefault, &e.OnSkip, &e.Fired, &e.FiredAgainst,
			&evaluated, &created, &updated); err != nil {
			return nil, fmt.Errorf("edgeagg: scan edge: %w", err)
		}
		e.IsDefault = isDefault != 0
		if evaluated != 0 {
			e.EvaluatedAt = time.Unix(evaluated, 0).UTC()
		}
		e.CreatedAt = time.Unix(created, 0).UTC()
		e.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// BoolToInt maps a bool to its SQLite-compatible 0/1 representation.
// Exported so state package edge writers share the same helper
// without keeping a file-private copy.
func BoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
