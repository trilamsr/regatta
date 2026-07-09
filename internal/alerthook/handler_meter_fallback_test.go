package alerthook

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

var errStubCounter = errors.New("stub-counter-fail")

// stubFailMeter embeds noop.Meter and overrides Int64Counter to return
// errStubCounter, so init()'s primary counter path fails and the
// fallback path is exercised.
type stubFailMeter struct {
	noop.Meter
}

func (stubFailMeter) Int64Counter(string, ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	return nil, errStubCounter
}

// TestHandlerInitLogsPrimaryMeterError asserts init() warn-logs when primary Int64Counter fails.
func TestHandlerInitLogsPrimaryMeterError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	h := &Handler{
		Client: &fakeGitHub{},
		Logger: logger,
		Meter:  stubFailMeter{},
	}
	h.init()

	out := buf.String()
	if !strings.Contains(out, "alerthook.meter_primary_failed") {
		t.Fatalf("expected primary-meter warn log; got %q", out)
	}
	if !strings.Contains(out, errStubCounter.Error()) {
		t.Fatalf("expected primary error %q in log; got %q", errStubCounter.Error(), out)
	}
}
