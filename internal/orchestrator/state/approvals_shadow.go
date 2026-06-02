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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// systemActor is the substrate written_by placeholder for events
// emitted by the timeout sweeper (no human actor).
const systemActor = "system"

// ShadowMode is the per-process tri-state flag from spec §3.4. Spec
// keeps `dry_run` for operator smoke-tests; this PR ships only Off+On.
// Adding `dry_run` is a follow-up if operator pilots need it.
type ShadowMode string

const (
	// ShadowModeOff disables the substrate mirror. Default Wave 1 ship.
	ShadowModeOff ShadowMode = "off"
	// ShadowModeOn runs the substrate mirror AFTER legacy commit; failure
	// is logged + audited but does not propagate.
	ShadowModeOn ShadowMode = "on"
)

// ShadowWriteConfig carries per-process knobs the shadow path needs.
// Caller (cmd/regatta serve) builds it once from env at startup and
// threads it to every approval write callsite.
type ShadowWriteConfig struct {
	Mode     ShadowMode
	Key      []byte
	KeyID    string
	TenantID string
	RunID    string
	Logger   *slog.Logger
}

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
	payload, err := buildApprovalShadowPayload(ev)
	if err != nil {
		return fmt.Errorf("shadow payload: %w", err)
	}
	at := d.now().UTC()
	// Nonce: substrate's UNIQUE(run_id, written_by, nonce) constraint
	// requires a fresh per-event nonce. Deterministic-derive from
	// (approval_id, kind, token_jti, ts) so replays of the same legacy
	// event collide on substrate instead of double-writing. token_jti is
	// NOT reused as the substrate nonce per spec §3.5 — it lives inside
	// payload_json and the substrate nonce is its own 16-byte value.
	nonce := approvalShadowNonce(ev)
	actor := ev.Actor
	if actor == "" {
		// substrate written_by CHECK forbids empty; the reaper sweep
		// uses the same placeholder when emitting timed_out events.
		actor = systemActor
	}
	e := substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         cfg.RunID,
		TenantID:      cfg.TenantID,
		Kind:          substrate.KindApprovalEvent,
		PayloadJSON:   payload,
		WrittenBy:     sanitizeWrittenBy(actor),
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         nonce,
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("shadow begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := substrate.AppendEvent(ctx, tx, e, cfg.Key, cfg.KeyID); err != nil {
		return fmt.Errorf("shadow append: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("shadow commit: %w", err)
	}
	return nil
}

// buildApprovalShadowPayload maps a legacy ApprovalEvent into the
// substrate `approval_event` payload shape validated by
// substrate.validateApprovalEvent. Legacy `kind` becomes `transition`
// (spec §3.2); the original token_jti is preserved inside payload_json,
// distinct from the substrate column nonce (spec §3.5 invariant).
func buildApprovalShadowPayload(ev ApprovalEvent) (json.RawMessage, error) {
	p := map[string]any{
		"approval_id": ev.ApprovalID,
		"transition":  ev.Kind,
		"actor":       ev.Actor,
	}
	if ev.TokenJTI != "" {
		p["token_jti"] = ev.TokenJTI
	}
	return json.Marshal(p)
}

// approvalShadowNonce derives a 16-byte hex nonce deterministically from
// the legacy event's identity tuple. Re-running the same shadow-write
// for the same legacy row produces the same nonce, so the UNIQUE
// constraint catches accidental double-mirroring rather than letting
// two substrate rows shadow one legacy row.
func approvalShadowNonce(ev ApprovalEvent) string {
	h := sha256.Sum256([]byte(ev.ApprovalID + "|" + ev.Kind + "|" + ev.TokenJTI + "|" + ev.Ts.UTC().Format("20060102T150405.000000000Z")))
	return hex.EncodeToString(h[:16])
}

// sanitizeWrittenBy maps a free-form actor string to the substrate
// written_by CHECK's character class. Substrate validates
// `written_by NOT GLOB '*[^a-zA-Z0-9_:.-]*'` (0006_substrate.sql); the
// approval-event actor is a username or "system" today, but a future
// LDAP-style email actor would fail the CHECK without this scrub.
func sanitizeWrittenBy(actor string) string {
	var b strings.Builder
	for _, r := range actor {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == ':', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return systemActor
	}
	if len(out) > 128 {
		out = out[:128]
	}
	return out
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
	diff := cause.Error()
	if len(diff) > 512 {
		diff = diff[:512]
	}
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
