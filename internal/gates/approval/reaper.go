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
)

// on_timeout policy enum + vote-side enum. Centralised so the goconst
// linter stays quiet and a future config-validator edit (A2) can point
// at a single set of canonical strings rather than re-spelling them.
const (
	policyFail        = "fail"
	policyAutoApprove = "auto_approve"
	policyEscalate    = "escalate"

	voteAllow = "allow"
	voteDeny  = "deny"

	reasonEscalated = "escalated"

	// Event-kind enum used by the reaper. Mirrors the wire values in
	// state's append API + spec §4.1; centralised so the goconst
	// linter and any later cross-package consumer share one symbol.
	kindTimedOut      = "timed_out"
	kindApproved      = "approved"
	kindRejected      = "rejected"
	kindEscalated     = "escalated"
	kindDecided       = "decided"
	kindTokenConsumed = "token_consumed"
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
	tx, err := r.db.SQL().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("approval/reaper: begin tx %q: %w", a.ID, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

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
		return nil
	}

	policy := a.OnTimeout
	if policy == "" {
		policy = policyFail
	}
	timedOutPayload, _ := json.Marshal(map[string]string{"policy": policy})
	if err := insertEvent(ctx, tx, a.ID, now, kindTimedOut, "system", string(timedOutPayload), ""); err != nil {
		return fmt.Errorf("approval/reaper: append timed_out: %w", err)
	}
	if r.txHook != nil {
		if err := r.txHook(tx); err != nil {
			return fmt.Errorf("approval/reaper: txHook abort: %w", err)
		}
	}

	switch policy {
	case policyFail:
		if err := markDecided(ctx, tx, a.ID, state.ApprovalStatusTimedOut, []string{}, now); err != nil {
			return fmt.Errorf("approval/reaper: mark timed_out: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("approval/reaper: commit: %w", err)
		}
		committed = true
		r.log.Info(string(obs.EventApprovalTimedOut),
			string(obs.KeyApprovalID), a.ID,
			string(obs.KeyWorkItemID), a.WorkItemID,
			string(obs.KeyGateID), a.GateName,
			string(obs.KeyPolicy), policyFail,
		)
		return nil

	case policyAutoApprove:
		approvedPayload, _ := json.Marshal(map[string]string{"reason": "timeout_default"})
		if err := insertEvent(ctx, tx, a.ID, now, kindApproved, "system:timeout-default", string(approvedPayload), ""); err != nil {
			return fmt.Errorf("approval/reaper: append approved: %w", err)
		}
		if err := markDecided(ctx, tx, a.ID, state.ApprovalStatusApproved, []string{"system:timeout-default"}, now); err != nil {
			return fmt.Errorf("approval/reaper: mark approved: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("approval/reaper: commit: %w", err)
		}
		committed = true
		r.log.Info(string(obs.EventApprovalTimedOut),
			string(obs.KeyApprovalID), a.ID,
			string(obs.KeyWorkItemID), a.WorkItemID,
			string(obs.KeyGateID), a.GateName,
			string(obs.KeyPolicy), policyAutoApprove,
		)
		r.log.Info(string(obs.EventApprovalAutoApproved),
			string(obs.KeyApprovalID), a.ID,
			string(obs.KeyWorkItemID), a.WorkItemID,
			string(obs.KeyGateID), a.GateName,
		)
		return nil

	case policyEscalate:
		priorIdx, newIdx, nextTier, ok := nextChainTier(a, prior)
		if !ok {
			// Chain exhausted — degrade to fail semantics so the row
			// still terminates and the audit trail records why.
			if err := markDecided(ctx, tx, a.ID, state.ApprovalStatusTimedOut, []string{}, now); err != nil {
				return fmt.Errorf("approval/reaper: mark exhausted: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("approval/reaper: commit: %w", err)
			}
			committed = true
			r.log.Warn(string(obs.EventApprovalTimedOut),
				string(obs.KeyApprovalID), a.ID,
				string(obs.KeyPolicy), policyEscalate,
				string(obs.KeyReason), ErrEscalationChainExhausted.Error(),
			)
			return nil
		}
		replayed := replayVotes(priorInTx, nextTier.Reviewers)
		revoked := outstandingJTIs(priorInTx)

		// Token revocation: one token_consumed row per outstanding JTI,
		// payload.reason='escalated'. Single-use UNIQUE on
		// (approval_id, kind, token_jti) means a later genuine use
		// fails with ErrTokenReplay.
		revokePayload, _ := json.Marshal(map[string]string{"reason": reasonEscalated})
		for _, jti := range revoked {
			if err := insertEvent(ctx, tx, a.ID, now, kindTokenConsumed, "system", string(revokePayload), jti); err != nil {
				return fmt.Errorf("approval/reaper: revoke token %q: %w", jti, err)
			}
		}

		// Advance the snapshot + quorum + timeout to the new tier.
		newSnap := state.ReviewerSet{
			Reviewers:         nextTier.Reviewers,
			Quorum:            nextTier.Quorum,
			PreventSelfReview: nextTier.PreventSelfReview,
		}
		newTimeoutAt := now.Add(nextTier.Timeout)
		if err := advanceTier(ctx, tx, a.ID, newSnap, nextTier.Quorum, newTimeoutAt, now); err != nil {
			return fmt.Errorf("approval/reaper: advance tier: %w", err)
		}

		escPayload, err := json.Marshal(map[string]any{
			"prior_chain_index": priorIdx,
			"new_chain_index":   newIdx,
			"prior_quorum":      a.Quorum,
			"new_quorum":        nextTier.Quorum,
			"replayed_votes":    replayed,
			"revoked_jtis":      revoked,
		})
		if err != nil {
			return fmt.Errorf("approval/reaper: marshal escalation payload: %w", err)
		}
		if err := insertEvent(ctx, tx, a.ID, now, kindEscalated, "system", string(escPayload), ""); err != nil {
			return fmt.Errorf("approval/reaper: append escalated: %w", err)
		}

		// Tally replayed (non-discarded) votes against the new quorum.
		// If a tier-n+1 quorum is already satisfied by replays alone,
		// emit the terminal event + mark decided in the SAME tx so the
		// scheduler picks up the resolution next tick — no second sweep.
		allow, deny, deciders := tallyReplay(replayed)
		switch {
		case allow >= nextTier.Quorum:
			if err := insertEvent(ctx, tx, a.ID, now, kindApproved, "system:escalation-replay", "{}", ""); err != nil {
				return fmt.Errorf("approval/reaper: append replay-approved: %w", err)
			}
			if err := markDecided(ctx, tx, a.ID, state.ApprovalStatusApproved, deciders, now); err != nil {
				return fmt.Errorf("approval/reaper: mark replay-approved: %w", err)
			}
		case deny >= nextTier.Quorum:
			if err := insertEvent(ctx, tx, a.ID, now, kindRejected, "system:escalation-replay", "{}", ""); err != nil {
				return fmt.Errorf("approval/reaper: append replay-rejected: %w", err)
			}
			if err := markDecided(ctx, tx, a.ID, state.ApprovalStatusRejected, deciders, now); err != nil {
				return fmt.Errorf("approval/reaper: mark replay-rejected: %w", err)
			}
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("approval/reaper: commit: %w", err)
		}
		committed = true
		r.log.Info(string(obs.EventApprovalTimedOut),
			string(obs.KeyApprovalID), a.ID,
			string(obs.KeyPolicy), policyEscalate,
		)
		r.log.Info(string(obs.EventApprovalEscalated),
			string(obs.KeyApprovalID), a.ID,
			string(obs.KeyWorkItemID), a.WorkItemID,
			string(obs.KeyGateID), a.GateName,
			string(obs.KeyPriorChainIndex), priorIdx,
			string(obs.KeyNewChainIndex), newIdx,
		)
		return nil

	default:
		return fmt.Errorf("%w: %q for approval %q", ErrUnknownTimeoutPolicy, policy, a.ID)
	}
}

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

// insertEvent writes one approval_events row inside a tx. Mirrors the
// shape of state.AppendApprovalEvent (modulo the typed-error wrap) so
// fold(events) sees identical bytes regardless of who wrote the row.
// Default payload "{}" matches the schema default on the column.
func insertEvent(ctx context.Context, tx *sql.Tx, approvalID string, ts time.Time, kind, actor, payload, jti string) error {
	if payload == "" {
		payload = "{}"
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO approval_events (
			approval_id, ts, kind, actor, payload_json, token_jti
		) VALUES (?, ?, ?, ?, ?, ?)`,
		approvalID, ts.UTC().Unix(), kind, actor, payload, jti)
	return err
}

// markDecided writes the denormalised status / decided_at / decided_by
// columns. Mirrors state.MarkApprovalDecided but operates on a *sql.Tx
// so the per-row commit boundary aligns with the event append.
func markDecided(ctx context.Context, tx *sql.Tx, approvalID, status string, decidedBy []string, at time.Time) error {
	by, err := json.Marshal(orEmpty(decidedBy))
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

// orEmpty turns nil into [] so the JSON wire shape matches the column
// default and fold-of-events math stays symmetric across NULL-vs-empty
// (same contract as state.orEmpty).
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// isTerminal returns true when the event log already contains an
// `approved`, `rejected`, or `timed_out` row. Defence against a
// concurrent Sweep that won the race: the second one observes the
// terminal marker and exits without appending a duplicate timed_out.
// (Spec §4.1 fold-equivalence: status = fold(events).)
func isTerminal(events []state.ApprovalEvent) bool {
	for _, e := range events {
		switch e.Kind {
		case kindApproved, kindRejected, kindTimedOut:
			return true
		}
	}
	return false
}

// nextChainTier returns the next tier in the escalation chain plus
// the prior / new indices. priorIdx is the count of `escalated`
// events already in the log (initial chain position 0 = original
// snapshot; each escalation event bumps the pointer one step).
func nextChainTier(a state.Approval, events []state.ApprovalEvent) (priorIdx, newIdx int, tier state.TierConfig, ok bool) {
	priorIdx = 0
	for _, e := range events {
		if e.Kind == kindEscalated {
			priorIdx++
		}
	}
	newIdx = priorIdx + 1
	// The original snapshot occupies chain slot 0 conceptually; the
	// EscalationChain slice holds the next-tier configs starting at
	// index 0 = tier 1. So tier n requires chain[n-1].
	if newIdx-1 < 0 || newIdx-1 >= len(a.EscalationChain) {
		return 0, 0, state.TierConfig{}, false
	}
	return priorIdx, newIdx, a.EscalationChain[newIdx-1], true
}

// ReplayedVote is the per-vote audit row embedded in the escalated
// event's payload. Exported as JSON via field tags only — the type
// itself stays package-private to keep the payload shape governed by
// the spec rather than by an external caller's import graph.
type replayedVote struct {
	Actor     string `json:"actor"`
	Vote      string `json:"vote"`
	Discarded bool   `json:"discarded"`
}

// replayVotes walks prior `decided` events and decides whether each
// reviewer's vote carries forward into the new tier. A vote carries
// forward iff the reviewer is also a member of the new tier; the
// payload still records discarded votes so the audit trail tells the
// full story (§3.3.1.1).
func replayVotes(events []state.ApprovalEvent, newReviewers []string) []replayedVote {
	in := make(map[string]struct{}, len(newReviewers))
	for _, r := range newReviewers {
		in[r] = struct{}{}
	}
	out := make([]replayedVote, 0)
	for _, e := range events {
		if e.Kind != kindDecided {
			continue
		}
		var p struct {
			Vote string `json:"vote"`
		}
		_ = json.Unmarshal(e.Payload, &p)
		_, present := in[e.Actor]
		out = append(out, replayedVote{
			Actor:     e.Actor,
			Vote:      p.Vote,
			Discarded: !present,
		})
	}
	return out
}

// outstandingJTIs returns every token JTI that was minted (kind=
// 'token_minted', or any non-empty token_jti on a non-consumed event)
// and has not already been consumed. Reaper inserts one
// token_consumed-with-reason=escalated row per JTI to revoke the
// outstanding token under the prior tier (§3.3.1.3).
func outstandingJTIs(events []state.ApprovalEvent) []string {
	minted := make([]string, 0)
	consumed := make(map[string]struct{})
	for _, e := range events {
		if e.TokenJTI == "" {
			continue
		}
		if e.Kind == kindTokenConsumed {
			consumed[e.TokenJTI] = struct{}{}
			continue
		}
		// First occurrence of a JTI on any non-consumed event is the
		// mint marker. Duplicate sightings (e.g. notified + decided
		// carrying the same JTI) collapse into one outstanding entry.
		exists := false
		for _, j := range minted {
			if j == e.TokenJTI {
				exists = true
				break
			}
		}
		if !exists {
			minted = append(minted, e.TokenJTI)
		}
	}
	out := make([]string, 0, len(minted))
	for _, j := range minted {
		if _, c := consumed[j]; !c {
			out = append(out, j)
		}
	}
	return out
}

// tallyReplay aggregates non-discarded votes by side, returning the
// allow / deny counts and the deciders list (for MarkApprovalDecided
// when the new tier is already satisfied by replays alone, §3.3.1.2).
func tallyReplay(replayed []replayedVote) (allow, deny int, deciders []string) {
	deciders = make([]string, 0)
	for _, v := range replayed {
		if v.Discarded {
			continue
		}
		switch v.Vote {
		case voteAllow:
			allow++
			deciders = append(deciders, v.Actor)
		case voteDeny:
			deny++
			deciders = append(deciders, v.Actor)
		}
	}
	return allow, deny, deciders
}
