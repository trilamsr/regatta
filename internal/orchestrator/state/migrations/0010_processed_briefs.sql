-- +goose Up
-- +goose StatementBegin
-- MVP-1 followup (issue #92): restart-persistent brief replay defence.
--
-- Today BriefLoader.Sync rejects a brief whose ProducedAt is <= the
-- MAX(updated_at) of existing brief-source children. That watermark is
-- derived at query time from work_items rows whose source = 'brief'. If
-- an operator deletes those rows (or wipes state.db and replays a brief
-- corpus), the watermark drops to zero and every previously-processed
-- brief becomes re-injectable.
--
-- This table moves the high-water mark into a dedicated row that is
-- independent of work_items lifecycle. BriefLoader writes one row per
-- (parent_program_id) on accept, and probes here BEFORE consulting the
-- legacy work_items watermark on every Sync. brief_hmac (the signed-
-- brief's HMAC mac field) is recorded so an exact-replay (same MAC) is
-- rejected even before timestamp comparison — defence-in-depth against
-- clock skew or a same-second re-sign.
--
-- Style mirrors 0003/0004/0006: bare CREATE so goose's version-tracking
-- stays the single source of truth — pinned by
-- TestMigrate_NoIfNotExistsInGooseManagedDDL.
CREATE TABLE processed_briefs (
    parent_program_id  TEXT    NOT NULL PRIMARY KEY,
    last_produced_at   INTEGER NOT NULL,
    brief_hmac         TEXT    NOT NULL,
    updated_at         INTEGER NOT NULL
);

CREATE INDEX idx_processed_briefs_hmac
    ON processed_briefs(brief_hmac);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only: matches 0001-0006. Dropping processed_briefs strips the
-- replay-defence watermark; operators who must roll back run DROP TABLE
-- manually after exporting the rows.
SELECT 1;
-- +goose StatementEnd
