// Package substrate ships the append-only signed event log primitive
// that collapses MVP-2's bespoke history tables (approval_events,
// work_item_outputs, per-agent events) into one canonical log.
//
// Wave 1 lands Phase A (dark — no writers in production, no readers).
// Phase B (Wave 2) wires shadow-writes; Phase C flips reads; Phase D
// drops the legacy tables. Source-of-truth spec:
// docs/engineer/specs/2026-06-01-unified-substrate-design.md.
//
// State for any wedge is fold(events WHERE kind=X) — never a row
// mutation. AppendEvent inserts; Fold reads.
package substrate

import "errors"

// ErrInvalidPayload is returned by AppendEvent (or Verify) when the
// per-kind typed-payload validator rejects e.PayloadJSON. The dispatch
// table lives in validate.go; per-kind validators register via init()
// using RegisterPayloadValidator.
var ErrInvalidPayload = errors.New("substrate: invalid payload for kind")

// ErrReplay is returned by AppendEvent when (run_id, written_by, nonce)
// collides with an existing row. The UNIQUE index makes replay
// structurally impossible at the DB layer; the error path lets callers
// distinguish "the writer already wrote this" from generic INSERT failure.
var ErrReplay = errors.New("substrate: replay detected (run_id, written_by, nonce collision)")

// ErrSupersedesCycle is returned by AppendEvent when inserting the new
// event would close a cycle in the same-run supersedes graph. Kahn's
// topo-sort runs inside the insert tx; cycle ⇒ rollback. Spec §8 R7.
var ErrSupersedesCycle = errors.New("substrate: supersedes graph would cycle")

// ErrClockRegression is returned by AppendEvent when e.WrittenAt is
// less than the process-local lastWrittenAt watermark. sqlite CHECK
// constraints don't see session state, so the guard lives in
// AppendEvent under a sync.Mutex. Spec §8 I2.
var ErrClockRegression = errors.New("substrate: clock regression (written_at < lastWrittenAt)")

// ErrTenantRequired is returned by AppendEvent when e.TenantID is
// empty. The schema enforces NOT NULL but a zero-length string would
// satisfy that — the Go-level constant substrate.DefaultTenantID is
// the explicit code default; empty is never a writer's choice.
var ErrTenantRequired = errors.New("substrate: tenant_id required (use DefaultTenantID for single-tenant)")

// ErrUnverifiable wraps signature-mismatch failures from Verify. The
// underlying schemas.ErrUnverifiable from contracts/schemas is the
// shared sentinel; substrate.ErrUnverifiable is the package-local view
// returned to callers who errors.Is at the substrate boundary.
var ErrUnverifiable = errors.New("substrate: signature unverifiable")

// DefaultTenantID is the per-process constant writers use on single-
// tenant deployments. Spec R3 forbids a SQL DEFAULT on tenant_id;
// the explicit code default is auditable.
const DefaultTenantID = "default"
