// S3-T2 Phase C — read-from-substrate seam for approval_events.
//
// Phase B (#369) lands the shadow-write side: every legacy append
// mirrors into substrate_events best-effort after the legacy commit.
// Phase C lets read paths opt in via ShadowWriteConfig.ReadMode and
// falls back to legacy when substrate returns empty but legacy has
// data (the canonical drift signal — substrate mirror failed silently
// in Phase B and went unnoticed). The miss is recorded in
// substrate_divergence_audit with detector='layer1_read' so an operator
// dashboard can attribute drift to the read path.
//
// Phase C scope (NOT Phase D):
//   - opt-in flag only; Mode=off → legacy reads unchanged.
//   - approvals only; work_item_outputs / blackboard / edges stay in Phase X.
//   - substrate→ApprovalEvent reconstruction maps {transition, actor,
//     token_jti} back to (Kind, Actor, TokenJTI). Phase B's mirror only
//     carries those four fields; the legacy event-id and original opaque
//     payload are lossy on substrate-sourced reads. For `decided`-kind
//     events whose Fold extracts {decision:"allow"|"deny"} from payload,
//     callers MUST keep ReadModeLegacy until Phase D widens the mirror.
//     The lint contract: only `requested`/`approved`/`rejected`/`timed_out`
//     kinds (Fold case-arms that read Kind+Actor only) are safe under
//     Phase C — TestApproval_ReadFromSubstrate_HappyPath uses `requested`.

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
// reading from substrate when cfg.ReadMode==substrate_first. Substrate-
// miss + legacy-non-empty records one divergence row (detector=
// layer1_read) and returns the legacy rows; both-empty returns the empty
// slice with no divergence. cfg.ReadMode==legacy delegates to
// ListApprovalEvents unchanged.
//
// Why substrate-first not parallel: Phase C's invariant is "substrate
// is reachable AND populated"; if it is, legacy stays untouched. If it
// is not, the legacy fallback is the safety net Phase B's best-effort
// mirror demands. Parallel reads would double sqlite traffic on every
// approval lookup for no Phase-C benefit.
func (d *DB) ListApprovalEventsWithShadow(ctx context.Context, approvalID string, cfg ShadowWriteConfig) ([]ApprovalEvent, error) {
	if cfg.ReadMode != ReadModeSubstrateFirst {
		return d.ListApprovalEvents(ctx, approvalID)
	}
	subEvents, err := d.readApprovalEventsFromSubstrate(ctx, approvalID, cfg.RunID)
	if err != nil {
		// Substrate read failure is itself a divergence signal but Phase
		// C's fallback contract is "miss-not-error"; we surface the
		// error so an operator probe rather than silently masking it.
		// The audit + log fire only on the empty-vs-non-empty mismatch.
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

// readApprovalEventsFromSubstrate folds substrate_events for the run and
// filters to approvalID. The filter is in-memory because substrate's
// per-run approval volume is bounded (one approval has O(reviewers)
// events) and adding a JSON-indexed column would block Phase C on a
// schema decision that belongs to Phase D / the substrate-side
// FoldByApprovalID helper.
func (d *DB) readApprovalEventsFromSubstrate(ctx context.Context, approvalID, runID string) ([]ApprovalEvent, error) {
	if runID == "" {
		// Without a RunID we cannot scope the fold; treat as miss so the
		// legacy fallback runs. Wave-1 callers always set RunID via
		// serve.go; this guard is for tests that forget the field.
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

// recordApprovalReadDivergence writes a layer1_read row + emits a
// structured log so an operator dashboard surfaces silent shadow-write
// gaps. Best-effort: divergence-write failure logs and returns; the
// caller already has the legacy fallback rows so the user-facing read
// is unaffected.
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
