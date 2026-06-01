package substrate_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// TestSubstrate_DefaultReducerExhaustive — for every kind in AllKinds(),
// DefaultReducer returns a strategy from the closed set
// {lww, append, write-once} per spec §4. Rapid spreads the test
// over generated input but the closed-set assertion is the property
// the spec authority pins. Co-locates the "no kind escapes the
// defaultReducer switch" invariant in a property-shaped check.
//
// Also satisfies the `make property-test` gate which passes
// -rapid.checks=200 to every package under internal/orchestrator/state/...
// — a package without a rapid-using test rejects the flag.
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
