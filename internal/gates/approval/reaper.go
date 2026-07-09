// Package approval — reaper sweep for pending approvals whose
// timeout_at has passed (spec §3.3 + §5.9). One Sweep call walks the
// expired-batch and applies the row's on_timeout policy in a single
// transaction per row, so a partial escalation (vote replay + token
// revocation + chain advance + maybe-terminal terminal event) rolls
// back fully on any statement failure (§3.2.1 atomicity contract).
package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/strutil"
)

// on_timeout policy enum + vote-side enum. Centralised so the goconst
// linter stays quiet and a future config-validator edit can point at
// a single set of canonical strings rather than re-spelling them.
const (
	policyFail        = "fail"
	policyAutoApprove = "auto_approve"
	policyEscalate    = "escalate"

	reasonEscalated = "escalated"
)

// ErrReaperClockRequired is returned by NewReaper when the supplied
// clock is nil. Falling back to time.Now would make Sweep
// nondeterministic across tests and across operator clock changes;
// the spec §3.3 / §5.9 contract pins constructor-injected clocks.
var ErrReaperClockRequired = errors.New("approval: reaper requires a non-nil clock")

// ErrEscalationChainExhausted fires when an `on_timeout=escalate` row
// has no further chain tier to advance to. Reaper falls back to the
// `fail` policy in that case so the work item still terminates rather
// than spinning forever; the typed sentinel lets the integration PR
// (A2's gate handler wiring) distinguish "chain done" from a genuine
// statement failure when it folds events.
var ErrEscalationChainExhausted = errors.New("approval: escalation chain exhausted")

// ErrUnknownTimeoutPolicy fires when a row's on_timeout column carries
// an enum value outside {fail, auto_approve, escalate}. The config
// validator (A2) catches this at startup; surfacing it as a typed
// sentinel here means a hand-edited DB cell trips a recognizable error
// rather than a string-formatted one.
var ErrUnknownTimeoutPolicy = errors.New("approval: unknown on_timeout policy")

// Reaper drives the timeout-sweep loop. Owns nothing — it reads from
// state.DB, writes into one tx per expired row, and emits structured
// slog. Caller decides the loop cadence (cmd/regatta wires Sweep into
// the existing tick goroutine in Wave 3).
type Reaper struct {
	db    *state.DB
	log   *slog.Logger
	clock func() time.Time

	// txHook fires inside Sweep's per-row tx after the timed_out event
	// is appended but before the policy branch writes its updates. A
	// non-nil error returned from the hook aborts the tx — sweepOne
	// rolls back and surfaces the error so atomicity tests can assert
	// "no partial state". Production callers leave it nil.
	txHook func(*sql.Tx) error
}

// NewReaper builds a Reaper. A nil log defaults to slog.Default so
// callers in tests can pass slog.New(handler) without a separate ctor.
// A nil clock returns ErrReaperClockRequired — see spec §3.3 / §5.9:
// production wiring must thread an explicit clock so Sweep is
// deterministic across tests and operator clock changes.
func NewReaper(db *state.DB, log *slog.Logger, clock func() time.Time) (*Reaper, error) {
	if clock == nil {
		return nil, ErrReaperClockRequired
	}
	if log == nil {
		log = slog.Default()
	}
	return &Reaper{db: db, log: log, clock: clock}, nil
}

// Sweep loads every pending approval whose timeout_at is strictly
// before clock() and applies the row's on_timeout policy. Each row's
// mutations run in their own tx so one row's failure does not poison
// the rest of the batch — the per-row error is logged and Sweep keeps
// going; Sweep returns the first error it saw (or nil).
func (r *Reaper) Sweep(ctx context.Context) error {
	now := r.clock()
	expired, err := r.db.ListApprovalsTimedOutBefore(ctx, now)
	if err != nil {
		return fmt.Errorf("approval/reaper: list expired: %w", err)
	}
	var firstErr error
	for _, a := range expired {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.sweepOne(ctx, a, now); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			r.log.Warn(string(obs.EventApprovalTimedOut),
				string(obs.KeyApprovalID), a.ID,
				string(obs.KeyErr), err.Error(),
			)
		}
	}
	return firstErr
}

// sweepOne runs the per-row tx. Reads prior events FIRST so the policy
// branch sees the full history (escalate needs the replayed-votes
// list), then opens the tx and issues all writes — append timed_out,
// branch on policy, maybe terminal event, mark decided / advance chain.
// All-or-nothing.
func (r *Reaper) sweepOne(ctx context.Context, a state.Approval, now time.Time) error {
	prior, err := r.db.ListApprovalEvents(ctx, a.ID)
	if err != nil {
		return fmt.Errorf("approval/reaper: list events %q: %w", a.ID, err)
	}
	if isTerminal(prior) {
		// Defence: a concurrent Sweep already terminated this row.
		// Idempotency: emit nothing further; the timed_out event was
		// already appended in the winning sweep's tx.
		return nil
	}
	policy := a.OnTimeout
	if policy == "" {
		policy = policyFail
	}
	// chainExhausted + escalateOK steer the post-commit slog parity emit
	// below (was inline pre-WithTx). Tests TestReaper_AutoApprovePolicy
	// and TestReaper_EscalatePolicy still pass through the same record.
	var (
		chainExhausted bool
		escalateOK     bool
	)
	if err := r.db.WithTx(ctx, func(tx *sql.Tx) error {
		// Re-check terminal status INSIDE the tx so a parallel Sweep that
		// won the race is observed. sqlite's max-1-conn pool serialises
		// transactions, so the second BeginTx blocks until the first
		// commits — by then the won row carries a terminal marker and we
		// exit clean (no second timed_out event).
		priorInTx, err := listApprovalEventsTx(ctx, tx, a.ID)
		if err != nil {
			return fmt.Errorf("approval/reaper: re-list events %q: %w", a.ID, err)
		}
		if isTerminal(priorInTx) {
			return errSweepSkip
		}
		// Issue #193: only write timed_out for policies whose denotation
		// matches "no decision in window" — fail and escalate (chain-
		// exhausted). auto_approve resolves to approved; writing timed_out
		// first would make Fold (id-ASC, first-terminal-wins) return
		// StatusTimedOut and the gate verdict would contradict the denorm
		// status column.
		if policy != policyAutoApprove {
			// fail emits its umbrella slog here; escalate suppresses it
			// because the trailing escalated slog below carries the chain-
			// index attrs operators need.
			logger := r.log
			if policy == policyEscalate {
				logger = nil
			}
			if err := recordEvent(ctx, recordEventOpts{
				Tx: tx, Logger: logger, ApprovalID: a.ID,
				Event: obs.EventApprovalTimedOut, Kind: EventKindTimedOut,
				Actor: systemActor, Now: now,
				Attrs: map[string]any{string(obs.KeyPolicy): policy},
			}); err != nil {
				return fmt.Errorf("approval/reaper: append timed_out: %w", err)
			}
		}
		if r.txHook != nil {
			if err := r.txHook(tx); err != nil {
				return fmt.Errorf("approval/reaper: txHook abort: %w", err)
			}
		}
		ce, eo, err := r.applyTimeoutPolicy(ctx, tx, a, policy, prior, priorInTx, now)
		chainExhausted = ce
		escalateOK = eo
		return err
	}); err != nil {
		if errors.Is(err, errSweepSkip) {
			return nil
		}
		return err
	}
	// Post-commit slog parity (was inline pre-WithTx). Slogs after commit
	// so a tx-write failure does not lie about a non-existent terminal.
	switch {
	case policy == policyAutoApprove:
		// Slog parity with the prior reaper: emit the umbrella timed_out
		// event too so TestReaper_AutoApprovePolicy still fires. No event
		// row — issue #193 forbids a timed_out row on this branch.
		r.log.Info(string(obs.EventApprovalTimedOut),
			string(obs.KeyApprovalID), a.ID,
			string(obs.KeyWorkItemID), a.WorkItemID,
			string(obs.KeyGateID), a.GateName,
			string(obs.KeyPolicy), policyAutoApprove,
		)
	case chainExhausted:
		r.log.Warn(string(obs.EventApprovalTimedOut),
			string(obs.KeyApprovalID), a.ID,
			string(obs.KeyPolicy), policyEscalate,
			string(obs.KeyReason), ErrEscalationChainExhausted.Error(),
		)
	case escalateOK:
		// Slog parity: emit timed_out (umbrella) so the existing
		// TestReaper_EscalatePolicy slog assertion still fires; escalated
		// was already emitted via recordEvent inside the tx.
		r.log.Info(string(obs.EventApprovalTimedOut),
			string(obs.KeyApprovalID), a.ID,
			string(obs.KeyPolicy), policyEscalate,
		)
	}
	return nil
}

// applyTimeoutPolicy dispatches the per-row on_timeout branch so sweepOne
// stays a linear tx skeleton — chainExhausted / escalateOK steer the
// post-commit slog parity emit in sweepOne.
func (r *Reaper) applyTimeoutPolicy(
	ctx context.Context, tx *sql.Tx, a state.Approval, policy string,
	prior, priorInTx []state.ApprovalEvent, now time.Time,
) (chainExhausted, escalateOK bool, err error) {
	switch policy {
	case policyFail:
		if err := markDecided(ctx, tx, a.ID, state.ApprovalStatusTimedOut, []string{}, now); err != nil {
			return false, false, fmt.Errorf("approval/reaper: mark timed_out: %w", err)
		}
		return false, false, nil
	case policyAutoApprove:
		// Row payload kept identical to the pre-WithTx shape so any
		// downstream consumer parsing approval_events.payload_json
		// sees no behavior change.
		if err := recordEvent(ctx, recordEventOpts{
			Tx: tx, Logger: r.log, ApprovalID: a.ID,
			Event: obs.EventApprovalAutoApproved, Kind: EventKindApproved,
			Actor: "system:timeout-default", Now: now,
			Attrs: map[string]any{"reason": "timeout_default"},
		}); err != nil {
			return false, false, fmt.Errorf("approval/reaper: append approved: %w", err)
		}
		if err := markDecided(ctx, tx, a.ID, state.ApprovalStatusApproved, []string{"system:timeout-default"}, now); err != nil {
			return false, false, fmt.Errorf("approval/reaper: mark approved: %w", err)
		}
		return false, false, nil
	case policyEscalate:
		return r.applyEscalatePolicy(ctx, tx, a, prior, priorInTx, now)
	default:
		return false, false, fmt.Errorf("%w: %q for approval %q", ErrUnknownTimeoutPolicy, policy, a.ID)
	}
}

// applyEscalatePolicy advances the reviewer chain (or degrades to fail
// semantics on chain-exhaustion) — split out so sweepOne isn't dominated
// by the token-revocation + replay-tally block.
func (r *Reaper) applyEscalatePolicy(
	ctx context.Context, tx *sql.Tx, a state.Approval,
	prior, priorInTx []state.ApprovalEvent, now time.Time,
) (chainExhausted, escalateOK bool, err error) {
	priorIdx, newIdx, nextTier, ok := nextChainTier(a, prior)
	if !ok {
		// Chain exhausted — degrade to fail semantics so the row
		// still terminates and the audit trail records why.
		if err := markDecided(ctx, tx, a.ID, state.ApprovalStatusTimedOut, []string{}, now); err != nil {
			return false, false, fmt.Errorf("approval/reaper: mark exhausted: %w", err)
		}
		return true, false, nil
	}
	replayed := replayVotes(priorInTx, nextTier.Reviewers)
	revoked := outstandingJTIs(priorInTx)
	// Token revocation: one token_consumed row per outstanding JTI,
	// payload.reason='escalated'. UNIQUE(approval_id, kind, token_jti)
	// turns a later genuine use into ErrTokenReplay.
	for _, jti := range revoked {
		if err := recordEvent(ctx, recordEventOpts{
			Tx: tx, Logger: nil, ApprovalID: a.ID,
			Event: obs.EventApprovalEscalated, Kind: EventKindTokenConsumed,
			Actor: systemActor, Now: now,
			Attrs:    map[string]any{string(obs.KeyReason): reasonEscalated},
			TokenJTI: jti,
		}); err != nil {
			return false, false, fmt.Errorf("approval/reaper: revoke token %q: %w", jti, err)
		}
	}
	newSnap := state.ReviewerSet{
		Reviewers:         nextTier.Reviewers,
		Quorum:            nextTier.Quorum,
		PreventSelfReview: nextTier.PreventSelfReview,
	}
	if err := advanceTier(ctx, tx, a.ID, newSnap, nextTier.Quorum, now.Add(nextTier.Timeout), now); err != nil {
		return false, false, fmt.Errorf("approval/reaper: advance tier: %w", err)
	}
	if err := recordEvent(ctx, recordEventOpts{
		Tx: tx, Logger: r.log, ApprovalID: a.ID,
		Event: obs.EventApprovalEscalated, Kind: EventKindEscalated,
		Actor: systemActor, Now: now,
		Attrs: map[string]any{
			"prior_chain_index": priorIdx,
			"new_chain_index":   newIdx,
			"prior_quorum":      a.Quorum,
			"new_quorum":        nextTier.Quorum,
			"replayed_votes":    replayed,
			"revoked_jtis":      revoked,
		},
	}); err != nil {
		return false, false, fmt.Errorf("approval/reaper: append escalated: %w", err)
	}
	// Tier-n+1 quorum already satisfied by replays alone: emit the
	// terminal in the SAME tx so the scheduler picks up resolution
	// next tick — no second sweep.
	allow, deny, deciders := tallyReplay(replayed)
	switch {
	case allow >= nextTier.Quorum:
		if err := recordEvent(ctx, recordEventOpts{
			Tx: tx, Logger: nil, ApprovalID: a.ID,
			Event: obs.EventApprovalAutoApproved, Kind: EventKindApproved,
			Actor: "system:escalation-replay", Now: now,
		}); err != nil {
			return false, false, fmt.Errorf("approval/reaper: append replay-approved: %w", err)
		}
		if err := markDecided(ctx, tx, a.ID, state.ApprovalStatusApproved, deciders, now); err != nil {
			return false, false, fmt.Errorf("approval/reaper: mark replay-approved: %w", err)
		}
	case deny >= nextTier.Quorum:
		if err := recordEvent(ctx, recordEventOpts{
			Tx: tx, Logger: nil, ApprovalID: a.ID,
			Event: obs.EventApprovalEscalated, Kind: EventKindRejected,
			Actor: "system:escalation-replay", Now: now,
		}); err != nil {
			return false, false, fmt.Errorf("approval/reaper: append replay-rejected: %w", err)
		}
		if err := markDecided(ctx, tx, a.ID, state.ApprovalStatusRejected, deciders, now); err != nil {
			return false, false, fmt.Errorf("approval/reaper: mark replay-rejected: %w", err)
		}
	}
	return false, true, nil
}

// errSweepSkip is the sentinel sweepOne uses to short-circuit the WithTx
// callback when a parallel Sweep already terminated this row. Returning
// a non-nil error from the closure rolls the empty tx back (no-op); the
// outer Sweep errors.Is-matches and treats the row as cleanly handled.
var errSweepSkip = errors.New("approval/reaper: parallel sweep won")

// listApprovalEventsTx mirrors state.DB.ListApprovalEvents but reads
// through the supplied tx so the in-tx re-check sees writes from a
// just-committed sibling sweep. Identical column order to keep the
// fold-equivalence contract.
func listApprovalEventsTx(ctx context.Context, tx *sql.Tx, approvalID string) ([]state.ApprovalEvent, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, approval_id, ts, kind, actor, payload_json, token_jti
		FROM approval_events
		WHERE approval_id = ?
		ORDER BY id`, approvalID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []state.ApprovalEvent
	for rows.Next() {
		var e state.ApprovalEvent
		var ts int64
		var payload string
		if err := rows.Scan(&e.ID, &e.ApprovalID, &ts, &e.Kind, &e.Actor, &payload, &e.TokenJTI); err != nil {
			return nil, err
		}
		e.Ts = time.Unix(ts, 0).UTC()
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

// markDecided writes the denormalised status / decided_at / decided_by
// columns. Mirrors state.MarkApprovalDecided but operates on a *sql.Tx
// so the per-row commit boundary aligns with the event append.
func markDecided(ctx context.Context, tx *sql.Tx, approvalID, status string, decidedBy []string, at time.Time) error {
	by, err := json.Marshal(strutil.OrEmpty(decidedBy))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE approvals
		SET status = ?, decided_at = ?, decided_by = ?, updated_at = ?
		WHERE id = ?`,
		status, at.UTC().Unix(), string(by), at.UTC().Unix(), approvalID)
	return err
}

// advanceTier rewrites the snapshot + quorum + timeout_at for a row
// that just escalated. Status stays 'pending' — sweepOne writes a
// terminal event after this if replayed votes already satisfied the
// new quorum.
func advanceTier(ctx context.Context, tx *sql.Tx, approvalID string, snap state.ReviewerSet, quorum int, timeoutAt, updatedAt time.Time) error {
	js, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE approvals
		SET reviewer_set_snapshot_json = ?,
		    quorum = ?,
		    timeout_at = ?,
		    updated_at = ?
		WHERE id = ?`,
		string(js), quorum, timeoutAt.UTC().Unix(), updatedAt.UTC().Unix(), approvalID)
	return err
}

