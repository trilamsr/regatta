package l0

import (
	"log/slog"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
)

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
	h := obstest.New()
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

	rec, ok := h.FindEvent(obs.EventGateVerdict)
	if !ok {
		t.Fatalf("gate.verdict event not emitted; records=%+v", h.Records())
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
