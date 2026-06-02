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
	// Per #195: gate persists one token_minted row per reviewer between
	// the requested + notified pair so reaper.outstandingJTIs is non-empty.
	wantEvents := 2 + len(cfg.Reviewers)
	if len(events) != wantEvents {
		t.Fatalf("len(events)=%d; want %d (requested, %d×token_minted, notified); events=%+v",
			len(events), wantEvents, len(cfg.Reviewers), events)
	}
	if events[0].Kind != EventKindRequested {
		t.Errorf("events[0].Kind=%q; want %q", events[0].Kind, EventKindRequested)
	}
	if events[len(events)-1].Kind != EventKindNotified {
		t.Errorf("events[last].Kind=%q; want %q", events[len(events)-1].Kind, EventKindNotified)
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
	wantEvents := 2 + len(cfg.Reviewers) // requested + N×token_minted + notified (#195)
	if len(events) != wantEvents {
		t.Errorf("len(events)=%d; want %d (no new events on second pending tick)", len(events), wantEvents)
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

// Concurrent first-evaluation: scheduler tick is single-writer in production (state.go:9 pool=1), but the gate's create+events sequence is als
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
	// Exactly one (requested, N×token_minted, notified) — race losers do not re-emit.
	wantEvents := 2 + len(cfg.Reviewers) // #195: per-JTI token_minted rows
	if len(events) != wantEvents {
		t.Errorf("len(events)=%d; want %d (one winner emits requested+token_minted×N+notified)", len(events), wantEvents)
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

// Token signer is fallback-injectable for deterministic tests of the gate's token-mint loop; production wires crypto/rand.Reader directly. Thi
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

// TestGate_PostEscalationMintAndNotify pins issue #194 — first Evaluate post-escalate mints+notifies tier-1; third call is a no-op.
func TestGate_PostEscalationMintAndNotify(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	// Single mutable clock so the reaper sweep can advance past
	// timeout_at while the gate still reads the same value on the
	// post-escalate tick — keeps the audit timestamps deterministic.
	clockT := t0
	clock := func() time.Time { return clockT }
	db := newGateTestDB(t, clock)
	notifier := &captureNotifier{}
	g := NewGate(db, notifier, testKeyring(), "k1", clock,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := testCfg()
	cfg.OnTimeout = OnTimeoutEscalate
	cfg.EscalationChain = []state.TierConfig{
		{Reviewers: []string{"dave", "erin"}, Quorum: 1, Timeout: time.Hour, DecisionWindow: 30 * time.Minute},
	}
	wi := testWorkItem()
	ctx := context.Background()
	seedWorkItem(t, db, wi, t0)

	// Tick #1 — first-sighting mint+notify for tier-0 (alice/bob/carol).
	if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
		t.Fatalf("Evaluate#1: %v", err)
	}
	if len(notifier.requests) != 1 {
		t.Fatalf("notifier.requests=%d after tick#1; want 1", len(notifier.requests))
	}
	tier0Tokens := make(map[string]string, len(notifier.requests[0].Tokens))
	for k, v := range notifier.requests[0].Tokens {
		tier0Tokens[k] = v
	}

	// Advance past timeout_at + run the real reaper. The reaper
	// performs snapshot advance + token revocation + appends the
	// `escalated` event but does NOT mint+notify for the new tier
	// (issue #194 root cause). Production-identical sequence.
	clockT = t0.Add(cfg.Timeout + time.Minute)
	reaper, err := NewReaper(db, slog.New(slog.NewTextHandler(io.Discard, nil)), clock)
	if err != nil {
		t.Fatalf("NewReaper: %v", err)
	}
	if err := reaper.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	// Tick #2 — gate observes pending row with `escalated` after the
	// last `notified` event. MUST mint fresh tokens for {dave,erin} +
	// append a fresh `notified` event + call Notify exactly once.
	res, err := g.Evaluate(ctx, wi, cfg)
	if err != nil {
		t.Fatalf("Evaluate#2: %v", err)
	}
	if res != ResultPause {
		t.Fatalf("Evaluate#2 res=%v; want ResultPause", res)
	}
	if len(notifier.requests) != 2 {
		t.Fatalf("notifier.requests=%d after tick#2; want 2 (fresh post-escalate notify)", len(notifier.requests))
	}
	postEscReq := notifier.requests[1]
	wantTier1 := map[string]bool{"dave": true, "erin": true}
	for _, r := range postEscReq.Reviewers {
		if !wantTier1[r] {
			t.Errorf("post-escalate notify reviewer=%q not in tier-1 set", r)
		}
	}
	if len(postEscReq.Reviewers) != 2 {
		t.Errorf("post-escalate reviewers=%v; want 2 (dave+erin)", postEscReq.Reviewers)
	}
	if len(postEscReq.Tokens) != 2 {
		t.Errorf("post-escalate tokens=%d; want 2 (one per tier-1 reviewer)", len(postEscReq.Tokens))
	}
	for reviewer, tok := range postEscReq.Tokens {
		if tok == "" {
			t.Errorf("post-escalate token for %q is empty", reviewer)
		}
		// Tier-1 tokens MUST differ from any tier-0 token — fresh mint
		// is the whole point of the post-escalation notify path.
		for _, t0tok := range tier0Tokens {
			if tok == t0tok {
				t.Errorf("post-escalate token for %q equals a tier-0 token; want fresh mint", reviewer)
			}
		}
	}

	// Tick #3 — fold sees `escalated` followed by `notified`; gate
	// returns ResultPause without re-notifying. Idempotency contract.
	if _, err := g.Evaluate(ctx, wi, cfg); err != nil {
		t.Fatalf("Evaluate#3: %v", err)
	}
	if len(notifier.requests) != 2 {
		t.Errorf("notifier.requests=%d after tick#3; want 2 (no third notify; idempotent)", len(notifier.requests))
	}
}

// Gate refuses to evaluate when given an invalid keyring kid: token mint surfaces ErrUnknownKeyID; the gate must propagate the typed sentinel 
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
