package approval

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/trilamsr/regatta/internal/obs"
)

// KindStub is the registry key for the audit-only fallback notifier.
// Exported so config loaders and gate wiring can register/lookup
// without re-hardcoding the string.
const KindStub = "stub"

// stubNotifier is the default audit-only adapter. It ships so that
// approval-gated work items still leave a structured-log breadcrumb
// even when no real channel is wired — the auditor reconstructs
// "we paused here and expected a human" from the slog stream alone.
// stubNotifier never blocks and never fails (transport-wise); a real
// channel that wants the same semantics MUST re-implement Notifier
// rather than wrap this.
type stubNotifier struct {
	log *slog.Logger
}

// NewStubNotifier returns the audit-only notifier bound to log. A nil
// logger falls back to slog.Default so callers can pass slog.New(h)
// for tests without a separate constructor.
func NewStubNotifier(log *slog.Logger) Notifier {
	if log == nil {
		log = slog.Default()
	}
	return &stubNotifier{log: log}
}

func (s *stubNotifier) Kind() string { return KindStub }

// Notify honours the four Notifier conformance invariants (see
// Notifier godoc). Ctx check + zero-reviewer check run BEFORE the
// audit emission so a fail-closed exit leaves no misleading
// "we notified" breadcrumb.
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
	// Copy the reviewer slice so a caller mutating req.Reviewers after
	// return cannot retroactively change the Receipt — same multiset
	// invariant on DeliveredTo holds across the full Receipt lifetime.
	delivered := append([]string(nil), req.Reviewers...)
	return Receipt{DeliveredTo: delivered, Channel: KindStub}, nil
}
