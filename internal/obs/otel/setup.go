// Package otel wires the OpenTelemetry Go SDK for regatta. Spec
// docs/superpowers/specs/2026-05-31-mvp-3-w6-otel-backbone.md §3.1
// pins this file as the single SDK init seam — every other component
// imports only the stable `trace.Tracer` API surface so a future SDK
// major-version migration stays a one-file rewrite (spec §9 R4).
package otel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
)

// Sentinel errors. Boundary callers use errors.Is to branch on these
// without leaking impl wrapping (memory rule: typed sentinels only).
var (
	// ErrOTelExporterConflict surfaces when --otel-dev-stdout AND
	// OTEL_EXPORTER_OTLP_ENDPOINT are both set. Spec §3.6 makes the
	// pair mutually exclusive so operators do not accidentally double-
	// emit spans to two backends with divergent shapes.
	ErrOTelExporterConflict = errors.New("obs/otel: --otel-dev-stdout cannot combine with OTEL_EXPORTER_OTLP_ENDPOINT")

	// ErrTraceExporter wraps span-exporter construction failures from
	// the OTel SDK so callers branch on the boundary without coupling
	// to upstream error types.
	ErrTraceExporter = errors.New("obs/otel: trace exporter init failed")

	// ErrLogExporter wraps log-exporter construction failures from the
	// OTel SDK; symmetric with ErrTraceExporter.
	ErrLogExporter = errors.New("obs/otel: log exporter init failed")

	// ErrResource wraps resource composition failures from the OTel SDK.
	ErrResource = errors.New("obs/otel: resource composition failed")
)

// Config carries the regatta-specific knobs Setup needs. Everything
// else (endpoint, headers, sampler, attribute limits) flows through
// the OTel SDK's documented env-var contract — spec §3.6 deliberately
// avoids inventing a parallel YAML schema.
type Config struct {
	// ServiceName is emitted as the OTel resource attribute
	// `service.name`. Defaults to "regatta" when empty.
	ServiceName string

	// ServiceVersion is emitted as `service.version`. Empty values are
	// dropped from the resource so the SDK's own default fills in.
	ServiceVersion string

	// TenantID is emitted as `regatta.tenant_id`. Defaults to
	// "default" when empty; W8 (RBAC) swaps the constant for a per-
	// context lookup.
	TenantID string

	// DevStdout routes spans + logs to the SDK's stdout exporters for
	// dev visibility when no OTLP backend is configured. Mutex w/
	// OTEL_EXPORTER_OTLP_ENDPOINT — both set returns ErrOTelExporterConflict.
	DevStdout bool

	// StdoutDest overrides the io.Writer the stdout exporters write
	// to. Nil falls back to os.Stdout. Tests inject *bytes.Buffer here
	// so they can assert on the serialised span payload without
	// stomping the test runner's stdout.
	StdoutDest io.Writer
}

// ShutdownFunc flushes every provider Setup wired up. Returned closure
// is idempotent: the first call drains and joins exporter errors, all
// subsequent calls return nil.
type ShutdownFunc func(context.Context) error

// Setup wires the global OTel TracerProvider and LoggerProvider per
// spec §3.1. Exporter selection:
//
//   - DevStdout + OTEL_EXPORTER_OTLP_ENDPOINT both set → ErrOTelExporterConflict.
//   - DevStdout → stdouttrace + stdoutlog (dev visibility, no backend needed).
//   - OTEL_EXPORTER_OTLP_ENDPOINT set → otlptracegrpc + otlploggrpc.
//   - Neither → noop; the SDK's default noop providers win and Setup
//     allocates no exporter goroutines (spec §B2 byte-identical-to-MVP-2
//     verification path).
//
// Returns a ShutdownFunc the caller stores for clean process exit; the
// closure is safe to call from a signal handler.
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

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)
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
			return nil, nil, fmt.Errorf("%w: %w", ErrLogExporter, err)
		}
		return te, le, nil
	}

	te, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrTraceExporter, err)
	}
	le, err := otlploggrpc.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrLogExporter, err)
	}
	return te, le, nil
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
