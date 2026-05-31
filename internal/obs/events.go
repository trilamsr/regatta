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
	EventEdgeFired                    EventName = "edge.fired"
	EventEdgeSkipped                  EventName = "edge.skipped"
	EventEdgeDefaultFallback          EventName = "edge.default_fallback"
	EventEdgeEvalError                EventName = "edge.eval_error"
	EventEdgeMarkFailed               EventName = "edge.mark_failed"
	EventEdgeEvalSkippedNoJournal     EventName = "edge.eval_skipped_no_journal"
	EventEdgeJournalLoadFailed        EventName = "edge.journal_load_failed"
	EventEdgeMultipleDefaultsPerFrom  EventName = "edge.multiple_defaults_per_from"

	// Scheduler materialization (new event per spec §5.2 — storage
	// write failure after an edge fires).
	EventSchedulerMaterializeFailure EventName = "scheduler.materialize_failure"

	// Spawner lifecycle (spec §5.3).
	EventSpawnStarted   EventName = "spawn.started"
	EventSpawnCompleted EventName = "spawn.completed"
	EventSpawnFailed    EventName = "spawn.failed"

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
	EventBriefLoaded             EventName = "brief.loaded"
	EventBriefRejected           EventName = "brief.rejected"
	EventBriefMaterialiseFailed  EventName = "brief.materialise_failed"
	EventBriefEdgesMaterialised  EventName = "brief.edges_materialised"

	// Adapter sync (spec §5.8).
	EventAdapterSyncSynced EventName = "adaptersync.synced"
	EventAdapterSyncFailed EventName = "adaptersync.failed"
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
		EventReapCandidateDetected,
		EventReapKilled,
		EventReapSkipped,
		EventGateVerdict,
		EventBriefLoaded,
		EventBriefRejected,
		EventBriefMaterialiseFailed,
		EventBriefEdgesMaterialised,
		EventAdapterSyncSynced,
		EventAdapterSyncFailed,
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
	}
}
