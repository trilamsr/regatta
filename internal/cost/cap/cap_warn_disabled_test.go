package costcap

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/cost/spend"
)

// TestEnforcer_CapZero_LogsWarn asserts cap.disabled WARN fires when CapMicro==0 (R-MEGA-2 C5).
func TestEnforcer_CapZero_LogsWarn(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	log := slog.New(h)

	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	_, err := New(Config{
		CapMicro: 0,
		TenantID: "default",
		Spend:    &fakeSpend{},
		Recorder: &fakeRecorder{},
		Resume:   &fakeResume{},
		Clock:    func() time.Time { return now },
		Logger:   log,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "cap.disabled") {
		t.Fatalf("missing cap.disabled WARN; got %q", out)
	}
	if !strings.Contains(out, "tenant_id=default") {
		t.Fatalf("missing tenant_id attr; got %q", out)
	}
}

// TestEnforcer_CapNonZero_NoWarn asserts no cap.disabled WARN when cap is configured (R-MEGA-2 C5).
func TestEnforcer_CapNonZero_NoWarn(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	log := slog.New(h)

	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	e, err := New(Config{
		CapMicro: spend.FromUSD(50),
		TenantID: "default",
		Spend:    &fakeSpend{},
		Recorder: &fakeRecorder{},
		Resume:   &fakeResume{},
		Clock:    func() time.Time { return now },
		Logger:   log,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_ = e.Allow(context.Background())
	if strings.Contains(buf.String(), "cap.disabled") {
		t.Fatalf("unexpected cap.disabled WARN with cap configured; got %q", buf.String())
	}
}
