package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Run is one row in the runs registry. CausalHash pins the inputs the
// dispatch was deterministic over; RerunOf links a rerun-from-hash
// child back to its parent. DeclaredEffectClass is the policy envelope
// the surprise detector folds tool_call observations against. Spec §3.2.
type Run struct {
	ID                  string
	StartedAt           time.Time
	FinishedAt          *time.Time
	Status              string
	SpecHash            string
	ModelHash           string
	PromptTemplateHash  string
	ToolImplHash        string
	Seed                string
	VersionsJSON        string
	CausalHash          string
	RerunOf             *string
	TraceID             string
	DeclaredEffectClass string
}

// InsertRun appends one runs row; duplicate id surfaces as error
// rather than upsert — the scheduler dispatch boundary is the sole
// writer and a collision means the ID generator is racing.
func (d *DB) InsertRun(ctx context.Context, r Run) error {
	var finishedAt sql.NullInt64
	if r.FinishedAt != nil {
		finishedAt = sql.NullInt64{Int64: r.FinishedAt.Unix(), Valid: true}
	}
	var rerunOf sql.NullString
	if r.RerunOf != nil {
		rerunOf = sql.NullString{String: *r.RerunOf, Valid: true}
	}
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO runs (
			id, started_at, finished_at, status,
			spec_hash, model_hash, prompt_template_hash, tool_impl_hash,
			seed, versions_json, causal_hash, rerun_of, trace_id,
			declared_effect_class
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		r.ID, r.StartedAt.Unix(), finishedAt, r.Status,
		r.SpecHash, r.ModelHash, r.PromptTemplateHash, r.ToolImplHash,
		r.Seed, r.VersionsJSON, r.CausalHash, rerunOf, r.TraceID,
		r.DeclaredEffectClass,
	)
	if err != nil {
		return fmt.Errorf("insert run %q: %w", r.ID, err)
	}
	return nil
}

// GetRun returns the full row for id; missing id surfaces a wrapped sql.ErrNoRows.
func (d *DB) GetRun(ctx context.Context, id string) (Run, error) {
	var r Run
	var startedAt int64
	var finishedAt sql.NullInt64
	var rerunOf sql.NullString
	err := d.sql.QueryRowContext(ctx, `
		SELECT id, started_at, finished_at, status,
		       spec_hash, model_hash, prompt_template_hash, tool_impl_hash,
		       seed, versions_json, causal_hash, rerun_of, trace_id,
		       declared_effect_class
		FROM runs WHERE id = ?
	`, id).Scan(
		&r.ID, &startedAt, &finishedAt, &r.Status,
		&r.SpecHash, &r.ModelHash, &r.PromptTemplateHash, &r.ToolImplHash,
		&r.Seed, &r.VersionsJSON, &r.CausalHash, &rerunOf, &r.TraceID,
		&r.DeclaredEffectClass,
	)
	if err != nil {
		return Run{}, fmt.Errorf("get run %q: %w", id, err)
	}
	r.StartedAt = time.Unix(startedAt, 0)
	if finishedAt.Valid {
		t := time.Unix(finishedAt.Int64, 0)
		r.FinishedAt = &t
	}
	if rerunOf.Valid {
		s := rerunOf.String
		r.RerunOf = &s
	}
	return r, nil
}

// ListRecentRuns returns up to limit runs in started_at-DESC order; backs the S1 runs index view (spec §3.3).
func (d *DB) ListRecentRuns(ctx context.Context, limit int) ([]Run, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT id, started_at, status, causal_hash, trace_id, declared_effect_class
		FROM runs ORDER BY started_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent runs: %w", err)
	}
	defer rows.Close()

	var out []Run
	for rows.Next() {
		var r Run
		var startedAt int64
		if err := rows.Scan(&r.ID, &startedAt, &r.Status, &r.CausalHash, &r.TraceID, &r.DeclaredEffectClass); err != nil {
			return nil, err
		}
		r.StartedAt = time.Unix(startedAt, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}
