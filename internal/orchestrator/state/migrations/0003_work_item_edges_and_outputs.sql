-- +goose Up
-- +goose StatementBegin
-- Outcome-conditional DAG (MVP-2 W1): edges become first-class with
-- optional CEL predicates over upstream output JSON. The journal
-- (work_item_outputs) gives predicates a deterministic, content-
-- addressed input. See docs/superpowers/specs/2026-05-31-mvp-2-
-- conditional-dag-design.md §3.
--
-- Why two tables (not JSON cols on work_items): scheduler tick filters
-- on (from_id, fired) every iteration — indexable in SQL, O(1)-per-edge
-- versus O(rows) scanning a JSON column. Same reasoning as MVP-1
-- chose a relational depends_on_features representation.
--
-- UNIQUE(content_sha) is intentionally OMITTED on work_item_outputs:
-- spec §7 risk 11 — two work_items can legally produce byte-identical
-- payloads (especially under the stub spawner). Idempotency is anchored
-- by UNIQUE(work_item_id, attempt_no) instead.

CREATE TABLE IF NOT EXISTS work_item_outputs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    work_item_id    TEXT    NOT NULL REFERENCES work_items(id),
    attempt_no      INTEGER NOT NULL,
    content_sha     TEXT    NOT NULL,
    output_json     TEXT    NOT NULL,
    produced_at     INTEGER NOT NULL,
    UNIQUE(work_item_id, attempt_no)
);
CREATE INDEX IF NOT EXISTS idx_work_item_outputs_wi
    ON work_item_outputs(work_item_id);
CREATE INDEX IF NOT EXISTS idx_work_item_outputs_sha
    ON work_item_outputs(content_sha);

CREATE TABLE IF NOT EXISTS work_item_edges (
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
CREATE INDEX IF NOT EXISTS idx_work_item_edges_from
    ON work_item_edges(from_id, fired);
CREATE INDEX IF NOT EXISTS idx_work_item_edges_to
    ON work_item_edges(to_id, fired);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
