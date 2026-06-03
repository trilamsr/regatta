package otel

import (
	"strings"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// ErrorOverrideSampler wraps a base sampler with two always-on
// overrides (spec §2.5):
//   - `error.type`-tagged spans always sample so failure traces
//     survive a low-ratio head decision.
//   - chain-verify + divergence-emit spans always sample — their
//     audit trails are load-bearing for forensic replay.
//
// Every other span delegates to base (typically the env-driven
// `ParentBased(TraceIDRatioBased(p))` Setup wires).
type ErrorOverrideSampler struct {
	base sdktrace.Sampler
}

func NewErrorOverrideSampler(base sdktrace.Sampler) *ErrorOverrideSampler {
	return &ErrorOverrideSampler{base: base}
}

// alwaysSamplePkgSuffixes lists substrate audit packages whose spans
// escape head-sampling unconditionally. Match-on-suffix so vendor /
// replay-test copies cannot bypass the override.
var alwaysSamplePkgSuffixes = []string{
	"orchestrator/state/substrate/sign",
	"orchestrator/state/substrate/divergence_emit",
}

// ShouldSample fires the override when either condition holds;
// otherwise base wins.
func (s *ErrorOverrideSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	for _, kv := range p.Attributes {
		if string(kv.Key) == "error.type" {
			return sampleResult(p)
		}
		if string(kv.Key) == "code.namespace" {
			ns := kv.Value.AsString()
			for _, suf := range alwaysSamplePkgSuffixes {
				if strings.HasSuffix(ns, suf) {
					return sampleResult(p)
				}
			}
		}
	}
	return s.base.ShouldSample(p)
}

// Description carries a stable sampler-name so SDK diagnostic logs
// surface which sampler decided a given span.
func (s *ErrorOverrideSampler) Description() string {
	return "ErrorOverride{" + s.base.Description() + "}"
}

// sampleResult preserves the parent's tracestate per SDK convention
// for override samplers that bypass the base decision.
func sampleResult(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	var ts trace.TraceState
	if sc := trace.SpanContextFromContext(p.ParentContext); sc.IsValid() {
		ts = sc.TraceState()
	}
	return sdktrace.SamplingResult{
		Decision:   sdktrace.RecordAndSample,
		Tracestate: ts,
	}
}
