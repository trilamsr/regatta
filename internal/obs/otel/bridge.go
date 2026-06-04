// Package otel wires regatta's slog into the OpenTelemetry pipeline.
// The bridge fans every slog.Record to the local handler AND the OTel
// LoggerProvider via contrib/bridges/otelslog, keeping local stderr
// byte-equal so *_obs_test.go corpus still asserts on plain
// slog.Records (spec §3.2).
//
// Boot ordering matters: otelslog.NewHandler captures the
// LoggerProvider at construction time. cmd/regatta MUST do
// Setup → SetLoggerProvider → NewBridgeHandler → slog.SetDefault —
// otherwise the bridge captures the noop global for the process life.
package otel

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	otellog "go.opentelemetry.io/otel/log"
)

// NewBridgeHandler fans every record to primary AND OTel via otelslog.
// nil primary falls back to a stderr text handler so callers wiring the
// root logger before Setup completes never nil-deref. nil provider falls
// back to the global LoggerProvider (noop until Setup runs) — preserves
// spec §3.2's zero-cost-when-unconfigured invariant.
func NewBridgeHandler(primary slog.Handler, component string, provider otellog.LoggerProvider) slog.Handler {
	if primary == nil {
		primary = slog.NewTextHandler(os.Stderr, nil)
	}
	var oslogOpts []otelslog.Option
	if provider != nil {
		oslogOpts = append(oslogOpts, otelslog.WithLoggerProvider(provider))
	}
	return &bridgeHandler{
		primary: primary,
		otel:    otelslog.NewHandler(component, oslogOpts...),
	}
}

// bridgeHandler fans slog.Handler calls. Both legs are concurrency-safe
// by their respective contracts (primary is obstest/TextHandler/
// JSONHandler — all serialise internally; otelslog routes through the
// sdk/log Processor chain).
type bridgeHandler struct {
	primary slog.Handler
	otel    slog.Handler
}

// Enabled OR's both legs — keeps records alive when the operator's
// primary sink is at Warn but the OTel backend wants Debug.
func (h *bridgeHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.primary.Enabled(ctx, lvl) || h.otel.Enabled(ctx, lvl)
}

// Handle dispatches to both legs and joins errors. Cloning before the
// second dispatch is mandatory: slog.Record's Attrs backing array is
// shared and otelslog walks Attrs to translate kinds — without the
// clone it would mutate state the primary handler later observes.
func (h *bridgeHandler) Handle(ctx context.Context, r slog.Record) error {
	primErr := h.primary.Handle(ctx, r.Clone())
	otelErr := h.otel.Handle(ctx, r)
	return errors.Join(primErr, otelErr)
}

// WithAttrs propagates to both legs so `.With(...)` chains stay byte-
// equal between local sink and OTel backend.
func (h *bridgeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &bridgeHandler{
		primary: h.primary.WithAttrs(attrs),
		otel:    h.otel.WithAttrs(attrs),
	}
}

func (h *bridgeHandler) WithGroup(name string) slog.Handler {
	return &bridgeHandler{
		primary: h.primary.WithGroup(name),
		otel:    h.otel.WithGroup(name),
	}
}
