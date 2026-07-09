package costcap

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/cost/spend"
)

// fakeSpend serves a configurable BudgetState response and counts calls.
type fakeSpend struct {
	mu     sync.Mutex
	value  spend.USDMicro
	calls  int32
	period time.Duration
	err    error
}

func (f *fakeSpend) BudgetState(_ context.Context, _ spend.ScopeKey, p time.Duration) (spend.USDMicro, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.period = p
	if f.err != nil {
		return 0, f.err
	}
	return f.value, nil
}
func (f *fakeSpend) set(v spend.USDMicro) { f.mu.Lock(); f.value = v; f.mu.Unlock() }
func (f *fakeSpend) callCount() int32     { return atomic.LoadInt32(&f.calls) }

// fakeRecorder captures audit events.
type fakeRecorder struct {
	mu     sync.Mutex
	events []recorded
}
type recorded struct{ kind, payload string }

func (r *fakeRecorder) RecordEvent(_ context.Context, _ int64, kind, payload string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recorded{kind: kind, payload: payload})
	return nil
}
func (r *fakeRecorder) count(kind string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.kind == kind {
			n++
		}
	}
	return n
}

// fakeResume serves LatestResumeAt; updates atomically.
type fakeResume struct {
	mu sync.Mutex
	t  time.Time
}

func (f *fakeResume) read(_ context.Context) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t, nil
}
func (f *fakeResume) set(t time.Time) { f.mu.Lock(); f.t = t; f.mu.Unlock() }

func newTestEnforcer(t *testing.T, capMicro spend.USDMicro, sp *fakeSpend, rec *fakeRecorder, res *fakeResume, clock func() time.Time, tz *time.Location, memo time.Duration) *Enforcer {
	t.Helper()
	e, err := New(Config{
		CapMicro:   capMicro,
		TenantID:   "default",
		TZ:         tz,
		MemoizeTTL: memo,
		Spend:      sp,
		Recorder:   rec,
		Resume:     res.read,
		Clock:      clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

// TestEnforcer_BelowCap_AllowsSpawn sum<cap → Active.
func TestEnforcer_BelowCap_AllowsSpawn(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(10)}
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	e := newTestEnforcer(t, spend.FromUSD(40), sp, &fakeRecorder{}, &fakeResume{}, func() time.Time { return now }, time.UTC, 0)
	if !e.Allow(context.Background()) {
		t.Fatalf("Allow=false; want true (sum<cap)")
	}
}

// TestEnforcer_AtCapBoundary_Allows sum==cap → Active (#651: > not >=).
func TestEnforcer_AtCapBoundary_Allows(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(40)}
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	e := newTestEnforcer(t, spend.FromUSD(40), sp, &fakeRecorder{}, &fakeResume{}, func() time.Time { return now }, time.UTC, 0)
	if !e.Allow(context.Background()) {
		t.Fatalf("Allow=false at boundary; want true (sum==cap allowed)")
	}
}

// TestEnforcer_AboveCap_Throttles sum>cap → Throttled (#651 boundary).
func TestEnforcer_AboveCap_Throttles(t *testing.T) {
	// $40.000001 (one micro over $40 cap) — the smallest excursion above
	// the boundary; throttle MUST fire.
	sp := &fakeSpend{value: spend.USDMicro(40_000_001)}
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	e := newTestEnforcer(t, spend.USDMicro(40_000_000), sp, &fakeRecorder{}, &fakeResume{}, func() time.Time { return now }, time.UTC, 0)
	if e.Allow(context.Background()) {
		t.Fatalf("Allow=true 1 micro over cap; want throttled")
	}
}

// TestEnforcer_DayRollover_AutoResumes clock past midnight clears throttle.
func TestEnforcer_DayRollover_AutoResumes(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(50)}
	cur := time.Date(2026, 6, 2, 23, 59, 0, 0, time.UTC)
	clk := func() time.Time { return cur }
	rec := &fakeRecorder{}
	e := newTestEnforcer(t, spend.FromUSD(40), sp, rec, &fakeResume{}, clk, time.UTC, 0)
	if e.Allow(context.Background()) {
		t.Fatalf("pre-rollover Allow=true; want throttled")
	}
	// New day: drop spend (24h window slides) and re-evaluate.
	cur = time.Date(2026, 6, 3, 0, 0, 1, 0, time.UTC)
	sp.set(spend.FromUSD(5))
	if !e.Allow(context.Background()) {
		t.Fatalf("post-rollover Allow=false; want resumed")
	}
}

// TestEnforcer_DSTSpringForward_HandlesGracefully PT DST skip 02:00→03:00 still anchors at midnight.
func TestEnforcer_DSTSpringForward_HandlesGracefully(t *testing.T) {
	pt, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skipf("tz not available: %v", err)
	}
	// 2026-03-08 is PT spring-forward; midnight PT exists and is valid.
	now := time.Date(2026, 3, 8, 3, 30, 0, 0, pt) // after the skip
	got := dayAnchor(now, pt)
	want := time.Date(2026, 3, 8, 0, 0, 0, 0, pt)
	if !got.Equal(want) {
		t.Fatalf("dayAnchor DST spring forward: got %v want %v", got, want)
	}
}

// TestEnforcer_DSTFallBack_HandlesGracefully PT Nov fall-back midnight remains unique.
func TestEnforcer_DSTFallBack_HandlesGracefully(t *testing.T) {
	pt, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skipf("tz not available: %v", err)
	}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, pt) // after fall-back
	got := dayAnchor(now, pt)
	want := time.Date(2026, 11, 1, 0, 0, 0, 0, pt)
	if !got.Equal(want) {
		t.Fatalf("dayAnchor DST fall back: got %v want %v", got, want)
	}
}

// TestEnforcer_OperatorResume_OverridesUntilNextRollover Resume mid-day clears throttle.
func TestEnforcer_OperatorResume_OverridesUntilNextRollover(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(50)}
	cur := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	clk := func() time.Time { return cur }
	rec := &fakeRecorder{}
	res := &fakeResume{}
	e := newTestEnforcer(t, spend.FromUSD(40), sp, rec, res, clk, time.UTC, 0)
	if e.Allow(context.Background()) {
		t.Fatalf("pre-resume Allow=true; want throttled")
	}
	// Operator runs Resume — fakeResume reflects what RecordEvent persisted.
	if err := e.Resume(context.Background(), "trilamsr"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	res.set(cur)
	if !e.Allow(context.Background()) {
		t.Fatalf("post-resume Allow=false; want active until rollover")
	}
	// Past midnight: override expires, still over cap → throttled again.
	cur = time.Date(2026, 6, 3, 0, 0, 1, 0, time.UTC)
	if e.Allow(context.Background()) {
		t.Fatalf("post-rollover Allow=true; want re-throttled (cap still breached)")
	}
}

// TestEnforcer_Memoize60s_AvoidsHotPathDBHit two calls within TTL hit DB once.
func TestEnforcer_Memoize60s_AvoidsHotPathDBHit(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(10)}
	cur := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	clk := func() time.Time { return cur }
	e := newTestEnforcer(t, spend.FromUSD(40), sp, &fakeRecorder{}, &fakeResume{}, clk, time.UTC, 60*time.Second)
	_ = e.Allow(context.Background())
	cur = cur.Add(30 * time.Second)
	_ = e.Allow(context.Background())
	if got := sp.callCount(); got != 1 {
		t.Fatalf("BudgetState calls=%d; want 1 (memoized)", got)
	}
}

// TestEnforcer_MemoizeExpires_RefreshesAfterTTL clock past TTL re-queries.
func TestEnforcer_MemoizeExpires_RefreshesAfterTTL(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(10)}
	cur := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	clk := func() time.Time { return cur }
	e := newTestEnforcer(t, spend.FromUSD(40), sp, &fakeRecorder{}, &fakeResume{}, clk, time.UTC, 60*time.Second)
	_ = e.Allow(context.Background())
	cur = cur.Add(61 * time.Second)
	_ = e.Allow(context.Background())
	if got := sp.callCount(); got != 2 {
		t.Fatalf("BudgetState calls=%d; want 2 (TTL expired)", got)
	}
}

// TestEnforcer_CapDisabled_NoSpendRead CapMicro=0 short-circuits.
func TestEnforcer_CapDisabled_NoSpendRead(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(1000)}
	e := newTestEnforcer(t, 0, sp, &fakeRecorder{}, &fakeResume{}, time.Now, time.UTC, 0)
	if !e.Allow(context.Background()) {
		t.Fatalf("Allow=false; want true (cap disabled)")
	}
	if got := sp.callCount(); got != 0 {
		t.Fatalf("BudgetState calls=%d; want 0 (cap disabled, no read)", got)
	}
}

// TestEnforcer_TwoSchedulersAgreeUnderRace concurrent Allow calls converge.
func TestEnforcer_TwoSchedulersAgreeUnderRace(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(50)}
	cur := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	clk := func() time.Time { return cur }
	rec := &fakeRecorder{}
	e := newTestEnforcer(t, spend.FromUSD(40), sp, rec, &fakeResume{}, clk, time.UTC, 60*time.Second)
	var wg sync.WaitGroup
	results := make([]bool, 16)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = e.Allow(context.Background())
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r {
			t.Fatalf("worker[%d] Allow=true under throttle", i)
		}
	}
	// Memoize MUST collapse the race to a single SUM read.
	if got := sp.callCount(); got != 1 {
		t.Fatalf("BudgetState calls=%d under race; want 1 (memoize collapses)", got)
	}
	// Exactly one throttled event for the transition Active→Throttled.
	if got := rec.count(EventKindThrottled); got != 1 {
		t.Fatalf("throttled events=%d; want 1 (once-per-transition)", got)
	}
}

// TestEnforcer_EmitsEventOnce_PerTransition repeated throttle ticks emit one event.
func TestEnforcer_EmitsEventOnce_PerTransition(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(50)}
	cur := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	clk := func() time.Time { return cur }
	rec := &fakeRecorder{}
	e := newTestEnforcer(t, spend.FromUSD(40), sp, rec, &fakeResume{}, clk, time.UTC, 0)
	for i := 0; i < 5; i++ {
		_ = e.Allow(context.Background())
	}
	if got := rec.count(EventKindThrottled); got != 1 {
		t.Fatalf("throttled events=%d; want 1 per transition", got)
	}
}

// TestEnforcer_MoneyDiscipline_ExactBoundary micro-USD comparison is exact at the > boundary (#651).
func TestEnforcer_MoneyDiscipline_ExactBoundary(t *testing.T) {
	// $39.999999 → 39_999_999 micro; $40 → 40_000_000 micro;
	// $40.000001 → 40_000_001 micro.
	dailyCap := spend.USDMicro(40_000_000)
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	// Just under: Active.
	sp1 := &fakeSpend{value: spend.USDMicro(39_999_999)}
	e1 := newTestEnforcer(t, dailyCap, sp1, &fakeRecorder{}, &fakeResume{}, func() time.Time { return now }, time.UTC, 0)
	if !e1.Allow(context.Background()) {
		t.Fatalf("$39.999999 should be Active under $40 cap")
	}
	// Exactly at cap: Active (#651 boundary: > not >=).
	sp2 := &fakeSpend{value: spend.USDMicro(40_000_000)}
	e2 := newTestEnforcer(t, dailyCap, sp2, &fakeRecorder{}, &fakeResume{}, func() time.Time { return now }, time.UTC, 0)
	if !e2.Allow(context.Background()) {
		t.Fatalf("$40 should be Active at boundary (> not >=)")
	}
	// One micro over: Throttled.
	sp3 := &fakeSpend{value: spend.USDMicro(40_000_001)}
	e3 := newTestEnforcer(t, dailyCap, sp3, &fakeRecorder{}, &fakeResume{}, func() time.Time { return now }, time.UTC, 0)
	if e3.Allow(context.Background()) {
		t.Fatalf("$40.000001 should be Throttled (1 micro above cap)")
	}
}

// TestEnforcer_Snapshot_BypassesMemo status CLI gets fresh numbers.
func TestEnforcer_Snapshot_BypassesMemo(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(10)}
	cur := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	clk := func() time.Time { return cur }
	e := newTestEnforcer(t, spend.FromUSD(40), sp, &fakeRecorder{}, &fakeResume{}, clk, time.UTC, 60*time.Second)
	_ = e.Allow(context.Background()) // memoize
	sp.set(spend.FromUSD(50))
	spendOut, capOut, state, _, err := e.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if spendOut != spend.FromUSD(50) {
		t.Fatalf("snapshot spend=%v; want $50", spendOut.USD())
	}
	if capOut != spend.FromUSD(40) {
		t.Fatalf("snapshot cap=%v; want $40", capOut.USD())
	}
	if state != Throttled {
		t.Fatalf("snapshot state=%v; want Throttled", state)
	}
}

// TestEnforcer_New_RequiresSpend nil spend reader rejected.
func TestEnforcer_New_RequiresSpend(t *testing.T) {
	if _, err := New(Config{TenantID: "default"}); err == nil {
		t.Fatalf("New(no spend) expected error; got nil")
	}
}

// TestEnforcer_New_RequiresTenant empty TenantID rejected.
func TestEnforcer_New_RequiresTenant(t *testing.T) {
	if _, err := New(Config{Spend: &fakeSpend{}}); err == nil {
		t.Fatalf("New(no tenant) expected error; got nil")
	}
}

// TestEnforcer_SpendReadError_FailsClosed substrate read error throttles (#650).
func TestEnforcer_SpendReadError_FailsClosed(t *testing.T) {
	sp := &fakeSpend{err: errors.New("simulated substrate hiccup")}
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	e := newTestEnforcer(t, spend.FromUSD(40), sp, &fakeRecorder{}, &fakeResume{}, func() time.Time { return now }, time.UTC, 0)
	if e.Allow(context.Background()) {
		t.Fatalf("Allow=true on spend read error; want false (fail-CLOSED)")
	}
	st, reason, err := e.State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if st != Throttled {
		t.Fatalf("state=%v; want Throttled on read error", st)
	}
	if !strings.Contains(reason, "failing closed") {
		t.Fatalf("reason missing fail-closed signal: %q", reason)
	}
}

// TestEnforcer_State_ReturnsReason status CLI uses textual reason.
func TestEnforcer_State_ReturnsReason(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(50)}
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	e := newTestEnforcer(t, spend.FromUSD(40), sp, &fakeRecorder{}, &fakeResume{}, func() time.Time { return now }, time.UTC, 0)
	st, reason, err := e.State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if st != Throttled {
		t.Fatalf("state=%v; want Throttled", st)
	}
	if !strings.Contains(reason, "auto-resume") {
		t.Fatalf("reason missing auto-resume hint: %q", reason)
	}
}
