-- +goose Up
-- +goose StatementBegin
-- Regatta orchestrator state schema, version 1.
--
-- Tables follow the agent state machine in docs/design.md §State,
-- persistence, recovery. Migrations are forward-only: append a new
-- section, never edit a shipped block. goose_db_version (managed by
-- pressly/goose) is the authoritative version table.

CREATE TABLE IF NOT EXISTS agents (
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

CREATE INDEX IF NOT EXISTS idx_agents_state ON agents(state);
CREATE INDEX IF NOT EXISTS idx_agents_lane  ON agents(lane);

CREATE TABLE IF NOT EXISTS locks (
    name         TEXT    NOT NULL PRIMARY KEY,
    agent_id     INTEGER NOT NULL REFERENCES agents(id),
    acquired_at  INTEGER NOT NULL,
    heartbeat_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_locks_agent ON locks(agent_id);

CREATE TABLE IF NOT EXISTS events (
    id           INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    agent_id     INTEGER REFERENCES agents(id),
    kind         TEXT    NOT NULL,
    payload_json TEXT    NOT NULL DEFAULT '{}',
    created_at   INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_agent ON events(agent_id);
CREATE INDEX IF NOT EXISTS idx_events_kind  ON events(kind);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only; down migrations are intentionally empty. Operators
-- recover by restoring from snapshot.
SELECT 1;
-- +goose StatementEnd
