-- +goose Up
-- +goose StatementBegin
-- HITL approval gates (MVP-2 W1): pause execution at a human-decision
-- point, snapshot the reviewer set at request-time, resume via signed
-- callback token, audit via append-only event log. See
-- docs/superpowers/specs/2026-05-31-mvp-approval-gates.md §3 + §5.
--
-- Style mirrors 0003: bare CREATE so goose's version-tracking is
-- the single source of truth (see TestMigrate_NoIfNotExistsInGooseManagedDDL).

CREATE TABLE approvals (
    id                          TEXT    NOT NULL PRIMARY KEY,
    work_item_id                TEXT    NOT NULL REFERENCES work_items(id),
    gate_name                   TEXT    NOT NULL,
    requested_at                INTEGER NOT NULL,
    requested_by                TEXT    NOT NULL,
    reviewer_set_snapshot_json  TEXT    NOT NULL,
    quorum                      INTEGER NOT NULL DEFAULT 1,
    status                      TEXT    NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending','approved','rejected','timed_out')),
    decided_at                  INTEGER,
    decided_by                  TEXT    NOT NULL DEFAULT '[]',
    callback_token_hmac_sig     TEXT    NOT NULL DEFAULT '',
    timeout_at                  INTEGER NOT NULL,
    on_timeout                  TEXT    NOT NULL DEFAULT 'fail',
    escalation_chain_json       TEXT    NOT NULL DEFAULT '[]',
    created_at                  INTEGER NOT NULL,
    updated_at                  INTEGER NOT NULL,
    UNIQUE(work_item_id, gate_name)
);
CREATE INDEX idx_approvals_status
    ON approvals(status);
CREATE INDEX idx_approvals_timeout
    ON approvals(timeout_at, status);

-- actor CHECK: alnum + _:.- only; caps at 128. The negated form
-- `NOT GLOB '*[^...]*'` is load-bearing — sqlite GLOB anchors the
-- whole-string match, and `[abc]*` after a one-char class matches
-- any trailing chars (the * is zero-or-more of ANY char in GLOB,
-- not a repetition of the preceding class as in regex). The negated
-- form asserts "no forbidden char anywhere in the string", which is
-- what we actually want. Tested by TestApproval_ActorCheckRejectsInvalid.
CREATE TABLE approval_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    approval_id     TEXT    NOT NULL REFERENCES approvals(id),
    ts              INTEGER NOT NULL,
    kind            TEXT    NOT NULL,
    actor           TEXT    NOT NULL
                    CHECK (length(actor) <= 128
                           AND actor NOT GLOB '*[^a-zA-Z0-9_:.-]*'
                           AND length(actor) > 0),
    payload_json    TEXT    NOT NULL DEFAULT '{}',
    token_jti       TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX idx_approval_events_approval
    ON approval_events(approval_id, ts);
CREATE UNIQUE INDEX uq_approval_events_token_consume
    ON approval_events(approval_id, kind, token_jti)
    WHERE kind = 'token_consumed';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only migration: matches 0003's no-op Down. Rolling back the
-- binary retains the approvals/approval_events tables and their data;
-- the no-op SELECT lets goose record the down-step without destroying
-- the human-decision audit log on a binary downgrade. Operators who
-- truly need to drop the tables run DROP TABLE manually after backing
-- up approval_events (audit-critical per spec §3 + §5).
SELECT 1;
-- +goose StatementEnd
