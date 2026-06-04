// Package obstest provides shared test helpers for asserting on the canonical structured-log events defined in internal/obs. Handler is the slog.Handler that captures every emitted record so callers can pin which obs events a component produced (spec docs/superpowers/specs/2026-05-31-obs-101-slog-wiring.md §6.1). Test-only package: importing from non-test code is a bug; repo convention (matches internal/testutil/*) keeps the file as a regular .go so cross-package *_test.go files can import it (a _test.go suffix would hide the symbol from those importers and build-tag gating would be heavier than the threat warrants).
package obstest

import (
	"context"
	"log/slog"
	"sync"

	"github.com/trilamsr/regatta/internal/obs"
)

// Handler records every slog.Record routed through it; safe for concurrent Handle calls so -race tests that touch goroutines (Run loops, fanned-out spawns) do not trip on shared handler state.
type Handler struct {
	mu      sync.Mutex
	records []slog.Record
}

// New returns a Handler ready to be wrapped in slog.New.
func New() *Handler { return &Handler{} }

// Enabled always returns true so every record reaches Handle.
func (h *Handler) Enabled(context.Context, slog.Level) bool { return true }

// Handle clones and stores the record under a mutex so concurrent producers under -race do not corrupt the slice.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

// WithAttrs returns the receiver unchanged — tests assert on the Record that reaches Handle, where slog has already baked in any attribute decoration applied at the logger layer.
func (h *Handler) WithAttrs([]slog.Attr) slog.Handler { return h }

// WithGroup — see WithAttrs.
func (h *Handler) WithGroup(string) slog.Handler { return h }

// Records returns a snapshot of cloned records; per slog.Record's "copies share state" contract, cloning each element severs the attr-backing tie so caller mutations cannot reach back into h.records.
func (h *Handler) Records() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	for i, r := range h.records {
		out[i] = r.Clone()
	}
	return out
}

// Messages returns the Message field of every captured record in emission order.
func (h *Handler) Messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.records))
	for i, r := range h.records {
		out[i] = r.Message
	}
	return out
}

// FindEvent returns the first captured record whose Message equals name; producers MUST populate slog's msg from an obs.EventName (spec §3.3), so matching against the typed constant is the canonical lookup.
func (h *Handler) FindEvent(name obs.EventName) (slog.Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message == string(name) {
			return r.Clone(), true
		}
	}
	return slog.Record{}, false
}
