package checks

import (
	"context"
	"fmt"
	"sync"
)

type GHCLI interface { //nolint:revive // gh-shell seam; production wires GHShell, tests inject a fake
	PRChecks(ctx context.Context, pr string) (CheckRun, error)
}

// CheckRun is the aggregated PR-checks rollup; Conclusion ∈ {"success","failure",""}, Status mirrors gh's per-check Status ("completed" once all required checks terminate).
type CheckRun struct {
	Conclusion string
	Status     string
}

type Emission struct { //nolint:revive // payload the poller hands to Emitter on each state change
	PR        string
	CheckRun  CheckRun
	PrevSeen  bool
	PrevValue CheckRun
}

type Emitter interface { //nolint:revive // substrate-write seam; production appends KindGateVerdict per spec §3.2
	Emit(ctx context.Context, e Emission) error
}

// Poller routes gh pr-checks state changes through Emitter; concurrent Poll is safe — last-seen map is mutex-guarded.
type Poller struct {
	gh   GHCLI
	sink Emitter

	mu   sync.Mutex
	last map[string]CheckRun
}

func New(gh GHCLI, sink Emitter) *Poller { //nolint:revive // empty last-seen cache so first observation per PR always emits
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
