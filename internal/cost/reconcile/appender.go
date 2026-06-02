package reconcile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// SubstrateAppender is the production Appender impl: every Tick lands a
// kind=budget_reconciled row in substrate_events inside one tx. Spec
// §3.5 — row is HMAC-signed under the configured key; the tx wraps the
// single INSERT so a sqlite-level error rolls back cleanly without
// polluting the audit log.
type SubstrateAppender struct {
	db    *sql.DB
	key   []byte
	keyID string

	// nonceSeq is the per-tick monotonic counter folded into the row
	// nonce. Reconciliation MAY emit at most one row per tick, but ad-hoc
	// Tick callers (operator-triggered cron) can collide on identical
	// timestamp; the counter guarantees UNIQUE(run_id, written_by, nonce)
	// holds even at 1-tick-per-millisecond rates.
	//
	// Process-local — multi-host reconcilers are out of scope until W9.
	nonceSeq uint64
}

// SubstrateAppenderConfig is the seam between cmd/regatta and the
// production Appender. The DB handle is shared with the rest of the
// orchestrator; the HMAC key is the brief keyring's active entry — the
// reconciler does not own a separate key surface.
type SubstrateAppenderConfig struct {
	DB    *sql.DB
	Key   []byte
	KeyID string
}

// NewSubstrateAppender wires the production Appender. Returns a nil-safe
// value — callers receive a *SubstrateAppender they invoke Append() on.
// Bad config (missing DB or HMAC key) surfaces at Append time, not
// constructor time, so a misconfigured operator sees the failure in
// the structured log rather than at startup parse.
func NewSubstrateAppender(cfg SubstrateAppenderConfig) *SubstrateAppender {
	return &SubstrateAppender{
		db:    cfg.DB,
		key:   cfg.Key,
		keyID: cfg.KeyID,
	}
}

// runIDReconciler is the substrate run_id every reconciler-emitted row
// carries. Run_id pins the audit-graph component the row lives in;
// reconciliation is its own component (decoupled from any user run).
const runIDReconciler = "cost-reconciler"

// writtenByReconciler is the substrate written_by tag every reconciler-
// emitted row carries. Operators grep substrate_events by this column
// when triaging "did the reconciler run?".
const writtenByReconciler = "cost-reconciler"

// Append implements the Tick Appender seam. Opens a tx, builds an
// Event with the spec §3.5 shape, hands it to substrate.AppendEvent,
// commits on success.
//
// Nonce = `rec-<unix_nano>-<seq>` rather than a payload-derived hash:
// payload-hash nonces tie replay-protection to canonicalised JSON
// bytes, which means a hostile writer who flips one whitespace-
// insensitive byte ships a "different" row past the UNIQUE constraint.
// The per-process seq counter guarantees uniqueness even when two
// ad-hoc Ticks share the same wall-clock millisecond.
func (a *SubstrateAppender) Append(ctx context.Context, tenantID, kind string, payload json.RawMessage, writtenAt time.Time) error {
	if a.db == nil {
		return errors.New("reconcile.SubstrateAppender: DB nil — wiring missing")
	}
	if len(a.key) == 0 {
		return errors.New("reconcile.SubstrateAppender: HMAC key empty — REGATTA_HMAC_KEY unset?")
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reconcile.SubstrateAppender: begin tx: %w", err)
	}
	// On any error past this point the deferred rollback runs; on
	// success the explicit Commit below short-circuits it. sqlite
	// returns ErrTxDone on a post-commit rollback — benign.
	defer func() { _ = tx.Rollback() }()

	at := writtenAt.UTC()
	a.nonceSeq++
	ev := substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         runIDReconciler,
		TenantID:      tenantID,
		Kind:          substrate.EventKind(kind),
		PayloadJSON:   payload,
		WrittenBy:     writtenByReconciler,
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         fmt.Sprintf("rec-%d-%d", at.UnixNano(), a.nonceSeq),
	}
	if err := substrate.AppendEvent(ctx, tx, ev, a.key, a.keyID); err != nil {
		return fmt.Errorf("reconcile.SubstrateAppender: append: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reconcile.SubstrateAppender: commit: %w", err)
	}
	return nil
}
