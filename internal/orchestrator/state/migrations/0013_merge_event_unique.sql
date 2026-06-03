-- +goose Up
-- +goose StatementBegin
-- PHASE AUTONOMY §11 W2 c0 adversarial-review Bug-3.
--
-- Two concurrent merge.Coordinator.Reconcile sweeps both enumerated the
-- same awaiting_merge agent, both probed GitHub, and both wrote
-- merge_completed (or merge_failed) — the second write was the silent
-- duplicate that drove FSM races.
--
-- The unique partial index guards the merge terminal-completion event
-- kinds at the storage layer so the racing writer's INSERT errors with
-- a UNIQUE-constraint violation; the coordinator catches that, logs
-- "duplicate completion event suppressed", and moves on. No new schema
-- column; no reservation-claim plumbing.
--
-- Scope rationale: the constraint is intentionally narrow to the three
-- merge terminal kinds — merge_intent is excluded (LatestIntent picks
-- by id DESC; a revert + re-push to a prior SHA legitimately re-writes
-- intent), and every other event kind is unbounded.
CREATE UNIQUE INDEX idx_merge_event_unique
    ON events (agent_id, kind)
    WHERE kind IN ('merge_completed', 'merge_failed', 'merge_recovered');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Forward-only; recover by restoring from snapshot.
SELECT 1;
-- +goose StatementEnd
