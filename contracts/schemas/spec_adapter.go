// Package schemas defines the canonical interfaces a SpecAdapter must satisfy.
//
// This file is the normative contract referenced by docs/design.md §Spec contract.
// Two independent implementations of SpecAdapter must be interchangeable; the
// orchestrator does not reach behind the interface for any source-specific
// behavior.
//
// Status: draft v1. Breaking changes bump the major version; new optional
// methods bump the minor version. The wire protocol for the `custom` adapter
// is also versioned (see CustomAdapterRequest.Version).
package schemas

import (
	"context"
	"errors"
	"time"
)

// SpecAdapter is the abstraction over every supported source of planned work:
// github_issues, gitlab_issues, markdown_catalog, jira, linear, custom.
//
// Implementations MUST:
//   - Honor context cancellation; return ctx.Err() on timeout.
//   - Be idempotent: identical List() inputs return identical WorkItem slices
//     when the upstream source is unchanged.
//   - Handle pagination internally; List returns the full result set.
//   - Surface upstream rate-limit signals as ErrRateLimited wrapping a
//     RateLimitHint with the suggested retry time.
//   - Never mutate a WorkItem's AcceptanceCriteria[*].Text; L0 enforces this
//     at the diff layer, but adapters must not provide a write path either.
type SpecAdapter interface {
	// List returns every WorkItem the adapter is configured to surface.
	// Filtering by status, label, etc. is configured at adapter construction.
	List(ctx context.Context) ([]WorkItem, error)

	// Get fetches a single WorkItem by ID.
	Get(ctx context.Context, id WorkItemID) (WorkItem, error)

	// UpdateStatus transitions a WorkItem's top-level status field. The
	// citation is the human-readable evidence string (e.g. "test=TestFoo,
	// file=src/foo.go:42, commit=abcd1234").
	//
	// Implementations MUST be idempotent: calling UpdateStatus with the same
	// (id, status, citation) twice produces the same end state and never
	// errors. Status transitions other than planned→in_progress→done MUST
	// return ErrInvalidStatus.
	UpdateStatus(ctx context.Context, id WorkItemID, status Status, citation string) error

	// Capabilities reports the adapter's supported features. Used by the
	// orchestrator to enable/disable downstream behavior (e.g. webhook
	// subscription vs polling).
	Capabilities() Capabilities
}

// WorkItem is the canonical unit of work the fleet operates on.
// See schemas/work_item.schema.json for the JSON form (snake_case).
type WorkItem struct {
	ID                 WorkItemID   `json:"id"`
	Kind               WorkItemKind `json:"kind,omitempty"` // "feature" (default) | "program"; "program" routes through the planner before spawning
	Title              string       `json:"title"`
	Body               string       `json:"body,omitempty"`
	AcceptanceCriteria []Criterion  `json:"acceptance_criteria"`
	Dependencies       []WorkItemID `json:"dependencies,omitempty"` // topological order; cycles MUST be reported via ErrDependencyCycle on List
	Lane               LaneID       `json:"lane,omitempty"`         // empty string means the default lane
	Status             Status       `json:"status"`
	LinkedArtifact     string       `json:"linked_artifact,omitempty"` // URL or repo-relative path to deeper context (RFC, design doc, ADR)
	Source             SourceRef    `json:"source"`                    // points to the immutable source-of-truth for L0 to verify
}

// WorkItemKind discriminates leaf features from programs. Programs
// route through the planner before spawning. See docs/design.md
// §Programs.
type WorkItemKind string

// WorkItemKind values; the only legal payload of WorkItem.Kind.
const (
	KindFeature WorkItemKind = "feature"
	KindProgram WorkItemKind = "program"
)

// Criterion is one acceptance criterion under a WorkItem.
type Criterion struct {
	ID    string         `json:"id"`    // stable within a WorkItem; format adapter-defined
	Text  string         `json:"text"`  // immutable post-publication; L0 enforces byte-equality after UTF-8 NFC normalization
	State CriterionState `json:"state,omitempty"`
}

// WorkItemID uniquely identifies a WorkItem within a spec source.
type WorkItemID string

// LaneID names a lane that a WorkItem participates in.
type LaneID string

// Status names a WorkItem's lifecycle state.
type Status string

// Status values; the only legal payload of WorkItem.Status.
const (
	StatusPlanned    Status = "planned"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

// CriterionState names a Criterion's lifecycle state.
type CriterionState string

// CriterionState values; the only legal payload of Criterion.State.
const (
	CriterionPlanned    CriterionState = "planned"
	CriterionInProgress CriterionState = "in_progress"
	CriterionDone       CriterionState = "done"
)

// SourceRef is the immutable pointer L0 uses to verify a criterion has not
// been mutated. For file-backed adapters (markdown_catalog), Kind="file" and
// SHA is the commit SHA the WorkItem was read at. For API-backed adapters,
// Kind="issue"|"ticket" and SHA is an opaque ETag or version string.
type SourceRef struct {
	Kind    string `json:"kind"`    // "file" | "issue" | "ticket"
	Locator string `json:"locator"` // file path with line range, or external system ID
	SHA     string `json:"sha"`
}

// Capabilities reports per-adapter feature flags. Defaults are zero-valued
// (no extra capabilities).
type Capabilities struct {
	Webhook           bool          // adapter can push updates instead of polling
	BulkUpdate        bool          // adapter supports atomic multi-item UpdateStatus
	MinPollInterval   time.Duration // adapter's recommended floor when polling
	SupportedStatuses []Status      // adapters MAY support a subset (e.g. read-only)
}

// RateLimitHint travels with ErrRateLimited; the orchestrator uses RetryAfter
// to schedule the next poll without thrashing.
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

// ─────────────────────────────────────────────────────────────────────────────
// Custom-adapter wire protocol
// ─────────────────────────────────────────────────────────────────────────────
//
// The `custom` adapter shells out to an executable on PATH (specified by
// `regatta.yaml: spec_adapter.command`). Communication is JSON-over-stdio.
//
// Each invocation:
//   stdin:  one CustomAdapterRequest object (newline-terminated)
//   stdout: one CustomAdapterResponse object (newline-terminated)
//   stderr: free-form diagnostic text (logged but ignored for verdict)
//
// Exit codes:
//   0  success (response payload on stdout)
//   1  transient failure (ErrTransient; retried with exponential backoff)
//   2  permanent failure (ErrPermanent; surfaced to operator)
//   3  rate limited (ErrRateLimited; stderr MAY include "retry_after_seconds=N")
//   4  invalid request (operator misconfigured the adapter; treated as permanent)
//
// Default timeout: 30s. Configurable via `spec_adapter.timeout_seconds`.
// SIGTERM at timeout + 5s; SIGKILL at timeout + 10s.

// CustomAdapterRequest is the stdin payload sent to a `custom` adapter.
type CustomAdapterRequest struct {
	Version int    `json:"version"` // protocol version; current = 1
	Op      string `json:"op"`      // "list" | "get" | "update_status" | "capabilities"
	ID      string `json:"id,omitempty"`
	Status  string `json:"status,omitempty"`
	Citation string `json:"citation,omitempty"`
}

// CustomAdapterResponse is the stdout payload returned by a `custom` adapter.
type CustomAdapterResponse struct {
	Version int       `json:"version"`
	Items   []WorkItem `json:"items,omitempty"`
	Item    *WorkItem  `json:"item,omitempty"`
	Capabilities *Capabilities `json:"capabilities,omitempty"`
}
