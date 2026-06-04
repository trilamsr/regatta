// S3-T2 Phase B — shadow-write seam for approval_events → substrate.
//
// Spec: docs/engineer/specs/2026-06-02-s3-t2-substrate-cutover.md §3.
// Substrate W1 §3 R2 forbids atomic dual-write (sqlite WAL nesting);
// the substrate mirror is a separate write that runs AFTER the legacy
// write commits. Substrate failure is best-effort: structured log +
// `substrate_divergence_audit` row + counter increment, never propagates.
//
// Phase B only; Phase C (read-from-substrate) is the follow-up PR.

package state

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/trilamsr/regatta/internal/orchestrator/state/approvals_shadow"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// AppendApprovalEventWithShadow writes the legacy approval_events row
// FIRST. If legacy fails, returns the error and writes nothing to
// substrate. If legacy succeeds AND Mode==on, mirrors the row into
// `substrate_events` in a separate independent write; substrate failure
// records a divergence audit row and logs but returns nil to the caller.
func (d *DB) AppendApprovalEventWithShadow(ctx context.Context, ev ApprovalEvent, cfg ShadowWriteConfig) error {
	if err := d.AppendApprovalEvent(ctx, ev); err != nil {
		return err
	}
	if cfg.Mode != ShadowModeOn {
		return nil
	}
	if err := d.shadowMirrorApprovalEvent(ctx, ev, cfg); err != nil {
		d.recordApprovalDivergence(ctx, ev, cfg, err)
	}
	return nil
}

// shadowMirrorApprovalEvent builds one substrate_events row from one
// legacy approval_events row and writes it via substrate.AppendEvent in
// its own tx. Errors surface to the caller; the caller decides whether
// to propagate (Phase B: log+audit, never propagate).
func (d *DB) shadowMirrorApprovalEvent(ctx context.Context, ev ApprovalEvent, cfg ShadowWriteConfig) error {
	payload, err := approvals_shadow.BuildShadowPayload(ev.ApprovalID, ev.Kind, ev.Actor, ev.TokenJTI)
	if err != nil {
		return fmt.Errorf("shadow payload: %w", err)
	}
	at := d.now().UTC()
	nonce := approvals_shadow.ShadowNonce(ev.ApprovalID, ev.Kind, ev.TokenJTI, ev.Ts)
	actor := ev.Actor
	if actor == "" {
		actor = approvals_shadow.SystemActor
	}
	e := substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         cfg.RunID,
		TenantID:      cfg.TenantID,
		Kind:          substrate.KindApprovalEvent,
		PayloadJSON:   payload,
		WrittenBy:     approvals_shadow.SanitizeWrittenBy(actor),
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         nonce,
	}
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		if err := substrate.AppendEvent(ctx, tx, e, cfg.Key, cfg.KeyID); err != nil {
			return fmt.Errorf("shadow append: %w", err)
		}
		return nil
	})
}

// recordApprovalDivergence writes one `substrate_divergence_audit` row
// + emits a structured log line so an operator dashboard or human
// runbook can spot drift. Best-effort: a divergence-write failure
// shadows itself and returns nil. Wave-1 substrate stays empty until
// SUBSTRATE_APPROVALS_SHADOW_WRITE=on, so audit-table noise is bounded
// by the operator's flip cadence.
func (d *DB) recordApprovalDivergence(ctx context.Context, ev ApprovalEvent, cfg ShadowWriteConfig, cause error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.WarnContext(ctx, "substrate.approvals.shadow_write_diverged",
		slog.String("approval_id", ev.ApprovalID),
		slog.String("kind", ev.Kind),
		slog.String("cause", cause.Error()),
	)
	diff := approvals_shadow.TruncateDiff(cause.Error())
	if _, err := d.sql.ExecContext(ctx, `
		INSERT INTO substrate_divergence_audit
		    (detected_at, detector, store, primary_key,
		     legacy_summary, substrate_summary, diff_summary)
		VALUES (?, 'layer1_write', 'approvals', ?, ?, '', ?)`,
		d.now().UTC().UnixMilli(),
		ev.ApprovalID,
		fmt.Sprintf("kind=%s actor=%s", ev.Kind, ev.Actor),
		diff,
	); err != nil {
		logger.ErrorContext(ctx, "substrate.approvals.divergence_audit_write_failed",
			slog.String("approval_id", ev.ApprovalID),
			slog.String("cause", err.Error()),
		)
	}
}
