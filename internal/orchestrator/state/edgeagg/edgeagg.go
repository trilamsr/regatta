package edgeagg // pure types + reducers behind state.CountNonDefaultEdgeStates; no state import (cycle).

import (
	"database/sql"
	"fmt"
	"time"
)

// EdgeRow mirrors a work_item_edges row; Fired is a string sum so the 'pending' sentinel is queryable via idx_work_item_edges_from(from_id, fired).
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

// EdgeFromAggregate carries the 5 facts the scheduler default-fallback needs about a from_id's sibling set; avoids per-group sibling-slice allocs (see #187).
type EdgeFromAggregate struct {
	NonDefaultCount      int
	AnyNonDefaultTrue    bool
	AnyNonDefaultPending bool
	DefaultCount         int
	DefaultEdgeID        int64
	DefaultFired         string
	DefaultProgramID     string
}

// BuildAggregate folds raw sql.Null* scan outputs into an EdgeFromAggregate; extracted so the reducer is table-testable without a real *sql.DB round-trip.
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

// ScanEdges materialises EdgeRows from rows over selectEdgeCols; co-located with EdgeRow so the row shape stays adjacent to the struct.
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

// BoolToInt maps a bool to its SQLite 0/1 representation; exported so state-side edge writers share the same helper.
func BoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
