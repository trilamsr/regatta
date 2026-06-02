package spend

import (
	"context"
	"database/sql"

	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// CallScope carries the per-process metadata SpawnerCallback stamps onto
// every CallRecord. WrittenBy identifies the producer (one writer per
// process — spec §3.5 R12 single-writer invariant). TenantID falls back
// to substrate.DefaultTenantID when empty so single-tenant operators
// stay zero-config.
type CallScope struct {
	WrittenBy string
	TenantID  string
}

// SpawnerCallback returns a per-Spawn factory the ClaudeSpawner invokes
// once per request — the closure receives the spawner.Request so it can
// stamp operator_id/dag_id/work_item_id derived from the request before
// RecordCall fires. Production wiring lands in cmd/regatta/serve.go;
// the closure runs inside ParseStream's result-event handler, opens a
// substrate tx, calls RecordCall, and commits on success.
//
// Rollback-on-error: the deferred Rollback is a no-op after a successful
// Commit, so the happy path commits exactly one substrate row. A
// RecordCall failure (pricing_missing, replay, marshal) returns the
// error to ParseStream which marks the chat span error+record_call_failed
// per the R4 open-span-as-smoke-alarm contract.
func SpawnerCallback(db *sql.DB, opt WriteOptions, scope CallScope) func(spawner.Request) spawner.ResultEventCallback {
	tenant := scope.TenantID
	if tenant == "" {
		tenant = substrate.DefaultTenantID
	}
	return func(req spawner.Request) spawner.ResultEventCallback {
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
				OperatorID:          req.Lane,
				DAGID:               req.WorkItemID,
				WorkItemID:          req.WorkItemID,
				TenantID:            tenant,
				WrittenBy:           scope.WrittenBy,
				RunID:               req.WorkItemID,
			}, opt)
			if err != nil {
				return err
			}
			return tx.Commit()
		}
	}
}
