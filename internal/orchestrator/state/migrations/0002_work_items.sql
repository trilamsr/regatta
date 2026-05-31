-- +goose Up
-- +goose StatementBegin
-- Universal queue (RFC-0001 §3): single source of truth for spawnable
-- work. AdapterSync (source=adapter) and BriefLoader (source=brief)
-- both upsert here; Scheduler.ListSpawnable joins against agents.

CREATE TABLE work_items (
    id                   TEXT    NOT NULL PRIMARY KEY,
    kind                 TEXT    NOT NULL,
    title                TEXT    NOT NULL,
    lane                 TEXT    NOT NULL,
    status               TEXT    NOT NULL,
    parent_program_id    TEXT,
    depends_on_features  TEXT    NOT NULL DEFAULT '[]',
    acceptance_json      TEXT    NOT NULL DEFAULT '[]',
    source               TEXT    NOT NULL,
    last_seen_at         INTEGER NOT NULL,
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);

CREATE INDEX idx_work_items_status ON work_items(status);
CREATE INDEX idx_work_items_parent ON work_items(parent_program_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only; down migrations are intentionally empty.
SELECT 1;
-- +goose StatementEnd
