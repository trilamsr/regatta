package spend_test

import (
	"fmt"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSpendCrashRecoveryProperty proves replay→record ≡ record for any crash-write across N spend rows.
func TestSpendCrashRecoveryProperty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 6).Draw(rt, "n_calls")
		prefix := fmt.Sprintf("prop-%d", rapid.IntRange(0, 1<<30).Draw(rt, "prefix_seed"))
		ids := callIDs(prefix, n)
		// Vary the model so per-call USD differs between cases — keeps
		// the diff's per-id USD comparison load-bearing rather than
		// trivially equal across runs.
		model := rapid.SampledFrom([]string{"claude-sonnet-4-7", "claude-haiku-4-5"}).Draw(rt, "model")

		h := &spendCrashHarness{
			callIDs: ids,
			model:   model,
			tenant:  substrate.DefaultTenantID,
			now:     time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		}
		baseline := h.runBaseline(t)
		k := rapid.IntRange(0, n-1).Draw(rt, "crash_index")
		recovered := h.runCrashAndRecover(t, k)
		if d := diffSpendSnapshots(baseline, recovered); d != "" {
			rt.Fatalf("baseline ≠ recovered (n=%d k=%d model=%s prefix=%s): %s",
				n, k, model, prefix, d)
		}

		// Sub-property: no call ID is lost — every seeded ID has a USD.
		// Asserted independently of the diff so a future snapshot-fold
		// change cannot silently mask a recovery-gap regression.
		for _, id := range ids {
			if _, ok := recovered.USD[id]; !ok {
				rt.Fatalf("P-NoLoss: call %s missing post-recover", id)
			}
		}
	})
}
