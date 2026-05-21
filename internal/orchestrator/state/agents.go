package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Agent is the in-memory view of a row in the agents table.
type Agent struct {
	ID             int64
	WorkItemID     string
	Lane           string
	State          AgentState
	PID            int
	SessionID      string
	PRSHA          string
	RejectionCount int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// transitions encodes the valid edges from docs/design.md §378.
// Self-edges and idempotent re-applies are allowed where useful (e.g.
// pending→pending lets the spec watcher upsert without bookkeeping).
var transitions = map[AgentState]map[AgentState]struct{}{
	AgentPending: {
		AgentPending:   {},
		AgentSpawning:  {},
		AgentWithdrawn: {},
	},
	AgentSpawning: {
		AgentRunning: {},
		AgentCrashed: {},
	},
	AgentRunning: {
		AgentPROpen:  {},
		AgentCrashed: {},
	},
	AgentPROpen: {
		AgentGatesRunning: {},
		AgentWithdrawn:    {},
		AgentCrashed:      {},
	},
	AgentGatesRunning: {
		AgentAwaitingMerge: {},
		AgentGatesFailed:   {},
		AgentCrashed:       {},
	},
	AgentGatesFailed: {
		AgentRunning:   {},
		AgentEscalated: {},
		AgentWithdrawn: {},
	},
	AgentAwaitingMerge: {
		AgentDone:      {},
		AgentWithdrawn: {},
	},
	// Terminal states have no outgoing edges.
	AgentDone:      {},
	AgentWithdrawn: {},
	AgentCrashed:   {AgentPending: {}}, // requeue on recovery
	AgentEscalated: {},
}

// AgentMutation carries the fields TransitionAgent may overwrite as
// part of the same transaction. Zero values leave the column
// untouched.
type AgentMutation struct {
	PID                *int
	SessionID          *string
	PRSHA              *string
	IncrementRejection bool
}

// UpsertPending inserts a pending agent for workItemID if none exists,
// or returns the existing row. The function is idempotent.
func (d *DB) UpsertPending(ctx context.Context, workItemID, lane string) (*Agent, error) {
	now := d.now().UTC().Unix()
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("state: begin upsert tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var a Agent
	row := tx.QueryRowContext(ctx,
		`SELECT id, work_item_id, lane, state, pid, session_id, pr_sha, rejection_count, created_at, updated_at
		 FROM agents WHERE work_item_id = ?`, workItemID)
	if err := scanAgent(row, &a); err == nil {
		if a.Lane != lane {
			a.Lane = lane
			a.UpdatedAt = time.Unix(now, 0).UTC()
			if _, err := tx.ExecContext(ctx,
				`UPDATE agents SET lane = ?, updated_at = ? WHERE id = ?`,
				lane, now, a.ID); err != nil {
				return nil, fmt.Errorf("state: update lane: %w", err)
			}
		}
		return &a, tx.Commit()
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("state: select agent: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO agents (work_item_id, lane, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		workItemID, lane, string(AgentPending), now, now)
	if err != nil {
		return nil, fmt.Errorf("state: insert agent: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("state: last insert id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("state: commit upsert: %w", err)
	}
	return &Agent{
		ID:         id,
		WorkItemID: workItemID,
		Lane:       lane,
		State:      AgentPending,
		CreatedAt:  time.Unix(now, 0).UTC(),
		UpdatedAt:  time.Unix(now, 0).UTC(),
	}, nil
}

// GetAgent fetches a single agent by ID.
func (d *DB) GetAgent(ctx context.Context, id int64) (*Agent, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, work_item_id, lane, state, pid, session_id, pr_sha, rejection_count, created_at, updated_at
		 FROM agents WHERE id = ?`, id)
	var a Agent
	if err := scanAgent(row, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// ListAgentsByState returns all agents currently in any of the given
// states. Ordering is by id ascending so callers see a stable order.
func (d *DB) ListAgentsByState(ctx context.Context, states ...AgentState) ([]Agent, error) {
	if len(states) == 0 {
		return nil, nil
	}
	q := "SELECT id, work_item_id, lane, state, pid, session_id, pr_sha, rejection_count, created_at, updated_at FROM agents WHERE state IN ("
	args := make([]any, 0, len(states))
	for i, s := range states {
		if i > 0 {
			q += ","
		}
		q += "?"
		args = append(args, string(s))
	}
	q += ") ORDER BY id ASC"
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("state: list agents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Agent
	for rows.Next() {
		var a Agent
		if err := scanAgent(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CountAgentsByLane returns the number of agents currently in any of
// the given states, grouped by lane. Used by the scheduler to honor
// per-lane concurrency caps.
func (d *DB) CountAgentsByLane(ctx context.Context, states ...AgentState) (map[string]int, error) {
	out := map[string]int{}
	if len(states) == 0 {
		return out, nil
	}
	q := "SELECT lane, COUNT(*) FROM agents WHERE state IN ("
	args := make([]any, 0, len(states))
	for i, s := range states {
		if i > 0 {
			q += ","
		}
		q += "?"
		args = append(args, string(s))
	}
	q += ") GROUP BY lane"
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("state: count by lane: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var lane string
		var n int
		if err := rows.Scan(&lane, &n); err != nil {
			return nil, err
		}
		out[lane] = n
	}
	return out, rows.Err()
}

// TransitionAgent moves an agent from its current state to next,
// applying any field overrides in mut. Returns ErrInvalidTransition if
// the edge is not permitted.
func (d *DB) TransitionAgent(ctx context.Context, id int64, next AgentState, mut AgentMutation) (*Agent, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("state: begin transition tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	a, err := txGetAgentForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	allowed, ok := transitions[a.State]
	if !ok {
		return nil, fmt.Errorf("%w: unknown source state %q", ErrInvalidTransition, a.State)
	}
	if _, ok := allowed[next]; !ok {
		return nil, fmt.Errorf("%w: %s → %s", ErrInvalidTransition, a.State, next)
	}

	a.State = next
	a.UpdatedAt = d.now().UTC()
	if mut.PID != nil {
		a.PID = *mut.PID
	}
	if mut.SessionID != nil {
		a.SessionID = *mut.SessionID
	}
	if mut.PRSHA != nil {
		a.PRSHA = *mut.PRSHA
	}
	if mut.IncrementRejection {
		a.RejectionCount++
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE agents SET state=?, pid=?, session_id=?, pr_sha=?, rejection_count=?, updated_at=?
		 WHERE id = ?`,
		string(a.State), a.PID, a.SessionID, a.PRSHA, a.RejectionCount, a.UpdatedAt.Unix(), a.ID); err != nil {
		return nil, fmt.Errorf("state: update agent: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("state: commit transition: %w", err)
	}
	return a, nil
}

func txGetAgentForUpdate(ctx context.Context, tx *sql.Tx, id int64) (*Agent, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id, work_item_id, lane, state, pid, session_id, pr_sha, rejection_count, created_at, updated_at
		 FROM agents WHERE id = ?`, id)
	var a Agent
	if err := scanAgent(row, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAgent(s scanner, a *Agent) error {
	var createdAt, updatedAt int64
	if err := s.Scan(&a.ID, &a.WorkItemID, &a.Lane, (*string)(&a.State),
		&a.PID, &a.SessionID, &a.PRSHA, &a.RejectionCount, &createdAt, &updatedAt); err != nil {
		return err
	}
	a.CreatedAt = time.Unix(createdAt, 0).UTC()
	a.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return nil
}
