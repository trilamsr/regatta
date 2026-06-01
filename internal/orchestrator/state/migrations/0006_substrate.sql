-- +goose Up
-- +goose StatementBegin
-- MVP-3 Wave 1 — substrate event log (Phase A: dark, no readers).
-- Source-of-truth spec: docs/engineer/specs/2026-06-01-unified-substrate-design.md §2.1.
--
-- Append-only signed log that will collapse approval_events,
-- work_item_outputs, work_item_edges (event rows), and per-agent events
-- into one primitive. Wave 1 ships the table empty; Phase B (Wave 2)
-- wires shadow-writes; Phase C (Wave 2+) flips reads. Phase D (Wave 3)
-- drops the legacy tables once cutover is stable.
--
-- Spec deviation (documented): the spec calls this migration 0006 and
-- assumes W6 T3 ships 0005_trace_id_columns first. As of dispatch the
-- W6 T3 PR (#209) is open but unmerged; main is at version 4. Numbering
-- this as 0006 with a 0005 hole would break goose's monotonic-apply
-- semantics (default forbids missing versions). Numbered 0005 here;
-- when #209 lands first, this PR rebases to 0006 + bumps version 5 → 6.
--
-- Style mirrors 0003/0004: bare CREATE so goose's version-tracking is
-- the single source of truth (pinned by TestMigrate_NoIfNotExistsInGooseManagedDDL).

CREATE TABLE substrate_events (
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
    supersedes      TEXT,                                      -- nullable: '' or NOT NULL would force FK to find a row for the empty string (sqlite FK does not skip ''); NULL means "no parent" and skips the FK check per sqlite semantics
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
                    'budget_reconciled','gate_verdict','heartbeat')),
    CHECK (length(payload_json) <= 1024),
    CHECK (trace_id = '' OR length(trace_id) = 32),
    CHECK (span_id  = '' OR length(span_id)  = 16),
    FOREIGN KEY (supersedes) REFERENCES substrate_events(id)
);

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
-- Forward-only: matches 0001-0004. Dropping substrate_events on a
-- production rollback is destructive (audit history loss); operators
-- who truly need to roll back run DROP TABLE substrate_events manually
-- after exporting the rows.
SELECT 1;
-- +goose StatementEnd
