package spend

import (
	"context"
	"database/sql"

	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// CallScope carries per-process metadata stamped onto every CallRecord.
// WrittenBy identifies the producer (spec §3.5 R12 single-writer).
// Empty TenantID falls back to substrate.DefaultTenantID.
type CallScope struct {
	WrittenBy string
	TenantID  string
}

// SpawnerCallback returns a per-Spawn factory ClaudeSpawner invokes
// once per request — the closure reads OperatorID/DAGID/RunID directly
// from spawner.Request (#295 supplies distinct values per spec §3.5)
// and falls back to Lane+WorkItemID when fields are empty (defense in
// depth at the writer seam). Closure runs inside ParseStream's result-
// event handler: open tx → RecordCall → Commit. A failure
// (pricing_missing, replay, marshal) returns the error to ParseStream
// which marks the chat span error+record_call_failed per the R4
// open-span-as-smoke-alarm contract.
func SpawnerCallback(db *sql.DB, opt WriteOptions, scope CallScope) func(spawner.Request) spawner.ResultEventCallback {
	tenant := scope.TenantID
	if tenant == "" {
		tenant = substrate.DefaultTenantID
	}
	return func(req spawner.Request) spawner.ResultEventCallback {
		operatorID := req.OperatorID
		if operatorID == "" {
			operatorID = req.Lane
		}
		dagID := req.DAGID
		if dagID == "" {
			dagID = req.WorkItemID
		}
		runID := req.RunID
		if runID == "" {
			runID = req.WorkItemID
		}
		return func(ctx context.Context, ev spawner.StreamResultEvent) error {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback() }()
			err = RecordCall(ctx, tx, CallRecord{
				CallID:              ev.MessageID,
				RetrySeq:            0,
				Model:               ev.Model,
				InputTokens:         ev.InputTokens,
				OutputTokens:        ev.OutputTokens,
				CacheReadTokens:     ev.CacheReadInputTokens,
				CacheCreationTokens: ev.CacheCreationInputTokens,
				OperatorID:          operatorID,
				DAGID:               dagID,
				WorkItemID:          req.WorkItemID,
				TenantID:            tenant,
				WrittenBy:           scope.WrittenBy,
				RunID:               runID,
			}, opt)
			if err != nil {
				return err
			}
			return tx.Commit()
		}
	}
}
