package history

import "errors"

// ErrCrossTenant is returned by Replay when a folded event's TenantID
// disagrees with opts.TenantID. Defence in depth per parent design R7
// — single-tenant self-host still ships the guard so the Phase X
// multi-tenant cutover does not have to retrofit it.
var ErrCrossTenant = errors.New("history: cross-tenant replay rejected")

// ErrUnsupported is returned by Tail, Replay, and any reserved
// ReplayOpts field (FromNodeID, PinOverride) — all Phase X. v1
// surfaces this so callers fail loud rather than silently get nil
// channels.
var ErrUnsupported = errors.New("history: unsupported in Wave 1 (Phase X)")

// ErrRunIDMismatch is returned by Append when ev.RunID disagrees with
// the runID argument. Same root-cause rule as substrate.ErrTenantRequired
// — the argument is the contract; a writer-supplied conflict is a bug,
// not a silent override.
var ErrRunIDMismatch = errors.New("history: ev.RunID disagrees with runID argument")
