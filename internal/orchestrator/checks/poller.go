// Package checks polls "gh pr checks" and emits one event per state
// change on each watched PR. v1 ships an Emitter seam so the producer
// can land separately from the substrate-write wiring (HMAC keying,
// run_id resolution) — the seam keeps test isolation cheap and the
// race-detector clean. Spec ref:
// docs/engineer/specs/2026-06-02-operator-console-design.md §3.2.
package checks

import (
	"context"
	"fmt"
	"sync"
)

// GHCLI is the gh-shell seam. Production wires GHShell; tests inject
// an in-memory fake.
type GHCLI interface {
	PRChecks(ctx context.Context, pr string) (CheckRun, error)
}

// CheckRun is the aggregated PR-checks rollup. Conclusion is one of
// "success", "failure", or empty (still running); Status mirrors gh's
// per-check Status field ("completed" once every required check has
// terminated).
type CheckRun struct {
	Conclusion string
	Status     string
}

// Emission is the payload the poller hands to the Emitter on each
// state change. PR is the gh PR identifier (number or URL) the caller
// passed to Poll; CheckRun is the rollup observed at that tick.
type Emission struct {
	PR        string
	CheckRun  CheckRun
	PrevSeen  bool
	PrevValue CheckRun
}

// Emitter is the substrate-write seam. The production wire-up appends
// a substrate event under KindGateVerdict with payload
// {pr, conclusion, status} per spec §3.2; tests use a capture sink.
type Emitter interface {
	Emit(ctx context.Context, e Emission) error
}

// Poller observes gh pr-checks per PR and routes each state change
// through the Emitter. Safe for concurrent Poll calls — internal
// last-seen map is mutex-guarded.
type Poller struct {
	gh   GHCLI
	sink Emitter

	mu   sync.Mutex
	last map[string]CheckRun
}

// New constructs a Poller wired to gh and sink. The last-seen cache
// starts empty so the first observation per PR fires unconditionally.
func New(gh GHCLI, sink Emitter) *Poller {
	return &Poller{gh: gh, sink: sink, last: map[string]CheckRun{}}
}

// Poll fetches the current check rollup for pr and emits one event
// when the rollup differs from the previously observed value. The
// first observation always emits. Concurrent callers serialize on the
// internal mutex; gh.PRChecks runs outside the lock so a slow network
// call does not block siblings.
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
