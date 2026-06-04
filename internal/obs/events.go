// Package obs defines the canonical structured-event vocabulary for
// regatta runtime components. Every state transition or notable lifecycle
// moment emitted to slog by Orchestrator, Scheduler, Spawner, Reaper,
// Gates, AdapterSync, and BriefLoader MUST go through the EventName +
// AttrKey constants below — dashboards, log shippers, and runbooks grep
// on these strings. Reshuffles need a code review that updates
// AllEventNames / AllAttrKeys and re-runs events_test.go schema tests.
package obs

// EventName tags a structured-log record with its canonical taxonomy
// slot. Producers populate slog's `msg` argument from this value and also
// set the KeyEvent attribute to the same string so both grep and jq
// workflows resolve cleanly (spec §3.3).
type EventName string

// AttrKey closes the top-level attribute namespace; component-specific keys land inside `attrs` instead of polluting the root JSON record (spec §3.3).
type AttrKey string

// Event vocabulary. Names use dotted component.event format so operators can filter by component prefix.
const (
	EventTickStarted   EventName = "tick.started"
	EventTickCompleted EventName = "tick.completed"

	EventEdgeFired                   EventName = "edge.fired"
	EventEdgeSkipped                 EventName = "edge.skipped"
	EventEdgeDefaultFallback         EventName = "edge.default_fallback"
	EventEdgeEvalError               EventName = "edge.eval_error"
	EventEdgeMarkFailed              EventName = "edge.mark_failed"
	EventEdgeEvalSkippedNoJournal    EventName = "edge.eval_skipped_no_journal"
	EventEdgeJournalLoadFailed       EventName = "edge.journal_load_failed"
	EventEdgeMultipleDefaultsPerFrom EventName = "edge.multiple_defaults_per_from"

	EventSchedulerMaterializeFailure EventName = "scheduler.materialize_failure"

	EventSpawnStarted   EventName = "spawn.started"
	EventSpawnCompleted EventName = "spawn.completed"
	EventSpawnFailed    EventName = "spawn.failed"

	// EventSpawnReconciled fires once per converged work_item; EventSpawnReconcileFailed surfaces per-row error so sweep continues on one bad id (#99).
	EventSpawnReconciled      EventName = "spawn.reconciled"
	EventSpawnReconcileFailed EventName = "spawn.reconcile_failed"

	EventReapCandidateDetected EventName = "reap.candidate_detected"
	EventReapKilled            EventName = "reap.killed"
	EventReapSkipped           EventName = "reap.skipped"

	// EventGateVerdict — single event covers l0, security, and HMAC seam alike. Outcome in KeyVerdict, gate identity in KeyGateID (spec §5.5).
	EventGateVerdict EventName = "gate.verdict"

	EventBriefLoaded            EventName = "brief.loaded"
	EventBriefRejected          EventName = "brief.rejected"
	EventBriefMaterialiseFailed EventName = "brief.materialise_failed"
	EventBriefEdgesMaterialised EventName = "brief.edges_materialised"
	// EventBriefCriteriaDrift fires WARN per in-flight child whose snapshotted
	// acceptance_json no longer matches a re-loaded brief's criteria. Snapshot
	// semantics intentional (spec §2.4/2.5 #5: no auto-resync); runbook tells
	// operator to archive affected children + re-plan (#78).
	EventBriefCriteriaDrift EventName = "brief.criteria_drift"

	EventAdapterSyncSynced EventName = "adaptersync.synced"
	EventAdapterSyncFailed EventName = "adaptersync.failed"

	EventApprovalNotifyStub EventName = "approval.notify_stub"

	EventApprovalRequested EventName = "approval.requested"
	EventApprovalNotified  EventName = "approval.notified"
	EventApprovalDecided   EventName = "approval.decided"
	// EventApprovalTokenMinted fires once per reviewer when the gate hands a
	// signed token to Notify. The approval_events row carries token_jti so the
	// reaper's escalate-revocation branch (spec §3.3.1.3) can find outstanding
	// tokens (#195).
	EventApprovalTokenMinted EventName = "approval.token_minted"

	// EventApprovalTimedOut/AutoApproved/Escalated — emitted per-row when pending approval crosses timeout_at; KeyPolicy identifies which on_timeout branch fired (spec §3.3 / §5.9).
	EventApprovalTimedOut     EventName = "approval.timed_out"
	EventApprovalAutoApproved EventName = "approval.auto_approved"
	EventApprovalEscalated    EventName = "approval.escalated"

	// EventPlannerFallback fires when LoadPlannerPrompt falls back to embedded default (operator prompt missing, no SHA pin); carries reason= attr (#118).
	EventPlannerFallback EventName = "planner.prompt_fallback"

	// Cost-governor reconciler events (cost-gov §3.4). Skipped/Fallback WARN; Failing ERROR after 5 consecutive upstream failures; DriftAlert WARN once per (period_start, drift_pct@2dp).
	EventCostReconcileSkipped  EventName = "cost.reconcile_skipped"
	EventCostReconcileFallback EventName = "cost.reconcile_fallback"
	EventCostReconcileFailing  EventName = "cost.reconcile_failing"
	EventCostDriftAlert        EventName = "cost.drift_alert"
)

// Attribute keys. The set is intentionally small — anything not listed lives under attrs.* so dashboards do not break when a new component-specific key is introduced.
const (
	KeyWorkItemID AttrKey = "work_item_id"
	KeyAgentID    AttrKey = "agent_id"
	KeyLane       AttrKey = "lane"
	KeyEventName  AttrKey = "event"
	KeyDurationMs AttrKey = "duration_ms"
	KeyTimestamp  AttrKey = "timestamp"

	KeyProgramID  AttrKey = "program_id"
	KeyFromID     AttrKey = "from_id"
	KeyToID       AttrKey = "to_id"
	KeyEdgeID     AttrKey = "edge_id"
	KeyJournalSHA AttrKey = "journal_sha"

	KeyGateID  AttrKey = "gate_id"
	KeyVerdict AttrKey = "verdict"
	KeyReason  AttrKey = "reason"

	KeyErr AttrKey = "err"

	KeyWorkItemsEvaluated AttrKey = "work_items_evaluated"

	KeyApprovalID    AttrKey = "approval_id"
	KeyReviewerCount AttrKey = "reviewer_count"

	KeyReviewerID AttrKey = "reviewer_id"
	KeyDecision   AttrKey = "decision"

	// KeyAuditPayload carries canonical JSON payload that recordEvent also writes to approval_events.payload_json, so slog↔DB byte-equality is a single string compare.
	KeyAuditPayload AttrKey = "audit_payload"

	// KeyPolicy/PriorChainIndex/NewChainIndex — KeyPolicy carries the on_timeout branch that fired; chain-index keys let operators reconstruct active tier without re-fetching (spec §3.3 / §5.9).
	KeyPolicy          AttrKey = "policy"
	KeyPriorChainIndex AttrKey = "prior_chain_index"
	KeyNewChainIndex   AttrKey = "new_chain_index"

	// KeyPeriodStart/AttemptCount — reconcile window start (epoch-ms) lets dashboards join Failing ERROR with prior BudgetReconciledPayload rows; AttemptCount carries the consecutive-retry count that crossed the persistent-failure threshold (spec §3.4).
	KeyPeriodStart  AttrKey = "period_start"
	KeyAttemptCount AttrKey = "attempt_count"
)

// AllEventNames enumerates every EventName constant — reflection-based schema tests iterate this slice; keep in sync when adding events.
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

// AllAttrKeys enumerates every AttrKey constant — symmetric with AllEventNames; same maintenance contract.
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
