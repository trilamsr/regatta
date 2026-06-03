package security

import (
	"context"
	"log/slog"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/slogutil"
)

func TestSecurityGate_EmitsVerdictOnCheck(t *testing.T) {
	h := obstest.New()
	cfg := Config{
		GateID: "security",
		DeterminismFloor: FloorConfig{
			Gitleaks:   ToolPin{Enabled: false},
			OSVScanner: ToolPin{Enabled: false},
		},
		AI:     AIConfig{Enabled: false},
		Logger: slog.New(h),
	}
	if _, err := Run(context.Background(), cfg, Input{PRSHA: "abc", RunID: "00000000-0000-0000-0000-000000000000"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	rec, ok := h.FindEvent(obs.EventGateVerdict)
	if !ok {
		t.Fatalf("gate.verdict event not emitted; records=%+v", h.Records())
	}
	gid, ok := slogutil.AttrValue(rec, string(obs.KeyGateID))
	if !ok {
		t.Fatalf("gate.verdict missing %s attr", obs.KeyGateID)
	}
	if gid.String() != "security" {
		t.Fatalf("gate_id=%q; want %q", gid.String(), "security")
	}
	if _, ok := slogutil.AttrValue(rec, string(obs.KeyVerdict)); !ok {
		t.Fatalf("gate.verdict missing %s attr", obs.KeyVerdict)
	}
	if _, ok := slogutil.AttrValue(rec, string(obs.KeyDurationMs)); !ok {
		t.Fatalf("gate.verdict missing %s attr", obs.KeyDurationMs)
	}
}
