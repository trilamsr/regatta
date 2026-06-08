-- +goose Up
-- +goose StatementBegin
-- Operator-console S0: thread run_id through approval_events so the
-- S3 postmortem view (spec §3.3) can join from runs back to the
-- approval audit log without re-walking work_items.
-- Spec ref: docs/engineer/specs/2026-06-02-operator-console-design.md §3.2.

ALTER TABLE approval_events ADD COLUMN run_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_approval_events_run ON approval_events(run_id) WHERE run_id != '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
