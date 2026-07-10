package checks

import (
	"context"
	"fmt"
	"sync"
)

// CheckRun is the aggregated PR-checks rollup; Conclusion ∈ {"success","failure",""}, Status mirrors gh's per-check Status ("completed" once all required checks terminate).
type CheckRun struct {
	Conclusion string
	Status     string
}

// Emission is the payload the Poller hands to Emitter on every observed
// state change; PrevSeen distinguishes first observation (transition from
// unknown) from a genuine transition between two known CheckRun values.
type Emission struct {
	PR        string
	CheckRun  CheckRun
	PrevSeen  bool
	PrevValue CheckRun
}

// Emitter is the substrate-write seam the Poller invokes on each state
// transition; production appends KindGateVerdict per spec §3.2.
type Emitter interface {
	Emit(ctx context.Context, e Emission) error
}

// Poller routes gh pr-checks state changes through Emitter; concurrent Poll is safe — last-seen map is mutex-guarded.
type Poller struct {
	gh   *GHShell
	sink Emitter

	mu   sync.Mutex
	last map[string]CheckRun
}

// New builds a Poller with an empty last-seen cache so the first
// observation per PR always emits.
func New(gh *GHShell, sink Emitter) *Poller {
	return &Poller{gh: gh, sink: sink, last: map[string]CheckRun{}}
}

// Poll fetches the rollup for pr and emits on change (first observation always emits); gh.PRChecks runs outside the lock so a slow call does not block siblings.
func (p *Poller) Poll(ctx context.Context, pr string) error {
	cur, err := p.gh.PRChecks(ctx, pr)
	if err != nil {
		return fmt.Errorf("gh pr checks %s: %w", pr, err)
	}

	p.mu.Lock()
	prev, seen := p.last[pr]
	if seen && prev == cur {
		p.mu.Unlock()
		return nil
	}
	p.last[pr] = cur
	p.mu.Unlock()

	return p.sink.Emit(ctx, Emission{
		PR:        pr,
		CheckRun:  cur,
		PrevSeen:  seen,
		PrevValue: prev,
	})
}
