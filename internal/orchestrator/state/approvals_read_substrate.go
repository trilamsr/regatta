// Phase C read-from-substrate seam for approval_events: opt-in via
// ShadowWriteConfig.ReadMode, falls back to legacy when substrate returns
// empty but legacy has data (the canonical drift signal — Phase B's
// best-effort mirror can fail silently). The miss is recorded in
// substrate_divergence_audit with detector='layer1_read'.
//
// Phase C scope: opt-in flag only, approvals only. Substrate→ApprovalEvent
// reconstruction is lossy (mirror carries only transition/actor/token_jti);
// callers using `decided`-kind Fold case-arms MUST keep ReadModeLegacy
// until Phase D widens the mirror — only requested/approved/rejected/
// timed_out kinds are safe under Phase C.

package state

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/approvals_shadow"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// ListApprovalEventsWithShadow returns approval events for approvalID,
// reading from substrate when cfg.ReadMode==substrate_first. Substrate-miss
// + legacy-non-empty records one divergence row and returns the legacy
// rows; both-empty returns the empty slice. ReadMode==legacy delegates to
// ListApprovalEvents. Substrate-first (not parallel) avoids doubling
// sqlite traffic for no Phase-C benefit.
func (d *DB) ListApprovalEventsWithShadow(ctx context.Context, approvalID string, cfg ShadowWriteConfig) ([]ApprovalEvent, error) {
	if cfg.ReadMode != ReadModeSubstrateFirst {
		return d.ListApprovalEvents(ctx, approvalID)
	}
	subEvents, err := d.readApprovalEventsFromSubstrate(ctx, approvalID, cfg.RunID)
	if err != nil {
		// Surface substrate read errors (fallback contract is "miss-not-error"); audit fires only on empty-vs-non-empty mismatch.
		return nil, fmt.Errorf("state: substrate read approval %q: %w", approvalID, err)
	}
	if len(subEvents) > 0 {
		return subEvents, nil
	}
	legacy, err := d.ListApprovalEvents(ctx, approvalID)
	if err != nil {
		return nil, err
	}
	if len(legacy) == 0 {
		return nil, nil
	}
	d.recordApprovalReadDivergence(ctx, approvalID, cfg, len(legacy))
	return legacy, nil
}

// readApprovalEventsFromSubstrate folds substrate_events for runID and filters to approvalID in-memory; per-run approval volume is O(reviewers), so a JSON-indexed column is deferred to Phase D.
func (d *DB) readApprovalEventsFromSubstrate(ctx context.Context, approvalID, runID string) ([]ApprovalEvent, error) {
	if runID == "" {
		return nil, nil
	}
	all, err := substrate.Fold(ctx, d.sql, runID, substrate.KindApprovalEvent)
	if err != nil {
		return nil, err
	}
	var out []ApprovalEvent
	for _, ev := range all {
		p, err := approvals_shadow.ParseSubstrateApprovalPayload(ev.PayloadJSON)
		if err != nil {
			return nil, err
		}
		if p.ApprovalID != approvalID {
			continue
		}
		out = append(out, ApprovalEvent{
			ApprovalID: p.ApprovalID,
			Ts:         time.UnixMilli(ev.WrittenAt).UTC(),
			Kind:       p.Transition,
			Actor:      p.Actor,
			Payload:    json.RawMessage(append([]byte(nil), ev.PayloadJSON...)),
			TokenJTI:   p.TokenJTI,
		})
	}
	return out, nil
}

// recordApprovalReadDivergence writes a layer1_read row + structured log so dashboards surface silent shadow-write gaps; best-effort — divergence-write failure logs but doesn't affect the legacy fallback rows the caller has.
func (d *DB) recordApprovalReadDivergence(ctx context.Context, approvalID string, cfg ShadowWriteConfig, legacyCount int) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.WarnContext(ctx, "substrate.approvals.read_diverged",
		slog.String("approval_id", approvalID),
		slog.Int("legacy_count", legacyCount),
	)
	if _, err := d.sql.ExecContext(ctx, `
		INSERT INTO substrate_divergence_audit
		    (detected_at, detector, store, primary_key,
		     legacy_summary, substrate_summary, diff_summary)
		VALUES (?, 'layer1_read', 'approvals', ?, ?, '', 'substrate_empty_legacy_nonempty')`,
		d.now().UTC().UnixMilli(),
		approvalID,
		fmt.Sprintf("legacy_rows=%d", legacyCount),
	); err != nil {
		logger.ErrorContext(ctx, "substrate.approvals.read_divergence_audit_write_failed",
			slog.String("approval_id", approvalID),
			slog.String("cause", err.Error()),
		)
	}
}
