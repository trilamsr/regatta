-- +goose Up
-- +goose StatementBegin
-- MVP-3 W6 T3 (spec §3.5): persist W3C trace-context trace_id alongside
-- every work_item + approval_event so post-hoc forensics joins a known
-- backend trace back to the row that drove it.
--
-- 32-hex lowercase per the OTel spec; empty string when no active span
-- (legacy / dev-without-otel paths). NOT NULL DEFAULT '' matches the
-- V11 reviewer-id charset convention (defence-in-depth: a future CHECK
-- can pin 32-hex-or-empty without a nullable branch). Partial indexes
-- skip the empty rows so legacy data pays no index cost.
--
-- Style mirrors 0003/0004: bare CREATE INDEX so goose's version-
-- tracking stays the single source of truth — pinned by
-- TestMigrate_NoIfNotExistsInGooseManagedDDL (existence guards on
-- DDL defeat the version log).
ALTER TABLE work_items      ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';
ALTER TABLE approval_events ADD COLUMN trace_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_work_items_trace
    ON work_items(trace_id) WHERE trace_id != '';
CREATE INDEX idx_approval_events_trace
    ON approval_events(trace_id) WHERE trace_id != '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only: matches 0001-0004. SQLite ALTER TABLE has no DROP
-- COLUMN before 3.35; even when supported, dropping a populated
-- trace_id loses the forensic-join column unrecoverably. Operators
-- who truly need to roll back run sqlite_rebuild manually after
-- backing up the rows.
SELECT 1;
-- +goose StatementEnd
