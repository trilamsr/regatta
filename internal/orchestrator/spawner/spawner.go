// Package spawner defines the contract between the orchestrator and
// the agent-launch mechanism.
//
// The skeleton ships a Stub implementation that records every Spawn
// call without touching the filesystem or starting a process. The
// production Spawner (`claude --resume` inside an isolated worktree)
// lands in a follow-up commit.
package spawner

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// Request carries the inputs a Spawner needs to launch an agent.
// Additional fields (prompt template, worktree path, credentials)
// land as the real Spawner takes shape.
type Request struct {
	AgentID    int64
	WorkItemID string
	Lane       string
}

// Result reports the spawned process identifiers the orchestrator
// records in state.agents so a restart can adopt or reap the child.
type Result struct {
	PID       int
	SessionID string
}

// Spawner is the abstraction over agent launch. Implementations MUST
// be safe for concurrent calls; the orchestrator may spawn multiple
// agents in the same tick (one per lane).
type Spawner interface {
	Spawn(ctx context.Context, req Request) (Result, error)
}

// Stub records every Spawn call and returns a synthetic
// (pid, session-id) pair. Used by tests and by `regatta serve` until
// the real spawner ships.
type Stub struct {
	mu    sync.Mutex
	seq   atomic.Int64
	calls []Request
}

// NewStub returns a ready-to-use stub spawner.
func NewStub() *Stub { return &Stub{} }

// Spawn returns a deterministic synthetic Result. PID is a negative
// counter (so it cannot collide with any real OS pid) and SessionID
// embeds the work-item ID for easier debugging.
func (s *Stub) Spawn(_ context.Context, req Request) (Result, error) {
	n := s.seq.Add(1)
	s.mu.Lock()
	s.calls = append(s.calls, req)
	s.mu.Unlock()
	return Result{
		PID:       int(-n),
		SessionID: fmt.Sprintf("stub-%d-%s", n, req.WorkItemID),
	}, nil
}

// Calls returns a snapshot of every Spawn request observed.
func (s *Stub) Calls() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, len(s.calls))
	copy(out, s.calls)
	return out
}
