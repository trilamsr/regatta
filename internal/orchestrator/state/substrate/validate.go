package substrate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
)

// PayloadValidator is the per-kind hook the dispatch table calls.
// Implementations typed-unmarshal raw into a per-kind struct and
// return nil on success, ErrInvalidPayload (wrapped) on shape failure.
type PayloadValidator func(raw json.RawMessage) error

// validatorRegistry holds the per-kind dispatch table. T-S1 ships the
// six non-gate validators below; T-S2 registers KindGateVerdict from
// its own init() in gate_verdict_payload.go. Future kinds register
// the same way — this keeps the dispatch open-extensible without
// edits to T-S1's surface.
var (
	validatorMu       sync.RWMutex
	validatorRegistry = map[EventKind]PayloadValidator{}
)

// RegisterPayloadValidator records fn as the validator for kind.
// Idempotent on identical re-registration (sibling init() ordering
// is undefined and a second copy of the same fn pointer is benign).
// Panics on conflicting re-registration (two different fns claim the
// same kind) — that's a programmer error, not a runtime path.
func RegisterPayloadValidator(kind EventKind, fn PayloadValidator) {
	validatorMu.Lock()
	defer validatorMu.Unlock()
	if existing, ok := validatorRegistry[kind]; ok {
		// fmt.Sprintf on function pointers is stable enough for the
		// duplicate-register panic shape — same pointer prints same.
		if fmt.Sprintf("%p", existing) != fmt.Sprintf("%p", fn) {
			panic(fmt.Sprintf("substrate: duplicate validator for kind %q", kind))
		}
		return
	}
	validatorRegistry[kind] = fn
}

// validatePayload dispatches raw to the per-kind validator. Unknown
// kind (not registered) ⇒ ErrInvalidPayload — fail-closed. Empty raw
// is allowed (some kinds carry no payload); the per-kind validator
// decides whether {} is a legitimate empty.
func validatePayload(kind EventKind, raw json.RawMessage) error {
	validatorMu.RLock()
	fn, ok := validatorRegistry[kind]
	validatorMu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidPayload, kind)
	}
	return fn(raw)
}

// nodeOutputPayload mirrors work_item_outputs row shape.
type nodeOutputPayload struct {
	WorkItemID string          `json:"work_item_id"`
	Attempt    int             `json:"attempt"`
	Output     json.RawMessage `json:"output"`
}

func validateNodeOutput(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: node_output payload empty", ErrInvalidPayload)
	}
	var p nodeOutputPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: node_output: %w", ErrInvalidPayload, err)
	}
	if p.WorkItemID == "" {
		return fmt.Errorf("%w: node_output missing work_item_id", ErrInvalidPayload)
	}
	return nil
}

type factPayload struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

func validateFact(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: fact payload empty", ErrInvalidPayload)
	}
	var p factPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: fact: %w", ErrInvalidPayload, err)
	}
	if p.Key == "" {
		return fmt.Errorf("%w: fact missing key", ErrInvalidPayload)
	}
	return nil
}

type approvalEventPayload struct {
	ApprovalID string `json:"approval_id"`
	Transition string `json:"transition"`
	Actor      string `json:"actor"`
	// TokenJTI carries the legacy approval_events.token_jti through
	// the shadow-write mirror (S3-T2 spec §3.2). NOT reused as the
	// substrate column nonce (spec §3.5) — token replay and substrate
	// replay are independent surfaces with different lifetimes.
	TokenJTI string `json:"token_jti,omitempty"`
}

func validateApprovalEvent(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: approval_event payload empty", ErrInvalidPayload)
	}
	var p approvalEventPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: approval_event: %w", ErrInvalidPayload, err)
	}
	if p.ApprovalID == "" || p.Transition == "" {
		return fmt.Errorf("%w: approval_event missing approval_id|transition", ErrInvalidPayload)
	}
	return nil
}

// tokenSpendPayload mirrors cost-governor design spec §3.5 lines 264-276
// verbatim. Cost-governor (P8) owns the field set per
// feedback_shared_primitive_owner; substrate hosts the validator only.
// USDMicro is the canonical integer-micro-USD signed value (#554); USD
// is the legacy float emission kept for forward-compat dashboards.
// Either field MAY be present — the writer emits both; pre-#554
// replay rows carry only USD and the reader synthesises the micro
// equivalent at read time.
type tokenSpendPayload struct {
	USDMicro            int64   `json:"usd_micro"`
	USD                 float64 `json:"usd"`
	Model               string  `json:"model"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	OperatorID          string  `json:"operator_id"`
	DAGID               string  `json:"dag_id"`
	WorkItemID          string  `json:"work_item_id"`
	PricingRev          string  `json:"pricing_rev"`
	CallID              string  `json:"call_id"`
}

func validateTokenSpend(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: token_spend payload empty", ErrInvalidPayload)
	}
	var p tokenSpendPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: token_spend: %w", ErrInvalidPayload, err)
	}
	if p.CallID == "" || p.Model == "" || p.WorkItemID == "" {
		return fmt.Errorf("%w: token_spend missing call_id|model|work_item_id", ErrInvalidPayload)
	}
	return nil
}

// budgetReconciledPayload mirrors cost-governor design spec §3.5 lines
// 278-289 verbatim. Reconciler (T4) writes one row per tenant per
// period; reducer is LWW per (tenant_id, period_start). Money fields
// dual-emit per #554: *_usd_micro (int64) is canonical signed; *_usd
// (float64) is legacy display.
type budgetReconciledPayload struct {
	PeriodStart      int64                      `json:"period_start"`
	PeriodEnd        int64                      `json:"period_end"`
	ActualUSDMicro   int64                      `json:"actual_usd_micro"`
	RecordedUSDMicro int64                      `json:"recorded_usd_micro"`
	DeltaUSDMicro    int64                      `json:"delta_usd_micro"`
	ActualUSD        float64                    `json:"actual_usd"`
	RecordedUSD      float64                    `json:"recorded_usd"`
	DeltaUSD         float64                    `json:"delta_usd"`
	DriftPct         float64                    `json:"drift_pct"`
	ModelBreakdown   []modelBreakdownPayloadRow `json:"model_breakdown"`
	APIResponseSig   string                     `json:"api_response_sig"`
}

type modelBreakdownPayloadRow struct {
	Model               string  `json:"model"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	USDMicro            int64   `json:"usd_micro"`
	USD                 float64 `json:"usd"`
}

func validateBudgetReconciled(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: budget_reconciled payload empty", ErrInvalidPayload)
	}
	var p budgetReconciledPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: budget_reconciled: %w", ErrInvalidPayload, err)
	}
	if p.PeriodStart == 0 || p.PeriodEnd == 0 {
		return fmt.Errorf("%w: budget_reconciled missing period_start|period_end", ErrInvalidPayload)
	}
	return nil
}

type heartbeatPayload struct {
	WorkItemID string `json:"work_item_id"`
	Timestamp  int64  `json:"timestamp"`
}

func validateHeartbeat(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: heartbeat payload empty", ErrInvalidPayload)
	}
	var p heartbeatPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: heartbeat: %w", ErrInvalidPayload, err)
	}
	if p.WorkItemID == "" {
		return fmt.Errorf("%w: heartbeat missing work_item_id", ErrInvalidPayload)
	}
	return nil
}

// briefRejectedPayload mirrors the audit-row shape brief_loader writes
// for issue #80. Path is the on-disk artifact the operator can re-read;
// Reason is the freeform error class (`hmac`, `unknown_parent`,
// `stale_produced_at`, `feature_id_collision`, etc.) — truncated to fit
// the 1024-byte payload CHECK at the writer.
type briefRejectedPayload struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func validateBriefRejected(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: brief_rejected payload empty", ErrInvalidPayload)
	}
	var p briefRejectedPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: brief_rejected: %w", ErrInvalidPayload, err)
	}
	if p.Path == "" || p.Reason == "" {
		return fmt.Errorf("%w: brief_rejected missing path|reason", ErrInvalidPayload)
	}
	return nil
}

// PRStage is the closed enum for OBS-WAVE-C-T2 PR lifecycle stages.
// Adding a stage MUST update both this enum AND parent spec §3.1; the
// closed shape is the cardinality fence for `from_stage` / `to_stage`
// metric labels.
type PRStage string

// PRStage closed-enum members — adding a stage MUST update both the
// list below AND parent spec §3.1. The shape is the cardinality fence
// for `from_stage` / `to_stage` metric labels.
const (
	PRStageDraft          PRStage = "draft"
	PRStageGatesRunning   PRStage = "gates_running"
	PRStageGatesPass      PRStage = "gates_pass"
	PRStageAwaitingReview PRStage = "awaiting_review"
	PRStageAwaitingMerge  PRStage = "awaiting_merge"
	PRStageMerged         PRStage = "merged"
	PRStageFailed         PRStage = "failed"
)

// AllPRStages returns the canonical PR-stage list in declaration order.
func AllPRStages() []PRStage {
	return []PRStage{
		PRStageDraft, PRStageGatesRunning, PRStageGatesPass,
		PRStageAwaitingReview, PRStageAwaitingMerge,
		PRStageMerged, PRStageFailed,
	}
}

// prStageTransitionPayload mirrors the OBS-WAVE-C-T2 substrate event
// shape per spec §3.4. PRNumber rides the payload (audit trail); the
// metric counter keeps from_stage / to_stage as closed-enum labels but
// banishes pr_number to preserve the cardinality budget.
type prStageTransitionPayload struct {
	PRNumber        int64   `json:"pr_number"`
	FromStage       string  `json:"from_stage"`
	ToStage         string  `json:"to_stage"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

func validPRStage(s string) bool {
	for _, allowed := range AllPRStages() {
		if string(allowed) == s {
			return true
		}
	}
	return false
}

func validatePRStageTransition(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: pr_stage_transition payload empty", ErrInvalidPayload)
	}
	var p prStageTransitionPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: pr_stage_transition: %w", ErrInvalidPayload, err)
	}
	if p.PRNumber <= 0 {
		return fmt.Errorf("%w: pr_stage_transition missing pr_number", ErrInvalidPayload)
	}
	if !validPRStage(p.FromStage) {
		return fmt.Errorf("%w: pr_stage_transition from_stage %q not in closed enum",
			ErrInvalidPayload, p.FromStage)
	}
	if !validPRStage(p.ToStage) {
		return fmt.Errorf("%w: pr_stage_transition to_stage %q not in closed enum",
			ErrInvalidPayload, p.ToStage)
	}
	return nil
}

// greenClockResetPayload carries the audit shape GreenClock reads
// when a manual_merge or operator_intervention breaks the streak
// (#659). SubjectID names the surface acted on (PR number, branch,
// run-id); Actor names the operator; Reason carries the freeform
// classification.
type greenClockResetPayload struct {
	SubjectID string `json:"subject_id"`
	Actor     string `json:"actor"`
	Reason    string `json:"reason"`
}

func validateGreenClockReset(kind EventKind) PayloadValidator {
	return func(raw json.RawMessage) error {
		if len(raw) == 0 {
			return fmt.Errorf("%w: %s payload empty", ErrInvalidPayload, kind)
		}
		var p greenClockResetPayload
		if err := strictUnmarshal(raw, &p); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrInvalidPayload, kind, err)
		}
		if p.SubjectID == "" || p.Actor == "" || p.Reason == "" {
			return fmt.Errorf("%w: %s missing subject_id|actor|reason",
				ErrInvalidPayload, kind)
		}
		return nil
	}
}

// costCapThrottledPayload mirrors cap.go's legacy `events`-table audit
// row so substrate forward-port (#622) is byte-equivalent. SpendMicro +
// CapMicro are the int-micro canonical fields; AutoResumeAt is RFC3339;
// TZ is the IANA name the Enforcer was configured with.
type costCapThrottledPayload struct {
	SpendMicro   int64  `json:"spend_micro"`
	CapMicro     int64  `json:"cap_micro"`
	AutoResumeAt string `json:"auto_resume_at"`
	TZ           string `json:"tz"`
}

func validateCostCapThrottled(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: cost_cap_throttled payload empty", ErrInvalidPayload)
	}
	var p costCapThrottledPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: cost_cap_throttled: %w", ErrInvalidPayload, err)
	}
	if p.CapMicro <= 0 || p.AutoResumeAt == "" || p.TZ == "" {
		return fmt.Errorf("%w: cost_cap_throttled missing cap_micro|auto_resume_at|tz",
			ErrInvalidPayload)
	}
	return nil
}

// costCapResumedPayload mirrors cap.go's Resume audit row. Actor is the
// operator identity; Reason is the freeform classification; Until is
// the RFC3339 timestamp of the next auto-rollover anchor.
type costCapResumedPayload struct {
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
	Until  string `json:"until"`
}

func validateCostCapResumed(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: cost_cap_resumed payload empty", ErrInvalidPayload)
	}
	var p costCapResumedPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: cost_cap_resumed: %w", ErrInvalidPayload, err)
	}
	if p.Actor == "" || p.Reason == "" || p.Until == "" {
		return fmt.Errorf("%w: cost_cap_resumed missing actor|reason|until",
			ErrInvalidPayload)
	}
	return nil
}

// strictUnmarshal forbids unknown fields. Pins payload shape per
// spec §S4 (typed Go structs, no JSON Schema files). A producer
// emitting extra fields silently is a forward-version-compat trap.
func strictUnmarshal(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing bytes after payload")
	}
	return nil
}

// toolCallPayload mirrors the operator-console S0 tool_call envelope:
// surprise detector folds DeclaredEffectClass against ObservedEffect at
// fold-time (spec §3.7). AgentID + Signature scope the event back to a
// single Claude-shim call boundary.
type toolCallPayload struct {
	AgentID             string   `json:"agent_id"`
	Signature           string   `json:"signature"`
	ArgsHash            string   `json:"args_hash"`
	DeclaredEffectClass string   `json:"declared_effect_class"`
	ObservedEffect      []string `json:"observed_effect"`
	StartedAt           int64    `json:"started_at"`
	FinishedAt          int64    `json:"finished_at"`
}

func validateToolCall(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: tool_call payload empty", ErrInvalidPayload)
	}
	var p toolCallPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: tool_call: %w", ErrInvalidPayload, err)
	}
	if p.AgentID == "" || p.Signature == "" {
		return fmt.Errorf("%w: tool_call missing agent_id|signature",
			ErrInvalidPayload)
	}
	return nil
}

func init() {
	RegisterPayloadValidator(KindNodeOutput, validateNodeOutput)
	RegisterPayloadValidator(KindFact, validateFact)
	RegisterPayloadValidator(KindApprovalEvent, validateApprovalEvent)
	RegisterPayloadValidator(KindTokenSpend, validateTokenSpend)
	RegisterPayloadValidator(KindBudgetReconciled, validateBudgetReconciled)
	RegisterPayloadValidator(KindHeartbeat, validateHeartbeat)
	RegisterPayloadValidator(KindBriefRejected, validateBriefRejected)
	RegisterPayloadValidator(KindPRStageTransition, validatePRStageTransition)
	RegisterPayloadValidator(KindManualMerge, validateGreenClockReset(KindManualMerge))
	RegisterPayloadValidator(KindOperatorIntervention, validateGreenClockReset(KindOperatorIntervention))
	RegisterPayloadValidator(KindCostCapThrottled, validateCostCapThrottled)
	RegisterPayloadValidator(KindCostCapResumed, validateCostCapResumed)
	RegisterPayloadValidator(KindToolCall, validateToolCall)
	// KindGateVerdict: registered by T-S2 in gate_verdict_payload.go init().
}
