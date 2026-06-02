package approval

import (
	"fmt"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// TestReaperCrashRecoveryProperty proves recover→Sweep ≡ Sweep for any crash-index across N expired approvals.
func TestReaperCrashRecoveryProperty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		nApprovals := rapid.IntRange(1, 5).Draw(rt, "n_approvals")
		ids := make([]string, nApprovals)
		for i := 0; i < nApprovals; i++ {
			ids[i] = fmt.Sprintf("a-%d", i)
		}
		t0 := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
		policy := rapid.SampledFrom([]string{policyFail, policyAutoApprove}).Draw(rt, "policy")
		h := &reaperCrashHarness{seedIDs: ids, t0: t0, policy: policy}

		baseline := h.runBaseline(t)
		k := rapid.IntRange(0, nApprovals-1).Draw(rt, "crash_index")
		recovered := h.runCrashAndRecover(t, k)
		if d := diffReaperSnapshots(baseline, recovered); d != "" {
			rt.Fatalf("baseline ≠ recovered (n=%d k=%d policy=%s ids=%v): %s",
				nApprovals, k, policy, ids, d)
		}

		// Sub-property: §3.2.1 atomicity contract — every expired row
		// terminates or rolls back to pending and is picked up on the
		// next Sweep. Asserted independently of the diff so a future
		// snapshot-fold drift cannot silently mask this regression.
		for _, id := range ids {
			s, ok := recovered.Status[id]
			if !ok {
				rt.Fatalf("P-NoLoss: approval %s missing post-recover", id)
			}
			if s == state.ApprovalStatusPending {
				rt.Fatalf("P-NoLoss: approval %s stuck pending post-recover", id)
			}
		}
	})
}
