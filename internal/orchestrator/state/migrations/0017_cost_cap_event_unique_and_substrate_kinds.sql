-- +goose Up
-- +goose StatementBegin
-- W5 cost-cap follow-up cluster (#622 + #652).
--
-- (1) Issue #652 — two-scheduler race on the legacy `events` table.
--     Two concurrent Enforcer.evaluate transitions can both write
--     `cost_cap_throttled` for the same agent_id=0 + same UTC day. The
--     partial UNIQUE index pins one throttled row per (agent_id, kind,
--     UTC day bucket). Losing writer catches the UNIQUE-constraint
--     violation in cap.go onTransition and logs the suppression.
--
--     The day-bucket expression `created_at / 86400` floors a unix-second
--     timestamp to the UTC day index. TZ-aware day anchoring lives in
--     the application (Enforcer.dayAnchor) — the constraint only needs
--     to be TIGHT enough to catch the race, not perfect across DST.
--
--     agent_id is omitted from the index columns because cap.go writes
--     these rows with agent_id=0 → NULL, and SQLite's UNIQUE treats
--     each NULL as distinct (`WHERE agent_id = NULL` never matches
--     itself). The kind filter is the cardinality fence — only
--     cost_cap_throttled rows pay the index cost.
--
-- (2) Issue #622 — substrate-event-schema cross-link (A+ deferred).
--     Widen substrate_events.kind CHECK to include cost_cap_throttled +
--     cost_cap_resumed so the W5 audit kinds have a forward-port
--     surface on the durable HMAC-chained substrate. Producer-rewire
--     stays on `events` for compat; this migration is the schema
--     parity bridge (pattern: 0016 GreenClock, 0015 PRStageTransition).
--
-- sqlite has no ALTER COLUMN; recreate-rename is the only way to widen
-- a CHECK constraint. Indexes + UNIQUE constraint preserved verbatim
-- from 0016.

-- (1) Partial UNIQUE index on legacy events for cost_cap_throttled.
CREATE UNIQUE INDEX idx_cost_cap_throttled_event_unique
    ON events (kind, (created_at / 86400))
    WHERE kind = 'cost_cap_throttled';

-- (2) Recreate substrate_events with widened kind CHECK.
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
                    'cost_cap_throttled','cost_cap_resumed')),
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
-- Forward-only: rolling back would lose cost_cap_throttled +
-- cost_cap_resumed substrate audit rows written under the wider
-- whitelist. Operators rolling back must export those rows before
-- DROP TABLE substrate_events.
SELECT 1;
-- +goose StatementEnd
