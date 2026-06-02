package approval

import (
	"context"
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
	// EventKindTokenMinted marks one approval_events row per JTI the
	// gate hands to a reviewer. Reaper.outstandingJTIs reads these rows
	// to decide which tokens to revoke on escalate (spec §3.3.1.3).
	// Issue #195: until the gate writes these, reaper revocation is
	// unreachable code.
	EventKindTokenMinted = "token_minted"
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
// would silently drop half the audit signal in production. DB may be
// nil for slog-only emission (e.g. structured pre-tx breadcrumb), but
// the canonical use is "both at once" — the byte-equality invariant.
type recordEventOpts struct {
	DB         *state.DB
	Logger     *slog.Logger
	ApprovalID string
	Event      obs.EventName
	Kind       string
	Actor      string
	Now        time.Time
	Attrs      map[string]any
	TokenJTI   string
}

// recordEvent is the single helper described in spec §5.7. Both surfaces
// receive the same canonical-JSON payload bytes so a property test can
// assert byte-equality; drift between slog and approval_events is
// physically impossible because both reads are the same string.
func recordEvent(ctx context.Context, o recordEventOpts) error {
	payload, err := canonicalisePayload(o.Attrs)
	if err != nil {
		return fmt.Errorf("approval: canonicalise audit payload: %w", err)
	}

	if o.Logger != nil {
		o.Logger.LogAttrs(ctx, slog.LevelInfo, string(o.Event),
			slog.String(string(obs.KeyApprovalID), o.ApprovalID),
			slog.String(string(obs.KeyAuditPayload), string(payload)),
		)
	}

	if o.DB != nil {
		ev := state.ApprovalEvent{
			ApprovalID: o.ApprovalID,
			Ts:         o.Now,
			Kind:       o.Kind,
			Actor:      o.Actor,
			Payload:    payload,
			TokenJTI:   o.TokenJTI,
		}
		if err := o.DB.AppendApprovalEvent(ctx, ev); err != nil {
			return fmt.Errorf("approval: append audit event: %w", err)
		}
	}
	return nil
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
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
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
