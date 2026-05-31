-- +goose Up
-- +goose StatementBegin
-- Regatta orchestrator state schema, v1.
-- See docs/design.md §State, persistence, recovery. Forward-only:
-- append a new migration, never edit a shipped block.

CREATE TABLE agents (
    id              INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    work_item_id    TEXT    NOT NULL UNIQUE,
    lane            TEXT    NOT NULL,
    state           TEXT    NOT NULL,
    pid             INTEGER NOT NULL DEFAULT 0,
    session_id      TEXT    NOT NULL DEFAULT '',
    pr_sha          TEXT    NOT NULL DEFAULT '',
    rejection_count INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);

CREATE INDEX idx_agents_state ON agents(state);
CREATE INDEX idx_agents_lane  ON agents(lane);

CREATE TABLE locks (
    name         TEXT    NOT NULL PRIMARY KEY,
    agent_id     INTEGER NOT NULL REFERENCES agents(id),
    acquired_at  INTEGER NOT NULL,
    heartbeat_at INTEGER NOT NULL
);

CREATE INDEX idx_locks_agent ON locks(agent_id);

CREATE TABLE events (
    id           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    agent_id     INTEGER REFERENCES agents(id),
    kind         TEXT    NOT NULL,
    payload_json TEXT    NOT NULL DEFAULT '{}',
    created_at   INTEGER NOT NULL
);

CREATE INDEX idx_events_agent ON events(agent_id);
CREATE INDEX idx_events_kind  ON events(kind);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only; recover by restoring from snapshot.
SELECT 1;
-- +goose StatementEnd
