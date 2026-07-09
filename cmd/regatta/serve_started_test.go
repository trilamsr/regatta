package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestServe_EmitsServeStarted asserts one serve.started substrate row + INFO log on boot (R-MEGA-2 P6).
func TestServe_EmitsServeStarted(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "serve-started.db")
	clock := func() time.Time { return time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC) }
	db, err := state.OpenWithClock(ctx, state.DSN(dbPath), clock)
	if err != nil {
		t.Fatalf("OpenWithClock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	bootStart := time.Now().Add(-50 * time.Millisecond)
	emitServeStarted(ctx, db, bootStart, log)

	events, err := db.ListEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	var got *state.StateEvent
	for i := range events {
		if events[i].Kind == string(obs.EventServeStarted) {
			got = &events[i]
		}
	}
	if got == nil {
		t.Fatalf("serve.started event not recorded; rows=%+v", events)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(got.PayloadJSON), &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if payload["version"] != version {
		t.Errorf("payload version=%v want %q", payload["version"], version)
	}
	if v, ok := payload["boot_duration_ms"].(float64); !ok || v < 0 {
		t.Errorf("boot_duration_ms missing or negative: %v", payload["boot_duration_ms"])
	}
	if !strings.Contains(buf.String(), "serve.started") {
		t.Errorf("INFO log missing serve.started: %s", buf.String())
	}
}

// TestServe_EmitsServeStarted_NilDBStillLogs asserts nil-DB still emits the INFO marker (R-MEGA-2 P6).
func TestServe_EmitsServeStarted_NilDBStillLogs(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	emitServeStarted(context.Background(), nil, time.Now(), log)
	if !strings.Contains(buf.String(), "serve.started") {
		t.Fatalf("INFO log missing serve.started with nil DB: %s", buf.String())
	}
}
