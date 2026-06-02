-- +goose Up
-- +goose StatementBegin
-- Issue #83: partial composite index on the planned-rows hot path.
-- Scheduler.Tick -> ListSpawnable (work_items_query.go) filters
-- status='planned' on every tick. The existing idx_work_items_status
-- (migration 0002) is a full-table index on the status column, so
-- every write to ANY row (including the long tail of archived/merged
-- rows that dominate a months-old DB) pays the index-update tax.
--
-- This partial index covers only status='planned' rows, the exact
-- subset ListSpawnable consults. Two wins:
--   1. write tax shrinks to O(active-planned-rows), not O(all-rows).
--      Once an item flips merged/archived, it leaves this index.
--   2. lane + last_seen_at columns are stored alongside, letting
--      future lane-affinity / age-ordered scheduler iterations
--      SEARCH the same index without a second lookup.
--
-- Bare CREATE — pinned by TestMigrate_NoIfNotExistsInGooseManagedDDL
-- (see 0004/0005/0006); goose's version tracker is the single source
-- of truth for whether this index already exists.
--
-- The existing idx_work_items_status stays. ListByParent (and any
-- future status-only scan over the full set) still relies on it; the
-- partial index here is additive, not a replacement.

CREATE INDEX idx_work_items_spawnable
    ON work_items(status, lane, last_seen_at)
    WHERE status = 'planned';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only; matches 0001-0006. Dropping the index on rollback
-- only loses a perf optimisation, so a manual DROP INDEX is fine
-- if an operator ever needs it.
SELECT 1;
-- +goose StatementEnd
