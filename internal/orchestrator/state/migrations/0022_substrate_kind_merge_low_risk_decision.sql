-- +goose Up
-- +goose StatementBegin
-- MAY-270: extend substrate_events.kind CHECK to include
-- 'merge_low_risk_decision'. The low-risk auto-merge gate emits one row
-- per classify decision (hold, eligible, OR fetch-failure) carrying
-- {pr, eligible, reason, loc_delta, soak_remaining_sec} so the audit
-- trail reconstructs the verdict without re-fetching from GitHub.
--
-- sqlite CHECK widening requires a full table-rewrite — mirror the
-- pattern from 0021 (tool_call), 0017 (cost-cap kinds), 0016 (green-
-- clock kinds), 0015 (pr_stage_transition). Indexes, UNIQUE constraint,
-- and column-set preserved verbatim from 0021's output schema.

CREATE TABLE substrate_events_new (
    id              TEXT    NOT NULL PRIMARY KEY,
    run_id          TEXT    NOT NULL,
    work_item_id    TEXT,
    tenant_id       TEXT    NOT NULL,
    trace_id        TEXT    NOT NULL DEFAULT '',
    span_id         TEXT    NOT NULL DEFAULT '',
    kind            TEXT    NOT NULL,
    key             TEXT    NOT NULL DEFAULT '',
    payload_json    TEXT    NOT NULL DEFAULT '{}',
    blob_digest     TEXT    NOT NULL DEFAULT '',
    supersedes      TEXT,
    written_by      TEXT    NOT NULL
                    CHECK (length(written_by) > 0
                           AND length(written_by) <= 128
                           AND written_by NOT GLOB '*[^a-zA-Z0-9_:.-]*'),
    written_at      INTEGER NOT NULL,
    schema_version  INTEGER NOT NULL DEFAULT 1,
    nonce           TEXT    NOT NULL,
    sig_alg         TEXT    NOT NULL,
    sig_key_id      TEXT    NOT NULL,
    sig_mac         TEXT    NOT NULL,
    CHECK (kind IN ('node_output','fact','approval_event','token_spend',
                    'budget_reconciled','gate_verdict','heartbeat',
                    'brief_rejected','pr_stage_transition',
                    'manual_merge','operator_intervention',
                    'cost_cap_throttled','cost_cap_resumed',
                    'tool_call','merge_low_risk_decision')),
    CHECK (length(payload_json) <= 1024),
    CHECK (trace_id = '' OR length(trace_id) = 32),
    CHECK (span_id  = '' OR length(span_id)  = 16),
    FOREIGN KEY (supersedes) REFERENCES substrate_events_new(id)
);

INSERT INTO substrate_events_new
    (id, run_id, work_item_id, tenant_id, trace_id, span_id,
     kind, key, payload_json, blob_digest, supersedes,
     written_by, written_at, schema_version, nonce,
     sig_alg, sig_key_id, sig_mac)
SELECT id, run_id, work_item_id, tenant_id, trace_id, span_id,
       kind, key, payload_json, blob_digest, supersedes,
       written_by, written_at, schema_version, nonce,
       sig_alg, sig_key_id, sig_mac
FROM substrate_events;

DROP TABLE substrate_events;
ALTER TABLE substrate_events_new RENAME TO substrate_events;

CREATE INDEX idx_substrate_events_kind
    ON substrate_events(run_id, kind, key, written_at DESC);
CREATE INDEX idx_substrate_events_wi
    ON substrate_events(work_item_id, kind, written_at DESC)
    WHERE work_item_id IS NOT NULL;
CREATE INDEX idx_substrate_events_tenant
    ON substrate_events(tenant_id, kind, written_at DESC);
CREATE INDEX idx_substrate_events_supersedes
    ON substrate_events(supersedes)
    WHERE supersedes IS NOT NULL;
CREATE INDEX idx_substrate_events_trace
    ON substrate_events(trace_id)
    WHERE trace_id != '';

CREATE UNIQUE INDEX uq_substrate_events_nonce
    ON substrate_events(run_id, written_by, nonce);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only: rolling back would drop any merge_low_risk_decision
-- audit rows.
SELECT 1;
-- +goose StatementEnd
