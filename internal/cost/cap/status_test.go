package costcap

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/cost/spend"
)

// TestPrintStatus_Active_RendersHeadroom Active state shows headroom.
func TestPrintStatus_Active_RendersHeadroom(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(12.30)}
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	e := newTestEnforcer(t, spend.FromUSD(40), sp, &fakeRecorder{}, &fakeResume{}, func() time.Time { return now }, time.UTC, 0)
	var buf bytes.Buffer
	if err := PrintStatus(context.Background(), &buf, e); err != nil {
		t.Fatalf("PrintStatus: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Active", "headroom", "$12.30", "$40.00"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q\n%s", want, out)
		}
	}
}

// TestPrintStatus_Throttled_RendersAutoResume Throttled shows resume horizon.
func TestPrintStatus_Throttled_RendersAutoResume(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(50)}
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	e := newTestEnforcer(t, spend.FromUSD(40), sp, &fakeRecorder{}, &fakeResume{}, func() time.Time { return now }, time.UTC, 0)
	var buf bytes.Buffer
	if err := PrintStatus(context.Background(), &buf, e); err != nil {
		t.Fatalf("PrintStatus: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Throttled", "auto-resume at", "$50.00", "$40.00", "regatta resume"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q\n%s", want, out)
		}
	}
}

// TestPrintStatus_CapUnset_ExplainsDegradedMode CapMicro=0 explains per-scope path.
func TestPrintStatus_CapUnset_ExplainsDegradedMode(t *testing.T) {
	sp := &fakeSpend{value: spend.FromUSD(12.30)}
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	e := newTestEnforcer(t, 0, sp, &fakeRecorder{}, &fakeResume{}, func() time.Time { return now }, time.UTC, 0)
	var buf bytes.Buffer
	if err := PrintStatus(context.Background(), &buf, e); err != nil {
		t.Fatalf("PrintStatus: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"unset", "per-scope"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q\n%s", want, out)
		}
	}
}
