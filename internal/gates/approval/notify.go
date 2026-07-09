// Package approval implements the HITL approval-gate DAG node (spec §3.1) — pause, notify the reviewer set, resume on signed-token callback. This file declares the notification-adapter seam (§5.8): interface + fail-closed registry + stub notifier recording audit-trail intent.
package approval

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Notifier is the channel-agnostic surface gates call to deliver a
// pending-approval message. Conformance invariants
// (TestNotifier_InterfaceContract, spec §5.8, §7): (1) nil error → len(Receipt.DeliveredTo)==len(req.Reviewers) so partial fan-out cannot drop a reviewer silently; (2) emits ≥1 obs record with the canonical four attrs (KeyApprovalID, KeyWorkItemID, KeyGateID, KeyReviewerCount); (3) empty Reviewers returns ErrNoReviewers WITHOUT an audit record (else gate stalls forever); (4) cancelled/expired ctx surfaces ctx.Err() before any external side effect or audit emission. Impls must be concurrent-safe and not block past their own transport timeout.
type Notifier interface {
	// Kind returns the registry key matching regatta.yaml channel config; mismatch fails at startup.
	Kind() string
	Notify(ctx context.Context, req Request) (Receipt, error)
}

// Request carries the minimum primitives a notifier needs to render an approval message and resume the gate — primitive shape (vs state.Approval) decouples adapters from the DB schema (#133).
type Request struct {
	ApprovalID       string
	WorkItemID       string
	GateName         string
	Reviewers        []string
	DecisionDeadline time.Time
	// Tokens maps reviewer_id -> signed callback token (issued by A3); notifiers embed the matching token into each per-reviewer message.
	Tokens map[string]string
}

// newNotifyRequest is the canonical state.Approval → Request adapter (#133); Reviewers is copy-on-read from ReviewerSetSnapshot so len() matches reviewer_count in approval_events.payload_json (spec §7) and a downstream reorder cannot mutate the audit-trail snapshot.
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

// Receipt records the channel's actual delivery for auditor reconciliation; on nil error DeliveredTo MUST contain the full req.Reviewers set (same multiset, order may differ).
type Receipt struct {
	DeliveredTo []string
	Channel     string
	ExternalID  string
}

// ErrUnknownNotifier signals Registry.Get got an unregistered kind; callers (and the regatta.yaml loader) MUST surface as a startup error rather than fall through to a default — silent stub fall-through in production would silently drop approvals (spec §5.8 fail-closed).
var ErrUnknownNotifier = errors.New("approval: unknown notifier kind")

// ErrNoReviewers signals Notifier.Notify got an empty req.Reviewers; the seam is fail-closed by design (spec §5.8) because an approval pause with no reviewers stalls the work item forever with no audit trail.
var ErrNoReviewers = errors.New("approval: zero reviewers in request")

// Registry is a fail-closed lookup from notifier kind to adapter; process-local so tests get a fresh Registry per case to avoid cross-test bleed.
type Registry struct {
	mu sync.RWMutex
	m  map[string]Notifier
}

// NewRegistry builds an empty registry; callers register adapters at process start (or test start) before any gate Tick runs.
func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Notifier)}
}

// Register installs n under name; errors on empty/duplicate so silent shadowing of an earlier adapter cannot become a hard-to-debug init race.
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

// Get returns the registered notifier for name; missing kind wraps ErrUnknownNotifier with the offending name so logs show which config row was wrong while errors.Is still matches the sentinel.
func (r *Registry) Get(name string) (Notifier, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.m[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownNotifier, name)
	}
	return n, nil
}

// Build resolves a notifier kind via the registry and enforces the fail-closed contract — nothing in production may construct a stub on silent miss.
func Build(r *Registry, name string) (Notifier, error) {
	if r == nil {
		return nil, fmt.Errorf("approval: registry is nil")
	}
	return r.Get(name)
}
