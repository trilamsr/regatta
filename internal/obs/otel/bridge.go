// Package otel wires regatta's structured logging into the OpenTelemetry
// signal pipeline. The bridge fans every slog.Record to (a) the existing
// local handler and (b) the OTel LoggerProvider via the upstream
// contrib/bridges/otelslog adapter, keeping the local stderr stream
// byte-equal so the *_obs_test.go corpus continues to assert on plain
// slog.Records (spec §3.2).
//
// When no OTel LoggerProvider is configured the upstream global is a
// noop and the OTel leg is a zero-cost call; the primary leg always
// fires. This file deliberately depends only on the OTel API surface
// (no SDK init) so the slog→OTel bridge can be installed before or
// after Setup runs without ordering hazards.
package otel

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	otellog "go.opentelemetry.io/otel/log"
)

// BridgeOption customises NewBridgeHandler. Mirrors otelslog.Option
// shape so the construction site reads identically to the upstream
// adapter; deliberately does NOT re-export otelslog.Option to avoid
// leaking the contrib import into callers that only consume the bridge.
type BridgeOption interface {
	apply(*bridgeConfig)
}

type bridgeConfig struct {
	provider otellog.LoggerProvider
}

type bridgeOptionFunc func(*bridgeConfig)

func (f bridgeOptionFunc) apply(c *bridgeConfig) { f(c) }

// WithLoggerProvider pins the OTel LoggerProvider the bridge emits to.
// Falls back to the global provider (noop until Setup runs) when unset,
// matching otelslog's default and the spec §3.2 zero-cost-when-unconfigured
// invariant.
func WithLoggerProvider(p otellog.LoggerProvider) BridgeOption {
	return bridgeOptionFunc(func(c *bridgeConfig) { c.provider = p })
}

// NewBridgeHandler returns a slog.Handler that fans every record to
// the primary handler AND the OTel LoggerProvider via otelslog. A nil
// primary falls back to a text handler on stderr so callers wiring the
// root logger before Setup completes never crash with a nil deref.
//
// The component name is forwarded verbatim to otelslog.NewHandler as
// the InstrumentationScope name — operators filter OTel logs by this
// value, so every regatta component passes its package name.
func NewBridgeHandler(primary slog.Handler, component string, opts ...BridgeOption) slog.Handler {
	if primary == nil {
		primary = slog.NewTextHandler(os.Stderr, nil)
	}
	var cfg bridgeConfig
	for _, o := range opts {
		o.apply(&cfg)
	}
	var oslogOpts []otelslog.Option
	if cfg.provider != nil {
		oslogOpts = append(oslogOpts, otelslog.WithLoggerProvider(cfg.provider))
	}
	return &bridgeHandler{
		primary: primary,
		otel:    otelslog.NewHandler(component, oslogOpts...),
	}
}

// bridgeHandler is the slog.Handler fan-out. Per slog.Handler contract
// every method must be safe for concurrent use; both legs already are
// (primary is an obstest.Handler / TextHandler / JSONHandler in
// production paths, all of which serialise internally; otelslog routes
// through the sdk/log Processor chain which is concurrency-safe by
// SDK contract).
type bridgeHandler struct {
	primary slog.Handler
	otel    slog.Handler
}

// Enabled returns true if either leg accepts the level. Returning the
// OR keeps a record alive when the operator's primary sink is at Warn
// but the OTel backend wants Debug, and vice versa.
func (h *bridgeHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.primary.Enabled(ctx, lvl) || h.otel.Enabled(ctx, lvl)
}

// Handle dispatches to both legs and joins their errors. Per slog
// contract, errors are not fatal — they propagate to slog's default
// error handler. errors.Join preserves both for diagnostics rather
// than dropping the second leg's failure.
func (h *bridgeHandler) Handle(ctx context.Context, r slog.Record) error {
	// Cloning before the second dispatch is mandatory: slog.Record's
	// attribute backing array is shared with the original, and a
	// handler is permitted to mutate it during conversion. otelslog
	// in particular walks Attrs to translate kinds; running it first
	// without a clone would mutate state the primary handler later
	// observes.
	primErr := h.primary.Handle(ctx, r.Clone())
	otelErr := h.otel.Handle(ctx, r)
	return errors.Join(primErr, otelErr)
}

// WithAttrs propagates the decoration to both legs so logger-side
// `.With(...)` chains are observed identically by the local sink and
// the OTel backend (spec §3.2 byte-equal-local-stream invariant
// extended to attr decoration).
func (h *bridgeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &bridgeHandler{
		primary: h.primary.WithAttrs(attrs),
		otel:    h.otel.WithAttrs(attrs),
	}
}

// WithGroup mirrors WithAttrs for slog groups.
func (h *bridgeHandler) WithGroup(name string) slog.Handler {
	return &bridgeHandler{
		primary: h.primary.WithGroup(name),
		otel:    h.otel.WithGroup(name),
	}
}
