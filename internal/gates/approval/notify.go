// Package approval implements the HITL approval-gate DAG node (spec
// §3.1) — pause, notify the reviewer set, resume on signed-token
// callback. This file declares the notification-adapter seam (§5.8).
// Ships the interface + fail-closed registry + a stub notifier
// recording audit-trail intent.
package approval

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Notifier is the channel-agnostic surface gates call to deliver a
// pending-approval message. Conformance invariants
// (TestNotifier_InterfaceContract, spec §5.8, §7 audit-trail):
//
//  1. Nil error → len(Receipt.DeliveredTo) == len(req.Reviewers).
//     Partial fan-out MUST surface as non-nil error, not a short
//     DeliveredTo — callers cannot silently lose a reviewer.
//  2. MUST emit at least one obs record with the canonical four attrs
//     (KeyApprovalID, KeyWorkItemID, KeyGateID, KeyReviewerCount).
//  3. Empty Reviewers MUST return ErrNoReviewers WITHOUT emitting an
//     audit record — would otherwise stall the gate forever.
//  4. Cancelled/expired ctx MUST surface ctx.Err() before any external
//     side effect or audit emission.
//
// Implementations must be concurrent-safe and must not block past
// their own transport timeout.
type Notifier interface {
	// Kind returns the registry key matching regatta.yaml channel config; mismatch fails at startup.
	Kind() string
	Notify(ctx context.Context, req Request) (Receipt, error)
}

// InteractiveNotifier extends Notifier with a self-served HTTP
// callback (e.g. a Slack interactive button POSTing back to regatta).
type InteractiveNotifier interface {
	Notifier
	CallbackRoute() (path string, handler http.Handler)
}

// Request carries the minimum primitives a notifier needs to render an
// approval message and resume the gate. Primitive shape (vs
// state.Approval directly) decouples the adapter from the DB schema so
// a Slack/PagerDuty/email impl has no business knowing the approvals
// row layout (#133).
type Request struct {
	ApprovalID       string
	WorkItemID       string
	GateName         string
	Reviewers        []string
	DecisionDeadline time.Time
	// Tokens maps reviewer_id -> signed callback token (issued by A3).
	// Notifiers embed the matching token into each per-reviewer message.
	Tokens map[string]string
}

// newNotifyRequest is the canonical state.Approval → Request adapter
// (#133). Reviewers comes from a.ReviewerSetSnapshot.Reviewers so
// len(req.Reviewers) equals the reviewer_count attr in
// approval_events.payload_json — the byte-equality guarantee in spec
// §7. Slice is copied so a downstream notifier reordering its own
// working copy cannot mutate the snapshot the audit trail trusts.
func newNotifyRequest(a state.Approval, wi state.WorkItem, deadline time.Time, tokens map[string]string) Request {
	reviewers := append([]string(nil), a.ReviewerSetSnapshot.Reviewers...)
	return Request{
		ApprovalID:       a.ID,
		WorkItemID:       wi.ID,
		GateName:         a.GateName,
		Reviewers:        reviewers,
		DecisionDeadline: deadline,
		Tokens:           tokens,
	}
}

// Receipt records the channel's actual delivery for auditor
// reconciliation. On nil error DeliveredTo MUST contain the full
// req.Reviewers set (same multiset, order may differ).
type Receipt struct {
	DeliveredTo []string
	Channel     string
	ExternalID  string
}

// ErrUnknownNotifier — Registry.Get got an unregistered kind. Callers
// (and the regatta.yaml loader) MUST surface as a startup error rather
// than fall through to a default — silent fall-through to stub in
// production would silently drop approvals (spec §5.8 fail-closed).
var ErrUnknownNotifier = errors.New("approval: unknown notifier kind")

// ErrNoReviewers — Notifier.Notify got an empty req.Reviewers. The
// seam is fail-closed by design (spec §5.8): an approval pause with no
// reviewers stalls the work item forever with no audit trail, so the
// gate-tick aborts rather than emit a misleading "we notified"
// breadcrumb.
var ErrNoReviewers = errors.New("approval: zero reviewers in request")

// Registry is a fail-closed lookup from notifier kind to concrete
// adapter. Process-local; tests get a fresh Registry per case to avoid
// cross-test bleed.
type Registry struct {
	mu sync.RWMutex
	m  map[string]Notifier
}

// NewRegistry builds an empty registry. Callers register adapters at
// process start (or test start) before any gate Tick runs.
func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Notifier)}
}

// Register installs n under name. Errors on empty/duplicate — silent
// shadowing of an earlier adapter would be a hard-to-debug init race.
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

// Get returns the registered notifier for name. Missing kind wraps
// ErrUnknownNotifier with the offending name so logs show which
// config row was wrong while errors.Is still matches the sentinel.
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
// fail-closed contract for config-driven construction. Nothing in
// production may construct a stub on silent miss.
func Build(r *Registry, name string) (Notifier, error) {
	if r == nil {
		return nil, fmt.Errorf("approval: registry is nil")
	}
	return r.Get(name)
}
