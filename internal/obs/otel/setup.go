// Package otel wires the OpenTelemetry Go SDK. Spec
// 2026-05-31-mvp-3-w6-otel-backbone.md §3.1 pins this file as the
// single SDK init seam — every other component imports only the
// stable trace.Tracer API so a future SDK major-version migration
// stays a one-file rewrite (spec §9 R4).
package otel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
)

// Sentinel errors — boundary callers errors.Is to branch without
// leaking upstream SDK error types.
var (
	// ErrOTelExporterConflict — --otel-dev-stdout AND
	// OTEL_EXPORTER_OTLP_ENDPOINT both set. Spec §3.6 makes the pair
	// mutex so operators do not accidentally double-emit.
	ErrOTelExporterConflict = errors.New("obs/otel: --otel-dev-stdout cannot combine with OTEL_EXPORTER_OTLP_ENDPOINT")

	// ErrTraceExporter wraps SDK span-exporter init failures.
	ErrTraceExporter = errors.New("obs/otel: trace exporter init failed")

	// ErrLogExporter wraps SDK log-exporter init failures.
	ErrLogExporter = errors.New("obs/otel: log exporter init failed")

	// ErrResource wraps SDK resource-composition failures.
	ErrResource = errors.New("obs/otel: resource composition failed")
)

// Config carries regatta-specific knobs Setup needs. Everything else
// (endpoint, headers, sampler, attribute limits) flows through the
// SDK's documented env-var contract — spec §3.6 avoids a parallel
// YAML schema.
type Config struct {
	// ServiceName → resource attribute `service.name`; defaults
	// "regatta".
	ServiceName string

	// ServiceVersion → `service.version` when non-empty; SDK omits
	// the key when blank.
	ServiceVersion string

	// TenantID → `regatta.tenant_id`; defaults "default". W8 (RBAC)
	// swaps the constant for a per-context lookup.
	TenantID string

	// DevStdout routes spans + logs to the SDK's stdout exporters.
	// Mutex with OTEL_EXPORTER_OTLP_ENDPOINT — both set →
	// ErrOTelExporterConflict.
	DevStdout bool

	// StdoutDest overrides the stdout exporters' writer (nil →
	// os.Stdout). Tests inject *bytes.Buffer to assert on serialised
	// span payload without stomping test stdout.
	StdoutDest io.Writer
}

// ShutdownFunc flushes every provider Setup wired up. Idempotent —
// first call drains and joins exporter errors, subsequent return nil.
type ShutdownFunc func(context.Context) error

// Setup wires the global TracerProvider and LoggerProvider (spec
// §3.1). Exporter selection:
//   - DevStdout + OTLP endpoint both set → ErrOTelExporterConflict.
//   - DevStdout → stdouttrace + stdoutlog.
//   - OTEL_EXPORTER_OTLP_ENDPOINT set → otlptracegrpc + otlploggrpc.
//   - Neither → noop providers; Setup allocates no exporter
//     goroutines (spec §B2 byte-identical-to-MVP-2 path).
//
// Caller stores the returned ShutdownFunc for clean process exit (safe
// from a signal handler). Setup is once-per-process — repeated calls
// without shutting the prior provider leak batcher goroutines.
func Setup(ctx context.Context, cfg Config) (ShutdownFunc, error) {
	cfg = withDefaults(cfg)

	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") +
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") +
		os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT")
	otlpSet := otlpEndpoint != ""

	if cfg.DevStdout && otlpSet {
		return nil, ErrOTelExporterConflict
	}

	if !cfg.DevStdout && !otlpSet {
		return noopShutdown, nil
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResource, err)
	}

	traceExp, logExp, err := buildExporters(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Sampler resolution: with OTEL_TRACES_SAMPLER set, the SDK env-config
	// path applies and Setup omits WithSampler so the operator override
	// wins. With neither env var set, the regatta default per spec §2.5
	// is ParentBased(TraceIDRatioBased(0.1)) wrapped in ErrorOverrideSampler
	// so error.type-tagged + chain-verify / divergence-audit spans always
	// sample regardless of the ratio bucket.
	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	}
	if base, useDefault := resolveDefaultTraceSampler(); useDefault {
		tpOpts = append(tpOpts, sdktrace.WithSampler(NewErrorOverrideSampler(base)))
	}
	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	lp := log.NewLoggerProvider(
		log.WithResource(res),
		log.WithProcessor(log.NewBatchProcessor(logExp)),
	)
	otellog.SetLoggerProvider(lp)

	return composedShutdown(tp, lp), nil
}

// resolveDefaultTraceSampler returns the regatta default head-sampling
// policy when no OTEL_TRACES_SAMPLER env var is set. The second return
// is true when the caller should wire WithSampler; false leaves the
// SDK env-config path untouched so an operator override
// (e.g. OTEL_TRACES_SAMPLER=parentbased_traceidratio arg=1.0) wins.
//
// Default ratio: 0.1 per spec §2.5; OTEL_TRACES_SAMPLER_ARG without a
// SAMPLER pick still acts as an explicit ratio override.
func resolveDefaultTraceSampler() (sdktrace.Sampler, bool) {
	if os.Getenv("OTEL_TRACES_SAMPLER") != "" {
		return nil, false
	}
	ratio := 0.1
	if arg := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); arg != "" {
		if v, err := strconv.ParseFloat(arg, 64); err == nil {
			ratio = v
		}
	}
	return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)), true
}

func withDefaults(cfg Config) Config {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "regatta"
	}
	if cfg.TenantID == "" {
		cfg.TenantID = "default"
	}
	if cfg.StdoutDest == nil {
		cfg.StdoutDest = os.Stdout
	}
	return cfg
}

func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		attribute.String("regatta.tenant_id", cfg.TenantID),
	}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(cfg.ServiceVersion))
	}
	// resource.New merges OTEL_RESOURCE_ATTRIBUTES via WithFromEnv per
	// the SDK contract — operator overrides land on top of regatta's
	// baseline without colliding here.
	return resource.New(ctx,
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithProcess(),
		resource.WithAttributes(attrs...),
	)
}

func buildExporters(ctx context.Context, cfg Config) (sdktrace.SpanExporter, log.Exporter, error) {
	if cfg.DevStdout {
		te, err := stdouttrace.New(stdouttrace.WithWriter(cfg.StdoutDest))
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %w", ErrTraceExporter, err)
		}
		le, err := stdoutlog.New(stdoutlog.WithWriter(cfg.StdoutDest))
		if err != nil {
			// Partial-init: trace exporter succeeded, log exporter
			// failed. Shut the trace exporter down so its background
			// goroutine does not leak past the failed Setup.
			_ = te.Shutdown(ctx)
			return nil, nil, fmt.Errorf("%w: %w", ErrLogExporter, err)
		}
		return te, le, nil
	}

	te, err := newOTLPTraceExporter(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrTraceExporter, err)
	}
	le, err := newOTLPLogExporter(ctx)
	if err != nil {
		_ = te.Shutdown(ctx)
		return nil, nil, fmt.Errorf("%w: %w", ErrLogExporter, err)
	}
	return te, le, nil
}

// newOTLPTraceExporter mirrors the meter.go::newOTLPMetricExporter
// pattern: branch on OTEL_EXPORTER_OTLP_PROTOCOL so http/protobuf
// endpoints (Prometheus 3 native OTLP receiver via Tempo/Grafana
// Loki HTTP path) route through otlptracehttp instead of dialing
// gRPC at :4317 forever.
func newOTLPTraceExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	proto := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL")
	if proto == "" {
		proto = os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	}
	switch proto {
	case "http/protobuf":
		return otlptracehttp.New(ctx)
	default:
		return otlptracegrpc.New(ctx)
	}
}

// newOTLPLogExporter — mirror of newOTLPTraceExporter for the log
// pipeline. Same protocol env var rules.
func newOTLPLogExporter(ctx context.Context) (log.Exporter, error) {
	proto := os.Getenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL")
	if proto == "" {
		proto = os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	}
	switch proto {
	case "http/protobuf":
		return otlploghttp.New(ctx)
	default:
		return otlploggrpc.New(ctx)
	}
}

// noopShutdown is the closure returned when no exporter wires; cached
// at package level so the no-OTel boot path allocates nothing.
var noopShutdown ShutdownFunc = func(context.Context) error { return nil }

// composedShutdown returns an idempotent ShutdownFunc that flushes the
// trace + log providers exactly once. Subsequent calls return nil so
// signal handlers that fire shutdown twice on rapid re-signal do not
// double-drain or panic on closed exporter channels.
func composedShutdown(tp *sdktrace.TracerProvider, lp *log.LoggerProvider) ShutdownFunc {
	var (
		once sync.Once
		err  error
	)
	return func(ctx context.Context) error {
		once.Do(func() {
			err = errors.Join(tp.Shutdown(ctx), lp.Shutdown(ctx))
		})
		return err
	}
}
