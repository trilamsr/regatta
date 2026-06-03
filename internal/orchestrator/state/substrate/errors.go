// Package substrate ships the append-only signed event log primitive
// that collapses MVP-2's bespoke history tables (approval_events,
// work_item_outputs, per-agent events) into one canonical log. State
// for any wedge is fold(events WHERE kind=X) — never a row mutation.
// AppendEvent inserts; Fold reads. Spec:
// docs/engineer/specs/2026-06-01-unified-substrate-design.md.
package substrate

import "errors"

// ErrInvalidPayload — per-kind typed-payload validator rejected the
// payload (validator dispatch table in validate.go).
var ErrInvalidPayload = errors.New("substrate: invalid payload for kind")

// ErrReplay — the UNIQUE(run_id, written_by, nonce) index made replay
// structurally impossible at the DB layer; the error path lets callers
// distinguish writer-replay from generic INSERT failure.
var ErrReplay = errors.New("substrate: replay detected (run_id, written_by, nonce collision)")

// ErrSupersedesCycle — Kahn's topo-sort inside the insert tx caught a
// cycle in the same-run supersedes graph. Spec §8 R7.
var ErrSupersedesCycle = errors.New("substrate: supersedes graph would cycle")

// ErrClockRegression — sqlite CHECK can't see session state, so the
// e.WrittenAt < lastWrittenAt guard lives in AppendEvent under a
// sync.Mutex. Spec §8 I2.
var ErrClockRegression = errors.New("substrate: clock regression (written_at < lastWrittenAt)")

// ErrTenantRequired — schema NOT NULL accepts the empty string; the
// Go-level DefaultTenantID is the explicit, auditable default.
var ErrTenantRequired = errors.New("substrate: tenant_id required (use DefaultTenantID for single-tenant)")

// ErrUnverifiable wraps contracts/schemas.ErrUnverifiable so callers
// can errors.Is at the substrate boundary without importing schemas.
var ErrUnverifiable = errors.New("substrate: signature unverifiable")

// DefaultTenantID is the explicit per-process constant for single-
// tenant deployments — spec R3 forbids a SQL DEFAULT on tenant_id.
const DefaultTenantID = "default"
