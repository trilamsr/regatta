package substrate_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_DefaultReducerExhaustive pins every kind's strategy to the closed set {lww, append, write-once} per spec §4.
func TestSubstrate_DefaultReducerExhaustive(t *testing.T) {
	allowed := map[substrate.ReducerStrategy]bool{
		substrate.StrategyLWW:       true,
		substrate.StrategyAppend:    true,
		substrate.StrategyWriteOnce: true,
	}
	rapid.Check(t, func(rt *rapid.T) {
		// Pick a kind from the canonical list; rapid spreads the
		// selection across runs. AllKinds is the spec-authoritative
		// enum; any change must be reflected in the SQL CHECK too
		// (pinned by T-S3's TestSubstrate_EventKindEnumMatchesSQLCheck).
		kinds := substrate.AllKinds()
		if len(kinds) == 0 {
			rt.Skip("AllKinds() empty — T-S1 not landed")
		}
		idx := rapid.IntRange(0, len(kinds)-1).Draw(rt, "kind_idx")
		k := kinds[idx]
		got := substrate.DefaultReducer(k)
		if !allowed[got] {
			rt.Fatalf("DefaultReducer(%q) = %q not in {lww,append,write-once}", k, got)
		}
	})
}
