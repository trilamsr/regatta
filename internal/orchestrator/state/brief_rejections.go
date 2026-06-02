// Brief-rejection audit reader (issue #80). Folds substrate
// `brief_rejected` events into a typed slice operators can list +
// query. The fold uses substrate.Fold which orders by
// (written_at ASC, id ASC) — Wave-1 single-writer makes that the
// canonical replay sequence.

package state

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// BriefRejection is one decoded substrate brief_rejected row. The
// freeform Reason is producer-formatted (brief_loader assembles a
// human-readable summary); operators dashboarding rejection rate group
// on a Reason prefix (e.g. `unknown_parent`, `stale_produced_at`).
type BriefRejection struct {
	Ts     time.Time
	Path   string
	Reason string
}

// ListBriefRejections returns every brief_rejected event under runID
// ordered by write time. runID is the producer-side identity the brief
// loader passed in BriefAuditConfig.RunID — production wires
// `brief-loader` so a single operator-visible scope holds every
// rejection across orchestrator restarts.
func (d *DB) ListBriefRejections(ctx context.Context, runID string) ([]BriefRejection, error) {
	if runID == "" {
		return nil, fmt.Errorf("state: ListBriefRejections requires runID")
	}
	events, err := substrate.Fold(ctx, d.sql, runID, substrate.KindBriefRejected)
	if err != nil {
		return nil, fmt.Errorf("state: fold brief_rejected: %w", err)
	}
	out := make([]BriefRejection, 0, len(events))
	for _, ev := range events {
		var p struct {
			Path   string `json:"path"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(ev.PayloadJSON, &p); err != nil {
			return nil, fmt.Errorf("state: decode brief_rejected payload: %w", err)
		}
		out = append(out, BriefRejection{
			Ts:     time.UnixMilli(ev.WrittenAt).UTC(),
			Path:   p.Path,
			Reason: p.Reason,
		})
	}
	return out, nil
}
