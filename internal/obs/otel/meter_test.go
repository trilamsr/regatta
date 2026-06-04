package otel_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	colmetricpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/grpc"

	otelpkg "github.com/trilamsr/regatta/internal/obs/otel"
	"github.com/trilamsr/regatta/internal/testutil"
)

// TestMeterSetup_NoEnvVar_ReturnsNoopMeter pins the zero-env path.
func TestMeterSetup_NoEnvVar_ReturnsNoopMeter(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	t.Setenv("OTEL_METRICS_PROMETHEUS_PORT", "")

	shutdown, err := otelpkg.SetupMeter(context.Background(), otelpkg.Config{ServiceName: "regatta"})
	if err != nil {
		t.Fatalf("SetupMeter err = %v; want nil", err)
	}
	if shutdown == nil {
		t.Fatal("SetupMeter returned nil shutdown; want noop closure")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("noop shutdown err = %v; want nil", err)
	}
}

// TestMeterSetup_BothExportersSet_ReturnsConflict pins the mutex validator.
func TestMeterSetup_BothExportersSet_ReturnsConflict(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "localhost:4317")
	t.Setenv("OTEL_METRICS_PROMETHEUS_PORT", "0")

	shutdown, err := otelpkg.SetupMeter(context.Background(), otelpkg.Config{ServiceName: "regatta"})
	if !errors.Is(err, otelpkg.ErrOTelMetricExporterConflict) {
		t.Errorf("SetupMeter err = %v; want ErrOTelMetricExporterConflict", err)
	}
	if shutdown != nil {
		t.Errorf("SetupMeter returned non-nil shutdown on conflict; want nil")
	}
}

// TestMeterSetup_OTLPExporter_WiresToCollector pins OTLP metric export.
func TestMeterSetup_OTLPExporter_WiresToCollector(t *testing.T) {
	srv, addr, sink := startStubOTLPMetricCollector(t)
	t.Cleanup(srv.Stop)

	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "http://"+addr)
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_INSECURE", "true")
	t.Setenv("OTEL_METRICS_PROMETHEUS_PORT", "")
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "100")

	shutdown, err := otelpkg.SetupMeter(context.Background(), otelpkg.Config{ServiceName: "regatta"})
	if err != nil {
		t.Fatalf("SetupMeter err = %v; want nil", err)
	}

	meter := otel.Meter("meter-test")
	ctr, err := meter.Int64Counter("regatta_test_counter")
	if err != nil {
		t.Fatalf("Int64Counter err = %v", err)
	}
	ctr.Add(context.Background(), 1)

	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown err = %v; want nil", err)
	}

	names := sink.metricNames()
	if len(names) == 0 {
		t.Fatal("stub OTLP collector received zero metrics; want >= 1")
	}
	var found bool
	for _, n := range names {
		if n == "regatta_test_counter" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected metric 'regatta_test_counter' in collector sink; got %v", names)
	}
}

// TestMeterSetup_PrometheusExporterWiresOnEnvVar pins the /metrics scrape.
func TestMeterSetup_PrometheusExporterWiresOnEnvVar(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	// Ephemeral port: bind to 0 lets the kernel pick; the handler still
	// reads OTEL_METRICS_PROMETHEUS_PORT to gate the wire.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	tcpAddr, ok := lis.Addr().(*net.TCPAddr)
	if !ok {
		_ = lis.Close()
		t.Fatalf("listener addr type = %T; want *net.TCPAddr", lis.Addr())
	}
	port := tcpAddr.Port
	_ = lis.Close()
	t.Setenv("OTEL_METRICS_PROMETHEUS_PORT", strconv.Itoa(port))

	shutdown, err := otelpkg.SetupMeter(context.Background(), otelpkg.Config{ServiceName: "regatta"})
	if err != nil {
		t.Fatalf("SetupMeter err = %v; want nil", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	meter := otel.Meter("prom-test")
	ctr, err := meter.Int64Counter("regatta_prom_counter")
	if err != nil {
		t.Fatalf("Int64Counter err = %v", err)
	}
	ctr.Add(context.Background(), 7)

	pollCtx, pollCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer pollCancel()
	var body string
	testutil.EventuallyT(t, pollCtx, 20*time.Millisecond, func() bool {
		resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/metrics")
		if err != nil {
			return false
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		body = string(b)
		return strings.Contains(body, "regatta_prom_counter")
	}, "Prometheus scrape did not surface counter")
	if !strings.Contains(body, "regatta_prom_counter") {
		t.Errorf("Prometheus /metrics scrape missing regatta_prom_counter; body=%q", body)
	}
}

// stubMetricSink captures the metric names a stub gRPC OTLP collector
// sees so the export-wiring test can assert on flushed metrics after shutdown.
type stubMetricSink struct {
	colmetricpb.UnimplementedMetricsServiceServer
	mu   sync.Mutex
	flat []string
}

func (s *stubMetricSink) Export(_ context.Context, req *colmetricpb.ExportMetricsServiceRequest) (*colmetricpb.ExportMetricsServiceResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rm := range req.GetResourceMetrics() {
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				s.flat = append(s.flat, m.GetName())
			}
		}
	}
	return &colmetricpb.ExportMetricsServiceResponse{}, nil
}

func (s *stubMetricSink) metricNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.flat))
	copy(out, s.flat)
	return out
}

func startStubOTLPMetricCollector(t *testing.T) (*grpc.Server, string, *stubMetricSink) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	sink := &stubMetricSink{}
	colmetricpb.RegisterMetricsServiceServer(srv, sink)
	go func() { _ = srv.Serve(lis) }()
	return srv, lis.Addr().String(), sink
}
