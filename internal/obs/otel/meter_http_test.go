package otel_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"

	otelpkg "github.com/trilamsr/regatta/internal/obs/otel"
)

// TestMeterSetup_OTLPHTTPProtocol_PostsToHTTPReceiver asserts http/protobuf routes through otlpmetrichttp (silent-drop fix).
func TestMeterSetup_OTLPHTTPProtocol_PostsToHTTPReceiver(t *testing.T) {
	var (
		hits atomic.Int64
		mu   sync.Mutex
		ct   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		mu.Lock()
		ct = r.Header.Get("Content-Type")
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_METRICS_PROMETHEUS_PORT", "")
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "100")

	shutdown, err := otelpkg.SetupMeter(context.Background(), otelpkg.Config{ServiceName: "regatta"})
	if err != nil {
		t.Fatalf("SetupMeter err = %v; want nil", err)
	}

	meter := otel.Meter("http-protocol-test")
	ctr, err := meter.Int64Counter("regatta_http_protocol_counter")
	if err != nil {
		t.Fatalf("Int64Counter err = %v", err)
	}
	ctr.Add(context.Background(), 1)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown err = %v; want nil", err)
	}

	if hits.Load() == 0 {
		t.Fatal("OTLP HTTP receiver got zero POSTs; SDK silently fell back to gRPC")
	}
	mu.Lock()
	got := ct
	mu.Unlock()
	if !strings.Contains(got, "application/x-protobuf") {
		t.Errorf("Content-Type = %q; want application/x-protobuf", got)
	}
}
