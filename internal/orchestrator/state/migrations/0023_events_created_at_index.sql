-- +goose Up
-- +goose StatementBegin
-- R31-I3 finding: `events` has no index on `created_at`. Every operator
-- query (`events tail --since DUR`, `regatta status` MAX-age, dashboard
-- panels) full-table-scans + Go-filters on a fast-growing table (R30
-- heartbeat adds ~17k rows/day on top of existing spawn/merge/PR-watch
-- volume). The composite index covers the common (kind, created_at)
-- filter while the bare created_at index serves the MAX query.
CREATE INDEX idx_events_created_at ON events(created_at);
CREATE INDEX idx_events_kind_created_at ON events(kind, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_events_kind_created_at;
DROP INDEX idx_events_created_at;
-- +goose StatementEnd
