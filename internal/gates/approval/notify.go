// Package approval implements the HITL approval-gate DAG node (spec
// §3.1) — pause, notify the reviewer set, resume on signed-token
// callback. This file declares the notification-adapter seam (§5.8)
// that real channels (slack/pagerduty/email) will plug into in later
// PRs. Wave 1 ships the interface + a fail-closed registry + a stub
// notifier that records the audit-trail intent.
package approval

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Notifier is the channel-agnostic surface gates call to deliver a
// pending-approval message. Real implementations hold their own
// credentials; the gate only sees Request/Receipt.
//
// Conformance — every Notify implementation MUST honour the four
// invariants pinned by TestNotifier_InterfaceContract (spec §5.8,
// §7 audit-trail). Channel PRs (slack, pagerduty, email) earn
// conformance by appending a factory row; the contract suite mutates
// each invariant and the impl MUST fail-closed:
//
//  1. On nil error, len(Receipt.DeliveredTo) == len(req.Reviewers).
//     Partial fan-out (e.g. one Slack DM bounces) MUST surface as a
//     non-nil error rather than a partial Receipt — callers cannot
//     silently lose a reviewer.
//  2. The impl MUST emit at least one obs record carrying the
//     canonical four attrs (KeyApprovalID, KeyWorkItemID, KeyGateID,
//     KeyReviewerCount). Event name MAY be channel-specific
//     (approval.notify_stub, approval.notified, …); attrs MUST NOT.
//  3. len(req.Reviewers) == 0 MUST return ErrNoReviewers (wrapped is
//     fine — callers use errors.Is) without emitting an audit record;
//     an approval pause with no reviewers would stall indefinitely.
//  4. A cancelled or expired ctx MUST surface ctx.Err() before any
//     external side effect or audit emission. Implementations MAY
//     check ctx.Err() at entry and again before commit; either way
//     a cancelled call MUST NOT leave a "we notified" breadcrumb.
//
// Implementations must be safe to call concurrently and must not
// block past their own transport timeout.
type Notifier interface {
	// Kind returns the registry key — "slack", "pagerduty", "email",
	// "stub", etc. The same value MUST appear in regatta.yaml channel
	// config; mismatch fails at startup (fail-closed contract below).
	Kind() string
	// Notify delivers the approval request. See the Notifier godoc for
	// the four conformance invariants every impl MUST satisfy.
	Notify(ctx context.Context, req Request) (Receipt, error)
}

// InteractiveNotifier extends Notifier with a self-served HTTP
// callback (e.g. a Slack interactive button POSTing back to regatta).
// Declared in W1 for the type-checker; no concrete impl ships until
// the dedicated channel PR.
type InteractiveNotifier interface {
	Notifier
	CallbackRoute() (path string, handler http.Handler)
}

// Request carries the minimum primitives a notifier needs to render
// an approval message and resume the gate. The spec §5.8 sketch
// references state.Approval / state.WorkItem directly, but those rows
// land in implementer A1's PR; passing primitives instead decouples
// the notification adapter from the DB schema and lets this seam ship
// independently of state-package churn. A thin adapter (state.Approval
// → Request) wires the two in a follow-up.
type Request struct {
	ApprovalID       string
	WorkItemID       string
	GateName         string
	Reviewers        []string
	DecisionDeadline time.Time
	// Tokens maps reviewer_id → signed callback token. Issued by the
	// HMAC token component (A3). Notifiers embed the matching token
	// into the per-reviewer rendered message.
	Tokens map[string]string
}

// Receipt records what the channel actually accomplished so the
// caller can persist it on the approval row for the auditor's later
// reconciliation. On nil error from Notify, DeliveredTo MUST contain
// the full req.Reviewers set (same multiset, order may differ) —
// partial fan-out surfaces as a non-nil error, never as a shorter
// DeliveredTo. See Notifier godoc for the full invariant.
type Receipt struct {
	DeliveredTo []string
	Channel     string
	ExternalID  string
}

// ErrUnknownNotifier is returned by Registry.Get when the requested
// kind has not been registered. Callers (and the regatta.yaml loader)
// MUST surface this as a startup error rather than fall through to a
// default — silent fall-through to stub in production would silently
// drop approvals (spec §5.8 fail-closed invariant).
var ErrUnknownNotifier = errors.New("approval: unknown notifier kind")

// ErrNoReviewers is returned by Notifier.Notify when req.Reviewers is
// empty. The notification seam is fail-closed by design (spec §5.8):
// an approval pause with no reviewers would stall the work item
// forever with no audit trail, so the gate-tick aborts rather than
// emits a misleading "we notified" breadcrumb. Callers use errors.Is.
var ErrNoReviewers = errors.New("approval: zero reviewers in request")

// Registry is a fail-closed lookup table from notifier kind to
// concrete adapter. Construction is process-local; tests get a fresh
// Registry per case to avoid cross-test bleed.
type Registry struct {
	mu sync.RWMutex
	m  map[string]Notifier
}

// NewRegistry builds an empty registry. Callers register adapters at
// process start (or at test start) before any gate Tick runs.
func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Notifier)}
}

// Register installs n under name. Returns an error if name is empty
// or already registered — duplicate registration is a programmer bug
// that would otherwise let a later Register silently shadow an
// earlier adapter (e.g. two import paths racing init).
func (r *Registry) Register(name string, n Notifier) error {
	if name == "" {
		return fmt.Errorf("approval: notifier name cannot be empty")
	}
	if n == nil {
		return fmt.Errorf("approval: notifier %q is nil", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.m[name]; exists {
		return fmt.Errorf("approval: notifier %q already registered", name)
	}
	r.m[name] = n
	return nil
}

// Get returns the registered notifier for name. Missing kind returns
// ErrUnknownNotifier (wrapped with the offending name so logs show
// which config row was wrong) so callers can `errors.Is` the sentinel.
func (r *Registry) Get(name string) (Notifier, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.m[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownNotifier, name)
	}
	return n, nil
}

// Build resolves a notifier kind via the registry and enforces the
// fail-closed contract for config-driven construction. Wrapping Get
// keeps the lookup site uniform across gate startup and reaper
// re-notification; nothing in production may construct a stub on
// silent miss.
func Build(r *Registry, name string) (Notifier, error) {
	if r == nil {
		return nil, fmt.Errorf("approval: registry is nil")
	}
	return r.Get(name)
}
