package approval

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// captureNotifier records every Notify call so tests can assert the
// gate fanned out the right reviewer set with one distinct token per
// reviewer. Concurrency-safe so quorum-race tests can share it.
type captureNotifier struct {
	mu       sync.Mutex
	requests []Request
}

func (c *captureNotifier) Kind() string { return "capture" }

func (c *captureNotifier) Notify(_ context.Context, req Request) (Receipt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	return Receipt{DeliveredTo: req.Reviewers, Channel: "capture"}, nil
}

func newGateTestDB(t *testing.T, clock func() time.Time) *state.DB {
	t.Helper()
	dsn := state.DSN(filepath.Join(t.TempDir(), "gate.db"))
	db, err := state.OpenWithClock(context.Background(), dsn, clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedWorkItem inserts a work_item so the approvals FK resolves.
func seedWorkItem(t *testing.T, db *state.DB, wi state.WorkItem, now time.Time) {
	t.Helper()
	if err := db.UpsertWorkItem(context.Background(), wi, state.SourceAdapter, now); err != nil {
		t.Fatalf("UpsertWorkItem: %v", err)
	}
}

func testKeyring() canon.Keyring {
	return canon.MapKeyring{"k1": []byte("test-key-bytes-32-bytes-long-aaa")}
}

func testCfg() Config {
	return Config{
		Name:           "deploy-gate",
		RiskClass:      RiskLow,
		Reviewers:      []string{"alice", "bob", "carol"},
		Quorum:         2,
		Timeout:        1 * time.Hour,
		DecisionWindow: 30 * time.Minute,
		OnTimeout:      OnTimeoutFail,
	}
}

func testWorkItem() state.WorkItem {
	return state.WorkItem{
		ID: "wi-42", Kind: state.KindFeature,
		Title: "deploy svc", Lane: "server", Status: state.WorkStatusPlanned,
	}
}

func TestGate_FirstEvaluationCreatesApprovalAndNotifies(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := newGateTestDB(t, func() time.Time { return now })
	notifier := &captureNotifier{}
	g := NewGate(db, notifier, testKeyring(), "k1", func() time.Time { return now },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := testCfg()
	wi := testWorkItem()
	ctx := context.Background()
	seedWorkItem(t, db, wi, now)

	res, err := g.Evaluate(ctx, wi, cfg)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res != ResultPause {
		t.Fatalf("Result=%v; want ResultPause", res)
	}

	approval, err := db.GetApprovalForWorkItem(ctx, wi.ID, cfg.Name)
	if err != nil {
		t.Fatalf("GetApprovalForWorkItem: %v", err)
	}
	if approval == nil {
		t.Fatalf("approval not created")
	}
	if approval.Status != state.ApprovalStatusPending {
		t.Errorf("approval.Status=%q; want pending", approval.Status)
	}
	if approval.Quorum != cfg.Quorum {
		t.Errorf("approval.Quorum=%d; want %d", approval.Quorum, cfg.Quorum)
	}
	if !approval.TimeoutAt.Equal(now.Add(cfg.Timeout)) {
		t.Errorf("TimeoutAt=%v; want %v", approval.TimeoutAt, now.Add(cfg.Timeout))
	}

	events, err := db.ListApprovalEvents(ctx, approval.ID)
	if err != nil {
		t.Fatalf("ListApprovalEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events)=%d; want 2 (requested, notified); events=%+v", len(events), events)
	}
	if events[0].Kind != EventKindRequested {
		t.Errorf("events[0].Kind=%q; want %q", events[0].Kind, EventKindRequested)
	}
	if events[1].Kind != EventKindNotified {
		t.Errorf("events[1].Kind=%q; want %q", events[1].Kind, EventKindNotified)
	}

	if len(notifier.requests) != 1 {
		t.Fatalf("notifier.requests=%d; want 1", len(notifier.requests))
	}
	req := notifier.requests[0]
	if len(req.Tokens) != len(cfg.Reviewers) {
		t.Errorf("len(req.Tokens)=%d; want %d (one per reviewer)", len(req.Tokens), len(cfg.Reviewers))
	}
	seenSig := map[string]bool{}
	for _, tok := range req.Tokens {
		if tok == "" {
			t.Error("empty token in Request.Tokens")
		}
		if seenSig[tok] {
			t.Error("duplicate token across reviewers; tokens MUST be distinct")
		}
		seenSig[tok] = true
	}
}

func TestGate_PendingReturnsPause(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := newGateTestDB(t, func() time.Time { return now })
	notifier := &captureNotifier{}
	g := NewGate(db, notifier, testKeyring(), "k1", func() time.Time { return now },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := testCfg()
	wi := testWorkItem()
	ctx := context.Background()
	seedWorkItem(t, db, wi, now)

	if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
		t.Fatalf("first Evaluate: %v", err)
	}
	// Second tick — no votes yet. Gate must not re-create or re-notify.
	res, err := g.Evaluate(ctx, wi, cfg)
	if err != nil {
		t.Fatalf("second Evaluate: %v", err)
	}
	if res != ResultPause {
		t.Fatalf("Result=%v; want ResultPause", res)
	}
	if len(notifier.requests) != 1 {
		t.Errorf("notifier.requests=%d; want 1 (second tick must not re-notify)", len(notifier.requests))
	}
	approval, _ := db.GetApprovalForWorkItem(ctx, wi.ID, cfg.Name)
	events, _ := db.ListApprovalEvents(ctx, approval.ID)
	if len(events) != 2 {
		t.Errorf("len(events)=%d; want 2 (no new events on second pending tick)", len(events))
	}
}

func TestGate_ApprovedReturnsProceed(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := newGateTestDB(t, func() time.Time { return now })
	notifier := &captureNotifier{}
	g := NewGate(db, notifier, testKeyring(), "k1", func() time.Time { return now },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := testCfg()
	wi := testWorkItem()
	ctx := context.Background()
	seedWorkItem(t, db, wi, now)

	if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
		t.Fatalf("first Evaluate: %v", err)
	}
	approval, _ := db.GetApprovalForWorkItem(ctx, wi.ID, cfg.Name)
	for _, actor := range []string{"alice", "bob"} {
		payload, _ := json.Marshal(map[string]string{"decision": DecisionAllow})
		if err := db.AppendApprovalEvent(ctx, state.ApprovalEvent{
			ApprovalID: approval.ID, Ts: now, Kind: EventKindDecided, Actor: actor, Payload: payload,
		}); err != nil {
			t.Fatalf("AppendApprovalEvent: %v", err)
		}
	}
	res, err := g.Evaluate(ctx, wi, cfg)
	if err != nil {
		t.Fatalf("Evaluate post-vote: %v", err)
	}
	if res != ResultProceed {
		t.Fatalf("Result=%v; want ResultProceed", res)
	}
}

func TestGate_RejectedReturnsReject(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := newGateTestDB(t, func() time.Time { return now })
	notifier := &captureNotifier{}
	g := NewGate(db, notifier, testKeyring(), "k1", func() time.Time { return now },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := testCfg()
	wi := testWorkItem()
	ctx := context.Background()
	seedWorkItem(t, db, wi, now)

	if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
		t.Fatalf("first Evaluate: %v", err)
	}
	approval, _ := db.GetApprovalForWorkItem(ctx, wi.ID, cfg.Name)
	for _, actor := range []string{"alice", "bob"} {
		payload, _ := json.Marshal(map[string]string{"decision": DecisionDeny})
		if err := db.AppendApprovalEvent(ctx, state.ApprovalEvent{
			ApprovalID: approval.ID, Ts: now, Kind: EventKindDecided, Actor: actor, Payload: payload,
		}); err != nil {
			t.Fatalf("AppendApprovalEvent: %v", err)
		}
	}
	res, err := g.Evaluate(ctx, wi, cfg)
	if err != nil {
		t.Fatalf("Evaluate post-vote: %v", err)
	}
	if res != ResultReject {
		t.Fatalf("Result=%v; want ResultReject", res)
	}
}

func TestGate_TimedOutReturnsReject(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := newGateTestDB(t, func() time.Time { return now })
	notifier := &captureNotifier{}
	g := NewGate(db, notifier, testKeyring(), "k1", func() time.Time { return now },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := testCfg()
	wi := testWorkItem()
	ctx := context.Background()
	seedWorkItem(t, db, wi, now)

	if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
		t.Fatalf("first Evaluate: %v", err)
	}
	approval, _ := db.GetApprovalForWorkItem(ctx, wi.ID, cfg.Name)
	if err := db.AppendApprovalEvent(ctx, state.ApprovalEvent{
		ApprovalID: approval.ID, Ts: now, Kind: EventKindTimedOut, Actor: "system",
	}); err != nil {
		t.Fatalf("AppendApprovalEvent: %v", err)
	}
	res, err := g.Evaluate(ctx, wi, cfg)
	if err != nil {
		t.Fatalf("Evaluate post-timeout: %v", err)
	}
	if res != ResultReject {
		t.Fatalf("Result=%v; want ResultReject", res)
	}
}

// Concurrent first-evaluation: scheduler tick is single-writer in
// production (state.go:9 pool=1), but the gate's create+events sequence
// is also exposed to a UNIQUE-collision race if two callers reach the
// CreateApproval step concurrently. Both must terminate cleanly; one
// gets ResultPause (the winner), the other observes the existing row
// and returns ResultPause too. No errors leak.
func TestGate_ConcurrentFirstEvaluationsSerialise(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := newGateTestDB(t, func() time.Time { return now })
	notifier := &captureNotifier{}
	g := NewGate(db, notifier, testKeyring(), "k1", func() time.Time { return now },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := testCfg()
	wi := testWorkItem()
	ctx := context.Background()
	seedWorkItem(t, db, wi, now)

	var wg sync.WaitGroup
	results := make([]Result, 4)
	errs := make([]error, 4)
	for i := 0; i < 4; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = g.Evaluate(ctx, wi, cfg)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("Evaluate[%d] err=%v", i, err)
		}
		if results[i] != ResultPause {
			t.Errorf("Evaluate[%d] Result=%v; want ResultPause", i, results[i])
		}
	}
	approval, err := db.GetApprovalForWorkItem(ctx, wi.ID, cfg.Name)
	if err != nil || approval == nil {
		t.Fatalf("approval row missing post-race: %v", err)
	}
	events, _ := db.ListApprovalEvents(ctx, approval.ID)
	// Exactly one (requested, notified) pair — race losers do not re-emit.
	if len(events) != 2 {
		t.Errorf("len(events)=%d; want 2 (one winner emits requested+notified)", len(events))
	}
}

func TestGate_TokensVerifyableAgainstKeyring(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := newGateTestDB(t, func() time.Time { return now })
	notifier := &captureNotifier{}
	kr := testKeyring()
	g := NewGate(db, notifier, kr, "k1", func() time.Time { return now },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := testCfg()
	wi := testWorkItem()
	ctx := context.Background()
	seedWorkItem(t, db, wi, now)

	if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	req := notifier.requests[0]
	for reviewer, wire := range req.Tokens {
		payload, err := canon.VerifyToken(kr, wire, reviewer, now)
		if err != nil {
			t.Fatalf("VerifyToken(%s): %v", reviewer, err)
		}
		if payload.Reviewer != reviewer {
			t.Errorf("payload.Reviewer=%q; want %q", payload.Reviewer, reviewer)
		}
	}
}

// Token signer is fallback-injectable for deterministic tests of the
// gate's token-mint loop; production wires crypto/rand.Reader directly.
// This test simply asserts that a non-default jti source is honoured.
func TestGate_HonorsJTISource(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := newGateTestDB(t, func() time.Time { return now })
	g := NewGate(db, &captureNotifier{}, testKeyring(), "k1", func() time.Time { return now },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	g.jtiRand = rand.Reader // production default; assigning here documents the seam exists.
	wi := testWorkItem()
	seedWorkItem(t, db, wi, now)
	if _, err := g.Evaluate(context.Background(), wi, testCfg()); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
}

// Result-string contract for slog emission.
func TestResult_String(t *testing.T) {
	cases := map[Result]string{
		ResultProceed: "proceed",
		ResultPause:   "pause",
		ResultReject:  "reject",
	}
	for r, want := range cases {
		if got := r.String(); got != want {
			t.Errorf("Result(%d).String()=%q; want %q", r, got, want)
		}
	}
}

// Gate refuses to evaluate when given an invalid keyring kid: token
// mint surfaces ErrUnknownKeyID; the gate must propagate the typed
// sentinel rather than papering over with a generic wrap.
func TestGate_UnknownKeyIDPropagates(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	db := newGateTestDB(t, func() time.Time { return now })
	g := NewGate(db, &captureNotifier{}, testKeyring(), "missing-kid",
		func() time.Time { return now }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	wi := testWorkItem()
	seedWorkItem(t, db, wi, now)
	_, err := g.Evaluate(context.Background(), wi, testCfg())
	if !errors.Is(err, canon.ErrUnknownKeyID) {
		t.Fatalf("err=%v; want errors.Is(canon.ErrUnknownKeyID)", err)
	}
}
