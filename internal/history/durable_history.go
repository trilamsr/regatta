// Package history exports DurableHistory — the typed interface
// wrapping the substrate event log (spec
// docs/engineer/specs/2026-06-02-s2-t1-w9-substrate-impl.md §3). Wave-1
// ships Append; Tail and Replay are reserved Phase X (§1 OUT + §11
// F1/F2/F3) — the substrate impl returns ErrUnsupported until then so
// callers fail loud rather than silently observe nil channels.
package history

import (
	"context"
	"io"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// DurableHistory wraps substrate's append-only event log so the Phase X
// Temporal-backed impl drops in without an API break.
type DurableHistory interface {
	Append(ctx context.Context, runID string, ev substrate.Event) error
	Tail(ctx context.Context, runID string, since string) (<-chan substrate.Event, io.Closer, error)
	Replay(ctx context.Context, runID string, opts ReplayOpts) (<-chan ReplayedEvent, io.Closer, error)
}

// ReplayOpts scopes a Replay call. FromNodeID and PinOverride are
// reserved Phase X fields (spec §11 F2/F3) — v1 rejects non-zero
// values with ErrUnsupported.
type ReplayOpts struct {
	TenantID     string
	IncludeKinds []substrate.EventKind
	FromNodeID   string
	PinOverride  PinSet
}

// PinSet pins re-execution inputs for edit-and-replay (Phase X F3).
// Zero value means "no pins".
type PinSet struct {
	ModelID   string
	Seed      int64
	PromptSHA string
}

// ReplayedEvent pairs an original event with its replayed counterpart
// and the structural diff verdict.
type ReplayedEvent struct {
	Original substrate.Event
	Replayed substrate.Event
	Diff     DiffResult
}

// DiffResult carries the structural-diff verdict per replayed event.
// DivergentKeys are JSON-path strings (spec §4).
type DiffResult struct {
	Verdict       DiffVerdict
	Reason        string
	DivergentKeys []string
}

// DiffVerdict enumerates the three terminal outcomes per replayed event.
type DiffVerdict string

// Match / Divergent / ReplaySkipped are the three terminal verdicts.
const (
	Match         DiffVerdict = "match"          // canonical-JSON byte-equal.
	Divergent     DiffVerdict = "divergent"      // ≥1 JSON-path key disagreement.
	ReplaySkipped DiffVerdict = "replay_skipped" // non-deterministic or quarantine-listed kind.
)
