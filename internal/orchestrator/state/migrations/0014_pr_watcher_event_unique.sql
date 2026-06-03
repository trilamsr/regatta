-- +goose Up
-- +goose StatementBegin
-- Issue #521 — pr-watcher multi-host race.
--
-- Two operators running `regatta serve` against the same GitHub repo
-- both poll `gh pr list --head regatta/agent-N`, both observe the same
-- head SHA, and both attempt to record `agent_pr_opened`. Without a
-- storage-layer guard the second writer silently appends a duplicate
-- audit row; downstream consumers (gate runner #33) double-schedule.
--
-- Mirroring 0013_merge_event_unique.sql, the partial UNIQUE index
-- pins one `agent_pr_opened` row per agent. The losing writer catches
-- the UNIQUE-constraint violation, logs `pr_watcher.duplicate_pr_opened_suppressed`,
-- and continues — the FSM-side transition is already idempotent via
-- TransitionAgentTx's row lock.
--
-- Scope rationale: the constraint guards only the *terminal* "opened"
-- audit kind. `agent_pr_head_changed` is intentionally unconstrained
-- because the same agent legitimately re-emits one event per SHA push.
CREATE UNIQUE INDEX idx_pr_opened_event_unique
    ON events (agent_id, kind)
    WHERE kind = 'agent_pr_opened';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only; recover by restoring from snapshot.
SELECT 1;
-- +goose StatementEnd
