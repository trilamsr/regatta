package adaptersync_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/orchestrator/adaptersync"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

type stubAdapter struct {
	items   []schemas.WorkItem
	listErr error
}

func (s *stubAdapter) List(context.Context) ([]schemas.WorkItem, error) {
	return s.items, s.listErr
}

// Capabilities returns zero MinPollInterval so the Syncer's MinPoll gate
// stays off-by-default in tests that don't exercise cadence.
func (s *stubAdapter) Capabilities() schemas.Capabilities { return schemas.Capabilities{} }

func newSyncTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(context.Background(), state.DSN(filepath.Join(t.TempDir(), "s.db")))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// mustNew wraps adaptersync.New so tests can keep their fluent
// inline construction style after the constructor switched to
// (*Syncer, error) for required-field validation.
func mustNew(t *testing.T, cfg adaptersync.Config) *adaptersync.Syncer {
	t.Helper()
	s, err := adaptersync.New(cfg)
	if err != nil {
		t.Fatalf("adaptersync.New: %v", err)
	}
	return s
}

// captureLogs swaps slog's default to a text handler writing into a
// buffer for the duration of the test so log-asserting tests can grep
// without globally leaking handler state.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func TestSync_UpsertsAdapterItems(t *testing.T) {
	db := newSyncTestDB(t)
	adapter := &stubAdapter{items: []schemas.WorkItem{
		{ID: "ITEM-1", Kind: schemas.KindFeature, Title: "a", Lane: "server", Status: schemas.StatusPlanned},
		{ID: "ITEM-2", Kind: schemas.KindFeature, Title: "b", Lane: "client", Status: schemas.StatusPlanned},
	}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	syncer := mustNew(t, adaptersync.Config{Adapter: adapter, DB: db})

	if err := syncer.Sync(context.Background(), now); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	for _, want := range []struct {
		id, title, lane string
	}{
		{"ITEM-1", "a", "server"},
		{"ITEM-2", "b", "client"},
	} {
		got, err := db.GetWorkItem(context.Background(), want.id)
		if err != nil {
			t.Fatalf("GetWorkItem %s: %v", want.id, err)
		}
		if got.Source != state.SourceAdapter {
			t.Fatalf("%s.source=%s want adapter", want.id, got.Source)
		}
		if got.Title != want.title {
			t.Fatalf("%s.title=%q want %q", want.id, got.Title, want.title)
		}
		if got.Lane != want.lane {
			t.Fatalf("%s.lane=%q want %q", want.id, got.Lane, want.lane)
		}
		if got.Kind != state.KindFeature {
			t.Fatalf("%s.kind=%q want %q", want.id, got.Kind, state.KindFeature)
		}
		if got.Status != state.WorkStatusPlanned {
			t.Fatalf("%s.status=%q want %q", want.id, got.Status, state.WorkStatusPlanned)
		}
		if got.LastSeenAt.Unix() != now.Unix() {
			t.Fatalf("%s.last_seen_at=%d want %d", want.id, got.LastSeenAt.Unix(), now.Unix())
		}
	}

	var n int
	if err := db.SQL().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM work_items`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("work_items row count=%d want 2", n)
	}
}

func TestSync_TombstonesMissingOnSecondTick(t *testing.T) {
	db := newSyncTestDB(t)
	adapter := &stubAdapter{items: []schemas.WorkItem{
		{ID: "ITEM-1", Kind: schemas.KindFeature, Title: "a", Lane: "server", Status: schemas.StatusPlanned},
		{ID: "ITEM-2", Kind: schemas.KindFeature, Title: "b", Lane: "server", Status: schemas.StatusPlanned},
	}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	syncer := mustNew(t, adaptersync.Config{Adapter: adapter, DB: db})

	if err := syncer.Sync(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	now = now.Add(1 * time.Second)
	adapter.items = adapter.items[:1]
	if err := syncer.Sync(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetWorkItem(context.Background(), "ITEM-2")
	if err != nil {
		t.Fatalf("GetWorkItem ITEM-2: %v", err)
	}
	if got.Status != state.WorkStatusArchived {
		t.Fatalf("ITEM-2.status=%s want archived", got.Status)
	}
}

// TestSync_SkipsUnmappableStatus asserts unmappable adapter Status is skipped with slog warn, not cast.
func TestSync_SkipsUnmappableStatus(t *testing.T) {
	logs := captureLogs(t)
	db := newSyncTestDB(t)
	adapter := &stubAdapter{items: []schemas.WorkItem{
		{ID: "OK", Kind: schemas.KindFeature, Title: "ok", Lane: "server", Status: schemas.StatusPlanned},
		{ID: "BAD-STATUS", Kind: schemas.KindFeature, Title: "bad", Lane: "server", Status: schemas.StatusInProgress},
	}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	syncer := mustNew(t, adaptersync.Config{Adapter: adapter, DB: db})

	if err := syncer.Sync(context.Background(), now); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if _, err := db.GetWorkItem(context.Background(), "OK"); err != nil {
		t.Fatalf("OK upsert missing: %v", err)
	}
	if _, err := db.GetWorkItem(context.Background(), "BAD-STATUS"); err == nil {
		t.Fatal("BAD-STATUS got upserted; unmappable status must skip")
	}
	out := logs.String()
	if !strings.Contains(out, "adapter.item_skipped") {
		t.Fatalf("logs missing adapter.item_skipped warn:\n%s", out)
	}
	if !strings.Contains(out, "BAD-STATUS") {
		t.Fatalf("logs missing BAD-STATUS id:\n%s", out)
	}
	if !strings.Contains(out, "unknown_status") {
		t.Fatalf("logs missing reason=unknown_status:\n%s", out)
	}
}

func TestSync_SkipsUnmappableKind(t *testing.T) {
	logs := captureLogs(t)
	db := newSyncTestDB(t)
	adapter := &stubAdapter{items: []schemas.WorkItem{
		{ID: "BAD-KIND", Kind: schemas.WorkItemKind("garbage"), Title: "x", Lane: "server", Status: schemas.StatusPlanned},
	}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	syncer := mustNew(t, adaptersync.Config{Adapter: adapter, DB: db})

	if err := syncer.Sync(context.Background(), now); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := db.GetWorkItem(context.Background(), "BAD-KIND"); err == nil {
		t.Fatal("BAD-KIND got upserted; unmappable kind must skip")
	}
	out := logs.String()
	if !strings.Contains(out, "adapter.item_skipped") || !strings.Contains(out, "unknown_kind") {
		t.Fatalf("logs missing unknown_kind skip warn:\n%s", out)
	}
}

func TestSync_SkipsEmptyLane(t *testing.T) {
	logs := captureLogs(t)
	db := newSyncTestDB(t)
	adapter := &stubAdapter{items: []schemas.WorkItem{
		{ID: "BAD-LANE", Kind: schemas.KindFeature, Title: "x", Lane: "", Status: schemas.StatusPlanned},
	}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	syncer := mustNew(t, adaptersync.Config{Adapter: adapter, DB: db})

	if err := syncer.Sync(context.Background(), now); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := db.GetWorkItem(context.Background(), "BAD-LANE"); err == nil {
		t.Fatal("BAD-LANE got upserted; empty lane must skip")
	}
	if !strings.Contains(logs.String(), "empty_lane") {
		t.Fatalf("logs missing empty_lane reason:\n%s", logs.String())
	}
}

// TestSync_EmptyListSkipsTombstone asserts empty adapter list does not purge rows (transient-poll guard).
func TestSync_EmptyListSkipsTombstone(t *testing.T) {
	logs := captureLogs(t)
	db := newSyncTestDB(t)
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

	seed := &stubAdapter{items: []schemas.WorkItem{
		{ID: "SEED", Kind: schemas.KindFeature, Title: "s", Lane: "server", Status: schemas.StatusPlanned},
	}}
	if err := mustNew(t, adaptersync.Config{Adapter: seed, DB: db}).Sync(context.Background(), now); err != nil {
		t.Fatalf("seed Sync: %v", err)
	}

	empty := &stubAdapter{items: nil}
	if err := mustNew(t, adaptersync.Config{Adapter: empty, DB: db}).Sync(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatalf("empty Sync: %v", err)
	}

	got, err := db.GetWorkItem(context.Background(), "SEED")
	if err != nil {
		t.Fatalf("GetWorkItem SEED: %v", err)
	}
	if got.Status == state.WorkStatusArchived {
		t.Fatal("SEED got archived on empty-list tick; tombstone must be skipped")
	}
	if !strings.Contains(logs.String(), "adapter.empty_list") {
		t.Fatalf("logs missing adapter.empty_list warn:\n%s", logs.String())
	}
}

// TestSync_EmitsAdapterSyncFailedEventOnListError asserts the declared adaptersync.failed event actually fires.
func TestSync_EmitsAdapterSyncFailedEventOnListError(t *testing.T) {
	db := newSyncTestDB(t)
	adapter := &stubAdapter{listErr: fmt.Errorf("boom")}
	syncer := mustNew(t, adaptersync.Config{Adapter: adapter, DB: db})

	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	if err := syncer.Sync(context.Background(), now); err == nil {
		t.Fatal("Sync want error on List failure")
	}

	row := db.SQL().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE kind = ?`, string(obs.EventAdapterSyncFailed))
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count adaptersync.failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("adaptersync.failed event count=%d; want 1", n)
	}
}

// TestSync_AdapterSyncFailedEventCarriesErrPayload asserts payload includes the err msg (R6-Bug-1).
func TestSync_AdapterSyncFailedEventCarriesErrPayload(t *testing.T) {
	db := newSyncTestDB(t)
	adapter := &stubAdapter{listErr: fmt.Errorf("adapter list: gh issue list: exit status 4")}
	syncer := mustNew(t, adaptersync.Config{Adapter: adapter, DB: db})

	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	if err := syncer.Sync(context.Background(), now); err == nil {
		t.Fatal("Sync want error on List failure")
	}

	row := db.SQL().QueryRowContext(context.Background(),
		`SELECT payload_json FROM events WHERE kind = ? ORDER BY id DESC LIMIT 1`,
		string(obs.EventAdapterSyncFailed))
	var payload string
	if err := row.Scan(&payload); err != nil {
		t.Fatalf("scan payload: %v", err)
	}
	if !strings.Contains(payload, "adapter list: gh issue list: exit status 4") {
		t.Fatalf("payload missing err msg: %q", payload)
	}
}

// TestSync_AdapterSyncFailedRedactsSecretsAndTruncates asserts payload scrubber strips known token shapes + caps length (R6-Bug-1 reviewer R1+R2).
func TestSync_AdapterSyncFailedRedactsSecretsAndTruncates(t *testing.T) {
	db := newSyncTestDB(t)
	leaked := "boom GH_TOKEN=ghp_AAAAAAAAAAAAAAAAAAAAAAAA stuff lin_api_BBBBBBBBBBBBBBBBBBBB more"
	adapter := &stubAdapter{listErr: fmt.Errorf("%s", leaked+strings.Repeat(" pad", 2000))}
	syncer := mustNew(t, adaptersync.Config{Adapter: adapter, DB: db})

	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	if err := syncer.Sync(context.Background(), now); err == nil {
		t.Fatal("Sync want error")
	}

	row := db.SQL().QueryRowContext(context.Background(),
		`SELECT payload_json FROM events WHERE kind = ? ORDER BY id DESC LIMIT 1`,
		string(obs.EventAdapterSyncFailed))
	var payload string
	if err := row.Scan(&payload); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if strings.Contains(payload, "ghp_AAAAAAAAAAAAAAAAAAAAAAAA") {
		t.Fatalf("payload leaked gh token: %q", payload)
	}
	if strings.Contains(payload, "lin_api_BBBBBBBBBBBBBBBBBBBB") {
		t.Fatalf("payload leaked linear token: %q", payload)
	}
	if !strings.Contains(payload, "[REDACTED]") {
		t.Fatalf("payload missing [REDACTED] marker: %q", payload)
	}
	if !strings.Contains(payload, "...[truncated]") {
		t.Fatalf("payload not truncated (length=%d): %q", len(payload), payload[:min(200, len(payload))])
	}
}

// TestSync_EmitsAdapterSyncSyncedEventOnRecovery asserts adaptersync.synced fires only on failed→ok transition.
func TestSync_EmitsAdapterSyncSyncedEventOnRecovery(t *testing.T) {
	db := newSyncTestDB(t)
	adapter := &stubAdapter{listErr: fmt.Errorf("boom")}
	syncer := mustNew(t, adaptersync.Config{Adapter: adapter, DB: db})
	base := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

	_ = syncer.Sync(context.Background(), base)

	adapter.listErr = nil
	adapter.items = []schemas.WorkItem{{ID: "OK", Kind: schemas.KindFeature, Title: "ok", Lane: "server", Status: schemas.StatusPlanned}}
	if err := syncer.Sync(context.Background(), base.Add(time.Minute)); err != nil {
		t.Fatalf("recovery Sync: %v", err)
	}

	if err := syncer.Sync(context.Background(), base.Add(2*time.Minute)); err != nil {
		t.Fatalf("steady Sync: %v", err)
	}

	row := db.SQL().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE kind = ?`, string(obs.EventAdapterSyncSynced))
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count adaptersync.synced: %v", err)
	}
	if n != 1 {
		t.Fatalf("adaptersync.synced event count=%d; want exactly 1 (one failed→ok transition)", n)
	}
}

// TestSync_SustainedEmptyDowngradesToDebug asserts empty steady-state polls do not spam WARN.
func TestSync_SustainedEmptyDowngradesToDebug(t *testing.T) {
	logs := captureLogs(t)
	db := newSyncTestDB(t)
	empty := &stubAdapter{items: nil}
	syncer := mustNew(t, adaptersync.Config{Adapter: empty, DB: db})

	base := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := syncer.Sync(context.Background(), base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("Sync %d: %v", i, err)
		}
	}

	warnCount := strings.Count(logs.String(), "level=WARN msg=adapter.empty_list")
	if warnCount > 1 {
		t.Fatalf("adapter.empty_list WARN fired %d times across 5 sustained-empty polls; want at most 1 (steady-state must drop to DEBUG)", warnCount)
	}
}

// TestSync_DedupsDuplicateIDsInSameTick asserts duplicate adapter IDs in one tick surface a bug, not two upserts.
func TestSync_DedupsDuplicateIDsInSameTick(t *testing.T) {
	logs := captureLogs(t)
	db := newSyncTestDB(t)
	adapter := &stubAdapter{items: []schemas.WorkItem{
		{ID: "DUP", Kind: schemas.KindFeature, Title: "first", Lane: "server", Status: schemas.StatusPlanned},
		{ID: "DUP", Kind: schemas.KindFeature, Title: "second", Lane: "server", Status: schemas.StatusPlanned},
	}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	syncer := mustNew(t, adaptersync.Config{Adapter: adapter, DB: db})

	if err := syncer.Sync(context.Background(), now); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	got, err := db.GetWorkItem(context.Background(), "DUP")
	if err != nil {
		t.Fatalf("GetWorkItem DUP: %v", err)
	}
	if got.Title != "first" {
		t.Fatalf("DUP.title=%q want %q (first occurrence wins, second deduped)", got.Title, "first")
	}
	if !strings.Contains(logs.String(), "adapter.duplicate_id") {
		t.Fatalf("logs missing adapter.duplicate_id warn:\n%s", logs.String())
	}
}

// TestSync_CascadeReconcilerConverges asserts next Sync archives orphan children after a mid-cascade crash (idempotent).
func TestSync_CascadeReconcilerConverges(t *testing.T) {
	db := newSyncTestDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

	seed := &stubAdapter{items: []schemas.WorkItem{
		{ID: "PROG-X", Kind: schemas.KindProgram, Title: "p", Lane: "server", Status: schemas.StatusPlanned},
	}}
	if err := mustNew(t, adaptersync.Config{Adapter: seed, DB: db}).Sync(ctx, t0); err != nil {
		t.Fatalf("seed Sync: %v", err)
	}
	// Children come from a different writer (BriefLoader in
	// production; SourceBrief here so the adapter tombstone sweep
	// won't touch them).
	for _, id := range []string{"CHILD-A", "CHILD-B"} {
		child := state.WorkItem{
			ID: id, Kind: state.KindFeature, Title: id, Lane: "server",
			Status: state.WorkStatusRunning, ParentProgramID: "PROG-X",
		}
		if err := db.UpsertWorkItem(ctx, child, state.SourceBrief, t0); err != nil {
			t.Fatalf("seed child %s: %v", id, err)
		}
	}

	// Simulate a prior crash: PROG-X archived directly but children
	// still live. (Real-world equivalent: previous tick crashed
	// between TombstoneBySource and CascadeArchiveChildren.)
	if _, err := db.SQL().ExecContext(ctx,
		`UPDATE work_items SET status='archived', updated_at=? WHERE id='PROG-X'`, t0.Unix()); err != nil {
		t.Fatalf("force-archive PROG-X: %v", err)
	}

	// Next tick: adapter no longer returns PROG-X. The reconciler
	// catches orphan children even though PROG-X is already archived
	// (so not in TombstoneBySource's RETURNING set).
	next := &stubAdapter{items: []schemas.WorkItem{
		{ID: "KEEP", Kind: schemas.KindFeature, Title: "k", Lane: "server", Status: schemas.StatusPlanned},
	}}
	if err := mustNew(t, adaptersync.Config{Adapter: next, DB: db}).Sync(ctx, t0.Add(time.Minute)); err != nil {
		t.Fatalf("next Sync: %v", err)
	}

	for _, id := range []string{"CHILD-A", "CHILD-B"} {
		got, err := db.GetWorkItem(ctx, id)
		if err != nil {
			t.Fatalf("GetWorkItem %s: %v", id, err)
		}
		if got.Status != state.WorkStatusArchived {
			t.Fatalf("%s.status=%s want archived (reconciler must converge)", id, got.Status)
		}
	}

	if err := mustNew(t, adaptersync.Config{Adapter: next, DB: db}).Sync(ctx, t0.Add(2*time.Minute)); err != nil {
		t.Fatalf("idempotent Sync: %v", err)
	}
}

// TestSync_CascadeReconciler_EmitsPerChildEvent asserts one log entry per cascade-archived child, not per parent (rubric §6).
func TestSync_CascadeReconciler_EmitsPerChildEvent(t *testing.T) {
	logs := captureLogs(t)
	db := newSyncTestDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

	seed := &stubAdapter{items: []schemas.WorkItem{
		{ID: "PROG-Y", Kind: schemas.KindProgram, Title: "p", Lane: "server", Status: schemas.StatusPlanned},
	}}
	if err := mustNew(t, adaptersync.Config{Adapter: seed, DB: db}).Sync(ctx, t0); err != nil {
		t.Fatalf("seed Sync: %v", err)
	}
	for _, id := range []string{"CHILD-1", "CHILD-2", "CHILD-3"} {
		child := state.WorkItem{
			ID: id, Kind: state.KindFeature, Title: id, Lane: "server",
			Status: state.WorkStatusRunning, ParentProgramID: "PROG-Y",
		}
		if err := db.UpsertWorkItem(ctx, child, state.SourceBrief, t0); err != nil {
			t.Fatalf("seed child %s: %v", id, err)
		}
	}
	if _, err := db.SQL().ExecContext(ctx,
		`UPDATE work_items SET status='archived', updated_at=? WHERE id='PROG-Y'`, t0.Unix()); err != nil {
		t.Fatalf("force-archive PROG-Y: %v", err)
	}

	next := &stubAdapter{items: []schemas.WorkItem{
		{ID: "KEEP", Kind: schemas.KindFeature, Title: "k", Lane: "server", Status: schemas.StatusPlanned},
	}}
	if err := mustNew(t, adaptersync.Config{Adapter: next, DB: db}).Sync(ctx, t0.Add(time.Minute)); err != nil {
		t.Fatalf("next Sync: %v", err)
	}

	out := logs.String()
	for _, id := range []string{"CHILD-1", "CHILD-2", "CHILD-3"} {
		needle := "child.cascade_archived"
		if !strings.Contains(out, needle) {
			t.Fatalf("logs missing %s event:\n%s", needle, out)
		}
		if !strings.Contains(out, "child="+id) {
			t.Fatalf("logs missing child=%s:\n%s", id, out)
		}
	}
	if !strings.Contains(out, "parent=PROG-Y") {
		t.Fatalf("logs missing parent=PROG-Y:\n%s", out)
	}
	count := strings.Count(out, "child.cascade_archived")
	if count != 3 {
		t.Fatalf("child.cascade_archived emitted %d times, want 3:\n%s", count, out)
	}
}

// TestSync_LogFieldRenamedToCutoff asserts tombstone log field renamed from "at" to "cutoff" (stable grep key).
func TestSync_LogFieldRenamedToCutoff(t *testing.T) {
	logs := captureLogs(t)
	db := newSyncTestDB(t)
	ctx := context.Background()
	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

	seed := &stubAdapter{items: []schemas.WorkItem{
		{ID: "TOMB-ME", Kind: schemas.KindFeature, Title: "x", Lane: "server", Status: schemas.StatusPlanned},
	}}
	if err := mustNew(t, adaptersync.Config{Adapter: seed, DB: db}).Sync(ctx, t0); err != nil {
		t.Fatal(err)
	}

	next := &stubAdapter{items: []schemas.WorkItem{
		{ID: "KEEP", Kind: schemas.KindFeature, Title: "k", Lane: "server", Status: schemas.StatusPlanned},
	}}
	if err := mustNew(t, adaptersync.Config{Adapter: next, DB: db}).Sync(ctx, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	out := logs.String()
	if !strings.Contains(out, "adapter.tombstoned") {
		t.Fatalf("logs missing adapter.tombstoned event:\n%s", out)
	}
	if !strings.Contains(out, "cutoff=") {
		t.Fatalf("logs missing cutoff= field:\n%s", out)
	}
	if strings.Contains(out, " at=") {
		t.Fatalf("legacy 'at=' field still present; must be renamed to cutoff:\n%s", out)
	}
}

// TestAdapterSync_LoggerInjected pins spec §5.8 + Task H contract.
func TestAdapterSync_LoggerInjected(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := obstest.New()
	logger := slog.New(h)

	db := newSyncTestDB(t)
	// Item with unknown_status triggers slog.Warn("adapter.item_skipped")
	// on the warn path — the canonical "this should route through the
	// injected logger" exercise.
	adapter := &stubAdapter{items: []schemas.WorkItem{
		{ID: "BAD", Kind: schemas.KindFeature, Title: "x", Lane: "server", Status: schemas.StatusInProgress},
	}}
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	syncer := mustNew(t, adaptersync.Config{Adapter: adapter, DB: db, Logger: logger})

	if err := syncer.Sync(context.Background(), now); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	msgs := h.Messages()
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "adapter.item_skipped") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("injected logger received no adapter.item_skipped record; got %v", msgs)
	}
}

// TestSyncer_Config_RequiresAdapter — partial Config (incl. Logger-only) must error at New, not nil-deref later.
func TestSyncer_Config_RequiresAdapter(t *testing.T) {
	db := newSyncTestDB(t)
	if _, err := adaptersync.New(adaptersync.Config{DB: db}); err == nil {
		t.Fatal("New with nil Adapter must error")
	} else if !strings.Contains(err.Error(), "Adapter") {
		t.Fatalf("error %q missing Adapter mention", err.Error())
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := adaptersync.New(adaptersync.Config{DB: db, Logger: logger}); err == nil {
		t.Fatal("New with nil Adapter (Logger set) must still error")
	}
}

// TestSyncer_Config_RequiresDB — nil DB used to defer the panic to UpsertWorkItem; New now surfaces it eagerly.
func TestSyncer_Config_RequiresDB(t *testing.T) {
	adapter := &stubAdapter{}
	if _, err := adaptersync.New(adaptersync.Config{Adapter: adapter}); err == nil {
		t.Fatal("New with nil DB must error")
	} else if !strings.Contains(err.Error(), "DB") {
		t.Fatalf("error %q missing DB mention", err.Error())
	}
	if _, err := adaptersync.New(adaptersync.Config{}); err == nil {
		t.Fatal("New with empty Config must error")
	}
}
