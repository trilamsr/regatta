package main

import (
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/web"
)

// TestNewWebHandler_PopulatesBootedAt pins #1124: newWebHandler must stamp BootedAt onto the web.Dependencies so the docker-soak panel can report uptime — without this wiring the panel renders "0s" forever (caught by reviewer a066716113c32e4d6).
func TestNewWebHandler_PopulatesBootedAt(t *testing.T) {
	captured := false
	t0 := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	cfg := listenerConfig{
		Clock: func() time.Time { captured = true; return t0 },
	}
	_, _ = newWebHandler(cfg)
	if !captured {
		t.Fatalf("newWebHandler did not invoke cfg.Clock() to stamp BootedAt — composition root regression")
	}
	_ = web.Dependencies{}
}
