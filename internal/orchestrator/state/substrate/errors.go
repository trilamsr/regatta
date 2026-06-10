// Package substrate ships the append-only signed event log primitive
// that collapses MVP-2's bespoke history tables (approval_events,
// work_item_outputs, per-agent events) into one canonical log. State
// for any wedge is fold(events WHERE kind=X) — never a row mutation.
// Spec: docs/engineer/specs/phase-x/2026-06-01-unified-substrate-design.md.
package substrate

import (
	"errors"
	"fmt"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// ErrInvalidPayload signals the per-kind typed-payload validator rejected the payload (dispatch in validate.go).
var ErrInvalidPayload = errors.New("substrate: invalid payload for kind")

// ErrReplay signals UNIQUE(run_id, written_by, nonce) caught a replay at the DB layer.
var ErrReplay = errors.New("substrate: replay detected (run_id, written_by, nonce collision)")

// ErrSupersedesCycle signals Kahn's topo-sort inside the insert tx caught a cycle (spec §8 R7).
var ErrSupersedesCycle = errors.New("substrate: supersedes graph would cycle")

// ErrClockRegression guards e.WrittenAt < lastWrittenAt under sync.Mutex — sqlite CHECK can't see session state (spec §8 I2).
var ErrClockRegression = errors.New("substrate: clock regression (written_at < lastWrittenAt)")

// ErrTenantRequired signals tenant_id="" — schema NOT NULL accepts empty strings, so DefaultTenantID is the explicit fallback.
var ErrTenantRequired = errors.New("substrate: tenant_id required (use DefaultTenantID for single-tenant)")

// ErrUnverifiable wraps schemas.ErrUnverifiable so callers can errors.Is at the substrate boundary without importing schemas.
var ErrUnverifiable = fmt.Errorf("%w: substrate signature unverifiable", schemas.ErrUnverifiable)

// DefaultTenantID is the explicit per-process constant for single-tenant deployments — spec R3 forbids a SQL DEFAULT on tenant_id.
const DefaultTenantID = "default"
