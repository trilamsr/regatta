package orchestrator

import (
	"context"

	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
)

// SetRejectionRouter installs the RejectionRouter used by Run to
// route gate_rejected events into agent transitions + K-escalation.
// Optional; without a Router the daemon still functions but agents
// never wake on AI-gate rejections and never escalate to needs-human.
func (o *Orchestrator) SetRejectionRouter(r *rejectionrouter.Router) {
	o.rejection = r
}

// RouteRejections drains pending gate_rejected events into agent
// transitions + K-escalation. Safe to call with no Router set;
// returns nil in that case.
func (o *Orchestrator) RouteRejections(ctx context.Context) error {
	if o.rejection == nil {
		return nil
	}
	return o.rejection.Tick(ctx)
}
