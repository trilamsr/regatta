//go:build integration_gh

package l4

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestL4ReviewerRealAnthropic exercises the real Messages API end-to-end through the L4 prompt path and asserts a second-call prompt-cache hit (#896).
func TestL4ReviewerRealAnthropic(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY unset; skipping real-Anthropic L4 integration test")
	}
	if v := os.Getenv("REGATTA_TEST_BUDGET_CENTS"); v == "0" {
		t.Skip("REGATTA_TEST_BUDGET_CENTS=0; cost-gated skip")
	}

	budgetTokens := int64(5000)
	if v := os.Getenv("REGATTA_TEST_L4_TOKEN_BUDGET"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed > 0 {
			budgetTokens = parsed
		}
	}

	model := os.Getenv("REGATTA_TEST_L4_MODEL")
	if model == "" {
		model = "claude-haiku-4-5"
	}

	adapter, err := NewAnthropicAdapter(apiKey)
	if err != nil {
		t.Fatalf("NewAnthropicAdapter: %v", err)
	}

	in := Input{
		PRSHA:   "deadbeefcafefeed00000000",
		BaseSHA: "feedface00000000deadbeef",
		RunID:   "run-integ-896",
		Diff: `diff --git a/internal/example/add.go b/internal/example/add.go
new file mode 100644
--- /dev/null
+++ b/internal/example/add.go
@@ -0,0 +1,3 @@
+package example
+
+func Add(a, b int) int { return a + b }
`,
		Spec:      "Trivial addition helper; no security or concurrency concerns.",
		Scorecard: "- [x] Unit-tested in add_test.go (TestAdd).",
	}

	req := InvokeRequest{
		Model:    model,
		Input:    in,
		GateID:   "l4_adversarial",
		MaxChars: DefaultMaxDiffChars,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	invoke := adapter.Invoke()

	first, err := invoke(ctx, req)
	if err != nil {
		t.Fatalf("first invoke: %v", err)
	}
	if first.PromptSHA == "" {
		t.Fatalf("first call: empty PromptSHA")
	}
	if first.TokensIn <= 0 {
		t.Fatalf("first call: TokensIn=%d, want >0", first.TokensIn)
	}

	second, err := invoke(ctx, req)
	if err != nil {
		t.Fatalf("second invoke: %v", err)
	}
	if second.TokensCacheRead <= 0 {
		t.Fatalf("second call: TokensCacheRead=%d, want >0 (prompt cache must hit on identical second call; first call cache_write=%d)", second.TokensCacheRead, first.TokensCacheWrite)
	}

	totalTokens := first.TokensIn + first.TokensOut + second.TokensIn + second.TokensOut
	if totalTokens > budgetTokens {
		t.Fatalf("total tokens %d exceeded budget %d (first in/out=%d/%d, second in/out=%d/%d)",
			totalTokens, budgetTokens,
			first.TokensIn, first.TokensOut,
			second.TokensIn, second.TokensOut)
	}

	t.Logf("L4 real-API run OK: model=%s totalTokens=%d (budget=%d) cache_write=%d cache_read=%d",
		model, totalTokens, budgetTokens, first.TokensCacheWrite, second.TokensCacheRead)
}
