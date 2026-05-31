-- +goose Up
-- +goose StatementBegin
-- Outcome-conditional DAG (MVP-2 W1): first-class edges with optional
-- CEL predicates over journaled upstream output JSON. Two tables (not
-- JSON cols on work_items) so scheduler tick filters on (from_id,
-- fired) hit an index. UNIQUE(content_sha) is intentionally absent —
-- distinct work_items may legally produce identical payloads;
-- idempotency anchors on UNIQUE(work_item_id, attempt_no).

CREATE TABLE work_item_outputs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    work_item_id    TEXT    NOT NULL REFERENCES work_items(id),
    attempt_no      INTEGER NOT NULL,
    content_sha     TEXT    NOT NULL,
    output_json     TEXT    NOT NULL,
    produced_at     INTEGER NOT NULL,
    UNIQUE(work_item_id, attempt_no)
);
CREATE INDEX idx_work_item_outputs_wi
    ON work_item_outputs(work_item_id);
CREATE INDEX idx_work_item_outputs_sha
    ON work_item_outputs(content_sha);

CREATE TABLE work_item_edges (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    program_id      TEXT    NOT NULL,
    from_id         TEXT    NOT NULL REFERENCES work_items(id),
    to_id           TEXT    NOT NULL REFERENCES work_items(id),
    predicate_cel   TEXT    NOT NULL DEFAULT '',
    is_default      INTEGER NOT NULL DEFAULT 0,
    on_skip         TEXT    NOT NULL DEFAULT 'cascade',
    fired           TEXT    NOT NULL DEFAULT 'pending',
    fired_against   TEXT    NOT NULL DEFAULT '',
    evaluated_at    INTEGER,
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    UNIQUE(program_id, from_id, to_id)
);
CREATE INDEX idx_work_item_edges_from
    ON work_item_edges(from_id, fired);
CREATE INDEX idx_work_item_edges_to
    ON work_item_edges(to_id, fired);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
