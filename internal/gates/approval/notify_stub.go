package approval

import (
	"context"
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

func (s *stubNotifier) Notify(ctx context.Context, req Request) (Receipt, error) {
	s.log.LogAttrs(ctx, slog.LevelInfo, string(obs.EventApprovalNotifyStub),
		slog.String(string(obs.KeyApprovalID), req.ApprovalID),
		slog.String(string(obs.KeyWorkItemID), req.WorkItemID),
		slog.String(string(obs.KeyGateID), req.GateName),
		slog.Int(string(obs.KeyReviewerCount), len(req.Reviewers)),
	)
	return Receipt{DeliveredTo: req.Reviewers, Channel: KindStub}, nil
}
