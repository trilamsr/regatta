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

type tokenSpendPayload struct {
	LLMCallID string  `json:"llm_call_id"`
	SpendUSD  float64 `json:"spend_usd"`
	Tokens    int     `json:"tokens"`
}

func validateTokenSpend(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: token_spend payload empty", ErrInvalidPayload)
	}
	var p tokenSpendPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: token_spend: %w", ErrInvalidPayload, err)
	}
	if p.LLMCallID == "" {
		return fmt.Errorf("%w: token_spend missing llm_call_id", ErrInvalidPayload)
	}
	return nil
}

type budgetReconciledPayload struct {
	TenantID    string  `json:"tenant_id"`
	PeriodStart int64   `json:"period_start"`
	SpendUSD    float64 `json:"spend_usd"`
}

func validateBudgetReconciled(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: budget_reconciled payload empty", ErrInvalidPayload)
	}
	var p budgetReconciledPayload
	if err := strictUnmarshal(raw, &p); err != nil {
		return fmt.Errorf("%w: budget_reconciled: %w", ErrInvalidPayload, err)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: budget_reconciled missing tenant_id", ErrInvalidPayload)
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

func init() {
	RegisterPayloadValidator(KindNodeOutput, validateNodeOutput)
	RegisterPayloadValidator(KindFact, validateFact)
	RegisterPayloadValidator(KindApprovalEvent, validateApprovalEvent)
	RegisterPayloadValidator(KindTokenSpend, validateTokenSpend)
	RegisterPayloadValidator(KindBudgetReconciled, validateBudgetReconciled)
	RegisterPayloadValidator(KindHeartbeat, validateHeartbeat)
	// KindGateVerdict: registered by T-S2 in gate_verdict_payload.go init().
}
