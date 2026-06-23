-- +goose Up
-- +goose StatementBegin
-- R-MEGA-2 P4: persist recheckBackoff state so a daemon restart does not
-- trigger a recheck storm — pre-fix, an in-memory backoff lost on crash
-- let every previously-suppressed work_item re-enter the fetch loop on
-- the first tick after boot. attempt_count mirrors recheckEntry.failures;
-- retry_at is wall-clock UTC seconds when the suppression window expires.
CREATE TABLE scheduler_backoff (
    work_item_id  TEXT    NOT NULL PRIMARY KEY,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    retry_at      INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);
CREATE INDEX idx_scheduler_backoff_retry_at ON scheduler_backoff(retry_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_scheduler_backoff_retry_at;
DROP TABLE scheduler_backoff;
-- +goose StatementEnd
