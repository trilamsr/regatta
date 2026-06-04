package state

import "github.com/trilamsr/regatta/internal/orchestrator/state/edgeagg"

// EdgeRow re-exports edgeagg.EdgeRow so callers keep their state.EdgeRow
// spelling after the T2 split (spec §4.2). Do NOT import state from
// edgeagg/, cycle/, jsonscan/, transitions/ — check-state-tier-order.sh
// (T6) enforces the one-direction rule mechanically.
type EdgeRow = edgeagg.EdgeRow

// EdgeFromAggregate re-exports edgeagg.EdgeFromAggregate; same rationale
// as EdgeRow above.
type EdgeFromAggregate = edgeagg.EdgeFromAggregate
