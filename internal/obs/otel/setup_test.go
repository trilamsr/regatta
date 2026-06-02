package otel_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	otrace "go.opentelemetry.io/otel/trace"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"

	otelpkg "github.com/trilamsr/regatta/internal/obs/otel"
)

func TestSetup_NoEnvVar_ReturnsNoopShutdown(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")

	before := runtime.NumGoroutine()
	shutdown, err := otelpkg.Setup(context.Background(), otelpkg.Config{
		ServiceName:    "regatta",
		ServiceVersion: "test",
	})
	if err != nil {
		t.Fatalf("Setup err = %v; want nil", err)
	}
	if shutdown == nil {
		t.Fatal("Setup returned nil shutdown; want noop closure")
	}

	// Global provider that has never been set wraps a noop; the
	// invariant we care about is that an opened span is non-recording
	// (zero-cost, no exporter goroutine). Asserting that directly
	// dodges coupling to the global wrapper's concrete type.
	_, span := otel.Tracer("noop-probe").Start(context.Background(), "probe")
	if span.IsRecording() {
		t.Errorf("noop default tracer returned a recording span; want non-recording")
	}
	span.End()

	if err := shutdown(context.Background()); err != nil {
		t.Errorf("noop shutdown err = %v; want nil", err)
	}

	time.Sleep(20 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+1 {
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}
}

func TestSetup_DevStdoutAndOTLP_RejectsConflict(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")

	shutdown, err := otelpkg.Setup(context.Background(), otelpkg.Config{
		ServiceName: "regatta",
		DevStdout:   true,
	})
	if !errors.Is(err, otelpkg.ErrOTelExporterConflict) {
		t.Errorf("Setup err = %v; want ErrOTelExporterConflict", err)
	}
	if shutdown != nil {
		t.Errorf("Setup returned non-nil shutdown on conflict; want nil")
	}
}

func TestSetup_ShutdownIsIdempotent(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	var buf bytes.Buffer
	shutdown, err := otelpkg.Setup(context.Background(), otelpkg.Config{
		ServiceName: "regatta",
		DevStdout:   true,
		StdoutDest:  &buf,
	})
	if err != nil {
		t.Fatalf("Setup err = %v; want nil", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("first shutdown err = %v; want nil", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("second shutdown err = %v; want nil on idempotent call", err)
	}
}

func TestSetup_ResourceCarriesServiceNameAndTenant(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	var buf bytes.Buffer
	shutdown, err := otelpkg.Setup(context.Background(), otelpkg.Config{
		ServiceName:    "regatta",
		ServiceVersion: "v0.0.0-test",
		DevStdout:      true,
		StdoutDest:     &buf,
	})
	if err != nil {
		t.Fatalf("Setup err = %v; want nil", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	tracer := otel.Tracer("setup-test")
	_, span := tracer.Start(context.Background(), "probe")
	span.End()

	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown err = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"service.name"`) || !strings.Contains(out, `"regatta"`) {
		t.Errorf("stdout exporter output missing service.name=regatta:\n%s", out)
	}
	if !strings.Contains(out, "v0.0.0-test") {
		t.Errorf("stdout exporter output missing service.version=v0.0.0-test:\n%s", out)
	}
	if !strings.Contains(out, `"regatta.tenant_id"`) || !strings.Contains(out, `"default"`) {
		t.Errorf("stdout exporter output missing regatta.tenant_id=default:\n%s", out)
	}
}

func TestSetup_OTLPEndpoint_WiresExporter(t *testing.T) {
	srv, addr, sink := startStubOTLPCollector(t)
	t.Cleanup(srv.Stop)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+addr)
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_INSECURE", "true")
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "50")

	shutdown, err := otelpkg.Setup(context.Background(), otelpkg.Config{
		ServiceName:    "regatta",
		ServiceVersion: "v0.0.0-test",
	})
	if err != nil {
		t.Fatalf("Setup err = %v; want nil", err)
	}

	tracer := otel.Tracer("setup-test")
	_, span := tracer.Start(context.Background(), "probe",
		otrace.WithSpanKind(otrace.SpanKindClient))
	span.End()

	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown err = %v; want nil", err)
	}

	spans := sink.spans()
	if len(spans) == 0 {
		t.Fatal("stub OTLP collector received zero spans; want >= 1")
	}
	var found bool
	for _, s := range spans {
		if s.name == "probe" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, 0, len(spans))
		for _, s := range spans {
			names = append(names, s.name)
		}
		t.Errorf("expected span name 'probe' in collector sink; got %v", names)
	}
}

// TestSetup_HonorsOTELTracesSamplerEnv asserts the SDK env-var sampler contract.
func TestSetup_HonorsOTELTracesSamplerEnv(t *testing.T) {
	srv, addr, sink := startStubOTLPCollector(t)
	t.Cleanup(srv.Stop)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+addr)
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_INSECURE", "true")
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "50")
	// traceidratio with arg 0.0 is the SDK's documented "drop every
	// span" knob; we use the deterministic floor (0%) rather than a
	// fractional rate so the assertion is exact, not statistical.
	t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.0")

	shutdown, err := otelpkg.Setup(context.Background(), otelpkg.Config{
		ServiceName: "regatta",
	})
	if err != nil {
		t.Fatalf("Setup err = %v; want nil", err)
	}

	tracer := otel.Tracer("setup-test")
	const opened = 100
	for i := 0; i < opened; i++ {
		_, span := tracer.Start(context.Background(), "probe")
		span.End()
	}

	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown err = %v; want nil", err)
	}

	if got := len(sink.spans()); got != 0 {
		t.Errorf("OTEL_TRACES_SAMPLER=traceidratio arg=0.0 should drop all spans; "+
			"stub collector received %d of %d", got, opened)
	}
}

// stubSpanSink captures the span names a stub gRPC OTLP collector sees
// so the export-wiring test can assert on flushed spans after shutdown.
type stubSpanSink struct {
	coltrace.UnimplementedTraceServiceServer
	mu   sync.Mutex
	flat []stubSpan
}

type stubSpan struct {
	name string
}

func (s *stubSpanSink) Export(_ context.Context, req *coltrace.ExportTraceServiceRequest) (*coltrace.ExportTraceServiceResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rs := range req.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			for _, sp := range ss.GetSpans() {
				s.flat = append(s.flat, stubSpan{name: sp.GetName()})
			}
		}
	}
	return &coltrace.ExportTraceServiceResponse{}, nil
}

func (s *stubSpanSink) spans() []stubSpan {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]stubSpan, len(s.flat))
	copy(out, s.flat)
	return out
}

func startStubOTLPCollector(t *testing.T) (*grpc.Server, string, *stubSpanSink) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	sink := &stubSpanSink{}
	coltrace.RegisterTraceServiceServer(srv, sink)
	go func() { _ = srv.Serve(lis) }()
	return srv, lis.Addr().String(), sink
}
