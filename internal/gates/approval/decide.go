package approval

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// systemActor stamps approval_events rows the decide path emits on
// behalf of the orchestrator (terminal approved|rejected wrappers).
const systemActor = "system"

// Decide-path sentinels distinct from canon verify-time errors so callers can branch CLI exit codes / HTTP status on the specific failure mode.
var (
	// ErrNotReviewer signals reviewerID is not in the approval's snapshot.
	ErrNotReviewer = errors.New("approval: reviewer not in snapshot")
	// ErrSelfReview signals the reviewer is the work_item's requested_by under prevent_self_review=true.
	ErrSelfReview = errors.New("approval: self-review blocked by gate policy")
	// ErrDoubleVote signals the reviewer already appears in decided_by.
	ErrDoubleVote = errors.New("approval: reviewer already voted")
)

// DecideTxResult is the decide-path slice of the event-log fold: running decided_by + counts + terminal verdict. Distinct from approval.FoldResult (canonical gate-tick verdict in fold.go).
type DecideTxResult struct {
	DecidedBy     []string
	AllowCount    int
	DenyCount     int
	Terminal      bool
	TerminalAllow bool
}

func foldDecisions(events []state.ApprovalEvent, quorum int, reviewerSetSize int) DecideTxResult {
	r := DecideTxResult{}
	for _, ev := range events {
		if ev.Kind != EventKindDecided {
			continue
		}
		var p struct {
			Decision string `json:"decision"`
		}
		_ = json.Unmarshal(ev.Payload, &p)
		r.DecidedBy = append(r.DecidedBy, ev.Actor)
		switch p.Decision {
		case DecisionAllow:
			r.AllowCount++
		case DecisionDeny:
			r.DenyCount++
		}
	}
	switch {
	case r.AllowCount >= quorum:
		r.Terminal = true
		r.TerminalAllow = true
	case r.DenyCount >= quorum:
		// ≥quorum denies terminates the gate as rejected (spec §6 N-of-M).
		r.Terminal = true
	case r.DenyCount+r.AllowCount >= reviewerSetSize:
		// Every reviewer has voted and neither side reached quorum:
		// the gate cannot reach approval, so we close as rejected.
		r.Terminal = true
	}
	return r
}

// DecideTx consumes the token, records the decision, folds the event log, and (when terminal) stamps the denorm status in one sqlite transaction (spec §3.2 step 6).
func DecideTx(ctx context.Context, db *state.DB, payload approvaltoken.TokenPayload, reviewerID, decision, reason string, clock func() time.Time) (DecideTxResult, string, error) {
	a, err := lookupAndValidate(ctx, db, payload.AID, reviewerID)
	if err != nil {
		return DecideTxResult{}, "", err
	}
	var (
		folded DecideTxResult
		status = state.ApprovalStatusPending
	)
	if err := db.WithTx(ctx, func(tx *sql.Tx) error {
		// Issue #206: re-check terminality INSIDE the tx so a reaper sweep
		// that committed a terminal event between lookupAndValidate above
		// and this BEGIN is observed. sqlite's single-writer pool
		// serialises BeginTx behind the reaper's COMMIT — the in-tx list
		// sees the winning terminal row and we exit clean rather than
		// silently overwriting it with decided+approved (the bug in #206,
		// sibling to the reaper-side guard in reaper.go::sweepOne). Reuse
		// state.ErrTokenReplay rather than minting a fresh sentinel: the
		// CLI / HTTP layers already map it to the canonical "this approval
		// is no longer decidable" exit code (notify_http.go::110).
		priorInTx, err := decideListEventsTx(ctx, tx, payload.AID)
		if err != nil {
			return err
		}
		if isTerminal(priorInTx) {
			return state.ErrTokenReplay
		}
		now := clock().UTC()
		if err := insertVoteEvents(ctx, tx, payload, reviewerID, decision, reason, now); err != nil {
			return err
		}
		folded, status, err = applyTerminalIfAny(ctx, tx, payload.AID, a, now)
		return err
	}); err != nil {
		return DecideTxResult{}, "", err
	}
	return folded, status, nil
}

// lookupAndValidate loads the approval and rejects non-reviewers, self-reviewers, and double-voters before opening the write tx.
func lookupAndValidate(ctx context.Context, db *state.DB, aid, reviewerID string) (state.Approval, error) {
	a, err := db.GetApproval(ctx, aid)
	if err != nil {
		return state.Approval{}, err
	}
	if !inReviewerSet(a.ReviewerSetSnapshot.Reviewers, reviewerID) {
		return state.Approval{}, ErrNotReviewer
	}
	if a.ReviewerSetSnapshot.PreventSelfReview && a.RequestedBy == reviewerID {
		return state.Approval{}, ErrSelfReview
	}
	// Plain == compare (not subtle.ConstantTimeCompare) is correct here:
	// a.DecidedBy is a public list of prior voter IDs surfaced in the
	// approval audit log; reviewerID is the active operator. Neither is
	// a secret, so timing leak is meaningless. Investigator sweep
	// 2026-06-22 (R21-I3) flagged this as a defect by analogy to the
	// approvaltoken HMAC compare; the analogy is wrong (HMAC compares
	// secret-derived bytes, this compares public usernames).
	for _, prev := range a.DecidedBy {
		if prev == reviewerID {
			return state.Approval{}, ErrDoubleVote
		}
	}
	return a, nil
}

// insertVoteEvents writes token_consumed before decided so a replayed token aborts on the UNIQUE(approval_id,kind,token_jti) index before any vote materialises.
func insertVoteEvents(ctx context.Context, tx *sql.Tx, payload approvaltoken.TokenPayload, reviewerID, decision, reason string, now time.Time) error {
	if err := InsertApprovalEvent(ctx, tx, state.ApprovalEvent{
		ApprovalID: payload.AID,
		Ts:         now,
		Kind:       "token_consumed",
		Actor:      reviewerID,
		TokenJTI:   payload.JTI,
	}); err != nil {
		return err
	}
	decidedPayload, _ := json.Marshal(struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason,omitempty"`
	}{Decision: decision, Reason: reason})
	return InsertApprovalEvent(ctx, tx, state.ApprovalEvent{
		ApprovalID: payload.AID,
		Ts:         now,
		Kind:       EventKindDecided,
		Actor:      reviewerID,
		Payload:    decidedPayload,
	})
}

// applyTerminalIfAny re-folds the event log inside the tx and, when quorum is reached, stamps the terminal event and denorm status.
func applyTerminalIfAny(ctx context.Context, tx *sql.Tx, aid string, a state.Approval, now time.Time) (DecideTxResult, string, error) {
	events, err := decideListEventsTx(ctx, tx, aid)
	if err != nil {
		return DecideTxResult{}, "", err
	}
	folded := foldDecisions(events, a.Quorum, len(a.ReviewerSetSnapshot.Reviewers))
	status := state.ApprovalStatusPending
	if !folded.Terminal {
		return folded, status, nil
	}
	terminalKind := EventKindRejected
	status = state.ApprovalStatusRejected
	if folded.TerminalAllow {
		terminalKind = EventKindApproved
		status = state.ApprovalStatusApproved
	}
	if err := InsertApprovalEvent(ctx, tx, state.ApprovalEvent{
		ApprovalID: aid, Ts: now, Kind: terminalKind, Actor: systemActor,
	}); err != nil {
		return DecideTxResult{}, "", err
	}
	if err := markApprovalDecidedTx(ctx, tx, aid, status, folded.DecidedBy, now); err != nil {
		return DecideTxResult{}, "", err
	}
	return folded, status, nil
}

// InsertApprovalEvent mirrors state.AppendApprovalEvent's INSERT but runs against the caller's *sql.Tx so all mutations share one BEGIN..COMMIT (spec §3.2.1).
func InsertApprovalEvent(ctx context.Context, tx *sql.Tx, ev state.ApprovalEvent) error {
	payload := ev.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	var traceID string
	state.PersistTraceIDFromContext(ctx, &traceID)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO approval_events (
			approval_id, ts, kind, actor, payload_json, token_jti, trace_id
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ev.ApprovalID, ev.Ts.UTC().Unix(), ev.Kind, ev.Actor,
		string(payload), ev.TokenJTI, traceID,
	)
	if err != nil {
		if isUniqueTokenConsumeTx(err) {
			return state.ErrTokenReplay
		}
		return fmt.Errorf("insert approval_event: %w", err)
	}
	return nil
}

// isUniqueTokenConsumeTx mirrors state.isUniqueTokenConsume — modernc.org/sqlite surfaces the colliding column tuple (not the index name) in the error text.
func isUniqueTokenConsumeTx(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "approval_events.approval_id") &&
		strings.Contains(msg, "approval_events.kind") &&
		strings.Contains(msg, "approval_events.token_jti")
}

func decideListEventsTx(ctx context.Context, tx *sql.Tx, approvalID string) ([]state.ApprovalEvent, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, approval_id, ts, kind, actor, payload_json, token_jti
		FROM approval_events
		WHERE approval_id = ?
		ORDER BY id`, approvalID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []state.ApprovalEvent
	for rows.Next() {
		var e state.ApprovalEvent
		var ts int64
		var payload string
		if err := rows.Scan(&e.ID, &e.ApprovalID, &ts, &e.Kind, &e.Actor, &payload, &e.TokenJTI); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Ts = time.Unix(ts, 0).UTC()
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

func markApprovalDecidedTx(ctx context.Context, tx *sql.Tx, id, status string, decidedBy []string, decidedAt time.Time) error {
	by, err := json.Marshal(decidedBy)
	if err != nil {
		return fmt.Errorf("marshal decided_by: %w", err)
	}
	now := decidedAt.UTC().Unix()
	res, err := tx.ExecContext(ctx, `
		UPDATE approvals
		SET status = ?, decided_at = ?, decided_by = ?, updated_at = ?
		WHERE id = ?`,
		status, decidedAt.UTC().Unix(), string(by), now, id)
	if err != nil {
		return fmt.Errorf("update approval: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("approval id=%q not found", id)
	}
	return nil
}

// inReviewerSet is a constant-time membership check across the snapshot. Linear scan + no early-exit matches the spec's belt-and-suspenders timing-safe posture.
func inReviewerSet(set []string, id string) bool {
	hit := 0
	for _, r := range set {
		if subtle.ConstantTimeCompare([]byte(r), []byte(id)) == 1 {
			hit = 1
		}
	}
	return hit == 1
}
