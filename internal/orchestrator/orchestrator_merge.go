package orchestrator

import "github.com/trilamsr/regatta/internal/orchestrator/merge"

// SetMergeCoordinator installs the merge.Coordinator used by Recover
// to reconcile agents stranded in AwaitingMerge after a crash. Optional
// — without a Coordinator the daemon still functions for everything
// except the auto-merge path (PHASE AUTONOMY §11 W2), in which case
// AwaitingMerge agents stay enumerated by Recover but only get their
// locks heartbeated (no PR re-probe). Production wiring installs this
// once auto-merge ships; tests inject a fake-prober-backed Coordinator
// to exercise the recovery branches.
func (o *Orchestrator) SetMergeCoordinator(c *merge.Coordinator) {
	o.mergeCoord = c
}
