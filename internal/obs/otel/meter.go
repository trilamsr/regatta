package otel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	promexp "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// ErrOTelMetricExporterConflict surfaces when OTEL_EXPORTER_OTLP_METRICS_ENDPOINT
// AND OTEL_METRICS_PROMETHEUS_PORT are both set. Two wire formats from
// one process would double-emit measurements with divergent unit-suffix
// rules (Prometheus appends `_total`, OTLP does not), so the operator
// picks one or the other at boot.
var ErrOTelMetricExporterConflict = errors.New(
	"obs/otel: OTEL_EXPORTER_OTLP_METRICS_ENDPOINT cannot combine with OTEL_METRICS_PROMETHEUS_PORT")

// ErrMetricExporter wraps metric-exporter construction failures from
// the OTel SDK so callers branch on the boundary without coupling to
// upstream error types.
var ErrMetricExporter = errors.New("obs/otel: metric exporter init failed")

// SetupMeter wires the global OTel MeterProvider per spec §2.4. Exporter
// selection is env-var driven so operators stay on the SDK's documented
// contract:
//
//   - OTEL_EXPORTER_OTLP_METRICS_ENDPOINT set → otlpmetric exporter
//     (otlpmetrichttp when OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf,
//     otlpmetricgrpc otherwise; mismatch silently drops every push).
//   - OTEL_METRICS_PROMETHEUS_PORT set → Prometheus pull endpoint on /metrics.
//   - Both set → ErrOTelMetricExporterConflict.
//   - Neither → noop; the SDK's default noop provider wins and SetupMeter
//     allocates no exporter goroutines.
//
// Returns a ShutdownFunc the caller stores for clean process exit; the
// closure flushes the provider and stops the Prometheus HTTP listener
// (when running) exactly once.
func SetupMeter(ctx context.Context, cfg Config) (ShutdownFunc, error) {
	cfg = withDefaults(cfg)

	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") +
		os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")
	otlpSet := otlpEndpoint != ""
	promPort := os.Getenv("OTEL_METRICS_PROMETHEUS_PORT")
	promSet := promPort != ""

	if otlpSet && promSet {
		return nil, ErrOTelMetricExporterConflict
	}
	if !otlpSet && !promSet {
		return noopShutdown, nil
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResource, err)
	}

	if otlpSet {
		exp, err := newOTLPMetricExporter(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrMetricExporter, err)
		}
		mp := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
		)
		otel.SetMeterProvider(mp)
		return composedMeterShutdown(mp, nil), nil
	}

	// Prometheus pull-mode: the exporter registers itself as a Reader
	// on the MeterProvider AND exposes a prometheus.Gatherer the HTTP
	// handler scrapes. The handler runs in its own goroutine; shutdown
	// drains it via http.Server.Shutdown.
	reg := prometheus.NewRegistry()
	exp, err := promexp.New(promexp.WithRegisterer(reg))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMetricExporter, err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(exp),
	)
	otel.SetMeterProvider(mp)

	port, err := strconv.Atoi(promPort)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid OTEL_METRICS_PROMETHEUS_PORT %q: %w",
			ErrMetricExporter, promPort, err)
	}
	srv, err := startPrometheusServer(port, reg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMetricExporter, err)
	}
	return composedMeterShutdown(mp, srv), nil
}

func startPrometheusServer(port int, reg *prometheus.Registry) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	// Bind the listener synchronously so a port-in-use error surfaces
	// from Setup rather than disappearing into a background goroutine.
	// Errors after Shutdown surface as ErrServerClosed, the expected
	// idempotent-exit signal — we drop it explicitly to match.
	lis, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return nil, err
	}
	go func() {
		serveErr := srv.Serve(lis)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			otel.Handle(serveErr)
		}
	}()
	return srv, nil
}

// newOTLPMetricExporter returns the right OTLP metric exporter for the
// active OTEL_EXPORTER_OTLP_PROTOCOL. http/protobuf routes through
// otlpmetrichttp so OTLP-HTTP receivers (Prometheus 3 native
// /api/v1/otlp) actually receive metrics — otlpmetricgrpc against an
// HTTP-only endpoint silently retries gRPC forever.
func newOTLPMetricExporter(ctx context.Context) (sdkmetric.Exporter, error) {
	proto := os.Getenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL")
	if proto == "" {
		proto = os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	}
	switch proto {
	case protoHTTPProtobuf:
		return otlpmetrichttp.New(ctx)
	default:
		return otlpmetricgrpc.New(ctx)
	}
}

// protoHTTPProtobuf is the OTLP HTTP/protobuf protocol selector. Shared
// across meter + tracer + log exporters so a future protocol-set
// expansion moves through one edit.
const protoHTTPProtobuf = "http/protobuf"

func composedMeterShutdown(mp *sdkmetric.MeterProvider, srv *http.Server) ShutdownFunc {
	var (
		once sync.Once
		err  error
	)
	return func(ctx context.Context) error {
		once.Do(func() {
			errs := []error{mp.Shutdown(ctx)}
			if srv != nil {
				errs = append(errs, srv.Shutdown(ctx))
			}
			err = errors.Join(errs...)
		})
		return err
	}
}
