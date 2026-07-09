// Package edgeagg holds the pure row types and reducers behind
// state.CountNonDefaultEdgeStates so the scheduler's default-edge
// fallback path is table-testable without a real *sql.DB round-trip.
//
// The split exists to break an import cycle: state depends on
// edgeagg, and edgeagg MUST NOT import state. BuildAggregate folds
// raw sql.Null* scan outputs into an EdgeFromAggregate carrying the
// 5 facts the scheduler needs about a from_id's sibling set,
// avoiding per-group sibling-slice allocations (#187). Does NOT open
// database handles or run queries — callers own the *sql.DB and hand
// rows in via ScanEdges.
package edgeagg
