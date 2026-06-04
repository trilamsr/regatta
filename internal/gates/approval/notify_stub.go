package approval

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/trilamsr/regatta/internal/obs"
)

// KindStub is the registry key for the audit-only fallback notifier.
const KindStub = "stub"

// stubNotifier is the default audit-only adapter so approval-gated work
// items leave a slog breadcrumb even when no real channel is wired.
// Never blocks, never fails transport-wise; a real channel wanting the
// same semantics MUST re-implement Notifier rather than wrap this.
type stubNotifier struct {
	log *slog.Logger
}

// NewStubNotifier returns the audit-only notifier. A nil logger falls
// back to slog.Default.
func NewStubNotifier(log *slog.Logger) Notifier {
	if log == nil {
		log = slog.Default()
	}
	return &stubNotifier{log: log}
}

func (s *stubNotifier) Kind() string { return KindStub }

// Notify honours the four Notifier conformance invariants. Ctx + zero-
// reviewer checks run BEFORE the audit emission so a fail-closed exit
// leaves no misleading "we notified" breadcrumb.
func (s *stubNotifier) Notify(ctx context.Context, req Request) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, fmt.Errorf("approval: notify cancelled: %w", err)
	}
	if len(req.Reviewers) == 0 {
		return Receipt{}, fmt.Errorf("%w (approval_id=%q gate=%q)", ErrNoReviewers, req.ApprovalID, req.GateName)
	}
	s.log.LogAttrs(ctx, slog.LevelInfo, string(obs.EventApprovalNotifyStub),
		slog.String(string(obs.KeyApprovalID), req.ApprovalID),
		slog.String(string(obs.KeyWorkItemID), req.WorkItemID),
		slog.String(string(obs.KeyGateID), req.GateName),
		slog.Int(string(obs.KeyReviewerCount), len(req.Reviewers)),
	)
	// Copy so a caller mutating req.Reviewers after return cannot
	// retroactively change the Receipt (DeliveredTo multiset invariant).
	delivered := append([]string(nil), req.Reviewers...)
	return Receipt{DeliveredTo: delivered, Channel: KindStub}, nil
}
