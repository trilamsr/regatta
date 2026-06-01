// E2E lifecycle test for the HITL approval gate (MVP-2 W1, Wave 5 A8).
// Drives the full path — fixture-loaded gate config → first scheduler tick
// (gate creates approval + notifies) → CLI `regatta approval decide` →
// second tick (work_item spawns or rejects) → reaper timeout sweep — with
// no mocks of regatta-side code. The notifier is the only swappable seam
// because its design contract is "any channel adapter"; everything else
// (scheduler, gate, reaper, decide-tx, state DB) is the real production code.
//
// Test location: `cmd/regatta/` (not `internal/orchestrator/scheduler/`)
// because the CLI decide path (runApprovalDecideWith) lives in package
// main — exporting it would widen the production surface for a test-only
// caller. Cross-package wiring is fine here: cmd/regatta already imports
// scheduler + gates/approval in serve.go.
package main

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/config"
	"github.com/trilamsr/regatta/internal/gates/approval"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// e2eGateYAML mirrors the operator runbook canonical example
// (docs/operator/approval-gates.md §"regatta.yaml example") modulo
// quorum=1 + on_timeout=fail so the E2E table-tests each terminate
// after a single decide call. Lane name "prod-deploy" matches the
// gate name so serve.go's lane-keyed resolver lights up unchanged.
const e2eGateYAML = `
version: 1
repo:
  host: github
  owner: trilamsr
  name: regatta
spec_adapter:
  type: github_issues
  selector: "label:planned"
ci:
  command: "go test ./..."
gates:
  - id: prod-deploy
    type: approval_gate
    name: prod-deploy
    risk_class: high
    reviewers: [alice, bob, carol]
    roles: []
    quorum: 1
    prevent_self_review: false
    timeout: 1h
    decision_window: 30m
    on_timeout: fail
safety:
  destructive_ops_deny: []
  agent_creds_scope: dev_only
`

// e2eCaptureNotifier records every Request the gate hands off. Tests
// pluck the per-reviewer token from the receipt to drive
// runApprovalDecideWith without re-minting. Concurrency-safe so a future
// parallel ticker variant can share it.
type e2eCaptureNotifier struct {
	mu       sync.Mutex
	requests []approval.Request
}

func (c *e2eCaptureNotifier) Kind() string { return "e2e-capture" }

func (c *e2eCaptureNotifier) Notify(_ context.Context, req approval.Request) (approval.Receipt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	return approval.Receipt{DeliveredTo: req.Reviewers, Channel: c.Kind()}, nil
}

func (c *e2eCaptureNotifier) lastRequest(t *testing.T) approval.Request {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests) == 0 {
		t.Fatalf("notifier.requests is empty; gate did not call Notify")
	}
	return c.requests[len(c.requests)-1]
}

// e2eHarness bundles the real production wiring plus the per-test
// notifier capture. One harness per sub-test so DBs do not bleed.
type e2eHarness struct {
	t        *testing.T
	db       *state.DB
	dsn      string
	now      time.Time
	clock    func() time.Time
	notifier *e2eCaptureNotifier
	sched    *scheduler.Scheduler
	gateCfg  approval.Config
	keyring  canon.MapKeyring
	keyID    string
}

func newE2EHarness(t *testing.T, workItemLane string) *e2eHarness {
	t.Helper()
	t0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return t0 }

	dsn := state.DSN(filepath.Join(t.TempDir(), "e2e.db"))
	db, err := state.OpenWithClock(context.Background(), dsn, clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	gates, err := config.LoadApprovalGates([]byte(e2eGateYAML))
	if err != nil {
		t.Fatalf("LoadApprovalGates: %v", err)
	}
	if len(gates) != 1 {
		t.Fatalf("LoadApprovalGates: got %d gates; want 1", len(gates))
	}
	gateCfg := convertApprovalGateConfig(gates[0])

	// Keyring + env wiring matches serve.go's approvalKeyring() — the
	// decide CLI re-reads env at each runApprovalDecideWith call, so the
	// test must stage env via t.Setenv (restored on cleanup).
	key := bytes.Repeat([]byte{0x77}, 32)
	keyID := "ke2e"
	kr := canon.MapKeyring{keyID: key}
	envName := "REGATTA_APPROVAL_TOKEN_KEY_E2E_" + sanitizeTestName(t.Name())
	t.Setenv(envName, string(key))
	t.Setenv("REGATTA_APPROVAL_TOKEN_KEY_ENV", envName)
	t.Setenv("REGATTA_APPROVAL_TOKEN_KEY_ID", keyID)

	notifier := &e2eCaptureNotifier{}
	gate := approval.NewGate(db, notifier, kr, keyID, clock,
		slog.New(slog.NewTextHandler(discardWriter{}, nil)))

	h := &e2eHarness{
		t:        t,
		db:       db,
		dsn:      dsn,
		now:      t0,
		clock:    clock,
		notifier: notifier,
		gateCfg:  gateCfg,
		keyring:  kr,
		keyID:    keyID,
	}
	// Resolver reads h.gateCfg at lookup time so a subtest may mutate the
	// on_timeout policy / risk_class before the first Tick — keeps the YAML
	// fixture canonical (fail policy) while still letting AutoApprove +
	// Escalate variants reuse the harness wiring without forking it.
	resolver := scheduler.GateResolver(func(wi state.WorkItem) (approval.Config, bool) {
		if wi.Lane == h.gateCfg.Name {
			return h.gateCfg, true
		}
		return approval.Config{}, false
	})

	h.sched = scheduler.New(db, scheduler.Config{
		Gate:         gate,
		GateResolver: resolver,
		Logger:       slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})

	// Reaper is constructed per-sub-test (TimeoutPath builds its own with a
	// future-clock); harness does not own one because HappyPath + RejectPath
	// never sweep.
	h.seedPlannedWorkItem("WI-E2E-1", workItemLane)
	return h
}

func (h *e2eHarness) seedPlannedWorkItem(id, lane string) {
	h.t.Helper()
	if err := h.db.UpsertWorkItem(context.Background(), state.WorkItem{
		ID:     id,
		Kind:   state.KindFeature,
		Title:  id,
		Lane:   lane,
		Status: state.WorkStatusPlanned,
	}, state.SourceBrief, h.now); err != nil {
		h.t.Fatalf("UpsertWorkItem: %v", err)
	}
}

// decideViaCLI invokes the production CLI decide entry point with the
// same arg parsing + env-loaded keyring serve.go uses. Returns the exit
// code so callers assert per spec §5.6.
func (h *e2eHarness) decideViaCLI(token, decision, reviewer string) (int, string) {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	code := runApprovalDecideWith(approvalDecideDeps{
		Stdout: &stdout,
		Stderr: &stderr,
		Clock:  h.clock,
		DSN:    h.dsn,
	}, []string{
		"--token", token,
		"--decision", decision,
		"--reviewer-id", reviewer,
	})
	return code, stderr.String()
}

// sanitizeTestName replaces non-posix-env chars from sub-test names so
// env keys stay clean across shells. '/' from t.Run subtests is the
// common offender; underscore-replace is sufficient since env keys are
// only consumed by this test process.
func sanitizeTestName(name string) string {
	out := make([]byte, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '_':
			out[i] = c
		default:
			out[i] = '_'
		}
	}
	return string(out)
}

// discardWriter is the io.Writer-only sink for slog handlers in tests
// that do not assert on log output. Avoids pulling io+io.Discard into
// the import set solely for the alias.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// workStatusRejectedStr mirrors the scheduler's raw-SQL literal for the
// post-deny / post-timeout work_item state. state has no typed
// WorkStatusRejected const yet (followup tracked in the PR body); the
// string is asserted via this name so goconst stays quiet across the
// repeated sub-test assertions.
const workStatusRejectedStr = "rejected"

// TestE2E_ApprovalGateLifecycle pins spec §6 end-to-end coverage (happy/reject/timeout).
func TestE2E_ApprovalGateLifecycle(t *testing.T) {
	t.Run("HappyPath_Approve", func(t *testing.T) {
		h := newE2EHarness(t, "prod-deploy")
		ctx := context.Background()

		// --- Tick #1: gate creates approval, fans out notification, pauses wi.
		ids, err := h.sched.Tick(ctx)
		if err != nil {
			t.Fatalf("Tick#1: %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("Tick#1 reserved=%v; want 0 (gate must pause)", ids)
		}

		ap, err := h.db.GetApprovalForWorkItem(ctx, "WI-E2E-1", h.gateCfg.Name)
		if err != nil {
			t.Fatalf("GetApprovalForWorkItem: %v", err)
		}
		if ap == nil {
			t.Fatalf("approval not created on first tick")
		}
		if ap.Status != state.ApprovalStatusPending {
			t.Fatalf("approval.status=%q; want pending", ap.Status)
		}

		events, err := h.db.ListApprovalEvents(ctx, ap.ID)
		if err != nil {
			t.Fatalf("ListApprovalEvents: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("events=%d; want 2 (requested + notified)", len(events))
		}
		if events[0].Kind != approval.EventKindRequested {
			t.Errorf("events[0].kind=%q; want %q", events[0].Kind, approval.EventKindRequested)
		}
		if events[1].Kind != approval.EventKindNotified {
			t.Errorf("events[1].kind=%q; want %q", events[1].Kind, approval.EventKindNotified)
		}

		wi, err := h.db.GetWorkItem(ctx, "WI-E2E-1")
		if err != nil {
			t.Fatalf("GetWorkItem: %v", err)
		}
		if wi.Status != state.WorkStatusPlanned {
			t.Fatalf("wi.status=%q; want planned (pause leaves wi unchanged)", wi.Status)
		}

		spawning, err := h.db.ListAgentsByState(ctx, state.AgentSpawning)
		if err != nil {
			t.Fatalf("ListAgentsByState: %v", err)
		}
		if len(spawning) != 0 {
			t.Fatalf("spawning agents=%d; want 0 (gate paused; no reservation)", len(spawning))
		}

		req := h.notifier.lastRequest(t)
		if req.WorkItemID != "WI-E2E-1" {
			t.Errorf("notify.wi=%q; want WI-E2E-1", req.WorkItemID)
		}
		if len(req.Reviewers) != 3 {
			t.Errorf("notify.reviewers=%d; want 3 (alice,bob,carol)", len(req.Reviewers))
		}
		if len(req.Tokens) != 3 {
			t.Errorf("notify.tokens=%d; want 3 (one per reviewer)", len(req.Tokens))
		}

		// --- Operator decides: alice allows. quorum=1 → terminal allow.
		token, ok := req.Tokens["alice"]
		if !ok {
			t.Fatalf("notifier did not mint token for alice")
		}
		code, stderr := h.decideViaCLI(token, "allow", "alice")
		if code != 0 {
			t.Fatalf("decideViaCLI: exit=%d stderr=%q", code, stderr)
		}

		ap2, err := h.db.GetApproval(ctx, ap.ID)
		if err != nil {
			t.Fatalf("GetApproval: %v", err)
		}
		if ap2.Status != state.ApprovalStatusApproved {
			t.Fatalf("post-decide status=%q; want approved", ap2.Status)
		}

		// --- Tick #2: scheduler observes approved → spawns the wi.
		preNotifyCount := len(h.notifier.requests)
		ids, err = h.sched.Tick(ctx)
		if err != nil {
			t.Fatalf("Tick#2: %v", err)
		}
		if len(ids) != 1 {
			t.Fatalf("Tick#2 reserved=%v; want 1 (approved → spawn)", ids)
		}

		spawning, err = h.db.ListAgentsByState(ctx, state.AgentSpawning)
		if err != nil {
			t.Fatalf("ListAgentsByState: %v", err)
		}
		if len(spawning) != 1 || spawning[0].WorkItemID != "WI-E2E-1" {
			t.Fatalf("spawning=%+v; want one agent for WI-E2E-1", spawning)
		}

		// Decide-path already terminated the gate; tick #2 must not re-notify.
		if len(h.notifier.requests) != preNotifyCount {
			t.Errorf("notifier.requests grew on tick#2: pre=%d post=%d", preNotifyCount, len(h.notifier.requests))
		}
	})

	t.Run("RejectPath_Deny", func(t *testing.T) {
		h := newE2EHarness(t, "prod-deploy")
		ctx := context.Background()

		if _, err := h.sched.Tick(ctx); err != nil {
			t.Fatalf("Tick#1: %v", err)
		}
		req := h.notifier.lastRequest(t)
		token, ok := req.Tokens["alice"]
		if !ok {
			t.Fatalf("notifier did not mint token for alice")
		}

		code, stderr := h.decideViaCLI(token, "deny", "alice")
		if code != 0 {
			t.Fatalf("decideViaCLI(deny): exit=%d stderr=%q", code, stderr)
		}

		ap, err := h.db.GetApprovalForWorkItem(ctx, "WI-E2E-1", h.gateCfg.Name)
		if err != nil {
			t.Fatalf("GetApprovalForWorkItem: %v", err)
		}
		if ap.Status != state.ApprovalStatusRejected {
			t.Fatalf("post-deny status=%q; want rejected", ap.Status)
		}

		// Tick #2: scheduler observes rejected → flips wi to rejected, no spawn.
		ids, err := h.sched.Tick(ctx)
		if err != nil {
			t.Fatalf("Tick#2: %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("Tick#2 reserved=%v; want 0 (rejected → no spawn)", ids)
		}

		wi, err := h.db.GetWorkItem(ctx, "WI-E2E-1")
		if err != nil {
			t.Fatalf("GetWorkItem: %v", err)
		}
		if string(wi.Status) != workStatusRejectedStr {
			t.Fatalf("wi.status=%q; want rejected", wi.Status)
		}

		spawning, err := h.db.ListAgentsByState(ctx, state.AgentSpawning)
		if err != nil {
			t.Fatalf("ListAgentsByState: %v", err)
		}
		if len(spawning) != 0 {
			t.Fatalf("spawning=%d; want 0", len(spawning))
		}
	})

	t.Run("TimeoutPath_Fail", func(t *testing.T) {
		// Reaper needs a clock that advances past timeout_at; the gate +
		// scheduler keep the original t0 so the approval row is created at
		// t0 with timeout_at = t0 + 1h. Hand-construct an "after-timeout"
		// reaper rather than mutating the harness clock so the rest of the
		// flow stays at t0.
		h := newE2EHarness(t, "prod-deploy")
		ctx := context.Background()

		if _, err := h.sched.Tick(ctx); err != nil {
			t.Fatalf("Tick#1: %v", err)
		}
		ap, err := h.db.GetApprovalForWorkItem(ctx, "WI-E2E-1", h.gateCfg.Name)
		if err != nil || ap == nil {
			t.Fatalf("GetApprovalForWorkItem: ap=%v err=%v", ap, err)
		}

		// Advance clock past timeout_at (t0 + 1h + epsilon) and sweep.
		afterTimeout := h.now.Add(h.gateCfg.Timeout + time.Minute)
		futureClock := func() time.Time { return afterTimeout }
		lateReaper, err := approval.NewReaper(h.db, slog.New(slog.NewTextHandler(discardWriter{}, nil)), futureClock)
		if err != nil {
			t.Fatalf("NewReaper: %v", err)
		}
		if err := lateReaper.Sweep(ctx); err != nil {
			t.Fatalf("Sweep: %v", err)
		}

		ap2, err := h.db.GetApproval(ctx, ap.ID)
		if err != nil {
			t.Fatalf("GetApproval: %v", err)
		}
		if ap2.Status != state.ApprovalStatusTimedOut {
			t.Fatalf("post-sweep approval.status=%q; want timed_out", ap2.Status)
		}

		// Tick #2: scheduler observes timed_out (Result=Reject) → flips wi rejected.
		ids, err := h.sched.Tick(ctx)
		if err != nil {
			t.Fatalf("Tick#2: %v", err)
		}
		if len(ids) != 0 {
			t.Fatalf("Tick#2 reserved=%v; want 0", ids)
		}

		wi, err := h.db.GetWorkItem(ctx, "WI-E2E-1")
		if err != nil {
			t.Fatalf("GetWorkItem: %v", err)
		}
		if string(wi.Status) != workStatusRejectedStr {
			t.Fatalf("wi.status=%q; want rejected", wi.Status)
		}
	})

	// TimeoutPath_AutoApprove pins the issue #193 contract: on_timeout=
	// auto_approve must take the spawn branch after the reaper sweep —
	// Fold over the post-sweep event log MUST resolve to StatusApproved
	// (ResultProceed). Prior bug: reaper wrote timed_out then approved;
	// Fold's id-ASC first-terminal-wins short-circuit returned StatusTimedOut
	// and the scheduler flipped the wi to rejected even though the denorm
	// column said approved.
	t.Run("TimeoutPath_AutoApprove", func(t *testing.T) {
		h := newE2EHarness(t, "prod-deploy")
		ctx := context.Background()
		// auto_approve requires risk_class=low (config invariant V5); flip
		// both before the first Tick so the gate row is created with the
		// auto_approve policy embedded.
		h.gateCfg.OnTimeout = approval.OnTimeoutAutoApprove
		h.gateCfg.RiskClass = approval.RiskLow

		if _, err := h.sched.Tick(ctx); err != nil {
			t.Fatalf("Tick#1: %v", err)
		}
		ap, err := h.db.GetApprovalForWorkItem(ctx, "WI-E2E-1", h.gateCfg.Name)
		if err != nil || ap == nil {
			t.Fatalf("GetApprovalForWorkItem: ap=%v err=%v", ap, err)
		}

		afterTimeout := h.now.Add(h.gateCfg.Timeout + time.Minute)
		futureClock := func() time.Time { return afterTimeout }
		lateReaper, err := approval.NewReaper(h.db, slog.New(slog.NewTextHandler(discardWriter{}, nil)), futureClock)
		if err != nil {
			t.Fatalf("NewReaper: %v", err)
		}
		if err := lateReaper.Sweep(ctx); err != nil {
			t.Fatalf("Sweep: %v", err)
		}

		ap2, err := h.db.GetApproval(ctx, ap.ID)
		if err != nil {
			t.Fatalf("GetApproval: %v", err)
		}
		if ap2.Status != state.ApprovalStatusApproved {
			t.Fatalf("post-sweep approval.status=%q; want approved", ap2.Status)
		}

		// Tick #2: scheduler observes Fold→approved (ResultProceed) → spawns wi.
		ids, err := h.sched.Tick(ctx)
		if err != nil {
			t.Fatalf("Tick#2: %v", err)
		}
		if len(ids) != 1 {
			t.Fatalf("Tick#2 reserved=%v; want 1 (auto_approve → spawn)", ids)
		}
		spawning, err := h.db.ListAgentsByState(ctx, state.AgentSpawning)
		if err != nil {
			t.Fatalf("ListAgentsByState: %v", err)
		}
		if len(spawning) != 1 || spawning[0].WorkItemID != "WI-E2E-1" {
			t.Fatalf("spawning=%+v; want one agent for WI-E2E-1", spawning)
		}
	})
}
