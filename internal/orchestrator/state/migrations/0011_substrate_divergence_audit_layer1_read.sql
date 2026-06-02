-- +goose Up
-- +goose StatementBegin
-- S3-T2 Phase C — extend substrate_divergence_audit.detector CHECK to
-- include 'layer1_read' so the read-path fallback can record a row
-- when substrate returns empty but legacy has data.
--
-- Migration number 0011 PINNED per feedback_migration_number_lock
-- (0010 reserved for #362 brief-replay; cross-PR collision avoidance).
-- sqlite has no ALTER COLUMN; the canonical recreate-rename idiom is
-- the only way to widen a CHECK constraint.
CREATE TABLE substrate_divergence_audit_new (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    detected_at       INTEGER NOT NULL,
    detector          TEXT    NOT NULL CHECK (detector IN ('layer1_write','layer1_read','layer2_test','layer3_cron')),
    store             TEXT    NOT NULL CHECK (store IN ('approvals','token_spend')),
    primary_key       TEXT    NOT NULL,
    legacy_summary    TEXT    NOT NULL DEFAULT '',
    substrate_summary TEXT    NOT NULL DEFAULT '',
    diff_summary      TEXT    NOT NULL,
    repaired_at       INTEGER,
    repair_action     TEXT    NOT NULL DEFAULT ''
);

INSERT INTO substrate_divergence_audit_new
    (id, detected_at, detector, store, primary_key,
     legacy_summary, substrate_summary, diff_summary,
     repaired_at, repair_action)
SELECT id, detected_at, detector, store, primary_key,
       legacy_summary, substrate_summary, diff_summary,
       repaired_at, repair_action
FROM substrate_divergence_audit;

DROP TABLE substrate_divergence_audit;
ALTER TABLE substrate_divergence_audit_new RENAME TO substrate_divergence_audit;

CREATE INDEX idx_substrate_divergence_audit_unrepaired
    ON substrate_divergence_audit(detected_at)
    WHERE repaired_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only: dropping rows whose detector='layer1_read' would lose
-- audit history. Operators who truly need to roll back may DROP TABLE
-- substrate_divergence_audit after exporting the layer1_read rows.
SELECT 1;
-- +goose StatementEnd
