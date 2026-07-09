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

var (
	errStubPrimary  = errors.New("stub-primary-fail")
	errStubFallback = errors.New("stub-fallback-fail")
)

// stubFailMeter embeds noop.Meter and overrides Int64Counter to return
// the injected err, so init()'s counter path fails and the caller can
// assert the log path fires.
type stubFailMeter struct {
	noop.Meter
	err error
}

func (s stubFailMeter) Int64Counter(string, ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	return nil, s.err
}

// TestHandlerInitLogsPrimaryMeterError asserts init() warn-logs when primary Int64Counter fails.
func TestHandlerInitLogsPrimaryMeterError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	h := &Handler{
		Client: &fakeGitHub{},
		Logger: logger,
		Meter:  stubFailMeter{err: errStubPrimary},
	}
	h.init()

	out := buf.String()
	if !strings.Contains(out, "alerthook.meter_primary_failed") {
		t.Fatalf("expected primary-meter warn log; got %q", out)
	}
	if !strings.Contains(out, errStubPrimary.Error()) {
		t.Fatalf("expected primary error %q in log; got %q", errStubPrimary.Error(), out)
	}
	// Log fields for aggregation: scope identifies which meter failed,
	// counter names the instrument so a filter can pin exact bindings.
	if !strings.Contains(out, "scope=alerthook") {
		t.Fatalf("expected scope=alerthook in log; got %q", out)
	}
	if !strings.Contains(out, "counter=regatta.alerthook.alerts.total") {
		t.Fatalf("expected counter name in log; got %q", out)
	}
}

// TestHandlerInitLogsFallbackMeterError asserts init() error-logs when both primary and fallback fail.
func TestHandlerInitLogsFallbackMeterError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	h := &Handler{
		Client:         &fakeGitHub{},
		Logger:         logger,
		Meter:          stubFailMeter{err: errStubPrimary},
		fallbackMeter:  stubFailMeter{err: errStubFallback},
	}
	h.init()

	out := buf.String()
	if !strings.Contains(out, "alerthook.meter_fallback_failed") {
		t.Fatalf("expected fallback-meter error log; got %q", out)
	}
	if !strings.Contains(out, errStubFallback.Error()) {
		t.Fatalf("expected fallback error %q in log; got %q", errStubFallback.Error(), out)
	}
	if !strings.Contains(out, "scope=alerthook-fallback") {
		t.Fatalf("expected scope=alerthook-fallback in log; got %q", out)
	}
	if h.alertCounter != nil {
		t.Fatalf("alertCounter must stay nil when both meters fail; bump()'s nil-guard is the request-path floor")
	}
}
