package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// Event kinds for approval_events.kind — single source of truth so the
// fold's switch table and the gate's append site cannot drift.
const (
	EventKindRequested   = "requested"
	EventKindNotified    = "notified"
	EventKindDecided     = "decided"
	EventKindApproved    = "approved"
	EventKindRejected    = "rejected"
	EventKindTimedOut    = "timed_out"
	EventKindEscalated   = "escalated"
	// EventKindTokenMinted marks one approval_events row per JTI the
	// gate hands to a reviewer. Reaper.outstandingJTIs reads these rows
	// to decide which tokens to revoke on escalate (spec §3.3.1.3).
	// Issue #195: until the gate writes these, reaper revocation is
	// unreachable code.
	EventKindTokenMinted   = "token_minted"
	EventKindTokenConsumed = "token_consumed"
)

// Decision payload values per spec §3.2 schema.
const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
)

// ActorOrchestrator stamps audit rows written by the gate itself
// rather than a human reviewer; keeps the audit-trail searchable.
const ActorOrchestrator = "orchestrator"

// recordEventOpts bundles the fan-out target so a single helper can
// write both surfaces (spec §5.7). Logger is REQUIRED; a nil logger
// would silently drop half the audit signal in production. Exactly one
// of DB or Tx may be set: DB opens its own connection (gate first-
// sighting + decide post-commit breadcrumb), Tx threads the write
// through the caller's open transaction so reaper's per-row tx keeps
// the §3.2.1 all-or-nothing atomicity contract. Both nil is allowed
// (slog-only emission); both set is a programmer error and surfaces
// via ErrRecordEventBothSinks.
type recordEventOpts struct {
	DB         *state.DB
	Tx         *sql.Tx
	Logger     *slog.Logger
	ApprovalID string
	Event      obs.EventName
	Kind       string
	Actor      string
	Now        time.Time
	Attrs      map[string]any
	TokenJTI   string
}

// ErrRecordEventBothSinks fires when a caller passes both DB and Tx —
// the helper would otherwise double-write the row, breaking the byte-
// equality contract (the slog payload is emitted once but the DB row
// would exist twice). Typed sentinel so the misuse surfaces as a test
// failure rather than a string-formatted error.
var ErrRecordEventBothSinks = fmt.Errorf("approval: recordEvent requires DB xor Tx, not both")

// recordEvent is the single helper described in spec §5.7. Both surfaces
// receive the same canonical-JSON payload bytes so a property test can
// assert byte-equality; drift between slog and approval_events is
// physically impossible because both reads are the same string.
//
// Tx-mode (reaper, #146): when Tx is non-nil, the row insert threads
// through state.InsertApprovalEvent so the slog emission + the row
// append + the caller's other tx mutations all commit together. The
// canonical-JSON payload bytes are identical in both modes — the only
// difference is which executor runs the INSERT.
func recordEvent(ctx context.Context, o recordEventOpts) error {
	if o.DB != nil && o.Tx != nil {
		return ErrRecordEventBothSinks
	}
	payload, err := canonicalisePayload(o.Attrs)
	if err != nil {
		return fmt.Errorf("approval: canonicalise audit payload: %w", err)
	}

	if o.Logger != nil {
		// Emit the canonical audit_payload (spec §5.7 byte-equality with
		// the row) PLUS each Attrs entry as a top-level slog attribute so
		// operator dashboards filter on individual keys (e.g. policy=fail)
		// without parsing audit_payload. The two surfaces stay in sync
		// because both derive from the same Attrs map.
		attrs := make([]slog.Attr, 0, 2+len(o.Attrs))
		attrs = append(attrs,
			slog.String(string(obs.KeyApprovalID), o.ApprovalID),
			slog.String(string(obs.KeyAuditPayload), string(payload)),
		)
		for _, k := range sortedKeys(o.Attrs) {
			attrs = append(attrs, slog.Any(k, o.Attrs[k]))
		}
		o.Logger.LogAttrs(ctx, slog.LevelInfo, string(o.Event), attrs...)
	}

	ev := state.ApprovalEvent{
		ApprovalID: o.ApprovalID,
		Ts:         o.Now,
		Kind:       o.Kind,
		Actor:      o.Actor,
		Payload:    payload,
		TokenJTI:   o.TokenJTI,
	}
	switch {
	case o.Tx != nil:
		if err := InsertApprovalEvent(ctx, o.Tx, ev); err != nil {
			return fmt.Errorf("approval: append audit event (tx): %w", err)
		}
	case o.DB != nil:
		if err := o.DB.AppendApprovalEvent(ctx, ev); err != nil {
			return fmt.Errorf("approval: append audit event: %w", err)
		}
	}
	return nil
}

// sortedKeys returns map keys in lex order. Shared by canonicalisePayload
// (deterministic JSON bytes) and recordEvent's slog-attr fan-out so the
// per-key emission order stays stable across runs — non-deterministic
// slog emission would otherwise be a needless source of test flake.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// canonicalisePayload emits keys in lex-sorted order so byte-equality
// between the slog attr and the DB row is deterministic. The repo's
// canon.CanonicaliseJSON does the same job for token bodies; using
// the same shape here keeps the two audit-trail formats parsed by
// the same downstream tooling.
func canonicalisePayload(attrs map[string]any) ([]byte, error) {
	if len(attrs) == 0 {
		return []byte("{}"), nil
	}
	keys := sortedKeys(attrs)
	var buf []byte
	buf = append(buf, '{')
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf = append(buf, kb...)
		buf = append(buf, ':')
		vb, err := json.Marshal(attrs[k])
		if err != nil {
			return nil, err
		}
		buf = append(buf, vb...)
	}
	buf = append(buf, '}')
	return buf, nil
}
