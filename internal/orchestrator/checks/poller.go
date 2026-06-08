// Package checks polls "gh pr checks" and emits one event per state change per PR. v1 ships an Emitter seam so the substrate-write wiring (HMAC keying, run_id resolution) can land separately. Spec §3.2.
package checks

import (
	"context"
	"fmt"
	"sync"
)

// GHCLI is the gh-shell seam; production wires GHShell, tests inject an in-memory fake.
type GHCLI interface {
	PRChecks(ctx context.Context, pr string) (CheckRun, error)
}

// CheckRun is the aggregated PR-checks rollup; Conclusion ∈ {"success","failure",""}, Status mirrors gh's per-check Status ("completed" once all required checks terminate).
type CheckRun struct {
	Conclusion string
	Status     string
}

// Emission is the payload the poller hands to Emitter on each state change.
type Emission struct {
	PR        string
	CheckRun  CheckRun
	PrevSeen  bool
	PrevValue CheckRun
}

// Emitter is the substrate-write seam; production appends KindGateVerdict {pr,conclusion,status} per spec §3.2.
type Emitter interface {
	Emit(ctx context.Context, e Emission) error
}

// Poller observes gh pr-checks per PR and routes each state change through Emitter; concurrent Poll is safe — last-seen map is mutex-guarded.
type Poller struct {
	gh   GHCLI
	sink Emitter

	mu   sync.Mutex
	last map[string]CheckRun
}

// New wires a Poller; last-seen cache starts empty so first observation per PR always emits.
func New(gh GHCLI, sink Emitter) *Poller {
	return &Poller{gh: gh, sink: sink, last: map[string]CheckRun{}}
}

// Poll fetches the current rollup for pr and emits when it differs from the previous observation (first observation always emits). gh.PRChecks runs outside the lock so a slow call does not block siblings.
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
