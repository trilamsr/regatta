-- +goose Up
-- +goose StatementBegin
-- R31-I3 + schema audit (2026-06-22) — three index gaps on prod-hot read paths.
--
-- 1. events.created_at: every operator query (`events tail --since DUR`,
-- `regatta status` MAX-age, dashboard panels) full-table-scanned then
-- Go-filtered on a fast-growing table (R30 heartbeat adds ~17k rows/day
-- on top of existing spawn/merge/PR-watch volume). The composite
-- (kind, created_at) covers the common filter; the bare created_at
-- serves the MAX query.
--
-- 2. agents.work_item_id: UpsertPending + GetAgentByWorkItemID both run
-- `SELECT … FROM agents WHERE work_item_id = ?` on the spawn + recovery
-- hot path. No index existed.
--
-- 3. substrate_events.written_at: cost/spend reader + audit pulls run
-- `WHERE written_at >= ?` time-window queries; no time-axis index
-- existed (the existing substrate indexes cover kind/wi/tenant/trace
-- but not the time filter).
CREATE INDEX idx_events_created_at ON events(created_at);
CREATE INDEX idx_events_kind_created_at ON events(kind, created_at);
CREATE INDEX idx_agents_work_item_id ON agents(work_item_id);
CREATE INDEX idx_substrate_events_written_at ON substrate_events(written_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_substrate_events_written_at;
DROP INDEX idx_agents_work_item_id;
DROP INDEX idx_events_kind_created_at;
DROP INDEX idx_events_created_at;
-- +goose StatementEnd
