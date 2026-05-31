package spawner

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
)

// captureHandler mirrors the orchestrator's test handler — records every
// slog.Record so assertions can pin which obs events the spawner emitted
// through its injected logger (spec §5.3).
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }

func (h *captureHandler) WithGroup(name string) slog.Handler { return h }

func (h *captureHandler) records_() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

func (h *captureHandler) findEvent(name obs.EventName) (slog.Record, bool) {
	for _, r := range h.records_() {
		if r.Message == string(name) {
			return r, true
		}
	}
	return slog.Record{}, false
}

func recordAttr(r slog.Record, key string) (slog.Value, bool) {
	var found slog.Value
	var ok bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = a.Value
			ok = true
			return false
		}
		return true
	})
	return found, ok
}

// TestSpawner_Spawn_EmitsStartedAndCompleted asserts injected-logger emission of spawn.started + spawn.completed (spec §5.3).
func TestSpawner_Spawn_EmitsStartedAndCompleted(t *testing.T) {
	ctx := context.Background()
	db := openSpawnerTestDB(t)
	seedPlannedWI(t, db, "F-1")

	h := &captureHandler{}
	logger := slog.New(h)

	sp := NewStubWithDB(db).WithLogger(logger)

	if _, err := sp.Spawn(ctx, Request{AgentID: 7, WorkItemID: "F-1", Lane: "server"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := sp.Complete(ctx, "F-1", json.RawMessage(`{"completed":true}`)); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	started, ok := h.findEvent(obs.EventSpawnStarted)
	if !ok {
		t.Fatalf("missing %q event; got %d records", obs.EventSpawnStarted, len(h.records_()))
	}
	if v, ok := recordAttr(started, string(obs.KeyWorkItemID)); !ok || v.String() != "F-1" {
		t.Errorf("%s missing work_item_id=F-1; got %v ok=%v", obs.EventSpawnStarted, v, ok)
	}
	if v, ok := recordAttr(started, string(obs.KeyAgentID)); !ok || v.Int64() != 7 {
		t.Errorf("%s missing agent_id=7; got %v ok=%v", obs.EventSpawnStarted, v, ok)
	}
	if v, ok := recordAttr(started, string(obs.KeyLane)); !ok || v.String() != "server" {
		t.Errorf("%s missing lane=server; got %v ok=%v", obs.EventSpawnStarted, v, ok)
	}

	completed, ok := h.findEvent(obs.EventSpawnCompleted)
	if !ok {
		t.Fatalf("missing %q event; got %d records", obs.EventSpawnCompleted, len(h.records_()))
	}
	if v, ok := recordAttr(completed, string(obs.KeyWorkItemID)); !ok || v.String() != "F-1" {
		t.Errorf("%s missing work_item_id=F-1; got %v ok=%v", obs.EventSpawnCompleted, v, ok)
	}
	if v, ok := recordAttr(completed, string(obs.KeyDurationMs)); !ok || v.Int64() < 0 {
		t.Errorf("%s missing duration_ms>=0; got %v ok=%v", obs.EventSpawnCompleted, v, ok)
	}
}

// TestSpawner_SpawnFailed_EmitsErr asserts the error path emits spawn.failed with KeyErr (spec §5.3).
func TestSpawner_SpawnFailed_EmitsErr(t *testing.T) {
	ctx := context.Background()
	db := openSpawnerTestDB(t)
	seedPlannedWI(t, db, "F-1")

	h := &captureHandler{}
	logger := slog.New(h)

	sp := NewStubWithDB(db).WithLogger(logger)

	if _, err := sp.Spawn(ctx, Request{AgentID: 9, WorkItemID: "F-1", Lane: "server"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	// Non-JSON payload trips AppendOutput's canonicaliser; Complete must
	// surface a spawn.failed event before returning the error.
	if err := sp.Complete(ctx, "F-1", json.RawMessage(`not-json`)); err == nil {
		t.Fatal("Complete with invalid payload must fail")
	}

	failed, ok := h.findEvent(obs.EventSpawnFailed)
	if !ok {
		t.Fatalf("missing %q event; got %d records", obs.EventSpawnFailed, len(h.records_()))
	}
	if v, ok := recordAttr(failed, string(obs.KeyErr)); !ok || v.String() == "" {
		t.Errorf("%s missing err; got %v ok=%v", obs.EventSpawnFailed, v, ok)
	}
	if v, ok := recordAttr(failed, string(obs.KeyWorkItemID)); !ok || v.String() != "F-1" {
		t.Errorf("%s missing work_item_id=F-1; got %v ok=%v", obs.EventSpawnFailed, v, ok)
	}
}

// TestSpawner_NilLogger_UsesDefault guards the slog.Default() fallback (spec §4.1).
func TestSpawner_NilLogger_UsesDefault(t *testing.T) {
	ctx := context.Background()
	sp := NewStub()
	if _, err := sp.Spawn(ctx, Request{AgentID: 1, WorkItemID: "WI", Lane: "server"}); err != nil {
		t.Fatalf("Spawn with nil logger panicked or errored: %v", err)
	}
}
