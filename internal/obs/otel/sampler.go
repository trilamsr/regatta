package otel

import (
	"strings"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// ErrorOverrideSampler wraps a base sampler with two always-on overrides
// the spec §2.5 sampling policy pins:
//
//   - Spans carrying an `error.type` attribute always sample so failure
//     traces survive a low-ratio head-sampling decision.
//   - Spans originating in the chain-verify (`substrate/sign.go`) or
//     divergence-emit (`substrate/divergence_emit.go`) packages always
//     sample because their audit trails are load-bearing for the
//     forensic-replay contract.
//
// Every other span delegates to the wrapped base sampler — typically
// the env-driven `ParentBased(TraceIDRatioBased(p))` Setup wires.
type ErrorOverrideSampler struct {
	base sdktrace.Sampler
}

// NewErrorOverrideSampler returns a Sampler that overrides the base
// decision to RecordAndSample for error-tagged + audit-package spans.
func NewErrorOverrideSampler(base sdktrace.Sampler) *ErrorOverrideSampler {
	return &ErrorOverrideSampler{base: base}
}

// alwaysSamplePkgSuffixes lists the substrate audit packages whose
// spans must escape head-sampling unconditionally. The match is on
// suffix so vendor / replay-test copies do not bypass the override.
var alwaysSamplePkgSuffixes = []string{
	"orchestrator/state/substrate/sign",
	"orchestrator/state/substrate/divergence_emit",
}

// ShouldSample implements sdktrace.Sampler. The override fires when
// either condition holds; otherwise the base sampler's decision wins.
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

// Description carries a stable sampler-name string so the SDK's
// diagnostic logs surface which sampler decided a given span.
func (s *ErrorOverrideSampler) Description() string {
	return "ErrorOverride{" + s.base.Description() + "}"
}

// sampleResult builds a RecordAndSample result that preserves the
// parent's tracestate, matching the SDK convention for override
// samplers that bypass the base decision.
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
