package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// reaperTestDB opens a fresh sqlite DB whose constructor-bound clock
// returns t0 forever. State writes that consult d.now() (CreateApproval
// stamps created_at/updated_at) thus stay deterministic.
func reaperTestDB(t *testing.T, t0 time.Time) *state.DB {
	t.Helper()
	db, err := state.OpenWithClock(context.Background(),
		state.DSN(filepath.Join(t.TempDir(), "reaper.db")),
		func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedReaperWorkItem inserts a planned work_item by id so approvals
// FK resolves. Wraps the package-level seedWorkItem helper (gate_test.go)
// with the reaper-test convention of constructing a minimal WorkItem
// from a string id — keeps the per-table-row callsites readable.
func seedReaperWorkItem(t *testing.T, db *state.DB, id string, at time.Time) {
	t.Helper()
	seedWorkItem(t, db, state.WorkItem{
		ID: id, Kind: state.KindFeature, Title: id,
		Lane: "server", Status: state.WorkStatusPlanned,
	}, at)
}

// seedApproval inserts a pending approvals row with the supplied policy
// and escalation chain. The initial reviewer set is alice/bob, q=2;
// nextTiers populates EscalationChain (each element = a next rung;
// chain index 1 == nextTiers[0]).
func seedApproval(t *testing.T, db *state.DB, id, wi string, requestedAt, timeoutAt time.Time, onTimeout string, nextTiers []state.TierConfig) {
	t.Helper()
	tier0 := state.ReviewerSet{Reviewers: []string{"alice", "bob"}, Quorum: 2}
	a := state.Approval{
		ID:                  id,
		WorkItemID:          wi,
		GateName:            "ship-gate",
		RequestedAt:         requestedAt,
		RequestedBy:         "system",
		ReviewerSetSnapshot: tier0,
		Quorum:              tier0.Quorum,
		Status:              state.ApprovalStatusPending,
		TimeoutAt:           timeoutAt,
		OnTimeout:           onTimeout,
		EscalationChain:     nextTiers,
	}
	if err := db.CreateApproval(context.Background(), a); err != nil {
		t.Fatalf("CreateApproval(%s): %v", id, err)
	}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// appendEvent inserts an event directly via the state API. The reaper
// tests use this to stage prior reviewer votes and token-mint markers.
func appendEvent(t *testing.T, db *state.DB, ev state.ApprovalEvent) {
	t.Helper()
	if err := db.AppendApprovalEvent(context.Background(), ev); err != nil {
		t.Fatalf("AppendApprovalEvent(%s,%s): %v", ev.ApprovalID, ev.Kind, err)
	}
}

// countEvents returns the number of events of a given kind on an
// approval. Used to assert idempotency — exactly one timed_out, etc.
func countEvents(t *testing.T, db *state.DB, approvalID, kind string) int {
	t.Helper()
	evs, err := db.ListApprovalEvents(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("ListApprovalEvents: %v", err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func findEvent(t *testing.T, db *state.DB, approvalID, kind string) state.ApprovalEvent {
	t.Helper()
	evs, err := db.ListApprovalEvents(context.Background(), approvalID)
	if err != nil {
		t.Fatalf("ListApprovalEvents: %v", err)
	}
	for _, e := range evs {
		if e.Kind == kind {
			return e
		}
	}
	t.Fatalf("no event kind=%q on approval %s; events=%+v", kind, approvalID, evs)
	return state.ApprovalEvent{}
}

func TestReaper_FailPolicy(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := reaperTestDB(t, t0)
	seedReaperWorkItem(t, db, "F-1", t0)

	timeoutAt := t0.Add(-time.Minute)
	seedApproval(t, db, "a-001", "F-1", t0.Add(-time.Hour), timeoutAt, "fail", nil)

	h := &captureHandler{}
	r, err := NewReaper(db, slog.New(h), func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("NewReaper: %v", err)
	}

	if err := r.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n := countEvents(t, db, "a-001", "timed_out"); n != 1 {
		t.Errorf("timed_out events=%d; want 1", n)
	}
	got, err := db.GetApproval(context.Background(), "a-001")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusTimedOut {
		t.Errorf("status=%q; want %q", got.Status, state.ApprovalStatusTimedOut)
	}
	rec, ok := h.findEvent(obs.EventApprovalTimedOut)
	if !ok {
		t.Fatalf("event %q not emitted", obs.EventApprovalTimedOut)
	}
	v, ok := attrValue(rec, string(obs.KeyPolicy))
	if !ok {
		t.Fatalf("attr %q missing", obs.KeyPolicy)
	}
	if v.String() != "fail" {
		t.Errorf("policy=%q; want %q", v.String(), "fail")
	}
}

func TestReaper_AutoApprovePolicy(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := reaperTestDB(t, t0)
	seedReaperWorkItem(t, db, "F-2", t0)

	timeoutAt := t0.Add(-time.Minute)
	seedApproval(t, db, "a-002", "F-2", t0.Add(-time.Hour), timeoutAt, "auto_approve", nil)

	h := &captureHandler{}
	r, err := NewReaper(db, slog.New(h), func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("NewReaper: %v", err)
	}
	if err := r.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n := countEvents(t, db, "a-002", "timed_out"); n != 1 {
		t.Errorf("timed_out events=%d; want 1", n)
	}
	if n := countEvents(t, db, "a-002", "approved"); n != 1 {
		t.Errorf("approved events=%d; want 1", n)
	}
	got, err := db.GetApproval(context.Background(), "a-002")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusApproved {
		t.Errorf("status=%q; want %q", got.Status, state.ApprovalStatusApproved)
	}
	if _, ok := h.findEvent(obs.EventApprovalTimedOut); !ok {
		t.Errorf("slog %q missing", obs.EventApprovalTimedOut)
	}
	if _, ok := h.findEvent(obs.EventApprovalAutoApproved); !ok {
		t.Errorf("slog %q missing", obs.EventApprovalAutoApproved)
	}
}

func TestReaper_EscalatePolicy_NoOverlap(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := reaperTestDB(t, t0)
	seedReaperWorkItem(t, db, "F-3", t0)

	nextTiers := []state.TierConfig{
		{Reviewers: []string{"carol", "dave"}, Quorum: 2, Timeout: time.Hour},
	}
	timeoutAt := t0.Add(-time.Minute)
	seedApproval(t, db, "a-003", "F-3", t0.Add(-time.Hour), timeoutAt, "escalate", nextTiers)

	// Prior tier-0 votes: alice allow, bob deny — quorum=2 not met for
	// either side at tier-0 (split), so reaper escalates.
	appendEvent(t, db, state.ApprovalEvent{
		ApprovalID: "a-003", Ts: t0.Add(-30 * time.Minute), Kind: "decided",
		Actor:    "alice",
		Payload:  mustMarshal(t, map[string]string{"vote": "allow"}),
		TokenJTI: "jti-alice",
	})
	appendEvent(t, db, state.ApprovalEvent{
		ApprovalID: "a-003", Ts: t0.Add(-20 * time.Minute), Kind: "decided",
		Actor:    "bob",
		Payload:  mustMarshal(t, map[string]string{"vote": "deny"}),
		TokenJTI: "jti-bob",
	})
	// Token mints for tier-0 reviewers (records reaper revokes).
	appendEvent(t, db, state.ApprovalEvent{
		ApprovalID: "a-003", Ts: t0.Add(-50 * time.Minute), Kind: "token_minted",
		Actor: "system", TokenJTI: "jti-alice",
	})
	appendEvent(t, db, state.ApprovalEvent{
		ApprovalID: "a-003", Ts: t0.Add(-50 * time.Minute), Kind: "token_minted",
		Actor: "system", TokenJTI: "jti-bob",
	})

	h := &captureHandler{}
	r, err := NewReaper(db, slog.New(h), func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("NewReaper: %v", err)
	}
	if err := r.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	// One escalated event, payload carries replayed votes both marked
	// discarded (no overlap with tier-2 reviewers).
	ev := findEvent(t, db, "a-003", "escalated")
	var payload struct {
		PriorChainIndex int  `json:"prior_chain_index"`
		NewChainIndex   int  `json:"new_chain_index"`
		PriorQuorum     int  `json:"prior_quorum"`
		NewQuorum       int  `json:"new_quorum"`
		ReplayedVotes   []struct {
			Actor     string `json:"actor"`
			Vote      string `json:"vote"`
			Discarded bool   `json:"discarded"`
		} `json:"replayed_votes"`
		RevokedJTIs []string `json:"revoked_jtis"`
	}
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v\nraw=%s", err, string(ev.Payload))
	}
	if payload.PriorChainIndex != 0 || payload.NewChainIndex != 1 {
		t.Errorf("chain indices=%d→%d; want 0→1", payload.PriorChainIndex, payload.NewChainIndex)
	}
	if len(payload.ReplayedVotes) != 2 {
		t.Fatalf("replayed_votes=%d; want 2", len(payload.ReplayedVotes))
	}
	for _, rv := range payload.ReplayedVotes {
		if !rv.Discarded {
			t.Errorf("vote by %q must be discarded (no overlap with tier-2)", rv.Actor)
		}
	}
	if len(payload.RevokedJTIs) != 2 {
		t.Errorf("revoked_jtis=%v; want 2", payload.RevokedJTIs)
	}

	// Two token_consumed-with-reason=escalated rows, one per prior JTI.
	consumed := 0
	evs, err := db.ListApprovalEvents(context.Background(), "a-003")
	if err != nil {
		t.Fatalf("ListApprovalEvents: %v", err)
	}
	for _, e := range evs {
		if e.Kind != "token_consumed" {
			continue
		}
		var p struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("token_consumed payload: %v", err)
		}
		if p.Reason == "escalated" {
			consumed++
		}
	}
	if consumed != 2 {
		t.Errorf("token_consumed reason=escalated rows=%d; want 2", consumed)
	}

	got, err := db.GetApproval(context.Background(), "a-003")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusPending {
		t.Errorf("status=%q; want %q (replayed votes have no overlap)", got.Status, state.ApprovalStatusPending)
	}
	// Snapshot is now tier-1 reviewer set.
	wantTier1 := []string{"carol", "dave"}
	if len(got.ReviewerSetSnapshot.Reviewers) != 2 ||
		got.ReviewerSetSnapshot.Reviewers[0] != wantTier1[0] ||
		got.ReviewerSetSnapshot.Reviewers[1] != wantTier1[1] {
		t.Errorf("reviewer snapshot=%v; want %v", got.ReviewerSetSnapshot.Reviewers, wantTier1)
	}
	if _, ok := h.findEvent(obs.EventApprovalEscalated); !ok {
		t.Errorf("slog %q missing", obs.EventApprovalEscalated)
	}
}

func TestReaper_EscalatePolicy_OverlapReplaysVote(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := reaperTestDB(t, t0)
	seedReaperWorkItem(t, db, "F-4", t0)

	nextTiers := []state.TierConfig{
		{Reviewers: []string{"alice", "carol"}, Quorum: 2, Timeout: time.Hour},
	}
	timeoutAt := t0.Add(-time.Minute)
	seedApproval(t, db, "a-004", "F-4", t0.Add(-time.Hour), timeoutAt, "escalate", nextTiers)

	// Only alice voted (allow) at tier-0; alice is in tier-1 too, so
	// her vote replays — counts toward tier-1 quorum (1 of 2).
	appendEvent(t, db, state.ApprovalEvent{
		ApprovalID: "a-004", Ts: t0.Add(-30 * time.Minute), Kind: "decided",
		Actor:    "alice",
		Payload:  mustMarshal(t, map[string]string{"vote": "allow"}),
		TokenJTI: "jti-alice-0",
	})
	appendEvent(t, db, state.ApprovalEvent{
		ApprovalID: "a-004", Ts: t0.Add(-50 * time.Minute), Kind: "token_minted",
		Actor: "system", TokenJTI: "jti-alice-0",
	})
	appendEvent(t, db, state.ApprovalEvent{
		ApprovalID: "a-004", Ts: t0.Add(-50 * time.Minute), Kind: "token_minted",
		Actor: "system", TokenJTI: "jti-bob-0",
	})

	r, err := NewReaper(db, slog.New(slog.NewTextHandler(discardWriter{}, nil)), func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("NewReaper: %v", err)
	}
	if err := r.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	ev := findEvent(t, db, "a-004", "escalated")
	var payload struct {
		ReplayedVotes []struct {
			Actor     string `json:"actor"`
			Vote      string `json:"vote"`
			Discarded bool   `json:"discarded"`
		} `json:"replayed_votes"`
	}
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if len(payload.ReplayedVotes) != 1 {
		t.Fatalf("replayed_votes=%d; want 1", len(payload.ReplayedVotes))
	}
	if payload.ReplayedVotes[0].Discarded {
		t.Errorf("alice in tier-1; her vote must NOT be discarded")
	}

	got, err := db.GetApproval(context.Background(), "a-004")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusPending {
		t.Errorf("status=%q; want %q (carol hasn't voted yet)", got.Status, state.ApprovalStatusPending)
	}
	if got.Quorum != 2 {
		t.Errorf("quorum=%d; want 2 (tier-1)", got.Quorum)
	}
}

// TestReaper_EscalatePolicy_OverlapImmediateTerminal — spec §3.3.1.2: replays satisfy new tier in same tx.
func TestReaper_EscalatePolicy_OverlapImmediateTerminal(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := reaperTestDB(t, t0)
	seedReaperWorkItem(t, db, "F-4b", t0)

	// tier-1 reviewers = [alice, bob], quorum 1. Alice's tier-0 allow
	// vote replays into tier-1 (overlap) and immediately satisfies
	// q=1 → status becomes approved in the same sweep.
	nextTiers := []state.TierConfig{
		{Reviewers: []string{"alice", "bob"}, Quorum: 1, Timeout: time.Hour},
	}
	timeoutAt := t0.Add(-time.Minute)
	seedApproval(t, db, "a-004b", "F-4b", t0.Add(-time.Hour), timeoutAt, "escalate", nextTiers)

	appendEvent(t, db, state.ApprovalEvent{
		ApprovalID: "a-004b", Ts: t0.Add(-30 * time.Minute), Kind: "decided",
		Actor:    "alice",
		Payload:  mustMarshal(t, map[string]string{"vote": "allow"}),
		TokenJTI: "jti-alice-0",
	})

	r, err := NewReaper(db, slog.New(slog.NewTextHandler(discardWriter{}, nil)), func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("NewReaper: %v", err)
	}
	if err := r.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	got, err := db.GetApproval(context.Background(), "a-004b")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusApproved {
		t.Errorf("status=%q; want %q (replay satisfied tier-1 quorum)", got.Status, state.ApprovalStatusApproved)
	}
	// Both escalated AND approved events in the SAME sweep.
	if countEvents(t, db, "a-004b", "escalated") != 1 {
		t.Errorf("escalated events=%d; want 1", countEvents(t, db, "a-004b", "escalated"))
	}
	if countEvents(t, db, "a-004b", "approved") != 1 {
		t.Errorf("approved events=%d; want 1", countEvents(t, db, "a-004b", "approved"))
	}
}

func TestReaper_AtomicityOnFailure(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := reaperTestDB(t, t0)
	seedReaperWorkItem(t, db, "F-5", t0)
	timeoutAt := t0.Add(-time.Minute)
	seedApproval(t, db, "a-005", "F-5", t0.Add(-time.Hour), timeoutAt, "fail", nil)

	// Fail injection: hook returns a sentinel after Sweep already
	// appended the timed_out event. sweepOne must roll back the tx —
	// neither the timed_out event nor any status change survives.
	wantErr := errors.New("injected fault for atomicity test")
	r := newTestReaperWithMidTxAbort(t, db, t0, func(tx *sql.Tx) error {
		return wantErr
	})
	err := r.Sweep(context.Background())
	if err == nil {
		t.Fatalf("Sweep: expected error; got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Sweep err=%v; want wraps %v", err, wantErr)
	}

	// No timed_out event survives — full rollback.
	if n := countEvents(t, db, "a-005", "timed_out"); n != 0 {
		t.Errorf("timed_out events after rollback=%d; want 0", n)
	}
	got, err := db.GetApproval(context.Background(), "a-005")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusPending {
		t.Errorf("status=%q after failed sweep; want %q (atomic rollback)", got.Status, state.ApprovalStatusPending)
	}
}

func TestReaper_RaceMultipleSweepsIdempotent(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := reaperTestDB(t, t0)
	seedReaperWorkItem(t, db, "F-6", t0)
	timeoutAt := t0.Add(-time.Minute)
	seedApproval(t, db, "a-006", "F-6", t0.Add(-time.Hour), timeoutAt, "fail", nil)

	r, err := NewReaper(db, slog.New(slog.NewTextHandler(discardWriter{}, nil)), func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("NewReaper: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		i := i
		go func() {
			defer wg.Done()
			errs[i] = r.Sweep(context.Background())
		}()
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Logf("sweep[%d]: %v", i, e)
		}
	}
	if n := countEvents(t, db, "a-006", "timed_out"); n != 1 {
		t.Errorf("timed_out events=%d under 2 concurrent sweeps; want exactly 1", n)
	}
	got, err := db.GetApproval(context.Background(), "a-006")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != state.ApprovalStatusTimedOut {
		t.Errorf("status=%q; want %q", got.Status, state.ApprovalStatusTimedOut)
	}
}

func TestReaper_ClockInjection(t *testing.T) {
	t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	db := reaperTestDB(t, t0)
	seedReaperWorkItem(t, db, "F-7", t0)

	// One row expired (t0-1s), one not (t0+1s).
	seedApproval(t, db, "a-expired", "F-7", t0.Add(-time.Hour), t0.Add(-time.Second), "fail", nil)

	// Same work_item can only host one approval per gate_name; use a
	// second work item for the non-expired row.
	seedReaperWorkItem(t, db, "F-7b", t0)
	seedApproval(t, db, "a-future", "F-7b", t0.Add(-time.Hour), t0.Add(time.Second), "fail", nil)

	r, err := NewReaper(db, slog.New(slog.NewTextHandler(discardWriter{}, nil)), func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("NewReaper: %v", err)
	}
	if err := r.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	gotExpired, err := db.GetApproval(context.Background(), "a-expired")
	if err != nil {
		t.Fatalf("GetApproval(a-expired): %v", err)
	}
	if gotExpired.Status != state.ApprovalStatusTimedOut {
		t.Errorf("a-expired.status=%q; want %q", gotExpired.Status, state.ApprovalStatusTimedOut)
	}
	gotFuture, err := db.GetApproval(context.Background(), "a-future")
	if err != nil {
		t.Fatalf("GetApproval(a-future): %v", err)
	}
	if gotFuture.Status != state.ApprovalStatusPending {
		t.Errorf("a-future.status=%q; want %q (not yet expired by injected clock)", gotFuture.Status, state.ApprovalStatusPending)
	}
}

// TestReaper_NilClockRejected — nil clock must surface as ErrReaperClockRequired, not fall back to time.Now.
func TestReaper_NilClockRejected(t *testing.T) {
	r, err := NewReaper(nil, slog.Default(), nil)
	if err == nil {
		t.Fatalf("NewReaper(nil clock): expected error; got nil")
	}
	if !errors.Is(err, ErrReaperClockRequired) {
		t.Errorf("err=%v; want ErrReaperClockRequired", err)
	}
	if r != nil {
		t.Errorf("NewReaper(nil clock): expected nil reaper; got %v", r)
	}
}

// newTestReaperWithMidTxAbort builds a reaper whose per-row tx fires
// abort right after the timed_out event is appended. Used by
// TestReaper_AtomicityOnFailure to exercise the §3.2.1 rollback
// contract — the assertion is that approvals.status stays pending.
func newTestReaperWithMidTxAbort(t *testing.T, db *state.DB, t0 time.Time, abort func(*sql.Tx) error) *Reaper {
	t.Helper()
	r, err := NewReaper(db, slog.New(slog.NewTextHandler(discardWriter{}, nil)), func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("NewReaper: %v", err)
	}
	r.txHook = abort
	return r
}

// discardWriter satisfies io.Writer for tests that don't capture slog.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
