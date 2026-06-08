-- +goose Up
-- +goose StatementBegin
-- Operator-console S0: runs registry. One row per regatta dispatch.
-- causal_hash = sha256(canon({spec, seed, versions, model_hash,
-- prompt_template_hash, tool_impl_hash})). rerun_of links rerun-from-
-- hash child runs back to the parent. declared_effect_class is the
-- policy envelope copied from agent-config at dispatch time; surprise
-- detector (spec §3.7) compares against observed_effect at tool-call.
-- Spec ref: docs/engineer/specs/2026-06-02-operator-console-design.md §3.2.

CREATE TABLE runs (
    id                       TEXT    NOT NULL PRIMARY KEY,
    started_at               INTEGER NOT NULL,
    finished_at              INTEGER,
    status                   TEXT    NOT NULL DEFAULT '',
    spec_hash                TEXT    NOT NULL DEFAULT '',
    model_hash               TEXT    NOT NULL DEFAULT '',
    prompt_template_hash     TEXT    NOT NULL DEFAULT '',
    tool_impl_hash           TEXT    NOT NULL DEFAULT '',
    seed                     TEXT    NOT NULL DEFAULT '',
    versions_json            TEXT    NOT NULL DEFAULT '{}',
    causal_hash              TEXT    NOT NULL DEFAULT '',
    rerun_of                 TEXT,
    trace_id                 TEXT    NOT NULL DEFAULT '',
    declared_effect_class    TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX idx_runs_started ON runs(started_at DESC);
CREATE INDEX idx_runs_causal_hash ON runs(causal_hash) WHERE causal_hash != '';
CREATE INDEX idx_runs_rerun_of ON runs(rerun_of) WHERE rerun_of IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only: matches 0001-0017 convention. Rolling back loses the
-- registry of historical agent dispatches (rerun_of audit trail).
SELECT 1;
-- +goose StatementEnd
