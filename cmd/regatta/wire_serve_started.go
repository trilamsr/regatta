package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// emitServeStarted records the serve.started boot-completion marker so
// the operator's journal carries an explicit ready signal with version
// and boot duration. Failures are best-effort: a substrate write that
// fails here would not justify aborting boot (R-MEGA-2 P6).
func emitServeStarted(ctx context.Context, db *state.DB, bootStart time.Time, log *slog.Logger) {
	payload := struct {
		Version       string `json:"version"`
		BootDurationMs int64  `json:"boot_duration_ms"`
	}{
		Version:       version,
		BootDurationMs: time.Since(bootStart).Milliseconds(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Warn("serve.started_payload_marshal_failed", "err", err.Error())
		return
	}
	if db != nil {
		if err := db.RecordEvent(ctx, 0, string(obs.EventServeStarted), string(raw)); err != nil {
			log.Warn("serve.started_record_failed", "err", err.Error())
		}
	}
	log.Info("serve.started",
		"version", payload.Version,
		"boot_duration_ms", payload.BootDurationMs,
	)
}
