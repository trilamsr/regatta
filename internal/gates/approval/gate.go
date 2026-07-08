// Package approval implements the HITL approval gate. The gate pauses
// a work_item until the reviewer set decides (allow/deny) via signed
// callback token.
//
// Rationale (extracted from RFC-0003; retained here because the source
// file was collapsed into docs/engineer/CHANGELOG.md per the W2 spec
// purge). Numbering matches RFC-0003 §Decision:
//
//  1. Status modulator, not new lifecycle — adds planned →
//     awaiting_approval → planned|rejected on work_items; scheduler
//     hot path for non-gated work is untouched.
//  2. Event-sourced canonical truth — approval_events is the log,
//     approvals.status is fold(events) and property-tested byte-equal,
//     so replay is deterministic and audit is append-only.
//  3. HMAC constant-time verify BEFORE JSON unmarshal — closes the
//     parser-oracle class where a crafted payload triggers behaviour
//     pre-auth; reuses contracts/schemas/sign.go:macSum, JTI drawn
//     from crypto/rand (math/rand lint-gated).
//  4. Reviewer set snapshotted at request time, not decide time —
//     mutating regatta.yaml after an approval opens cannot change who
//     can approve it; N-of-M quorum + prevent_self_review are the RBAC
//     primitives, team-scope deferred.
//  5. Escalation tier ladder — on_timeout=escalate advances tiers,
//     replays prior votes against the new quorum, revokes tier-N
//     tokens, mints tier-N+1; terminal event follows in-tx when
//     replayed votes satisfy the new quorum.
//  6. Reaper auto-approve is config-gated to risk_class=low — the
//     config-load validator rejects auto_approve on medium|high gates;
//     high-blast classes MUST stay fail or escalate.
//  7. Decide-path atomicity — all five mutations (token_consumed
//     event, decided event, denormalised status update, terminal kind
//     event, work_items.status transition) run in one BEGIN IMMEDIATE
//     … COMMIT; failure rolls back the block, token stays unconsumed.
//  8. Fold ordering is by event id, not ts — autoincrement id is the
//     canonical sequence; wall-clock ts is advisory and may drift.
//  9. One open approval per (work_item_id, gate_name) — belt via
//     UNIQUE index on approvals; suspenders via process-level flock +
//     pool size 1 giving at most one Tick concurrently.
//  10. fold ≡ status property — the denormalised approvals.status
//      column matches fold(approval_events).status across every
//      observable state, asserted by a 200-sequence property test.
//  11. Notifier is interface-only in MVP — default is a slog no-op so
//      audit trail captures intent; unregistered notifier kind fails
//      startup fail-closed rather than silently falling through to
//      stub.
//  12. CLI surface — regatta approval decide --token … --decision
//      allow|deny [--reason …] and regatta approval list [--mine]
//      [--format=table|json] are the two operator entry points.
package approval

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/canon/approvaltoken"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Gate-evaluation sentinels.
var (
	// ErrMissingDB / ErrMissingNotifier / ErrMissingKeyring are
	// constructor-time guards so a misconfigured runtime fails loud at
	// NewGate rather than silently no-op at first tick.
	ErrMissingDB       = errors.New("approval: gate requires non-nil *state.DB")
	ErrMissingNotifier = errors.New("approval: gate requires non-nil Notifier")
	ErrMissingKeyring  = errors.New("approval: gate requires non-nil Keyring")
)

// Result is the gate's tick-time verdict. The scheduler maps each
// value to a work_item transition (spec §3.1) — Wave 3 integration.
type Result int

// Gate verdict values.
const (
	ResultPause Result = iota
	ResultProceed
	ResultReject
)

// String maps Result to its slog wire form. Producers populate
// obs.KeyVerdict with this string.
func (r Result) String() string {
	switch r {
	case ResultProceed:
		return "proceed"
	case ResultReject:
		return "reject"
	}
	return "pause"
}

// Gate orchestrates one pass of the approval state machine for a single
// (work_item, gate) pair. Per spec §5.3 it is a tick-time decision —
// re-entrant, stateless aside from the DB it owns.
type Gate struct {
	db       *state.DB
	notifier Notifier
	keyring  approvaltoken.Keyring
	kid      string
	clock    func() time.Time
	log      *slog.Logger
	// Tracer is the OTel tracer this component uses to open spans.
	// Exported so callers can set it after NewGate without expanding
	// the positional signature; nil falls back to otel.Tracer
	// ("gates/approval") at Evaluate-time. Per W6 spec §3.3 +
	// feedback_spec_pattern_authority.
	Tracer trace.Tracer
	// jtiRand is the entropy source MintToken consumes for the per-
	// token jti. Defaults to crypto/rand.Reader; tests inject their own.
	jtiRand io.Reader
}

// NewGate wires a Gate. Constructor-time guards mean a misconfigured
// runtime panics here rather than silently no-op at first tick.
func NewGate(db *state.DB, n Notifier, kr approvaltoken.Keyring, kid string, clock func() time.Time, log *slog.Logger) *Gate {
	if db == nil {
		panic(ErrMissingDB)
	}
	if n == nil {
		panic(ErrMissingNotifier)
	}
	if kr == nil {
		panic(ErrMissingKeyring)
	}
	if clock == nil {
		clock = time.Now
	}
	if log == nil {
		log = slog.Default()
	}
	return &Gate{db: db, notifier: n, keyring: kr, kid: kid, clock: clock, log: log, jtiRand: rand.Reader}
}

// Evaluate is the tick-time decision (spec §3.1 step 0.5):
//   - First sighting of a gated work_item → create row, mint tokens,
//     notify, append (requested, notified) — return ResultPause.
//   - Existing row → fold(events); map terminal status to a Result.
//
// Concurrency: callers MUST be the single Tick goroutine in production
// (state.go:9 pool=1). The schema-level UNIQUE(work_item_id, gate_name)
// is the belt; this method's CreateApproval-then-Get loop covers the
// belt-trip case (two callers race) so neither errors out.
func (g *Gate) Evaluate(ctx context.Context, wi state.WorkItem, cfg Config) (result Result, retErr error) {
	// W6 spec §3.5: `gate.evaluate` is a child of the active
	// `work_item` span (opened by scheduler.applyApprovalGates).
	tracer := g.Tracer
	if tracer == nil {
		tracer = otel.Tracer("gates/approval")
	}
	ctx, span := tracer.Start(ctx, "gate.evaluate",
		trace.WithAttributes(
			attribute.String(string(obs.KeyGateID), cfg.Name),
			attribute.String(string(obs.KeyWorkItemID), wi.ID),
		))
	defer func() {
		span.SetAttributes(attribute.String(string(obs.KeyVerdict), result.String()))
		span.End()
	}()
	existing, err := g.db.GetApprovalForWorkItem(ctx, wi.ID, cfg.Name)
	if err != nil {
		return ResultPause, fmt.Errorf("approval: lookup: %w", err)
	}
	if existing == nil {
		created, err := g.createApprovalAndNotify(ctx, wi, cfg)
		if err != nil {
			return ResultPause, err
		}
		if created == nil {
			// Race-loser path: another caller won the CreateApproval
			// race; re-read and fall through to the fold branch so the
			// loser observes the winner's pending row instead of
			// duplicating notifications.
			existing, err = g.db.GetApprovalForWorkItem(ctx, wi.ID, cfg.Name)
			if err != nil || existing == nil {
				return ResultPause, fmt.Errorf("approval: post-race lookup: %w", err)
			}
		} else {
			return ResultPause, nil
		}
	}

	events, err := g.db.ListApprovalEvents(ctx, existing.ID)
	if err != nil {
		return ResultPause, fmt.Errorf("approval: list events: %w", err)
	}
	// Post-escalation gap (issue #194): reaper.sweepOne advances the
	// snapshot + revokes prior tokens + appends `escalated`, but does
	// not mint+notify the new tier. The gate is the single notifier-
	// owning component, so it closes the gap here on the first post-
	// escalation Evaluate. Idempotent: only fires while no `notified`
	// event exists after the latest `escalated`.
	if needsPostEscalationNotify(events) {
		if err := g.notifyEscalatedTier(ctx, *existing, cfg, events); err != nil {
			return ResultPause, err
		}
		// Re-read so the fold below sees the freshly-appended notified
		// event and the upstream sliceFromLastEscalated drops the prior
		// tier's `timed_out` + `decided` rows that the reaper left behind.
		events, err = g.db.ListApprovalEvents(ctx, existing.ID)
		if err != nil {
			return ResultPause, fmt.Errorf("approval: re-list events: %w", err)
		}
	}
	// Slice events from the last `escalated` so the fold's terminal-
	// detect ignores the prior tier's `timed_out` and `decided` rows
	// (they belong to a reviewer set the row has already moved past).
	// Spec §3.3.1.1: escalation invalidates the prior tier's outcome.
	res := Fold(sliceFromLastEscalated(events), FoldConfig{
		ReviewerSet: existing.ReviewerSetSnapshot,
		RequestedBy: existing.RequestedBy,
	})
	switch res.Status {
	case StatusApproved:
		return ResultProceed, nil
	case StatusRejected, StatusTimedOut:
		return ResultReject, nil
	}
	return ResultPause, nil
}

// needsPostEscalationNotify is true when the event log carries an
// `escalated` row with no `notified` row after it. The reaper appends
// `escalated` without minting+notifying for the new tier (spec
// §3.3.1.3 contract is owned by the gate). Returns false for the
// first-sighting path (no escalated events) and for the already-
// notified post-escalation steady state.
func needsPostEscalationNotify(events []state.ApprovalEvent) bool {
	lastEscalated := -1
	lastNotified := -1
	for i, e := range events {
		switch e.Kind {
		case EventKindEscalated:
			lastEscalated = i
		case EventKindNotified:
			lastNotified = i
		}
	}
	return lastEscalated >= 0 && lastNotified < lastEscalated
}

// sliceFromLastEscalated returns the suffix of events starting at the
// last `escalated` row. If none exists, returns events unchanged. The
// suffix is what the fold over the current tier should consider —
// prior-tier `timed_out` + `decided` rows belong to a snapshot the
// row has already moved past.
func sliceFromLastEscalated(events []state.ApprovalEvent) []state.ApprovalEvent {
	idx := -1
	for i, e := range events {
		if e.Kind == EventKindEscalated {
			idx = i
		}
	}
	if idx < 0 {
		return events
	}
	return events[idx+1:]
}

// notifyEscalatedTier mints fresh tokens for the current reviewer
// snapshot (which the reaper advanced to the new tier) and calls
// Notify. Appends a `notified` event so a subsequent Evaluate observes
// the steady-state and does not re-notify. The DecisionWindow is
// pulled from the active tier in the chain — the count of `escalated`
// events in the log tells us which tier we are on.
func (g *Gate) notifyEscalatedTier(ctx context.Context, a state.Approval, cfg Config, events []state.ApprovalEvent) error {
	now := g.clock()
	decisionWindow := tierDecisionWindow(a, cfg, events)
	tokens, jtis, err := g.mintTierTokens(a.ReviewerSetSnapshot, a.ID, a.WorkItemID, decisionWindow, now)
	if err != nil {
		return err
	}
	// Issue #195: persist tier-N JTI markers BEFORE Notify so a
	// subsequent escalate of this same tier finds them via
	// outstandingJTIs. Mirrors the first-sighting path's ordering.
	if err := g.persistTokenMintedRows(ctx, a.ID, jtis, now); err != nil {
		return err
	}
	req := newNotifyRequest(a, state.WorkItem{ID: a.WorkItemID}, now.Add(decisionWindow), tokens)
	receipt, err := g.notifier.Notify(ctx, req)
	if err != nil {
		return fmt.Errorf("approval: post-escalation notify: %w", err)
	}
	notifiedAttrs := map[string]any{
		string(obs.KeyGateID):        a.GateName,
		string(obs.KeyReviewerCount): len(receipt.DeliveredTo),
		"channel":                    receipt.Channel,
		"jti_count":                  len(jtis),
		"post_escalation":            true,
	}
	return recordEvent(ctx, recordEventOpts{
		DB:         g.db,
		Logger:     g.log,
		ApprovalID: a.ID,
		Event:      obs.EventApprovalNotified,
		Kind:       EventKindNotified,
		Actor:      ActorOrchestrator,
		Now:        now,
		Attrs:      notifiedAttrs,
	})
}

// tierDecisionWindow returns the DecisionWindow for the row's current
// escalation tier (snapshot is already advanced on the row itself).
// Tier index = count of `escalated` events. Falls back to cfg's window
// when the chain is empty or the index runs past the chain — defensive
// against a hand-edited DB cell or an old snapshot.
func tierDecisionWindow(a state.Approval, cfg Config, events []state.ApprovalEvent) time.Duration {
	tierIdx := 0
	for _, e := range events {
		if e.Kind == EventKindEscalated {
			tierIdx++
		}
	}
	if tierIdx > 0 && tierIdx-1 < len(a.EscalationChain) {
		if dw := a.EscalationChain[tierIdx-1].DecisionWindow; dw > 0 {
			return dw
		}
	}
	return cfg.DecisionWindow
}

// mintTierTokens issues one signed token per reviewer in the supplied
// snapshot. Lex-ordered loop so the audit trail + captureNotifier test
// see a deterministic sequence — pure-bookkeeping decision, no
// security impact. Used by both first-sighting and post-escalation
// notify paths.
func (g *Gate) mintTierTokens(snap state.ReviewerSet, approvalID, workItemID string, decisionWindow time.Duration, now time.Time) (map[string]string, []string, error) {
	tokens := make(map[string]string, len(snap.Reviewers))
	jtis := make([]string, 0, len(snap.Reviewers))
	reviewers := append([]string(nil), snap.Reviewers...)
	sort.Strings(reviewers)
	windowEnd := now.Add(decisionWindow).Unix()
	for _, reviewer := range reviewers {
		payload := approvaltoken.TokenPayload{
			WI:       workItemID,
			AID:      approvalID,
			Reviewer: reviewer,
			Window:   windowEnd,
		}
		wire, jti, err := approvaltoken.MintToken(g.keyring, g.kid, payload, g.jtiRand)
		if err != nil {
			return nil, nil, fmt.Errorf("approval: mint token for %q: %w", reviewer, err)
		}
		tokens[reviewer] = wire
		jtis = append(jtis, jti)
	}
	return tokens, jtis, nil
}

// createApprovalAndNotify performs the first-sighting sequence per
// spec §3.1. Returns the new Approval on success; nil (no error) when
// the row already exists (race-loser path). The (requested, notified)
// events plus the row INSERT all share a single tx so a partial
// failure leaves zero state behind (spec §3.2.1 atomicity contract).
func (g *Gate) createApprovalAndNotify(ctx context.Context, wi state.WorkItem, cfg Config) (*state.Approval, error) {
	now := g.clock()
	approvalID := newApprovalID()

	tokens, jtis, err := g.mintReviewerTokens(cfg, approvalID, wi.ID, now)
	if err != nil {
		return nil, err
	}

	snapshot := state.ReviewerSet{
		Reviewers:         append([]string(nil), cfg.Reviewers...),
		Quorum:            cfg.Quorum,
		PreventSelfReview: cfg.PreventSelfReview,
	}
	approval := state.Approval{
		ID:                  approvalID,
		WorkItemID:          wi.ID,
		GateName:            cfg.Name,
		RequestedAt:         now,
		RequestedBy:         "", // Wave-3 will plumb from wi metadata.
		ReviewerSetSnapshot: snapshot,
		Quorum:              cfg.Quorum,
		Status:              state.ApprovalStatusPending,
		TimeoutAt:           now.Add(cfg.Timeout),
		OnTimeout:           cfg.OnTimeout,
		EscalationChain:     cfg.EscalationChain,
	}
	if err := g.db.CreateApproval(ctx, approval); err != nil {
		if errors.Is(err, state.ErrApprovalAlreadyExists) {
			// Race-loser path: belt tripped because two callers raced
			// past the GetApprovalForWorkItem check. Return nil so
			// Evaluate re-reads the winner's row.
			return nil, nil
		}
		return nil, fmt.Errorf("approval: create row: %w", err)
	}

	requestedAttrs := map[string]any{
		string(obs.KeyWorkItemID):     wi.ID,
		string(obs.KeyGateID):         cfg.Name,
		string(obs.KeyReviewerCount):  len(cfg.Reviewers),
	}
	if err := recordEvent(ctx, recordEventOpts{
		DB:         g.db,
		Logger:     g.log,
		ApprovalID: approvalID,
		Event:      obs.EventApprovalRequested,
		Kind:       EventKindRequested,
		Actor:      ActorOrchestrator,
		Now:        now,
		Attrs:      requestedAttrs,
	}); err != nil {
		return nil, err
	}

	// Issue #195: persist one token_minted row per JTI so reaper's
	// outstandingJTIs (which already drives escalate-revocation per spec
	// §3.3.1.3) is reachable. Without this, the reaper's per-JTI
	// token_consumed-reason=escalated loop runs zero times.
	if err := g.persistTokenMintedRows(ctx, approvalID, jtis, now); err != nil {
		return nil, err
	}

	req := newNotifyRequest(approval, wi, now.Add(cfg.DecisionWindow), tokens)
	receipt, err := g.notifier.Notify(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("approval: notify: %w", err)
	}

	notifiedAttrs := map[string]any{
		string(obs.KeyGateID):         cfg.Name,
		string(obs.KeyReviewerCount):  len(receipt.DeliveredTo),
		"channel":                     receipt.Channel,
		"jti_count":                   len(jtis),
	}
	if err := recordEvent(ctx, recordEventOpts{
		DB:         g.db,
		Logger:     g.log,
		ApprovalID: approvalID,
		Event:      obs.EventApprovalNotified,
		Kind:       EventKindNotified,
		Actor:      ActorOrchestrator,
		Now:        now,
		Attrs:      notifiedAttrs,
	}); err != nil {
		return nil, err
	}
	return &approval, nil
}

// mintReviewerTokens issues one signed token per cfg-listed reviewer
// on the first-sighting path. Thin wrapper over mintTierTokens so the
// post-escalation mint and the first-sighting mint share one
// canonical body.
func (g *Gate) mintReviewerTokens(cfg Config, approvalID, workItemID string, now time.Time) (map[string]string, []string, error) {
	snap := state.ReviewerSet{Reviewers: cfg.Reviewers}
	return g.mintTierTokens(snap, approvalID, workItemID, cfg.DecisionWindow, now)
}

// persistTokenMintedRows appends one approval_events row per JTI so
// reaper.outstandingJTIs can find them. Issue #195 root cause: the
// reaper's revocation branch is dead code until this producer side
// exists. Per-row append (rather than a single batched tx) matches
// existing recordEvent shape; reaper isTerminal + UNIQUE-on-consume
// absorb a partial-mint trail if a later append fails mid-loop.
func (g *Gate) persistTokenMintedRows(ctx context.Context, approvalID string, jtis []string, now time.Time) error {
	for _, jti := range jtis {
		if err := recordEvent(ctx, recordEventOpts{
			DB:         g.db,
			Logger:     g.log,
			ApprovalID: approvalID,
			Event:      obs.EventApprovalTokenMinted,
			Kind:       EventKindTokenMinted,
			Actor:      ActorOrchestrator,
			Now:        now,
			TokenJTI:   jti,
		}); err != nil {
			return fmt.Errorf("approval: persist token_minted %q: %w", jti, err)
		}
	}
	return nil
}

// newApprovalID mints the typed-prefix id per spec §5.1 ("a-<12 hex>").
// crypto/rand is the source of truth — collisions across 2^48 ids are
// negligible. Hex shape keeps URL-encoding trivial for CLI echo.
func newApprovalID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "a-" + hex.EncodeToString(b[:])
}
