package approval

import (
	"log/slog"
	"testing"

	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/obstest"
	"github.com/trilamsr/regatta/internal/slogutil"
)

func TestL0Gate_EmitsVerdictOnL0Check(t *testing.T) {
	t.Parallel()
	h := obstest.New()
	cfg := L0Default()
	cfg.Logger = slog.New(h)

	d := `diff --git a/M.md b/M.md
--- a/M.md
+++ b/M.md
@@ -1,2 +1,2 @@
 # M
-- [ ] First criterion.
+- [x] First criterion. test=TestFoo
`
	_ = L0Check(cfg, L0ParseUnifiedDiff(d))

	rec, ok := h.FindEvent(obs.EventGateVerdict)
	if !ok {
		t.Fatalf("gate.verdict event not emitted; records=%+v", h.Records())
	}
	gid, ok := slogutil.AttrValue(rec, string(obs.KeyGateID))
	if !ok {
		t.Fatalf("gate.verdict missing %s attr", obs.KeyGateID)
	}
	if gid.String() != "l0" {
		t.Fatalf("gate_id=%q; want %q", gid.String(), "l0")
	}
	if _, ok := slogutil.AttrValue(rec, string(obs.KeyVerdict)); !ok {
		t.Fatalf("gate.verdict missing %s attr", obs.KeyVerdict)
	}
	if _, ok := slogutil.AttrValue(rec, string(obs.KeyDurationMs)); !ok {
		t.Fatalf("gate.verdict missing %s attr", obs.KeyDurationMs)
	}
}
