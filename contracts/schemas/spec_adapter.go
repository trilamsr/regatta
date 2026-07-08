// Package schemas defines the canonical SpecAdapter contract referenced
// by docs/design.md §Spec contract. Two independent implementations
// must be interchangeable; the orchestrator never reaches behind the
// interface for source-specific behavior. Breaking changes bump major;
// new optional methods bump minor. The custom-adapter wire protocol is
// independently versioned (see CustomAdapterRequest.Version).
package schemas

import (
	"context"
	"errors"
	"time"
)

// SpecAdapter abstracts every supported source of planned work
// (github_issues, markdown_catalog, linear). Implementations MUST honor context cancellation, be
// idempotent across identical inputs, paginate internally, surface
// rate-limit signals as ErrRateLimited wrapping RateLimitHint, and
// never expose a write path for Criterion.Text (L0 enforces immutable
// acceptance-criteria text at the diff layer).
type SpecAdapter interface {
	List(ctx context.Context) ([]WorkItem, error)

	Get(ctx context.Context, id WorkItemID) (WorkItem, error)

	// UpdateStatus transitions a WorkItem's top-level status.
	// Citation is human-readable evidence (e.g.
	// "test=TestFoo,file=src/foo.go:42,commit=abcd1234").
	// MUST be idempotent on identical (id, status, citation). Targets
	// outside the Status enum MUST return ErrInvalidStatus.
	UpdateStatus(ctx context.Context, id WorkItemID, status Status, citation string) error

	Capabilities() Capabilities
}

// WorkItem is the canonical unit of work the fleet operates on.
// See schemas/work_item.schema.json for the JSON (snake_case) form.
type WorkItem struct {
	ID                 WorkItemID   `json:"id"`
	Kind               WorkItemKind `json:"kind,omitempty"` // "feature" (default) | "program" routes through planner
	Title              string       `json:"title"`
	Body               string       `json:"body,omitempty"`
	AcceptanceCriteria []Criterion  `json:"acceptance_criteria"`
	Dependencies       []WorkItemID `json:"dependencies,omitempty"` // topological order; cycles MUST surface as ErrDependencyCycle on List
	Lane               LaneID       `json:"lane,omitempty"`         // empty = default lane
	Status             Status       `json:"status"`
	LinkedArtifact     string       `json:"linked_artifact,omitempty"`
	Source             SourceRef    `json:"source"` // L0 uses this to verify immutability
}

// WorkItemKind discriminates leaf features from programs; programs
// route through the planner before spawning (docs/design.md §Programs).
type WorkItemKind string

// WorkItemKind values; the only legal payload of WorkItem.Kind.
const (
	KindFeature WorkItemKind = "feature"
	KindProgram WorkItemKind = "program"
)

// Criterion is one acceptance criterion under a WorkItem; Text is
// immutable post-publication (L0 enforces byte-equality after UTF-8
// NFC normalization).
type Criterion struct {
	ID    string         `json:"id"`
	Text  string         `json:"text"`
	State CriterionState `json:"state,omitempty"`
}

// WorkItemID uniquely identifies a WorkItem within a spec source.
type WorkItemID string

// LaneID names a lane a WorkItem participates in.
type LaneID string

// Status names a WorkItem's lifecycle state.
type Status string

// Status values; the only legal payload of WorkItem.Status.
// StatusClosedResolved is terminal-via-supersession (e.g. consolidated
// into a parent brief), distinct from StatusDone (agent reached done);
// adaptersync filters both out the same — terminal never re-enqueues.
const (
	StatusPlanned        Status = "planned"
	StatusInProgress     Status = "in_progress"
	StatusDone           Status = "done"
	StatusClosedResolved Status = "closed-resolved"
)

// CriterionState names a Criterion's lifecycle state.
type CriterionState string

// CriterionClosed mirrors StatusClosedResolved at the bullet level —
// terminal via supersession, not via the agent reaching done.
const (
	CriterionPlanned    CriterionState = "planned"
	CriterionInProgress CriterionState = "in_progress"
	CriterionDone       CriterionState = "done"
	CriterionClosed     CriterionState = "closed"
)

// SourceRef is the immutable pointer L0 uses to verify a criterion has
// not been mutated. File-backed: Kind="file", SHA=commit SHA at read.
// API-backed: Kind="issue"|"ticket", SHA=opaque ETag/version string.
type SourceRef struct {
	Kind    string `json:"kind"`
	Locator string `json:"locator"`
	SHA     string `json:"sha"`
}

// Capabilities reports per-adapter feature flags; zero value = no
// extra capabilities.
type Capabilities struct {
	Webhook           bool
	BulkUpdate        bool
	MinPollInterval   time.Duration
	SupportedStatuses []Status
}

// RateLimitHint travels with ErrRateLimited; the orchestrator uses
// RetryAfter to schedule the next poll without thrashing.
type RateLimitHint struct {
	RetryAfter time.Duration
	Reset      time.Time
}

// Sentinel errors. Adapters MUST wrap these via fmt.Errorf("%w", ...).
var (
	ErrNotFound          = errors.New("regatta: work item not found")
	ErrRateLimited       = errors.New("regatta: rate limited")
	ErrInvalidStatus     = errors.New("regatta: invalid status transition")
	ErrSourceMutated     = errors.New("regatta: source mutated since last read")
	ErrDependencyCycle   = errors.New("regatta: dependency cycle detected")
	ErrTransient         = errors.New("regatta: transient adapter failure")
	ErrPermanent         = errors.New("regatta: permanent adapter failure")
	ErrAdapterUnsupported = errors.New("regatta: operation unsupported by this adapter")
)

// Custom-adapter wire protocol: shells out to an executable on PATH
// (regatta.yaml: spec_adapter.command). JSON-over-stdio, one
// request/response per invocation, newline-terminated. Exit codes:
//   0 success / 1 ErrTransient (retried w/ backoff) / 2 ErrPermanent
//   3 ErrRateLimited (stderr MAY carry "retry_after_seconds=N")
//   4 invalid request (treated as permanent — operator misconfig).
// Timeout default 30s (spec_adapter.timeout_seconds);
// SIGTERM at +5s, SIGKILL at +10s.

// CustomAdapterRequest is the stdin payload sent to a `custom` adapter.
type CustomAdapterRequest struct {
	Version  int    `json:"version"` // protocol version; current = 1
	Op       string `json:"op"`      // "list" | "get" | "update_status" | "capabilities"
	ID       string `json:"id,omitempty"`
	Status   string `json:"status,omitempty"`
	Citation string `json:"citation,omitempty"`
}

// CustomAdapterResponse is the stdout payload returned by a `custom` adapter.
type CustomAdapterResponse struct {
	Version      int           `json:"version"`
	Items        []WorkItem    `json:"items,omitempty"`
	Item         *WorkItem     `json:"item,omitempty"`
	Capabilities *Capabilities `json:"capabilities,omitempty"`
}
