-- +goose Up
-- +goose StatementBegin
-- S3-T2 Phase B — divergence audit table for substrate shadow-write.
-- Spec: docs/engineer/specs/2026-06-02-s3-t2-substrate-cutover.md §3.6.
--
-- Migration number: spec called for 0007; this PR pins 0009 because
-- #352 took 0007 and #362 took 0008 in the same session. Per
-- feedback_migration_number_lock the dispatcher pins N; implementer
-- subagents do not pick.
--
-- One row per detected divergence between the legacy approval_events
-- write and its substrate-events shadow mirror. Wave-1 only writes
-- detector='layer1_write'; Layer 2/3 detectors land in follow-ups.

CREATE TABLE substrate_divergence_audit (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    detected_at       INTEGER NOT NULL,
    detector          TEXT    NOT NULL CHECK (detector IN ('layer1_write','layer2_test','layer3_cron')),
    store             TEXT    NOT NULL CHECK (store IN ('approvals','token_spend')),
    primary_key       TEXT    NOT NULL,
    legacy_summary    TEXT    NOT NULL DEFAULT '',
    substrate_summary TEXT    NOT NULL DEFAULT '',
    diff_summary      TEXT    NOT NULL,
    repaired_at       INTEGER,
    repair_action     TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX idx_substrate_divergence_audit_unrepaired
    ON substrate_divergence_audit(detected_at)
    WHERE repaired_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only: matches 0001-0008. Dropping the audit table on rollback
-- would lose divergence history; operators who truly need to reset can
-- DROP TABLE substrate_divergence_audit manually after exporting rows.
SELECT 1;
-- +goose StatementEnd
