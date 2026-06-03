package substrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// EventKind enumerates the substrate's append-only event channels.
// SQL CHECK on substrate_events.kind pins the same set
// (TestSubstrate_EventKindEnumMatchesSQLCheck enforces parity).
// Spec S3: locks have mutable lifecycle and stay on the locks table.
type EventKind string

const (
	// KindNodeOutput replaces work_item_outputs (lww per spec §4).
	KindNodeOutput EventKind = "node_output"
	// KindFact — blackboard fact write (W11; lww default).
	KindFact EventKind = "fact"
	// KindApprovalEvent — append; replaces approval_events.
	KindApprovalEvent EventKind = "approval_event"
	// KindTokenSpend — one LLM-call spend record (append; budget = SUM).
	KindTokenSpend EventKind = "token_spend"
	// KindBudgetReconciled — Anthropic Usage API cron output (lww).
	KindBudgetReconciled EventKind = "budget_reconciled"
	// KindGateVerdict — signed CELDecider output (append; T-S2 owns
	// payload validator + producer).
	KindGateVerdict EventKind = "gate_verdict"
	// KindHeartbeat — running-agent liveness (lww per work_item_id).
	KindHeartbeat EventKind = "heartbeat"
	// KindBriefRejected — durable audit row for every brief the loader
	// rejects (issue #80, replaces slog-only retention so months-long
	// rejection history stays under the HMAC chain).
	KindBriefRejected EventKind = "brief_rejected"
	// KindPRStageTransition — OBS-WAVE-C-T2 PR lifecycle stage change.
	// Carries {pr_number, from_stage, to_stage, duration_seconds};
	// HMAC chain is the long-term audit trail, the counter is the
	// operational signal. Append is async (channel-batched) to keep
	// the PR-watch hot path off substrate Append latency.
	KindPRStageTransition EventKind = "pr_stage_transition"
	// KindManualMerge — GreenClock reset signal: an operator merged a
	// PR bypassing the autonomous path (#659). Append; reducer scans
	// the day bucket for a non-zero count to break the streak.
	KindManualMerge EventKind = "manual_merge"
	// KindOperatorIntervention — GreenClock reset signal: an operator
	// touched the loop outside the normal merge path (rollback, force
	// rebase, branch-protection override). Append; same streak-break
	// semantics as KindManualMerge (#659).
	KindOperatorIntervention EventKind = "operator_intervention"
	// KindCostCapThrottled — W5 cost-cap autonomic-enforcement transition
	// to Throttled (#622 substrate schema parity for forward-port).
	// Append; one row per Active→Throttled transition (the partial
	// UNIQUE index on the legacy `events` table de-dupes the
	// two-scheduler race per #652).
	KindCostCapThrottled EventKind = "cost_cap_throttled"
	// KindCostCapResumed — W5 cost-cap operator-override audit (#622).
	// Append; one row per `regatta resume` invocation.
	KindCostCapResumed EventKind = "cost_cap_resumed"
)

// AllKinds returns the canonical kind list in declaration order —
// used by the enum-parity test (T-S3) and reducer.go's default table.
func AllKinds() []EventKind {
	return []EventKind{
		KindNodeOutput, KindFact, KindApprovalEvent, KindTokenSpend,
		KindBudgetReconciled, KindGateVerdict, KindHeartbeat,
		KindBriefRejected, KindPRStageTransition,
		KindManualMerge, KindOperatorIntervention,
		KindCostCapThrottled, KindCostCapResumed,
	}
}

// Event is one row in substrate_events. Append-only; state for any
// wedge is fold(events WHERE kind=X). Callers DO NOT set SigAlg /
// SigKeyID / SigMAC — AppendEvent populates them via Sign().
//
// PayloadJSON must be ≤ 1024 bytes (sqlite CHECK enforces) and must
// typed-unmarshal against the per-kind validator registered in
// validate.go.
type Event struct {
	ID            string
	RunID         string
	WorkItemID    string
	TenantID      string
	TraceID       string
	SpanID        string
	Kind          EventKind
	Key           string
	PayloadJSON   json.RawMessage
	BlobDigest    string
	Supersedes    string
	WrittenBy     string
	WrittenAt     int64 // unix milliseconds UTC
	SchemaVersion int
	Nonce         string

	// Signature fields. Populated by AppendEvent's sign step; callers
	// must leave them zero on input.
	SigAlg   string
	SigKeyID string
	SigMAC   string
}

// clockMu guards lastWrittenAt for the process-local monotonicity
// check per spec §8 I2. The mutex scope is the AppendEvent critical
// section; this is intentionally process-local — multi-host writers
// are out of scope until W9 (UUIDv7 follow-up F6).
//
// Test seam: ResetClockForTesting() lets the test suite reset state
// between cases without exporting the mutex itself.
var (
	clockMu       sync.Mutex
	lastWrittenAt int64
)

// ResetClockForTesting resets the process-local monotonicity watermark.
// Test-only; callers in production paths SHOULD NOT use this — the
// watermark exists to catch clock regression.
func ResetClockForTesting() {
	clockMu.Lock()
	lastWrittenAt = 0
	clockMu.Unlock()
}

// AppendEvent inserts one row and verifies invariants in this order:
//  1. tenant/kind/payload validation (per-kind dispatch in validate.go).
//  2. process-local clock-regression check (spec §8 I2).
//  3. HMAC sign (mutates e.Sig* in-place; spec §5).
//  4. supersedes FK + Kahn's cycle-check restricted to run_id (spec §8 R7).
//  5. INSERT; UNIQUE(run_id, written_by, nonce) collision ⇒ ErrReplay.
//
// Ordering rationale: validate before sign so a malformed payload
// never costs an HMAC pass. Sign before cycle-check so a cycle rollback
// never wastes a Kahn's-sort over the run. Cycle-check before INSERT
// so a cycle never wastes a sqlite write. ErrReplay surfaces only
// after the row would have been written — the UNIQUE collision IS the
// detection mechanism.
func AppendEvent(ctx context.Context, tx *sql.Tx, e Event, key []byte, keyID string) error {
	if e.TenantID == "" {
		return ErrTenantRequired
	}
	if err := validatePayload(e.Kind, e.PayloadJSON); err != nil {
		return err
	}

	clockMu.Lock()
	if e.WrittenAt < lastWrittenAt {
		clockMu.Unlock()
		return fmt.Errorf("%w: got %d < watermark %d", ErrClockRegression, e.WrittenAt, lastWrittenAt)
	}
	// Bump watermark only on a successful insert path; defer the
	// monotonicity commit until after the row lands. Hold the mutex
	// for the whole AppendEvent body to serialize concurrent writers
	// against the watermark — sqlite's single-writer pool already
	// serializes at the SQL layer, so the lock cost is amortized.
	defer clockMu.Unlock()

	if err := Sign(&e, key, keyID); err != nil {
		return fmt.Errorf("substrate: sign: %w", err)
	}

	if e.Supersedes != "" {
		if err := cycleCheck(ctx, tx, e.RunID, e.ID, e.Supersedes); err != nil {
			return err
		}
	}

	_, err := tx.ExecContext(ctx,
		`INSERT INTO substrate_events
		   (id, run_id, work_item_id, tenant_id, trace_id, span_id,
		    kind, key, payload_json, blob_digest, supersedes,
		    written_by, written_at, schema_version, nonce,
		    sig_alg, sig_key_id, sig_mac)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.RunID, nullableString(e.WorkItemID), e.TenantID,
		e.TraceID, e.SpanID, string(e.Kind), e.Key,
		payloadOrEmpty(e.PayloadJSON), e.BlobDigest, nullableString(e.Supersedes),
		e.WrittenBy, e.WrittenAt, e.SchemaVersion, e.Nonce,
		e.SigAlg, e.SigKeyID, e.SigMAC,
	)
	if err != nil {
		// modernc.org/sqlite surfaces UNIQUE violations as a generic
		// error string; match on the substring sqlite emits. The
		// alternative (PRAGMA-driven extended error codes) requires a
		// driver-specific cast we'd rather not bind to.
		if isUniqueNonceViolation(err) {
			return fmt.Errorf("%w: %w", ErrReplay, err)
		}
		return fmt.Errorf("substrate: insert: %w", err)
	}

	if e.WrittenAt > lastWrittenAt {
		lastWrittenAt = e.WrittenAt
	}
	// T1 (OBS Wave-B): one atomic counter increment per appended row.
	// Bumped post-INSERT so rejected rows do not double-count the
	// validator's bounce path. Tag set is the closed-enum guarded
	// `kind` only — `layer` is rolled up at Prom-query time from the
	// recorded kind (cardinality-bounded by AllKinds()).
	recordEventAppended(ctx, e.Kind)
	return nil
}

// AppendEventCanonicalized is the F1 fast-path twin of AppendEvent for
// hot writers (token_spend, etc.) that pre-canonicalise the payload
// once and reuse the bytes across many rows. Signs via SignCanonicalized
// (skips the slow CanonicalJSON round-trip); all other invariants —
// validate, clock, supersedes cycle-check, replay, metric — are
// identical to AppendEvent. Spec §7 + §12 F1 (#216).
//
// preCanonPayload MUST be canon.CanonicaliseJSON(e.PayloadJSON); the
// contract is caller-trusted (Verify catches drift downstream). Cold
// callers should keep using AppendEvent.
func AppendEventCanonicalized(ctx context.Context, tx *sql.Tx, e Event, preCanonPayload []byte, key []byte, keyID string) error {
	if e.TenantID == "" {
		return ErrTenantRequired
	}
	if err := validatePayload(e.Kind, e.PayloadJSON); err != nil {
		return err
	}

	clockMu.Lock()
	if e.WrittenAt < lastWrittenAt {
		clockMu.Unlock()
		return fmt.Errorf("%w: got %d < watermark %d", ErrClockRegression, e.WrittenAt, lastWrittenAt)
	}
	defer clockMu.Unlock()

	if err := SignCanonicalized(&e, preCanonPayload, key, keyID); err != nil {
		return fmt.Errorf("substrate: sign: %w", err)
	}

	if e.Supersedes != "" {
		if err := cycleCheck(ctx, tx, e.RunID, e.ID, e.Supersedes); err != nil {
			return err
		}
	}

	_, err := tx.ExecContext(ctx,
		`INSERT INTO substrate_events
		   (id, run_id, work_item_id, tenant_id, trace_id, span_id,
		    kind, key, payload_json, blob_digest, supersedes,
		    written_by, written_at, schema_version, nonce,
		    sig_alg, sig_key_id, sig_mac)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.RunID, nullableString(e.WorkItemID), e.TenantID,
		e.TraceID, e.SpanID, string(e.Kind), e.Key,
		payloadOrEmpty(e.PayloadJSON), e.BlobDigest, nullableString(e.Supersedes),
		e.WrittenBy, e.WrittenAt, e.SchemaVersion, e.Nonce,
		e.SigAlg, e.SigKeyID, e.SigMAC,
	)
	if err != nil {
		if isUniqueNonceViolation(err) {
			return fmt.Errorf("%w: %w", ErrReplay, err)
		}
		return fmt.Errorf("substrate: insert: %w", err)
	}

	if e.WrittenAt > lastWrittenAt {
		lastWrittenAt = e.WrittenAt
	}
	recordEventAppended(ctx, e.Kind)
	return nil
}

// payloadOrEmpty returns "{}" when PayloadJSON is empty so the schema
// CHECK (length(payload_json) <= 1024) sees a valid JSON literal. Empty
// payloads are a legitimate writer choice (heartbeat); the schema
// DEFAULT '{}' covers omitted columns but Go's RawMessage zero value
// is `nil` not `[]byte("{}")`.
func payloadOrEmpty(p json.RawMessage) string {
	if len(p) == 0 {
		return "{}"
	}
	return string(p)
}

// nullableString returns sql.NullString so the work_item_id column
// (NULL allowed for run-level events) is set to NULL on empty input
// rather than '' — the schema's NULL semantics distinguish "no
// work_item" from "work_item with empty id".
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// isUniqueNonceViolation matches the modernc.org/sqlite UNIQUE-collision
// error shape. The substring check is bound to the sqlite driver's
// stable error text — the alternative (driver-internal sqlite3.Error
// extended-code probe) couples us to a driver-private API.
func isUniqueNonceViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed: substrate_events") &&
		(strings.Contains(msg, "nonce") || strings.Contains(msg, "uq_substrate_events_nonce"))
}

// cycleCheck verifies that adding edge (newID -> supersedes) inside
// run_id does not close a cycle. Loads the existing supersedes graph
// for this run, overlays the new edge, and runs Kahn's topo-sort —
// nodes remaining undrained signal a cycle. Mirrors the pattern at
// internal/orchestrator/state/work_items_query.go::CycleCheck.
//
// Same-run restriction (spec §8 R7 / §10 #3): cross-run supersedes is
// forbidden at the app layer (the new event's run_id must match the
// supersedes target's run_id). The check both rejects cross-run links
// AND keeps the graph load bounded by per-run cardinality.
func cycleCheck(ctx context.Context, tx *sql.Tx, runID, newID, supersedes string) error {
	// Self-loop is the smallest possible cycle (newID -> newID). Catch
	// it before the FK lookup; the FK would otherwise reject it as
	// "target not found" because newID has not been inserted yet, and
	// that error masks the cycle-detection invariant the caller cares
	// about.
	if newID == supersedes {
		return fmt.Errorf("%w: self-loop (id == supersedes == %q)",
			ErrSupersedesCycle, newID)
	}

	// Confirm supersedes target lives in the same run. Cross-run
	// supersedes is forbidden per spec §8 R7 / §10 #3 — both because
	// the Kahn's-scope contract is per-run and because cross-run
	// causality is an audit-trail smell.
	var targetRun string
	err := tx.QueryRowContext(ctx,
		`SELECT run_id FROM substrate_events WHERE id = ?`, supersedes).Scan(&targetRun)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("substrate: supersedes target %q not found", supersedes)
	}
	if err != nil {
		return fmt.Errorf("substrate: cycle lookup: %w", err)
	}
	if targetRun != runID {
		return fmt.Errorf("substrate: supersedes target %q lives in run %q, not %q",
			supersedes, targetRun, runID)
	}

	// Load existing edges (child -> parent via supersedes column) for
	// this run. Treat (child, parent) as a directed edge from child to
	// parent — i.e. "child supersedes parent". The new event introduces
	// an edge (newID -> supersedes). A cycle exists iff the graph with
	// the new edge has a node whose indegree never hits zero in Kahn's
	// sweep.
	rows, err := tx.QueryContext(ctx,
		`SELECT id, supersedes FROM substrate_events
		 WHERE run_id = ? AND supersedes IS NOT NULL`, runID)
	if err != nil {
		return fmt.Errorf("substrate: cycle scan: %w", err)
	}
	defer func() { _ = rows.Close() }()

	idx := map[string]int{}
	addNode := func(id string) int {
		if i, ok := idx[id]; ok {
			return i
		}
		i := len(idx)
		idx[id] = i
		return i
	}
	// outgoing[i] = list of node indices that node i supersedes.
	type edge struct{ from, to int }
	var edges []edge
	for rows.Next() {
		var id, sup string
		if err := rows.Scan(&id, &sup); err != nil {
			return fmt.Errorf("substrate: cycle row scan: %w", err)
		}
		f := addNode(id)
		t := addNode(sup)
		edges = append(edges, edge{from: f, to: t})
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("substrate: cycle rows close: %w", err)
	}
	// Overlay the new edge: newID -> supersedes. newID may not exist
	// yet (it's being inserted); supersedes must already exist (FK).
	f := addNode(newID)
	t := addNode(supersedes)
	edges = append(edges, edge{from: f, to: t})

	n := len(idx)
	indeg := make([]int, n)
	// Build adjacency in reverse: for Kahn's we need parents-of(child),
	// then drain parents whose indegree hits 0. Easier: treat edge
	// child -> parent as a "depends on" edge; parent must come BEFORE
	// child in any topological order. So indeg[child]++ for each
	// edge child -> parent; the "no remaining nodes" sweep starts
	// from indeg == 0 leaves (parents nobody supersedes).
	for _, e := range edges {
		indeg[e.from]++
	}
	// Reverse adjacency: parent -> children (so when parent drains,
	// we can decrement its children's indegree).
	revAdj := make([][]int, n)
	for _, e := range edges {
		revAdj[e.to] = append(revAdj[e.to], e.from)
	}
	queue := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	for head := 0; head < len(queue); head++ {
		for _, succ := range revAdj[queue[head]] {
			indeg[succ]--
			if indeg[succ] == 0 {
				queue = append(queue, succ)
			}
		}
	}
	if len(queue) != n {
		return fmt.Errorf("%w: run=%q new=%q supersedes=%q",
			ErrSupersedesCycle, runID, newID, supersedes)
	}
	return nil
}
