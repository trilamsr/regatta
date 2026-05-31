package l0

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
)

// captureHandler — minimal inline slog.Handler so Task D doesn't
// block on the shared internal/obstest helper (not yet landed).
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler     { return h }

func (h *captureHandler) findEvent(name obs.EventName) (slog.Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message == string(name) {
			return r, true
		}
	}
	return slog.Record{}, false
}

func attrValue(r slog.Record, key string) (slog.Value, bool) {
	var out slog.Value
	var found bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			out = a.Value
			found = true
			return false
		}
		return true
	})
	return out, found
}

func TestL0Gate_EmitsVerdictOnCheck(t *testing.T) {
	t.Parallel()
	h := &captureHandler{}
	cfg := Default()
	cfg.Logger = slog.New(h)

	d := `diff --git a/M.md b/M.md
--- a/M.md
+++ b/M.md
@@ -1,2 +1,2 @@
 # M
-- [ ] First criterion.
+- [x] First criterion. test=TestFoo
`
	_ = Check(cfg, ParseUnifiedDiff(d))

	rec, ok := h.findEvent(obs.EventGateVerdict)
	if !ok {
		t.Fatalf("gate.verdict event not emitted; records=%+v", h.records)
	}
	gid, ok := attrValue(rec, string(obs.KeyGateID))
	if !ok {
		t.Fatalf("gate.verdict missing %s attr", obs.KeyGateID)
	}
	if gid.String() != "l0" {
		t.Fatalf("gate_id=%q; want %q", gid.String(), "l0")
	}
	if _, ok := attrValue(rec, string(obs.KeyVerdict)); !ok {
		t.Fatalf("gate.verdict missing %s attr", obs.KeyVerdict)
	}
	if _, ok := attrValue(rec, string(obs.KeyDurationMs)); !ok {
		t.Fatalf("gate.verdict missing %s attr", obs.KeyDurationMs)
	}
}
