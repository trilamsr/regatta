package pkga

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type Inner struct {
	Tracer trace.Tracer
	Meter  metric.Meter
}

type EmbedsInnerOK struct {
	*Inner
}

type LeakyInner struct {
	Tracer trace.Tracer
}

type EmbedsLeakyInner struct {
	*LeakyInner
}
