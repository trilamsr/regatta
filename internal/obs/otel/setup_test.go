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
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
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

// TestSetup_CostAttrsSurviveParentBasedSampling pins cost-governor spec §3.7 — regatta.cost.* attrs on llm_call survive operator-set ParentBased sampling.
func TestSetup_CostAttrsSurviveParentBasedSampling(t *testing.T) {
	srv, addr, sink := startStubOTLPCollector(t)
	t.Cleanup(srv.Stop)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+addr)
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_INSECURE", "true")
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "50")
	// parentbased_traceidratio with arg=1.0 is the SDK's "sample every
	// trace" knob through the same ParentBased decorator operators
	// reach for in prod. Pins the contract that operator-chosen
	// sampling does not strip the cost attr set off the llm_call span.
	t.Setenv("OTEL_TRACES_SAMPLER", "parentbased_traceidratio")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "1.0")

	shutdown, err := otelpkg.Setup(context.Background(), otelpkg.Config{
		ServiceName: "regatta",
	})
	if err != nil {
		t.Fatalf("Setup err = %v; want nil", err)
	}

	// llm_call is the W6 spec name; runtime emits "chat <model>" per
	// W6 §3.4 A2. Either form carries the regatta.cost.* attrs identically
	// — the assertion targets the attr set, not the span name.
	tracer := otel.Tracer("setup-test")
	_, span := tracer.Start(context.Background(), "chat claude-sonnet-4-7",
		otrace.WithSpanKind(otrace.SpanKindClient),
		otrace.WithAttributes(
			attribute.String("regatta.cost.error", "pricing_missing"),
			attribute.Float64("regatta.cost.usd_estimate", 0.42),
			attribute.Bool("regatta.cost.allow", true),
		),
	)
	span.End()

	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown err = %v; want nil", err)
	}

	spans := sink.spans()
	if len(spans) == 0 {
		t.Fatal("stub collector received zero spans; want >= 1 under parentbased_traceidratio arg=1.0")
	}

	var got *stubSpan
	for i := range spans {
		if spans[i].name == "chat claude-sonnet-4-7" {
			got = &spans[i]
			break
		}
	}
	if got == nil {
		names := make([]string, 0, len(spans))
		for _, s := range spans {
			names = append(names, s.name)
		}
		t.Fatalf("llm_call-shaped span absent from sink; got names %v", names)
	}

	wantAttrs := map[string]any{
		"regatta.cost.error":        "pricing_missing",
		"regatta.cost.usd_estimate": 0.42,
		"regatta.cost.allow":        true,
	}
	for k, want := range wantAttrs {
		gotVal, ok := got.attrs[k]
		if !ok {
			t.Errorf("attr %q absent from exported span; sampling stripped the cost attr set", k)
			continue
		}
		if gotVal != want {
			t.Errorf("attr %q = %v (%T); want %v (%T)", k, gotVal, gotVal, want, want)
		}
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
	name  string
	attrs map[string]any
}

func (s *stubSpanSink) Export(_ context.Context, req *coltrace.ExportTraceServiceRequest) (*coltrace.ExportTraceServiceResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rs := range req.GetResourceSpans() {
		for _, ss := range rs.GetScopeSpans() {
			for _, sp := range ss.GetSpans() {
				kvs := sp.GetAttributes()
				attrs := make(map[string]any, len(kvs))
				for _, kv := range kvs {
					attrs[kv.GetKey()] = anyValueToGo(kv.GetValue())
				}
				s.flat = append(s.flat, stubSpan{name: sp.GetName(), attrs: attrs})
			}
		}
	}
	return &coltrace.ExportTraceServiceResponse{}, nil
}

// anyValueToGo unwraps an OTLP AnyValue oneof into the native Go type
// the test cases compare against — keeps the stubSpan API ergonomic
// without leaking proto types into per-test assertions.
func anyValueToGo(v *commonpb.AnyValue) any {
	if v == nil {
		return nil
	}
	switch v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return v.GetStringValue()
	case *commonpb.AnyValue_BoolValue:
		return v.GetBoolValue()
	case *commonpb.AnyValue_IntValue:
		return v.GetIntValue()
	case *commonpb.AnyValue_DoubleValue:
		return v.GetDoubleValue()
	default:
		return nil
	}
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
