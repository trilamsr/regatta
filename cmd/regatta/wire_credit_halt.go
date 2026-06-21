package main

import (
	"context"
	"log/slog"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// recordCreditExhausted emits the LOUD operator signal (ERROR slog +
// durable audit row) for a provider_credit_exhausted agent exit and
// invokes halt to stop further dispatch. Credit exhaustion is an
// account-level terminal fault no retry can clear, so the orchestrator
// must surface it and stop rather than silently burn more invocations
// against a dead account (MAY-78). halt is nil-safe.
func recordCreditExhausted(ctx context.Context, db *state.DB, logger *slog.Logger, agentID int64, workItemID string, halt func()) {
	_ = db.RecordEvent(ctx, agentID, string(obs.EventCreditExhausted),
		`{"reason":"provider_credit_exhausted"}`)
	logger.Error(string(obs.EventCreditExhausted),
		string(obs.KeyAgentID), agentID,
		string(obs.KeyWorkItemID), workItemID,
		string(obs.KeyExitReason), string(spawner.ExitReasonProviderCreditExhausted),
	)
	if halt != nil {
		halt()
	}
}
