-- +goose Up
-- +goose StatementBegin
-- Operator-console S0: thread run_id through work_items so the runs
-- registry (0018) joins back to the FSM rows. Producer wiring lands in
-- a downstream PR; default '' means legacy items continue to work.
-- Spec ref: docs/engineer/specs/2026-06-02-operator-console-design.md §3.2.

ALTER TABLE work_items ADD COLUMN run_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_work_items_run ON work_items(run_id) WHERE run_id != '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
