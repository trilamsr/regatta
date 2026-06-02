package otel_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	trace "go.opentelemetry.io/otel/trace"

	otelpkg "github.com/trilamsr/regatta/internal/obs/otel"
)

// TestErrorOverrideSampler_ErrorTypedSpanAlwaysSampled pins the always-on override for spans carrying error.type.
func TestErrorOverrideSampler_ErrorTypedSpanAlwaysSampled(t *testing.T) {
	// Wrap a base sampler that always drops; the override must flip
	// the decision to RecordAndSample whenever the span attrs include
	// "error.type". Asserting the override directly (rather than via
	// global Setup) keeps the test deterministic.
	s := otelpkg.NewErrorOverrideSampler(sdktrace.NeverSample())
	dec := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Name:          "any-span",
		Attributes: []attribute.KeyValue{
			attribute.String("error.type", "exporter_timeout"),
		},
	})
	if dec.Decision != sdktrace.RecordAndSample {
		t.Errorf("ErrorOverride dec = %v; want RecordAndSample for error.type-tagged span", dec.Decision)
	}
}

// TestErrorOverrideSampler_ChainVerifyAlwaysSampled pins the always-on override for chain-verify package spans.
func TestErrorOverrideSampler_ChainVerifyAlwaysSampled(t *testing.T) {
	s := otelpkg.NewErrorOverrideSampler(sdktrace.NeverSample())
	dec := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Name:          "chain.verify",
		Attributes: []attribute.KeyValue{
			attribute.String("code.namespace",
				"github.com/trilamsr/regatta/internal/orchestrator/state/substrate/sign"),
		},
	})
	if dec.Decision != sdktrace.RecordAndSample {
		t.Errorf("ErrorOverride dec = %v; want RecordAndSample for chain-verify span", dec.Decision)
	}
}

// TestErrorOverrideSampler_DivergenceAuditAlwaysSampled pins the divergence-emit override.
func TestErrorOverrideSampler_DivergenceAuditAlwaysSampled(t *testing.T) {
	s := otelpkg.NewErrorOverrideSampler(sdktrace.NeverSample())
	dec := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Name:          "divergence.emit",
		Attributes: []attribute.KeyValue{
			attribute.String("code.namespace",
				"github.com/trilamsr/regatta/internal/orchestrator/state/substrate/divergence_emit"),
		},
	})
	if dec.Decision != sdktrace.RecordAndSample {
		t.Errorf("ErrorOverride dec = %v; want RecordAndSample for divergence-audit span", dec.Decision)
	}
}

// TestErrorOverrideSampler_NormalSpanRespectsBase pins delegation to the wrapped base sampler.
func TestErrorOverrideSampler_NormalSpanRespectsBase(t *testing.T) {
	s := otelpkg.NewErrorOverrideSampler(sdktrace.NeverSample())
	dec := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Name:          "normal-span",
	})
	if dec.Decision != sdktrace.Drop {
		t.Errorf("ErrorOverride dec = %v; want Drop (base sampler) for unflagged span", dec.Decision)
	}

	s2 := otelpkg.NewErrorOverrideSampler(sdktrace.AlwaysSample())
	dec2 := s2.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Name:          "normal-span",
	})
	if dec2.Decision != sdktrace.RecordAndSample {
		t.Errorf("ErrorOverride dec = %v; want RecordAndSample (base sampler) for unflagged span", dec2.Decision)
	}
}

// TestErrorOverrideSampler_Description carries a stable, sampler-name string.
func TestErrorOverrideSampler_Description(t *testing.T) {
	s := otelpkg.NewErrorOverrideSampler(sdktrace.NeverSample())
	if got := s.Description(); got == "" {
		t.Errorf("Description() empty; want a non-empty sampler description")
	}
}

// trace.TraceState compile-time guard so the SDK alias is wired.
var _ trace.TraceState
