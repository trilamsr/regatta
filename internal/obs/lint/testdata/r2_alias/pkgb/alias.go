package pkgb

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type MyTracer = trace.Tracer
type MyMeter = metric.Meter

type WithAliasOK struct {
	Tracer MyTracer
	Meter  MyMeter
}

type WithAliasLeaky struct {
	Tracer MyTracer
}
