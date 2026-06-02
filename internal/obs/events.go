// Package obs defines the canonical structured-event vocabulary for
// regatta runtime components. Every state transition or notable
// lifecycle moment emitted to slog by Orchestrator, Scheduler,
// Spawner, Reaper, Gates, AdapterSync, and BriefLoader MUST go through
// the EventName + AttrKey constants below.
//
// Single source of truth: dashboards, log shippers, and operator
// runbooks grep on these strings. Reshuffles need a code review that
// updates AllEventNames / AllAttrKeys and re-runs the schema tests in
// events_test.go.
package obs

// EventName tags a structured-log record with its canonical taxonomy
// slot. Producers populate slog's `msg` argument from this value and
// also set the KeyEvent attribute to the same string so both grep and
// jq workflows resolve cleanly (spec §3.3).
type EventName string

// AttrKey closes the top-level attribute namespace. Component-
// specific keys land inside `attrs` instead of polluting the root
// JSON record (spec §3.3).
type AttrKey string

// Event vocabulary. Names use dotted component.event format so
// operators can filter by component prefix without enumerating every
// event. Existing string literals at scheduler/edge_eval, spawner,
// brief loader, and adaptersync callsites adopt these constants in
// the follow-up tasks (A–E, H of the obs-101 spec).
const (
	// Orchestrator — emitted unconditionally on every Tick() exit
	// (spec §3.3 invariant).
	EventTickStarted   EventName = "tick.started"
	EventTickCompleted EventName = "tick.completed"

	// Scheduler edge evaluation. Names match the literals already
	// present in internal/orchestrator/scheduler/scheduler.go so
	// Task B is a mechanical literal→constant swap.
	EventEdgeFired                   EventName = "edge.fired"
	EventEdgeSkipped                 EventName = "edge.skipped"
	EventEdgeDefaultFallback         EventName = "edge.default_fallback"
	EventEdgeEvalError               EventName = "edge.eval_error"
	EventEdgeMarkFailed              EventName = "edge.mark_failed"
	EventEdgeEvalSkippedNoJournal    EventName = "edge.eval_skipped_no_journal"
	EventEdgeJournalLoadFailed       EventName = "edge.journal_load_failed"
	EventEdgeMultipleDefaultsPerFrom EventName = "edge.multiple_defaults_per_from"

	// Scheduler materialization (new event per spec §5.2 — storage
	// write failure after an edge fires).
	EventSchedulerMaterializeFailure EventName = "scheduler.materialize_failure"

	// Spawner lifecycle (spec §5.3).
	EventSpawnStarted   EventName = "spawn.started"
	EventSpawnCompleted EventName = "spawn.completed"
	EventSpawnFailed    EventName = "spawn.failed"

	// Orphan-journal reconciler (#99). spawn.reconciled fires once
	// per converged work_item; spawn.reconcile_failed surfaces a
	// per-row error so the sweep can keep going on one bad id.
	EventSpawnReconciled      EventName = "spawn.reconciled"
	EventSpawnReconcileFailed EventName = "spawn.reconcile_failed"

	// Reaper lifecycle (spec §5.4).
	EventReapCandidateDetected EventName = "reap.candidate_detected"
	EventReapKilled            EventName = "reap.killed"
	EventReapSkipped           EventName = "reap.skipped"

	// Gates — single verdict event covers l0, security, and HMAC
	// seam alike. Outcome lives in KeyVerdict, gate identity in
	// KeyGateID (spec §5.5).
	EventGateVerdict EventName = "gate.verdict"

	// Brief loader (spec §5.7). brief.rejected is already emitted
	// by internal/program/brief_loader.go and stays load-bearing
	// for the rejection-bookkeeping test corpus.
	EventBriefLoaded            EventName = "brief.loaded"
	EventBriefRejected          EventName = "brief.rejected"
	EventBriefMaterialiseFailed EventName = "brief.materialise_failed"
	EventBriefEdgesMaterialised EventName = "brief.edges_materialised"
	// EventBriefCriteriaDrift fires WARN per in-flight child whose
	// snapshotted acceptance_json no longer matches the criteria a
	// re-loaded brief carries. Snapshot semantics stay intentional (spec
	// §2.4/2.5 Locked decision #5: no auto-resync). This event is the
	// operator-visibility seam — runbook entry tells the operator to
	// archive affected children + re-plan to apply the new bar. Issue #78.
	EventBriefCriteriaDrift EventName = "brief.criteria_drift"

	// Adapter sync (spec §5.8).
	EventAdapterSyncSynced EventName = "adaptersync.synced"
	EventAdapterSyncFailed EventName = "adaptersync.failed"

	// Approval gates — stub notifier audit emission. Real channel
	// emissions (slack/pagerduty/email) land alongside their adapters.
	EventApprovalNotifyStub EventName = "approval.notify_stub"

	// Approval gate lifecycle events (spec §4.1, §5.7). The gate
	// handler emits these via the single recordEvent helper so the
	// slog stream and approval_events row carry byte-equal payloads.
	EventApprovalRequested   EventName = "approval.requested"
	EventApprovalNotified    EventName = "approval.notified"
	EventApprovalDecided     EventName = "approval.decided"
	// EventApprovalTokenMinted fires once per reviewer when the gate
	// hands a signed token over to Notify. The corresponding
	// approval_events row carries the per-JTI token_jti column so the
	// reaper's escalate-revocation branch (spec §3.3.1.3) can find
	// outstanding tokens. Issue #195.
	EventApprovalTokenMinted EventName = "approval.token_minted"

	// Approval-gate reaper sweep events (spec §3.3 / §5.9). Emitted
	// per-row when a pending approval crosses its timeout_at. The
	// dotted `policy` attribute identifies which on_timeout branch
	// fired (fail | auto_approve | escalate).
	EventApprovalTimedOut     EventName = "approval.timed_out"
	EventApprovalAutoApproved EventName = "approval.auto_approved"
	EventApprovalEscalated    EventName = "approval.escalated"

	// Planner — emitted when LoadPlannerPrompt falls back to the
	// embedded default because the operator-configured prompt file is
	// missing AND no SHA pin demands fail-closed (#118 follow-up to
	// Task H). Carries reason= attr so dashboards can distinguish
	// fallback causes if the matrix later grows.
	EventPlannerFallback EventName = "planner.prompt_fallback"

	// Cost-governor reconciler (cost-gov §3.4). Tick lifecycle + fail
	// modes. Skipped fires WARN when the admin key is unset; Fallback
	// fires WARN when Cost API is unavailable and the run uses the
	// Usage API + local pricing path. Failing fires ERROR after 5
	// consecutive upstream failures so dashboards see a sustained
	// outage (single failure is below the noise floor by design).
	// DriftAlert fires WARN once per (period_start, drift_pct@2dp) per
	// A4 dedup.
	EventCostReconcileSkipped  EventName = "cost.reconcile_skipped"
	EventCostReconcileFallback EventName = "cost.reconcile_fallback"
	EventCostReconcileFailing  EventName = "cost.reconcile_failing"
	EventCostDriftAlert        EventName = "cost.drift_alert"
)

// Attribute keys. The set is intentionally small — anything not on
// this list lives under attrs.* so dashboards do not break when a
// new component-specific key is introduced.
const (
	// Universal lifecycle keys (spec §3.3).
	KeyWorkItemID AttrKey = "work_item_id"
	KeyAgentID    AttrKey = "agent_id"
	KeyLane       AttrKey = "lane"
	KeyEventName  AttrKey = "event"
	KeyDurationMs AttrKey = "duration_ms"
	KeyTimestamp  AttrKey = "timestamp"

	// Scheduler / edge eval keys. Mirror the field names already in
	// scheduler.go slog literals so Task B does not break operator
	// queries.
	KeyProgramID  AttrKey = "program_id"
	KeyFromID     AttrKey = "from_id"
	KeyToID       AttrKey = "to_id"
	KeyEdgeID     AttrKey = "edge_id"
	KeyJournalSHA AttrKey = "journal_sha"

	// Gate decision keys (spec §5.5).
	KeyGateID  AttrKey = "gate_id"
	KeyVerdict AttrKey = "verdict"
	KeyReason  AttrKey = "reason"

	// Error surfacing on .failed / .eval_error events (spec §3.3).
	KeyErr AttrKey = "err"

	// Orchestrator tick.completed payload (spec §3.2 JSON example).
	KeyWorkItemsEvaluated AttrKey = "work_items_evaluated"

	// Approval gate notification keys (spec §5.8).
	KeyApprovalID    AttrKey = "approval_id"
	KeyReviewerCount AttrKey = "reviewer_count"

	// Approval gate decision keys (spec §4.1, §5.7). KeyReviewerID
	// tags the per-vote actor; KeyDecision carries allow|deny.
	KeyReviewerID AttrKey = "reviewer_id"
	KeyDecision   AttrKey = "decision"

	// KeyAuditPayload carries the canonical JSON payload that the
	// recordEvent helper also writes to approval_events.payload_json,
	// so byte-equality between slog and DB is a single string compare.
	KeyAuditPayload AttrKey = "audit_payload"

	// Approval-gate reaper sweep attributes (spec §3.3 / §5.9).
	// KeyPolicy carries the on_timeout branch that fired; the chain-
	// index keys ride along on escalation events so an operator can
	// reconstruct which tier became active without re-fetching the row.
	KeyPolicy          AttrKey = "policy"
	KeyPriorChainIndex AttrKey = "prior_chain_index"
	KeyNewChainIndex   AttrKey = "new_chain_index"

	// Cost-governor reconciler failure-mode attributes (spec §3.4
	// line 247-248). KeyPeriodStart tags the reconcile window's start
	// in epoch-millis so dashboards can join Failing ERROR records
	// with the BudgetReconciledPayload rows from the prior happy-path
	// tick. KeyAttemptCount carries the consecutive-retry count that
	// crossed the persistent-failure threshold so on-call sees the
	// retry budget exhaustion without re-reading the spec.
	KeyPeriodStart   AttrKey = "period_start"
	KeyAttemptCount  AttrKey = "attempt_count"
)

// AllEventNames enumerates every EventName constant declared above.
// Reflection-based schema tests in events_test.go (and follow-up
// producer-coverage tests in obs-101) iterate this slice — keep in
// sync when adding events.
func AllEventNames() []EventName {
	return []EventName{
		EventTickStarted,
		EventTickCompleted,
		EventEdgeFired,
		EventEdgeSkipped,
		EventEdgeDefaultFallback,
		EventEdgeEvalError,
		EventEdgeMarkFailed,
		EventEdgeEvalSkippedNoJournal,
		EventEdgeJournalLoadFailed,
		EventEdgeMultipleDefaultsPerFrom,
		EventSchedulerMaterializeFailure,
		EventSpawnStarted,
		EventSpawnCompleted,
		EventSpawnFailed,
		EventSpawnReconciled,
		EventSpawnReconcileFailed,
		EventReapCandidateDetected,
		EventReapKilled,
		EventReapSkipped,
		EventGateVerdict,
		EventBriefLoaded,
		EventBriefRejected,
		EventBriefMaterialiseFailed,
		EventBriefEdgesMaterialised,
		EventBriefCriteriaDrift,
		EventAdapterSyncSynced,
		EventAdapterSyncFailed,
		EventApprovalNotifyStub,
		EventApprovalRequested,
		EventApprovalNotified,
		EventApprovalDecided,
		EventApprovalTokenMinted,
		EventApprovalTimedOut,
		EventApprovalAutoApproved,
		EventApprovalEscalated,
		EventPlannerFallback,
		EventCostReconcileSkipped,
		EventCostReconcileFallback,
		EventCostReconcileFailing,
		EventCostDriftAlert,
	}
}

// AllAttrKeys enumerates every AttrKey constant declared above.
// Symmetric with AllEventNames; same maintenance contract.
func AllAttrKeys() []AttrKey {
	return []AttrKey{
		KeyWorkItemID,
		KeyAgentID,
		KeyLane,
		KeyEventName,
		KeyDurationMs,
		KeyTimestamp,
		KeyProgramID,
		KeyFromID,
		KeyToID,
		KeyEdgeID,
		KeyJournalSHA,
		KeyGateID,
		KeyVerdict,
		KeyReason,
		KeyErr,
		KeyWorkItemsEvaluated,
		KeyApprovalID,
		KeyReviewerCount,
		KeyReviewerID,
		KeyDecision,
		KeyAuditPayload,
		KeyPolicy,
		KeyPriorChainIndex,
		KeyNewChainIndex,
		KeyPeriodStart,
		KeyAttemptCount,
	}
}
