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
	EventTickSlow      EventName = "tick.slow"

	EventEdgeFired                   EventName = "edge.fired"
	EventEdgeSkipped                 EventName = "edge.skipped"
	EventEdgeDefaultFallback         EventName = "edge.default_fallback"
	EventEdgeEvalError               EventName = "edge.eval_error"
	EventEdgeMarkFailed              EventName = "edge.mark_failed"
	EventEdgeEvalSkippedNoJournal    EventName = "edge.eval_skipped_no_journal"
	EventEdgeJournalLoadFailed       EventName = "edge.journal_load_failed"
	EventEdgeMultipleDefaultsPerFrom EventName = "edge.multiple_defaults_per_from"

	EventSchedulerMaterializeFailure EventName = "scheduler.materialize_failure"

	// EventSchedulerFileScopeCollisionDeferred fires per candidate that
	// the scheduler defers because its predicted file scope overlaps an
	// in-flight agent on the same lane (#1065). Carries candidate work
	// item, conflicting agent + work item, and overlap paths so an
	// operator can trace which shared anchor parked the candidate.
	EventSchedulerFileScopeCollisionDeferred EventName = "scheduler.file_scope_collision_deferred"

	// EventSchedulerFileScopeCycleStalled fires WARN once per tick when
	// every spawnable candidate collides (#1065 c3). Prevents per-item
	// log spam in a saturated cascade-rebase scenario while still
	// surfacing the stall.
	EventSchedulerFileScopeCycleStalled EventName = "scheduler.file_scope_cycle_stalled"

	// EventSchedulerTickStarved fires INFO once per tick when the
	// L0 spawnable set is non-empty but reservation produces zero
	// agents — lane caps or hotspot locks blocked every candidate.
	// Closes the silent-stall window in #1218 where ticks fire every
	// 5s but the operator log surface stays at DEBUG (zero reserved
	// is masked by tickLogLevel) so the saturated phantom-agent
	// deadlock is invisible.
	EventSchedulerTickStarved EventName = "scheduler.tick_starved"

	EventSpawnStarted   EventName = "spawn.started"
	EventSpawnCompleted EventName = "spawn.completed"
	EventSpawnFailed    EventName = "spawn.failed"

	// EventSpawnBackoffSkipped fires when a work_item's spawn is
	// suppressed because a prior spawn.failed put it in an exponential
	// cooldown window — without it the 5s scheduler tick re-dispatches
	// the same failing item every tick forever (MAY-80).
	EventSpawnBackoffSkipped EventName = "spawn.backoff_skipped"

	// EventSpawnHeldNoBody fires when a reserved work_item is held back
	// from spawn because its brief body is unavailable — spawning blind
	// burns a real agent invocation on a context-less prompt (MAY-81).
	EventSpawnHeldNoBody EventName = "spawn.held_no_body"

	// EventAgentExited fires once per spawned child after cmd.Wait
	// returns. Surfaces the silent-exit case in BUG-1051 where a
	// permission-denied tool call made the agent stop while the
	// orchestrator's state-DB row stayed `running` forever (#1051).
	EventAgentExited EventName = "agent.exited"

	// EventSpawnReconciled fires once per converged work_item; EventSpawnReconcileFailed surfaces per-row error so sweep continues on one bad id (#99).
	EventSpawnReconciled      EventName = "spawn.reconciled"
	EventSpawnReconcileFailed EventName = "spawn.reconcile_failed"

	EventReapCandidateDetected EventName = "reap.candidate_detected"
	EventReapKilled            EventName = "reap.killed"
	EventReapSkipped           EventName = "reap.skipped"

	// EventSpawnerBackendChanged fires WARN once per crash-recovery row
	// whose session_id was written by the stub spawner but the daemon now
	// runs under the claude backend — a reboot ghost with no live process.
	// Distinct from recovered_crashed (a real dead PID that IS requeued):
	// the ghost is drained to withdrawn so its lane frees, not re-spawned
	// (MAY-79).
	EventSpawnerBackendChanged EventName = "orchestrator.spawner_backend_changed"

	// EventCreditExhausted fires ERROR once when an agent exits with the
	// provider_credit_exhausted reason — a terminal account-level fault no
	// retry can clear. Loud so the operator refills credit rather than
	// silently burning further dispatch attempts against a dead account
	// (MAY-78).
	EventCreditExhausted EventName = "orchestrator.credit_exhausted"

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

	// Subsystem event-kind names registered here so DB.RecordEvent can
	// fail-closed on typos against one unified vocabulary (Bug S4). The
	// subsystem packages keep their EventKind* constants for
	// readability, but their string values MUST equal these consts.
	EventCostCapThrottled    EventName = "cost_cap_throttled"
	EventCostCapResumed      EventName = "cost_cap_resumed"
	EventMergeIntent         EventName = "merge_intent"
	EventMergeCompleted      EventName = "merge_completed"
	EventMergeExecuted       EventName = "merge_executed"
	EventMergeFailed         EventName = "merge_failed"
	EventMergeRecovered      EventName = "merge_recovered"
	EventGateRejected        EventName = "gate_rejected"
	EventEscalated           EventName = "escalated"
	EventLabeled             EventName = "labeled"
	EventRecoveredCrashed    EventName = "recovered_crashed"
	EventReaped              EventName = "reaped"
	EventSecretsRotated      EventName = "secrets_rotated"
	EventAgentPROpened       EventName = "agent_pr_opened"
	EventAgentPRHeadChanged  EventName = "agent_pr_head_changed"
	EventAgentBranchRenamed  EventName = "agent_branch_renamed"
	EventAgentPRDirty        EventName = "agent_pr_dirty"
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

	// KeyExitCode / KeyLastTextFingerprint surface the child's final
	// breath when the spawner emits agent.exited (#1051). Fingerprint
	// is sha256-prefix(last-stdout-window) — bounded, identifier-shape,
	// safe to ship to log shippers without leaking arbitrary text.
	KeyExitCode            AttrKey = "exit_code"
	KeyLastTextFingerprint AttrKey = "last_text_fingerprint"

	// KeyExitReason classifies an agent.exited stop bucket beyond bare
	// exit_code (#1063). Values come from spawner.ExitReason —
	// "completed" / "provider_credit_exhausted" / "tool_denied" / etc.
	KeyExitReason AttrKey = "exit_reason"

	KeyWorkItemsEvaluated AttrKey = "work_items_evaluated"

	// KeyAgentsReserved pairs with KeyWorkItemsEvaluated on
	// scheduler.tick_starved: evaluated>0 + reserved==0 means lane
	// caps or hotspot locks blocked dispatch this tick (#1218).
	KeyAgentsReserved AttrKey = "agents_reserved"

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
		EventTickSlow,
		EventEdgeFired,
		EventEdgeSkipped,
		EventEdgeDefaultFallback,
		EventEdgeEvalError,
		EventEdgeMarkFailed,
		EventEdgeEvalSkippedNoJournal,
		EventEdgeJournalLoadFailed,
		EventEdgeMultipleDefaultsPerFrom,
		EventSchedulerMaterializeFailure,
		EventSchedulerFileScopeCollisionDeferred,
		EventSchedulerFileScopeCycleStalled,
		EventSchedulerTickStarved,
		EventSpawnStarted,
		EventSpawnCompleted,
		EventSpawnFailed,
		EventSpawnBackoffSkipped,
		EventSpawnHeldNoBody,
		EventAgentExited,
		EventSpawnReconciled,
		EventSpawnReconcileFailed,
		EventReapCandidateDetected,
		EventReapKilled,
		EventReapSkipped,
		EventSpawnerBackendChanged,
		EventCreditExhausted,
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
		EventCostCapThrottled,
		EventCostCapResumed,
		EventMergeIntent,
		EventMergeCompleted,
		EventMergeExecuted,
		EventMergeFailed,
		EventMergeRecovered,
		EventGateRejected,
		EventEscalated,
		EventLabeled,
		EventRecoveredCrashed,
		EventReaped,
		EventSecretsRotated,
		EventAgentPROpened,
		EventAgentPRHeadChanged,
		EventAgentBranchRenamed,
		EventAgentPRDirty,
	}
}

// knownEvents indexes AllEventNames for O(1) IsKnownEvent lookup.
var knownEvents = func() map[string]struct{} {
	all := AllEventNames()
	m := make(map[string]struct{}, len(all))
	for _, e := range all {
		m[string(e)] = struct{}{}
	}
	return m
}()

// IsKnownEvent reports whether kind matches a declared EventName
// constant. DB.RecordEvent + RecordEventTx call this at the write
// boundary so a typo'd kind (e.g. "spawn_compeleted") fails-closed
// rather than silently inserting a row no dashboard recognizes (Bug
// S4). Keep the AllEventNames slice in sync when adding new events.
func IsKnownEvent(kind string) bool {
	_, ok := knownEvents[kind]
	return ok
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
		KeyExitCode,
		KeyLastTextFingerprint,
		KeyWorkItemsEvaluated,
		KeyAgentsReserved,
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
