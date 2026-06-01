// Package state — approvals CRUD + event-log API.
//
// MVP-2 W1 approval-gates: an `approvals` row is created the first
// time the scheduler observes a gated work_item; the truth of its
// decision lives in the append-only `approval_events` log. The
// `status`/`decided_at`/`decided_by` columns are denormalised cache
// for the hot-path scheduler query — canonical truth is fold(events).
// See docs/superpowers/specs/2026-05-31-mvp-approval-gates.md §3 + §5.
package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrTokenReplay is returned by AppendApprovalEvent when a second
// `token_consumed` event with the same (approval_id, token_jti)
// collides on uq_approval_events_token_consume. The CLI surfaces this
// as the canonical single-use violation; see spec §4.3.
var ErrTokenReplay = errors.New("state: approval token already consumed")

// ReviewerSet is the snapshot persisted in approvals.reviewer_set_snapshot_json
// at request-time. Snapshotting (rather than re-resolving at decide-time)
// keeps the decision attributable to the policy that was in force when
// the human was asked, even if the operator edits regatta.yaml mid-flight.
type ReviewerSet struct {
	Reviewers         []string `json:"reviewers"`
	Quorum            int      `json:"quorum"`
	PreventSelfReview bool     `json:"prevent_self_review"`
}

// TierConfig is one rung of the escalation chain. Mirrors the YAML
// gate config slice so the snapshot round-trips byte-identically.
type TierConfig struct {
	Reviewers         []string      `json:"reviewers"`
	Roles             []string      `json:"roles,omitempty"`
	Quorum            int           `json:"quorum"`
	PreventSelfReview bool          `json:"prevent_self_review,omitempty"`
	Timeout           time.Duration `json:"timeout"`
	DecisionWindow    time.Duration `json:"decision_window"`
}

// Approval is the in-memory shape of a row in `approvals`. ID is
// caller-supplied (typed prefix per spec §5.1: "a-<12 hex>") so the
// CLI can echo it to humans without an extra round-trip.
type Approval struct {
	ID                   string
	WorkItemID           string
	GateName             string
	RequestedAt          time.Time
	RequestedBy          string
	ReviewerSetSnapshot  ReviewerSet
	Quorum               int
	Status               string
	DecidedAt            time.Time
	DecidedBy            []string
	CallbackTokenHMACSig string
	TimeoutAt            time.Time
	OnTimeout            string
	EscalationChain      []TierConfig
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// ApprovalEvent is one row of the append-only `approval_events` log.
// Ts is wall-clock advisory; fold ordering is by id ASC because
// AUTOINCREMENT is monotonic under sqlite's single-writer guarantee.
type ApprovalEvent struct {
	ID         int64
	ApprovalID string
	Ts         time.Time
	Kind       string
	Actor      string
	Payload    json.RawMessage
	TokenJTI   string
}

// CreateApproval inserts a new approvals row. Quorum/DecidedBy/
// ReviewerSetSnapshot/EscalationChain are persisted via JSON so the
// state-machine can evolve their shape without re-running ALTER TABLE.
// Status defaults to "pending" when caller leaves it zero.
func (d *DB) CreateApproval(ctx context.Context, a Approval) error {
	rsJSON, err := json.Marshal(a.ReviewerSetSnapshot)
	if err != nil {
		return fmt.Errorf("state: marshal reviewer set: %w", err)
	}
	chainJSON, err := json.Marshal(a.EscalationChain)
	if err != nil {
		return fmt.Errorf("state: marshal escalation chain: %w", err)
	}
	decidedByJSON, err := json.Marshal(orEmpty(a.DecidedBy))
	if err != nil {
		return fmt.Errorf("state: marshal decided_by: %w", err)
	}
	now := d.now().UTC().Unix()
	status := a.Status
	if status == "" {
		status = "pending"
	}
	onTimeout := a.OnTimeout
	if onTimeout == "" {
		onTimeout = "fail"
	}
	if _, err := d.sql.ExecContext(ctx, `
		INSERT INTO approvals (
			id, work_item_id, gate_name, requested_at, requested_by,
			reviewer_set_snapshot_json, quorum, status, decided_at,
			decided_by, callback_token_hmac_sig, timeout_at, on_timeout,
			escalation_chain_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.WorkItemID, a.GateName,
		a.RequestedAt.UTC().Unix(), a.RequestedBy,
		string(rsJSON), a.Quorum, status,
		string(decidedByJSON), a.CallbackTokenHMACSig,
		a.TimeoutAt.UTC().Unix(), onTimeout,
		string(chainJSON), now, now,
	); err != nil {
		return fmt.Errorf("state: insert approval: %w", err)
	}
	return nil
}

const selectApprovalCols = `SELECT id, work_item_id, gate_name, requested_at,
	requested_by, reviewer_set_snapshot_json, quorum, status,
	COALESCE(decided_at, 0), decided_by, callback_token_hmac_sig,
	timeout_at, on_timeout, escalation_chain_json,
	created_at, updated_at`

// GetApproval fetches an approval by id. Returns sql.ErrNoRows (wrapped)
// when absent; callers that need nil-on-absent should call
// GetApprovalForWorkItem instead.
func (d *DB) GetApproval(ctx context.Context, id string) (Approval, error) {
	row := d.sql.QueryRowContext(ctx, selectApprovalCols+` FROM approvals WHERE id = ?`, id)
	a, err := scanApproval(row)
	if err != nil {
		return Approval{}, fmt.Errorf("state: get approval %q: %w", id, err)
	}
	return a, nil
}

// GetApprovalForWorkItem returns the open approval for a (work_item,
// gate) pair. Nil-on-absent rather than ErrNoRows so the scheduler's
// step-0.5 (spec §3.1) can branch on presence without a wrapped-error
// dance.
func (d *DB) GetApprovalForWorkItem(ctx context.Context, workItemID, gateName string) (*Approval, error) {
	row := d.sql.QueryRowContext(ctx,
		selectApprovalCols+` FROM approvals WHERE work_item_id = ? AND gate_name = ?`,
		workItemID, gateName)
	a, err := scanApproval(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("state: get approval for work item: %w", err)
	}
	return &a, nil
}

// AppendApprovalEvent inserts a new row in approval_events. A second
// kind='token_consumed' with the same token_jti collides on
// uq_approval_events_token_consume and returns ErrTokenReplay so the
// decide path surfaces single-use violations as a typed error.
func (d *DB) AppendApprovalEvent(ctx context.Context, ev ApprovalEvent) error {
	payload := ev.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO approval_events (
			approval_id, ts, kind, actor, payload_json, token_jti
		) VALUES (?, ?, ?, ?, ?, ?)`,
		ev.ApprovalID, ev.Ts.UTC().Unix(), ev.Kind, ev.Actor,
		string(payload), ev.TokenJTI,
	)
	if err != nil {
		if isUniqueTokenConsume(err) {
			return ErrTokenReplay
		}
		return fmt.Errorf("state: append approval event: %w", err)
	}
	return nil
}

// ListApprovalEvents returns every event for an approval in id-ASC
// order. Fold(events) is canonical (spec §4.1); id is the canonical
// ordering key because AUTOINCREMENT is monotonic under sqlite's
// single-writer guarantee while ts is wall-clock advisory.
func (d *DB) ListApprovalEvents(ctx context.Context, approvalID string) ([]ApprovalEvent, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT id, approval_id, ts, kind, actor, payload_json, token_jti
		FROM approval_events
		WHERE approval_id = ?
		ORDER BY id`, approvalID)
	if err != nil {
		return nil, fmt.Errorf("state: list approval events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ApprovalEvent
	for rows.Next() {
		var e ApprovalEvent
		var ts int64
		var payload string
		if err := rows.Scan(&e.ID, &e.ApprovalID, &ts, &e.Kind, &e.Actor, &payload, &e.TokenJTI); err != nil {
			return nil, fmt.Errorf("state: scan approval event: %w", err)
		}
		e.Ts = time.Unix(ts, 0).UTC()
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListPendingApprovals returns approvals whose status is still pending.
// Hits idx_approvals_status so the CLI 'approval list' stays O(pending)
// rather than O(rows). Ordered by created_at ASC so operators see the
// oldest waiting decision first.
func (d *DB) ListPendingApprovals(ctx context.Context) ([]Approval, error) {
	rows, err := d.sql.QueryContext(ctx,
		selectApprovalCols+` FROM approvals WHERE status = 'pending' ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("state: list pending approvals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanApprovals(rows)
}

// ListApprovalsTimedOutBefore returns pending approvals whose
// timeout_at is strictly less than t. The reaper sweep (spec §3.3)
// calls this every tick with t=now(). Strict-LT on the boundary
// matches the semantics of "deadline has passed".
func (d *DB) ListApprovalsTimedOutBefore(ctx context.Context, t time.Time) ([]Approval, error) {
	rows, err := d.sql.QueryContext(ctx,
		selectApprovalCols+` FROM approvals
		WHERE status = 'pending' AND timeout_at < ?
		ORDER BY timeout_at ASC, id ASC`,
		t.UTC().Unix())
	if err != nil {
		return nil, fmt.Errorf("state: list approvals timed out: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanApprovals(rows)
}

// MarkApprovalDecided writes the denormalised status/decided_at/
// decided_by columns derived from a fold the caller already performed.
// Callers MUST have appended the corresponding terminal event in the
// same tx (spec §5.4); this helper does NOT write to the event log
// itself, by design — bypassing AppendApprovalEvent here would let a
// row's status diverge from fold(events).
func (d *DB) MarkApprovalDecided(ctx context.Context, id, status string, decidedBy []string, decidedAt time.Time) error {
	by, err := json.Marshal(orEmpty(decidedBy))
	if err != nil {
		return fmt.Errorf("state: marshal decided_by: %w", err)
	}
	now := d.now().UTC().Unix()
	res, err := d.sql.ExecContext(ctx, `
		UPDATE approvals
		SET status = ?, decided_at = ?, decided_by = ?, updated_at = ?
		WHERE id = ?`,
		status, decidedAt.UTC().Unix(), string(by), now, id)
	if err != nil {
		return fmt.Errorf("state: mark approval decided: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("state: mark approval rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("state: approval id=%q not found", id)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanApproval(r rowScanner) (Approval, error) {
	var a Approval
	var reqAt, decAt, timeoutAt, created, updated int64
	var rsJSON, decByJSON, chainJSON string
	if err := r.Scan(&a.ID, &a.WorkItemID, &a.GateName, &reqAt,
		&a.RequestedBy, &rsJSON, &a.Quorum, &a.Status,
		&decAt, &decByJSON, &a.CallbackTokenHMACSig,
		&timeoutAt, &a.OnTimeout, &chainJSON,
		&created, &updated); err != nil {
		return Approval{}, err
	}
	a.RequestedAt = time.Unix(reqAt, 0).UTC()
	if decAt != 0 {
		a.DecidedAt = time.Unix(decAt, 0).UTC()
	}
	a.TimeoutAt = time.Unix(timeoutAt, 0).UTC()
	a.CreatedAt = time.Unix(created, 0).UTC()
	a.UpdatedAt = time.Unix(updated, 0).UTC()
	if err := json.Unmarshal([]byte(rsJSON), &a.ReviewerSetSnapshot); err != nil {
		return Approval{}, fmt.Errorf("state: unmarshal reviewer set: %w", err)
	}
	if err := json.Unmarshal([]byte(decByJSON), &a.DecidedBy); err != nil {
		return Approval{}, fmt.Errorf("state: unmarshal decided_by: %w", err)
	}
	if err := json.Unmarshal([]byte(chainJSON), &a.EscalationChain); err != nil {
		return Approval{}, fmt.Errorf("state: unmarshal escalation chain: %w", err)
	}
	return a, nil
}

func scanApprovals(rows *sql.Rows) ([]Approval, error) {
	var out []Approval
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, fmt.Errorf("state: scan approval: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// orEmpty turns a nil slice into [] so the JSON encoding is "[]" not
// "null"; matches the DEFAULT '[]' on the columns and keeps fold-of-
// events math symmetric across NULL-vs-empty edge cases.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// isUniqueTokenConsume returns true when err is the sqlite UNIQUE
// collision on uq_approval_events_token_consume. modernc.org/sqlite
// surfaces the colliding column tuple (not the index name) in the
// error text, so the probe matches on the tuple. The tuple is
// unique to this index — it's the only UNIQUE over those 3 cols.
func isUniqueTokenConsume(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "approval_events.approval_id") &&
		strings.Contains(msg, "approval_events.kind") &&
		strings.Contains(msg, "approval_events.token_jti")
}
