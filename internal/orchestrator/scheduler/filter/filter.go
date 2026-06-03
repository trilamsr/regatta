// Package filter consolidates the per-wi gate-pass loop shared by the
// scheduler's approval / cost-governor / L4 passes (#251). Apply runs
// one Pass over the spawnable slice; the caller owns nil short-circuit
// + warn-drop side effects via the Evaluate closure.
package filter

import (
	"context"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Pass is a single gate-pass over a spawnable slice. Resolve maps a wi
// to a per-wi scope (gated=false skips Evaluate). Evaluate decides
// keep/drop and runs any side effect via callbacks closed over by the
// caller; returning err halts the tick — reserve for failures that
// would silently retry a terminal wi.
type Pass[Scope any] struct {
	Name     string
	Resolve  func(state.WorkItem) (scope Scope, gated bool)
	Evaluate func(ctx context.Context, wi state.WorkItem, scope Scope) (keep bool, err error)
}

// Apply runs p over spawnable and returns the kept subset in input
// order. Caller pre-checks gate/resolver nil — Apply has no
// short-circuit so the contract stays total.
func Apply[Scope any](ctx context.Context, p Pass[Scope], spawnable []state.WorkItem) ([]state.WorkItem, error) {
	kept := make([]state.WorkItem, 0, len(spawnable))
	for _, wi := range spawnable {
		scope, gated := p.Resolve(wi)
		if !gated {
			kept = append(kept, wi)
			continue
		}
		keep, err := p.Evaluate(ctx, wi, scope)
		if err != nil {
			return nil, err
		}
		if keep {
			kept = append(kept, wi)
		}
	}
	return kept, nil
}
