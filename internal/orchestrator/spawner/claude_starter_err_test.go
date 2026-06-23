package spawner

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/testutil"
)

// TestSpawn_StarterError_NoLooseGoroutine asserts starter failure does not leak the ParseStream reader (R-MEGA-2 C6).
func TestSpawn_StarterError_NoLooseGoroutine(t *testing.T) {
	cs, fs, _ := newClaudeHarness(t)
	fs.failNow.Store(true)

	before := runtime.NumGoroutine()
	const N = 50
	for i := 0; i < N; i++ {
		_, err := cs.Spawn(context.Background(), Request{AgentID: int64(i + 1)})
		if err == nil {
			t.Fatalf("spawn %d: expected error", i)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	testutil.Eventually(t, ctx, 20*time.Millisecond, func() bool {
		return runtime.NumGoroutine()-before <= 4
	}, "ParseStream reader goroutines leaked across failed spawns")
}
